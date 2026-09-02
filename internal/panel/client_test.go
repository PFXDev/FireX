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

	if err := c.DeleteClient(context.Background(), "alice@firex"); err == nil {
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
