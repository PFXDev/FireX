package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
)

const (
	defaultLiveInterval    = 2 * time.Second
	defaultLivePollTimeout = 2500 * time.Millisecond
	defaultLiveIdleGrace   = 2 * time.Second
	defaultLiveHeartbeat   = 15 * time.Second
	defaultLiveHistoryCap  = 30
	liveBackoffThreshold   = 2
	liveBackoffSkipTicks   = 4
)

type LivePanelStat struct {
	ID            uint    `json:"id"`
	Name          string  `json:"name"`
	Online        bool    `json:"online"`
	Error         string  `json:"error,omitempty"`
	RttMs         int64   `json:"rttMs"`
	CPU           float64 `json:"cpu"`
	CPUCores      int     `json:"cpuCores"`
	MemUsed       uint64  `json:"memUsed"`
	MemTotal      uint64  `json:"memTotal"`
	MemPercent    float64 `json:"memPercent"`
	DiskUsed      uint64  `json:"diskUsed"`
	DiskTotal     uint64  `json:"diskTotal"`
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
	LoadIndex     float64 `json:"loadIndex"`
	NetUp         uint64  `json:"netUp"`
	NetDown       uint64  `json:"netDown"`
	TCPCount      int     `json:"tcpCount"`
	UDPCount      int     `json:"udpCount"`
	OnlineUsers   int     `json:"onlineUsers"`
	OnlineClients int     `json:"onlineClients"`
	XrayState     string  `json:"xrayState"`
	XrayVersion   string  `json:"xrayVersion"`
	Uptime        uint64  `json:"uptime"`
	SampledAt     int64   `json:"sampledAt"`
}

type LiveMetricExtreme struct {
	Value     float64 `json:"value"`
	PanelID   uint    `json:"panelId"`
	PanelName string  `json:"panelName"`
}

type LiveMetricStats struct {
	Avg float64           `json:"avg"`
	Min LiveMetricExtreme `json:"min"`
	Max LiveMetricExtreme `json:"max"`
}

type LiveTrendPoint struct {
	T           int64   `json:"t"`
	NetUp       uint64  `json:"netUp"`
	NetDown     uint64  `json:"netDown"`
	CPUAvg      float64 `json:"cpuAvg"`
	MemAvg      float64 `json:"memAvg"`
	LoadAvg     float64 `json:"loadAvg"`
	SystemLoad  float64 `json:"systemLoad"`
	Connections int     `json:"connections"`
	OnlineUsers int     `json:"onlineUsers"`
}

type LiveSummary struct {
	TotalPanels        int             `json:"totalPanels"`
	OnlinePanels       int             `json:"onlinePanels"`
	ActivePanels       int             `json:"activePanels"`
	TotalNetUp         uint64          `json:"totalNetUp"`
	TotalNetDown       uint64          `json:"totalNetDown"`
	TotalTCP           int             `json:"totalTcp"`
	TotalUDP           int             `json:"totalUdp"`
	TotalConnections   int             `json:"totalConnections"`
	OnlineUsers        int             `json:"onlineUsers"`
	OnlineUserSessions int             `json:"onlineUserSessions"`
	OnlineClients      int             `json:"onlineClients"`
	TotalCPUCores      int             `json:"totalCpuCores"`
	TotalMemUsed       uint64          `json:"totalMemUsed"`
	TotalMemTotal      uint64          `json:"totalMemTotal"`
	FleetLoad1         float64         `json:"fleetLoad1"`
	FleetLoad5         float64         `json:"fleetLoad5"`
	FleetLoad15        float64         `json:"fleetLoad15"`
	SystemLoadIndex    float64         `json:"systemLoadIndex"`
	SystemLoadLevel    string          `json:"systemLoadLevel"`
	CPU                LiveMetricStats `json:"cpu"`
	Mem                LiveMetricStats `json:"mem"`
	LoadIndex          LiveMetricStats `json:"loadIndex"`
}

type LiveSnapshot struct {
	Timestamp  int64            `json:"timestamp"`
	IntervalMs int64            `json:"intervalMs"`
	Summary    LiveSummary      `json:"summary"`
	Panels     []LivePanelStat  `json:"panels"`
	Point      LiveTrendPoint   `json:"point"`
	History    []LiveTrendPoint `json:"history,omitempty"`
}

type panelFailState struct {
	failures      int
	skipRemaining int
	lastError     string
	lastSampledAt int64
}

// LiveHub coordinates strictly on-demand panel telemetry polling for active
// SSE viewers. Zero subscribers means zero background goroutines and zero
// requests to remote 3x-ui panels.
type LiveHub struct {
	db  *store.DB
	mgr *provision.Manager

	interval    time.Duration
	pollTimeout time.Duration
	idleGrace   time.Duration
	heartbeat   time.Duration
	historyCap  int

	mu            sync.Mutex
	subs          map[chan LiveSnapshot]struct{}
	closed        bool
	stopCh        chan struct{}
	epoch         uint64
	collectCancel context.CancelFunc
	idleTimer     *time.Timer
	last          *LiveSnapshot
	history       []LiveTrendPoint
	failState     map[uint]*panelFailState
}

func newLiveHub(db *store.DB, mgr *provision.Manager) *LiveHub {
	return &LiveHub{
		db:          db,
		mgr:         mgr,
		interval:    defaultLiveInterval,
		pollTimeout: defaultLivePollTimeout,
		idleGrace:   defaultLiveIdleGrace,
		heartbeat:   defaultLiveHeartbeat,
		historyCap:  defaultLiveHistoryCap,
		subs:        make(map[chan LiveSnapshot]struct{}),
		stopCh:      make(chan struct{}),
		failState:   make(map[uint]*panelFailState),
	}
}

// Done is closed when the hub shuts down so active SSE handlers exit cleanly.
func (h *LiveHub) Done() <-chan struct{} { return h.stopCh }

// Subscribe registers an SSE viewer, starting the shared collector on 0 -> 1
// and returning any recent catch-up snapshot with buffered trend history.
func (h *LiveHub) Subscribe() (chan LiveSnapshot, *LiveSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, false
	}

	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}

	ch := make(chan LiveSnapshot, 4)
	h.subs[ch] = struct{}{}

	var initial *LiveSnapshot
	if h.last != nil {
		snap := *h.last
		snap.Panels = append([]LivePanelStat(nil), h.last.Panels...)
		if len(h.history) > 0 {
			snap.History = append([]LiveTrendPoint(nil), h.history...)
		}
		initial = &snap
	}

	if h.collectCancel == nil {
		h.epoch++
		epoch := h.epoch
		ctx, cancel := context.WithCancel(context.Background())
		h.collectCancel = cancel
		interval := h.interval
		go h.run(ctx, epoch, interval)
	}

	return ch, initial, true
}

// Unsubscribe removes a viewer and stops the collector after idleGrace once no
// viewers remain.
func (h *LiveHub) Unsubscribe(ch chan LiveSnapshot) {
	if ch == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.subs[ch]; !exists {
		return
	}
	delete(h.subs, ch)

	if len(h.subs) == 0 && h.collectCancel != nil && !h.closed {
		expectedEpoch := h.epoch
		if h.idleGrace <= 0 {
			h.stopCollectorLocked()
			return
		}
		if h.idleTimer != nil {
			h.idleTimer.Stop()
		}
		h.idleTimer = time.AfterFunc(h.idleGrace, func() {
			h.stopIdle(expectedEpoch)
		})
	}
}

func (h *LiveHub) stopIdle(expectedEpoch uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subs) > 0 || h.epoch != expectedEpoch {
		return
	}
	h.stopCollectorLocked()
}

func (h *LiveHub) stopCollectorLocked() {
	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}
	if h.collectCancel != nil {
		h.collectCancel()
		h.collectCancel = nil
	}
	h.epoch++
	h.last = nil
	h.history = nil
	h.failState = make(map[uint]*panelFailState)
}

// Close permanently stops the hub and unblocks all active SSE handlers.
func (h *LiveHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.stopCh)
	h.stopCollectorLocked()
}

func (h *LiveHub) run(ctx context.Context, epoch uint64, interval time.Duration) {
	h.collectOnce(ctx, epoch)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.collectOnce(ctx, epoch)
		}
	}
}

type panelPollResult struct {
	stat         LivePanelStat
	userEmails   []string
	clientEmails []string
	probed       bool
	failed       bool
}

func (h *LiveHub) collectOnce(ctx context.Context, epoch uint64) {
	if ctx.Err() != nil {
		return
	}

	var panels []model.Panel
	if err := h.db.Where("enabled = ?", true).Order("id ASC").Find(&panels).Error; err != nil {
		return
	}

	h.mu.Lock()
	if h.closed || h.epoch != epoch || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	pollTimeout := h.pollTimeout
	intervalMs := h.interval.Milliseconds()
	historyCap := h.historyCap
	skipMap := make(map[uint]*panelFailState, len(panels))
	for _, p := range panels {
		if st, ok := h.failState[p.ID]; ok {
			copied := *st
			skipMap[p.ID] = &copied
		}
	}
	h.mu.Unlock()

	results := make([]panelPollResult, len(panels))
	var wg sync.WaitGroup
	for i := range panels {
		p := panels[i]
		prevFail := skipMap[p.ID]
		if prevFail != nil && prevFail.skipRemaining > 0 {
			results[i] = panelPollResult{
				stat: LivePanelStat{
					ID:        p.ID,
					Name:      p.Name,
					Online:    false,
					Error:     prevFail.lastError,
					SampledAt: prevFail.lastSampledAt,
				},
			}
			continue
		}

		wg.Add(1)
		go func(idx int, p model.Panel) {
			defer wg.Done()
			results[idx] = h.pollPanel(ctx, &p, pollTimeout)
		}(i, p)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return
	}

	now := time.Now().UnixMilli()
	stats := make([]LivePanelStat, len(panels))
	uniqueUsers := make(map[string]struct{})
	uniqueClients := make(map[string]struct{})
	for i, res := range results {
		stats[i] = res.stat
		if res.stat.Online {
			for _, em := range res.userEmails {
				uniqueUsers[em] = struct{}{}
			}
			for _, em := range res.clientEmails {
				uniqueClients[em] = struct{}{}
			}
		}
	}

	summary := buildLiveSummary(stats, len(uniqueUsers), len(uniqueClients))
	point := LiveTrendPoint{
		T:           now,
		NetUp:       summary.TotalNetUp,
		NetDown:     summary.TotalNetDown,
		CPUAvg:      summary.CPU.Avg,
		MemAvg:      summary.Mem.Avg,
		LoadAvg:     summary.LoadIndex.Avg,
		SystemLoad:  summary.SystemLoadIndex,
		Connections: summary.TotalConnections,
		OnlineUsers: summary.OnlineUsers,
	}

	snap := LiveSnapshot{
		Timestamp:  now,
		IntervalMs: intervalMs,
		Summary:    summary,
		Panels:     stats,
		Point:      point,
	}

	h.mu.Lock()
	if h.closed || h.epoch != epoch || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}

	// Update per-panel backoff state and prune panels that were disabled/deleted.
	activeIDs := make(map[uint]struct{}, len(panels))
	for i, p := range panels {
		activeIDs[p.ID] = struct{}{}
		res := results[i]
		if !res.probed {
			if st, ok := h.failState[p.ID]; ok && st.skipRemaining > 0 {
				st.skipRemaining--
			}
			continue
		}
		if res.failed {
			st, ok := h.failState[p.ID]
			if !ok {
				st = &panelFailState{}
				h.failState[p.ID] = st
			}
			st.failures++
			st.lastError = res.stat.Error
			st.lastSampledAt = res.stat.SampledAt
			if st.failures >= liveBackoffThreshold {
				st.skipRemaining = liveBackoffSkipTicks
			}
		} else {
			delete(h.failState, p.ID)
		}
	}
	for id := range h.failState {
		if _, ok := activeIDs[id]; !ok {
			delete(h.failState, id)
		}
	}

	h.history = append(h.history, point)
	if historyCap > 0 && len(h.history) > historyCap {
		h.history = append([]LiveTrendPoint(nil), h.history[len(h.history)-historyCap:]...)
	}

	stored := snap
	stored.Panels = append([]LivePanelStat(nil), stats...)
	h.last = &stored

	for ch := range h.subs {
		select {
		case ch <- snap:
		default:
			// Drop one stale buffered frame so slow consumers catch up to the latest tick.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
	h.mu.Unlock()
}

func (h *LiveHub) pollPanel(ctx context.Context, p *model.Panel, timeout time.Duration) panelPollResult {
	pCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := h.mgr.ClientFor(p)

	var (
		status    *panel.Status
		statusErr error
		rttMs     int64
		onlines   []string
		onlineErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		status, statusErr = client.StatusLive(pCtx)
		rttMs = max(int64(0), time.Since(start).Milliseconds())
	}()
	go func() {
		defer wg.Done()
		onlines, onlineErr = client.OnlineClients(pCtx)
	}()
	wg.Wait()

	sampledAt := time.Now().UnixMilli()
	if statusErr != nil {
		return panelPollResult{
			stat: LivePanelStat{
				ID:        p.ID,
				Name:      p.Name,
				Online:    false,
				Error:     statusErr.Error(),
				SampledAt: sampledAt,
			},
			probed: true,
			failed: true,
		}
	}

	cores := status.LogicalPro
	if cores <= 0 {
		cores = status.CpuCores
	}
	if cores <= 0 {
		cores = 1
	}

	cpuPct := round1(clampFloat(status.Cpu, 0, 100))
	var memPct float64
	if status.Mem.Total > 0 {
		memPct = round1(clampFloat(float64(status.Mem.Current)/float64(status.Mem.Total)*100, 0, 100))
	}

	var load1, load5, load15 float64
	if len(status.Loads) > 0 {
		load1 = round2(max(0.0, status.Loads[0]))
	}
	if len(status.Loads) > 1 {
		load5 = round2(max(0.0, status.Loads[1]))
	}
	if len(status.Loads) > 2 {
		load15 = round2(max(0.0, status.Loads[2]))
	}

	var loadIdx float64
	if len(status.Loads) > 0 {
		loadNorm := clampFloat(load1/float64(cores)*100, 0, 100)
		loadIdx = round1(0.45*cpuPct + 0.30*memPct + 0.25*loadNorm)
	} else {
		loadIdx = round1(0.60*cpuPct + 0.40*memPct)
	}

	var userEmails, clientEmails []string
	if onlineErr == nil && len(onlines) > 0 {
		seen := make(map[string]struct{}, len(onlines))
		suffix := strings.ToLower(provision.EmailSuffix)
		for _, raw := range onlines {
			norm := strings.ToLower(strings.TrimSpace(raw))
			if norm == "" {
				continue
			}
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			clientEmails = append(clientEmails, norm)
			if strings.HasSuffix(norm, suffix) {
				userEmails = append(userEmails, norm)
			}
		}
	}

	return panelPollResult{
		stat: LivePanelStat{
			ID:            p.ID,
			Name:          p.Name,
			Online:        true,
			RttMs:         rttMs,
			CPU:           cpuPct,
			CPUCores:      cores,
			MemUsed:       status.Mem.Current,
			MemTotal:      status.Mem.Total,
			MemPercent:    memPct,
			DiskUsed:      status.Disk.Current,
			DiskTotal:     status.Disk.Total,
			Load1:         load1,
			Load5:         load5,
			Load15:        load15,
			LoadIndex:     loadIdx,
			NetUp:         status.NetIO.Up,
			NetDown:       status.NetIO.Down,
			TCPCount:      max(0, status.TcpCount),
			UDPCount:      max(0, status.UdpCount),
			OnlineUsers:   len(userEmails),
			OnlineClients: len(clientEmails),
			XrayState:     status.Xray.State,
			XrayVersion:   status.Xray.Version,
			Uptime:        status.Uptime,
			SampledAt:     sampledAt,
		},
		userEmails:   userEmails,
		clientEmails: clientEmails,
		probed:       true,
	}
}

func buildLiveSummary(stats []LivePanelStat, uniqueUsers, uniqueClients int) LiveSummary {
	summary := LiveSummary{
		TotalPanels:   len(stats),
		OnlineUsers:   uniqueUsers,
		OnlineClients: uniqueClients,
	}

	var (
		cpuSum, memSum, loadSum float64
		hasOnline               bool
	)

	for _, st := range stats {
		if !st.Online {
			continue
		}
		summary.OnlinePanels++
		if st.NetUp+st.NetDown > 0 || st.OnlineClients > 0 || st.TCPCount+st.UDPCount > 0 {
			summary.ActivePanels++
		}
		summary.TotalNetUp += st.NetUp
		summary.TotalNetDown += st.NetDown
		summary.TotalTCP += st.TCPCount
		summary.TotalUDP += st.UDPCount
		summary.OnlineUserSessions += st.OnlineUsers
		summary.TotalCPUCores += st.CPUCores
		summary.TotalMemUsed += st.MemUsed
		summary.TotalMemTotal += st.MemTotal
		summary.FleetLoad1 += st.Load1
		summary.FleetLoad5 += st.Load5
		summary.FleetLoad15 += st.Load15

		cpuSum += st.CPU
		memSum += st.MemPercent
		loadSum += st.LoadIndex

		if !hasOnline {
			hasOnline = true
			summary.CPU.Min = LiveMetricExtreme{Value: st.CPU, PanelID: st.ID, PanelName: st.Name}
			summary.CPU.Max = summary.CPU.Min
			summary.Mem.Min = LiveMetricExtreme{Value: st.MemPercent, PanelID: st.ID, PanelName: st.Name}
			summary.Mem.Max = summary.Mem.Min
			summary.LoadIndex.Min = LiveMetricExtreme{Value: st.LoadIndex, PanelID: st.ID, PanelName: st.Name}
			summary.LoadIndex.Max = summary.LoadIndex.Min
			continue
		}

		updateMetricExtremes(&summary.CPU, st.CPU, st.ID, st.Name)
		updateMetricExtremes(&summary.Mem, st.MemPercent, st.ID, st.Name)
		updateMetricExtremes(&summary.LoadIndex, st.LoadIndex, st.ID, st.Name)
	}

	summary.TotalConnections = summary.TotalTCP + summary.TotalUDP
	summary.FleetLoad1 = round2(summary.FleetLoad1)
	summary.FleetLoad5 = round2(summary.FleetLoad5)
	summary.FleetLoad15 = round2(summary.FleetLoad15)

	if summary.OnlinePanels == 0 {
		summary.SystemLoadLevel = "idle"
		return summary
	}

	n := float64(summary.OnlinePanels)
	summary.CPU.Avg = round1(cpuSum / n)
	summary.Mem.Avg = round1(memSum / n)
	summary.LoadIndex.Avg = round1(loadSum / n)

	summary.SystemLoadIndex = round1(0.75*summary.LoadIndex.Avg + 0.25*summary.LoadIndex.Max.Value)
	switch {
	case summary.SystemLoadIndex < 35:
		summary.SystemLoadLevel = "idle"
	case summary.SystemLoadIndex < 65:
		summary.SystemLoadLevel = "normal"
	case summary.SystemLoadIndex < 85:
		summary.SystemLoadLevel = "busy"
	default:
		summary.SystemLoadLevel = "high"
	}
	return summary
}

func updateMetricExtremes(m *LiveMetricStats, value float64, panelID uint, panelName string) {
	if value < m.Min.Value {
		m.Min = LiveMetricExtreme{Value: value, PanelID: panelID, PanelName: panelName}
	}
	if value > m.Max.Value {
		m.Max = LiveMetricExtreme{Value: value, PanelID: panelID, PanelName: panelName}
	}
}

func clampFloat(v, minVal, maxVal float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, -1) || v < minVal {
		return minVal
	}
	if math.IsInf(v, 1) || v > maxVal {
		return maxVal
	}
	return v
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func (s *Server) handleOverviewLive(c *gin.Context) {
	ch, initial, ok := s.live.Subscribe()
	if !ok {
		failMsg(c, http.StatusServiceUnavailable, "实时监控服务正在关闭")
		return
	}
	defer s.live.Unsubscribe(ch)

	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache, no-transform")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()

	if initial != nil {
		if !writeSSEEvent(c.Writer, "snapshot", initial) {
			return
		}
	}

	hbInterval := s.live.heartbeat
	if hbInterval <= 0 {
		hbInterval = defaultLiveHeartbeat
	}
	ping := time.NewTicker(hbInterval)
	defer ping.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-s.live.Done():
			return
		case snap := <-ch:
			if !writeSSEEvent(c.Writer, "snapshot", snap) {
				return
			}
		case <-ping.C:
			if !writeSSEComment(c.Writer, "ping") {
				return
			}
		}
	}
}

func writeSSEEvent(w gin.ResponseWriter, event string, payload any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	w.Flush()
	return true
}

func writeSSEComment(w gin.ResponseWriter, comment string) bool {
	if _, err := fmt.Fprintf(w, ": %s\n\n", comment); err != nil {
		return false
	}
	w.Flush()
	return true
}
