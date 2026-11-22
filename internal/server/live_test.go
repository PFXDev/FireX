package server

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PFXDev/FireX/internal/paneltest"
)

func configureFastLiveHub(h *harness) {
	h.srv.live.mu.Lock()
	h.srv.live.interval = 25 * time.Millisecond
	h.srv.live.idleGrace = 20 * time.Millisecond
	h.srv.live.heartbeat = 100 * time.Millisecond
	h.srv.live.pollTimeout = 500 * time.Millisecond
	h.srv.live.mu.Unlock()
}

func openSSE(t *testing.T, h *harness, ctx context.Context) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+"/api/overview/live", nil)
	if err != nil {
		t.Fatalf("new SSE request: %v", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/overview/live: %v", err)
	}
	return resp
}

func readNextSnapshot(t *testing.T, r *bufio.Reader, timeout time.Duration) LiveSnapshot {
	t.Helper()
	type result struct {
		snap LiveSnapshot
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var eventName string
		var dataLines []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if eventName == "snapshot" && len(dataLines) > 0 {
					var snap LiveSnapshot
					if jErr := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &snap); jErr != nil {
						ch <- result{err: jErr}
						return
					}
					ch <- result{snap: snap}
					return
				}
				eventName = ""
				dataLines = nil
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if after, ok := strings.CutPrefix(line, "event:"); ok {
				eventName = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(line, "data:"); ok {
				dataLines = append(dataLines, strings.TrimSpace(after))
			}
		}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read SSE snapshot: %v", res.err)
		}
		return res.snap
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SSE snapshot")
		return LiveSnapshot{}
	}
}

func TestOverviewLiveRequiresAuth(t *testing.T) {
	h := newHarness(t)
	configureFastLiveHub(h)

	resp, _ := h.do(http.MethodGet, "/api/overview/live", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if calls := h.fake.Calls(); len(calls) != 0 {
		t.Fatalf("panel calls = %v, want none when unauthenticated", calls)
	}
}

func TestOverviewLiveIdleDoesNotPoll(t *testing.T) {
	h := newHarness(t)
	configureFastLiveHub(h)
	h.login()
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "p1", "baseUrl": h.fake.URL(), "apiToken": "tok",
	})
	h.fake.ResetCalls()

	time.Sleep(85 * time.Millisecond)
	if calls := h.fake.Calls(); len(calls) != 0 {
		t.Fatalf("idle panel calls = %v, want 0 when nobody watches", calls)
	}
}

func TestOverviewLiveStreamAggregationAndStop(t *testing.T) {
	h := newHarness(t)
	configureFastLiveHub(h)

	fake2 := paneltest.New("tok2", paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "sg", Enable: true})
	t.Cleanup(fake2.Close)

	h.fake.SetStatus(map[string]any{
		"cpu": 20.0, "cpuCores": 2, "logicalPro": 4,
		"mem":      map[string]any{"current": uint64(1000), "total": uint64(4000)},
		"disk":     map[string]any{"current": uint64(5000), "total": uint64(20000)},
		"loads":    []float64{0.4, 0.3, 0.2},
		"tcpCount": 100, "udpCount": 25,
		"netIO":        map[string]any{"up": uint64(1024), "down": uint64(2048)},
		"xray":         map[string]any{"state": "running", "version": "25.1.1"},
		"panelVersion": "3.7.0",
		"uptime":       3600,
	})
	h.fake.SetOnlineClients([]string{"Alice@FireX", "bob@FireX"})

	fake2.SetStatus(map[string]any{
		"cpu": 80.0, "cpuCores": 2, "logicalPro": 2,
		"mem":      map[string]any{"current": uint64(3000), "total": uint64(4000)},
		"disk":     map[string]any{"current": uint64(10000), "total": uint64(20000)},
		"loads":    []float64{1.6, 1.2, 0.8},
		"tcpCount": 200, "udpCount": 50,
		"netIO":        map[string]any{"up": uint64(4096), "down": uint64(8192)},
		"xray":         map[string]any{"state": "running", "version": "25.1.1"},
		"panelVersion": "3.7.0",
		"uptime":       7200,
	})
	fake2.SetOnlineClients([]string{"alice@FireX", "manual-user"})

	h.login()
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "node-hk", "baseUrl": h.fake.URL(), "apiToken": "tok",
	})
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "node-sg", "baseUrl": fake2.URL(), "apiToken": "tok2",
	})
	h.fake.ResetCalls()
	fake2.ResetCalls()

	ctx1, cancel1 := context.WithCancel(context.Background())
	resp1 := openSSE(t, h, ctx1)
	defer func() {
		cancel1()
		resp1.Body.Close()
	}()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp1.StatusCode)
	}
	if ct := resp1.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if xab := resp1.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", xab)
	}

	r1 := bufio.NewReader(resp1.Body)
	snap := readNextSnapshot(t, r1, 2*time.Second)

	sum := snap.Summary
	if sum.TotalPanels != 2 || sum.OnlinePanels != 2 || sum.ActivePanels != 2 {
		t.Fatalf("panel counts = %+v, want 2/2/2", sum)
	}
	if sum.TotalNetUp != 5120 || sum.TotalNetDown != 10240 {
		t.Fatalf("net speed = %d/%d, want 5120/10240", sum.TotalNetUp, sum.TotalNetDown)
	}
	if sum.TotalTCP != 300 || sum.TotalUDP != 75 || sum.TotalConnections != 375 {
		t.Fatalf("connections = %d/%d/%d, want 300/75/375", sum.TotalTCP, sum.TotalUDP, sum.TotalConnections)
	}
	if sum.OnlineUsers != 2 || sum.OnlineUserSessions != 3 || sum.OnlineClients != 3 {
		t.Fatalf("online users/sessions/clients = %d/%d/%d, want 2/3/3", sum.OnlineUsers, sum.OnlineUserSessions, sum.OnlineClients)
	}
	if sum.TotalCPUCores != 6 || sum.TotalMemUsed != 4000 || sum.TotalMemTotal != 8000 {
		t.Fatalf("resources = cores:%d mem:%d/%d", sum.TotalCPUCores, sum.TotalMemUsed, sum.TotalMemTotal)
	}
	if math.Abs(sum.FleetLoad1-2.0) > 0.01 || math.Abs(sum.FleetLoad5-1.5) > 0.01 || math.Abs(sum.FleetLoad15-1.0) > 0.01 {
		t.Fatalf("fleet loads = %v/%v/%v, want 2/1.5/1", sum.FleetLoad1, sum.FleetLoad5, sum.FleetLoad15)
	}
	if sum.CPU.Min.Value != 20 || sum.CPU.Min.PanelName != "node-hk" || sum.CPU.Max.Value != 80 || sum.CPU.Max.PanelName != "node-sg" || sum.CPU.Avg != 50 {
		t.Fatalf("CPU stats = %+v", sum.CPU)
	}
	if sum.Mem.Min.Value != 25 || sum.Mem.Min.PanelName != "node-hk" || sum.Mem.Max.Value != 75 || sum.Mem.Max.PanelName != "node-sg" || sum.Mem.Avg != 50 {
		t.Fatalf("Mem stats = %+v", sum.Mem)
	}
	// node-hk loadIndex = 0.45*20 + 0.30*25 + 0.25*(0.4/4*100) = 9 + 7.5 + 2.5 = 19.0
	// node-sg loadIndex = 0.45*80 + 0.30*75 + 0.25*(1.6/2*100) = 36 + 22.5 + 20 = 78.5
	// avg = 48.8 (48.75 rounded to 1 decimal), systemLoadIndex = 0.75*48.8 + 0.25*78.5 = 56.2
	if sum.LoadIndex.Min.Value != 19 || sum.LoadIndex.Max.Value != 78.5 || sum.LoadIndex.Avg != 48.8 {
		t.Fatalf("LoadIndex stats = %+v", sum.LoadIndex)
	}
	if sum.SystemLoadIndex != 56.2 || sum.SystemLoadLevel != "normal" {
		t.Fatalf("SystemLoad = %v (%q), want 56.2 (normal)", sum.SystemLoadIndex, sum.SystemLoadLevel)
	}

	// Second subscriber catches up immediately with buffered history.
	ctx2, cancel2 := context.WithCancel(context.Background())
	resp2 := openSSE(t, h, ctx2)
	defer func() {
		cancel2()
		resp2.Body.Close()
	}()
	r2 := bufio.NewReader(resp2.Body)
	initial2 := readNextSnapshot(t, r2, 2*time.Second)
	if len(initial2.History) == 0 {
		t.Fatalf("second subscriber initial snapshot missing catch-up history")
	}

	// Close both subscribers and verify polling stops completely after idleGrace.
	cancel1()
	resp1.Body.Close()
	cancel2()
	resp2.Body.Close()

	time.Sleep(60 * time.Millisecond)
	h.fake.ResetCalls()
	fake2.ResetCalls()
	time.Sleep(90 * time.Millisecond)

	if calls := h.fake.Calls(); len(calls) != 0 {
		t.Fatalf("fake1 calls after unsubscribe = %v, want 0", calls)
	}
	if calls := fake2.Calls(); len(calls) != 0 {
		t.Fatalf("fake2 calls after unsubscribe = %v, want 0", calls)
	}
}

func TestOverviewLiveOfflineIsolationAndShutdown(t *testing.T) {
	h := newHarness(t)
	configureFastLiveHub(h)

	fake2 := paneltest.New("tok2", paneltest.Inbound{ID: 1, Port: 443, Protocol: "vless", Remark: "down", Enable: true})
	fake2URL := fake2.URL()

	h.fake.SetStatus(map[string]any{
		"cpu": 40.0, "cpuCores": 2, "logicalPro": 2,
		"mem":      map[string]any{"current": uint64(2000), "total": uint64(4000)},
		"loads":    []float64{1.0},
		"tcpCount": 50, "udpCount": 10,
		"netIO": map[string]any{"up": uint64(500), "down": uint64(1500)},
		"xray":  map[string]any{"state": "running", "version": "25.1.1"},
	})
	h.fake.SetOnlineClients([]string{"user1@FireX"})

	h.login()
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "online-node", "baseUrl": h.fake.URL(), "apiToken": "tok",
	})
	h.mustDo(http.MethodPost, "/api/panels", map[string]any{
		"name": "offline-node", "baseUrl": fake2URL, "apiToken": "tok2",
	})
	// Take the second panel offline before opening the live stream.
	fake2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := openSSE(t, h, ctx)
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	snap := readNextSnapshot(t, r, 2*time.Second)

	if snap.Summary.TotalPanels != 2 || snap.Summary.OnlinePanels != 1 {
		t.Fatalf("panel counts = %d/%d, want 2/1", snap.Summary.TotalPanels, snap.Summary.OnlinePanels)
	}
	if snap.Summary.CPU.Min.Value != 40 || snap.Summary.CPU.Max.Value != 40 || snap.Summary.CPU.Avg != 40 {
		t.Fatalf("CPU stats = %+v, offline panel should not skew min/avg", snap.Summary.CPU)
	}
	if len(snap.Panels) != 2 || snap.Panels[1].Online || snap.Panels[1].Error == "" {
		t.Fatalf("panels = %+v, want panel 2 offline with error", snap.Panels)
	}

	// Closing the hub must immediately unblock active SSE readers so Shutdown never stalls.
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := r.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	h.srv.live.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not exit promptly when LiveHub closed")
	}
}
