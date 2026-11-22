package panel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// flakyTransport fails the first failures round trips with err, then forwards
// to the real transport.
type flakyTransport struct {
	failures int
	err      error
	attempts int
}

func (t *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.attempts++
	if t.attempts <= t.failures {
		return nil, t.err
	}
	return http.DefaultTransport.RoundTrip(req)
}

func newTestClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"xray":{"state":"running","version":"25.1.1"}}}`))
	}))
	t.Cleanup(srv.Close)
	return &Client{baseURL: srv.URL, token: "tok", http: &http.Client{Transport: rt}}
}

func TestReadRetriesTransientDialFailure(t *testing.T) {
	rt := &flakyTransport{
		failures: 1,
		err:      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
	}
	c := newTestClient(t, rt)

	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want the retry to recover", err)
	}
	if status.Xray.Version != "25.1.1" {
		t.Errorf("xray version = %q, want 25.1.1", status.Xray.Version)
	}
	if rt.attempts != 2 {
		t.Errorf("attempts = %d, want 2", rt.attempts)
	}
}

func TestReadRetriesTemporaryDNSFailure(t *testing.T) {
	// What a resolver answering SERVFAIL looks like to net/http.
	rt := &flakyTransport{
		failures: 1,
		err:      &net.DNSError{Err: "server misbehaving", Name: "panel.example.com", IsTemporary: true},
	}
	c := newTestClient(t, rt)

	if _, err := c.Status(context.Background()); err != nil {
		t.Fatalf("Status() error = %v, want the retry to recover", err)
	}
	if rt.attempts != 2 {
		t.Errorf("attempts = %d, want 2", rt.attempts)
	}
}

func TestReadDoesNotRetryUnknownHost(t *testing.T) {
	rt := &flakyTransport{
		failures: 1,
		err:      &net.DNSError{Err: "no such host", Name: "typo.example.com", IsNotFound: true},
	}
	c := newTestClient(t, rt)

	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("Status() error = nil, want the lookup failure")
	}
	if rt.attempts != 1 {
		t.Errorf("attempts = %d, want 1: a name that does not exist will not appear on retry", rt.attempts)
	}
}

func TestWriteIsNeverRetried(t *testing.T) {
	rt := &flakyTransport{
		failures: 1,
		err:      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
	}
	c := newTestClient(t, rt)

	if err := c.DeleteClient(context.Background(), "alice@FireX"); err == nil {
		t.Fatal("DeleteClient() error = nil, want the dial failure")
	}
	if rt.attempts != 1 {
		t.Errorf("attempts = %d, want 1: a repeated write is a second edit", rt.attempts)
	}
}

func TestPanelErrorIsNotRetried(t *testing.T) {
	rt := &flakyTransport{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"msg":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{baseURL: srv.URL, token: "tok", http: &http.Client{Transport: rt}}

	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("Status() error = nil, want the panel's rejection")
	}
	if rt.attempts != 1 {
		t.Errorf("attempts = %d, want 1: the panel answered, so the answer stands", rt.attempts)
	}
}

func TestStatusLiveDoesNotRetryAndDecodesTelemetry(t *testing.T) {
	rt := &flakyTransport{
		failures: 1,
		err:      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
	}
	c := newTestClient(t, rt)

	if _, err := c.StatusLive(context.Background()); err == nil {
		t.Fatal("StatusLive() error = nil, want immediate dial failure without retries")
	}
	if rt.attempts != 1 {
		t.Errorf("attempts = %d, want 1", rt.attempts)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{
			"cpu":34.5,"cpuCores":2,"logicalPro":4,"cpuSpeedMhz":2400,
			"mem":{"current":1073741824,"total":4294967296},
			"swap":{"current":1024,"total":2048},
			"disk":{"current":5000,"total":10000},
			"loads":[0.8,0.6,0.4],
			"tcpCount":120,"udpCount":30,
			"netIO":{"up":1000,"down":2000,"pktUp":10,"pktDown":20},
			"netTraffic":{"sent":10000,"recv":20000,"pktSent":100,"pktRecv":200},
			"xray":{"state":"running","version":"25.1.1"},
			"panelVersion":"3.7.0","uptime":3600
		}}`))
	}))
	t.Cleanup(srv.Close)

	liveClient := New(srv.URL, "tok", false)
	st, err := liveClient.StatusLive(context.Background())
	if err != nil {
		t.Fatalf("StatusLive() error = %v", err)
	}
	if st.LogicalPro != 4 || st.TcpCount != 120 || st.UdpCount != 30 || st.NetIO.Up != 1000 || st.NetIO.Down != 2000 || len(st.Loads) != 3 {
		t.Fatalf("unexpected decoded status: %+v", st)
	}
}

func TestOnlineClientsAndLegacyFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/clients/onlines":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"msg":"not found"}`))
		case "/panel/api/inbounds/onlines":
			_, _ = w.Write([]byte(`{"success":true,"obj":["alice@FireX","bob@FireX"]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	emails, err := c.OnlineClients(context.Background())
	if err != nil {
		t.Fatalf("OnlineClients() error = %v", err)
	}
	if len(emails) != 2 || emails[0] != "alice@FireX" || emails[1] != "bob@FireX" {
		t.Fatalf("emails = %v, want [alice@FireX bob@FireX]", emails)
	}

	nullSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":null}`))
	}))
	t.Cleanup(nullSrv.Close)

	nullClient := New(nullSrv.URL, "tok", false)
	empty, err := nullClient.OnlineClients(context.Background())
	if err != nil {
		t.Fatalf("OnlineClients(null) error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, want non-nil empty slice", empty)
	}
}
