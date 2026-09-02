// Package panel is the HTTP client for a remote 3x-ui panel's /panel/api surface.
//
// Every call authenticates with a panel API token of admin scope; the panel's
// CSRF middleware exempts bearer-authenticated requests.
package panel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 20 * time.Second

// Panels sit behind a WAN link, so a resolver that answers SERVFAIL or a
// connection refused mid-restart is a blip often enough that a single one must
// not report a healthy panel as offline. Reads are safe to repeat; writes are
// not.
const (
	readRetries    = 2
	readRetryDelay = 500 * time.Millisecond
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string, skipTLSVerify bool) *Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if skipTLSVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in per panel, for self-signed panel certs
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout, Transport: tr},
	}
}

// APIError is a panel response with success=false; it carries the panel's own
// message so the admin UI can show why an operation was rejected.
type APIError struct {
	Status int
	Msg    string
	Path   string
}

func (e *APIError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("panel %s: http %d", e.Path, e.Status)
	}
	return fmt.Sprintf("panel %s: %s", e.Path, e.Msg)
}

type envelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

// IsNotFound reports whether the panel answered 404, which for an unknown route
// means the panel predates the endpoint rather than that the object is missing.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		payload = buf
	}

	resp, err := c.send(ctx, method, path, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var env envelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
		return &APIError{Status: resp.StatusCode, Path: path, Msg: shortBody(resp.StatusCode, raw)}
	}
	if resp.StatusCode != http.StatusOK || !env.Success {
		return &APIError{Status: resp.StatusCode, Path: path, Msg: env.Msg}
	}
	if out != nil && len(env.Obj) > 0 {
		if err := json.Unmarshal(env.Obj, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

// send issues the request and hands back the panel's response. A GET that
// never reached the panel is retried: the whole read is repeatable, and once
// the panel has answered, the answer stands. Writes go out exactly once,
// because a retried add or delete would be a second edit, not a second look.
func (c *Client) send(ctx context.Context, method, path string, payload []byte) (*http.Response, error) {
	attempts := 1
	if method == http.MethodGet {
		attempts += readRetries
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * readRetryDelay):
			case <-ctx.Done():
				return nil, lastErr
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return nil, fmt.Errorf("build %s: %w", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		// The panel answers 404 instead of 401 for unauthenticated browser-shaped
		// requests; this header makes an auth failure surface as a real 401.
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = fmt.Errorf("call %s: %w", path, err)
		if ctx.Err() != nil || !retryable(err) {
			break
		}
	}
	return nil, lastErr
}

// retryable reports whether a transport error is a blip on the way out — a
// resolver that answered SERVFAIL, a connection refused at dial time — as
// opposed to a panel that is genuinely misconfigured. A name that does not
// exist, a request that ran out of time, and a connection that broke after the
// request was already on the wire are all left alone.
func retryable(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func shortBody(status int, raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return fmt.Sprintf("http %d with empty body", status)
	}
	return fmt.Sprintf("http %d: %s", status, s)
}

// Status fetches panel health. Doubles as the credential check.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var s Status
	if err := c.do(ctx, http.MethodGet, "/panel/api/server/status", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Inbounds lists inbounds with their full settings, which is what reveals
// which clients are attached to which inbound.
func (c *Client) Inbounds(ctx context.Context) ([]Inbound, error) {
	var out []Inbound
	if err := c.do(ctx, http.MethodGet, "/panel/api/inbounds/list", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InboundsSlim is the traffic-polling variant: same per-client counters without
// the per-client settings blobs. Panels that predate the endpoint fall back.
func (c *Client) InboundsSlim(ctx context.Context) ([]Inbound, error) {
	var out []Inbound
	err := c.do(ctx, http.MethodGet, "/panel/api/inbounds/list/slim", nil, &out)
	if err == nil {
		return out, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	return c.Inbounds(ctx)
}

// GetClient returns the client record plus the inbound ids it is attached to.
// A missing client is reported as ErrClientMissing.
func (c *Client) GetClient(ctx context.Context, email string) (*ClientDetail, error) {
	var d ClientDetail
	if err := c.do(ctx, http.MethodGet, "/panel/api/clients/get/"+url.PathEscape(email), nil, &d); err != nil {
		return nil, err
	}
	if d.Client.Email == "" {
		return nil, ErrClientMissing
	}
	return &d, nil
}

func (c *Client) AddClient(ctx context.Context, cl RemoteClient, inboundIDs []int) error {
	body := map[string]any{"client": cl, "inboundIds": inboundIDs}
	return c.do(ctx, http.MethodPost, "/panel/api/clients/add", body, nil)
}

// UpdateClient rewrites the client's fields panel-wide. The panel takes the
// client object at the top level here, unlike add.
func (c *Client) UpdateClient(ctx context.Context, email string, cl RemoteClient) error {
	return c.do(ctx, http.MethodPost, "/panel/api/clients/update/"+url.PathEscape(email), cl, nil)
}

func (c *Client) DeleteClient(ctx context.Context, email string) error {
	return c.do(ctx, http.MethodPost, "/panel/api/clients/del/"+url.PathEscape(email), nil, nil)
}

func (c *Client) AttachClient(ctx context.Context, email string, inboundIDs []int) error {
	body := map[string]any{"inboundIds": inboundIDs}
	return c.do(ctx, http.MethodPost, "/panel/api/clients/"+url.PathEscape(email)+"/attach", body, nil)
}

func (c *Client) DetachClient(ctx context.Context, email string, inboundIDs []int) error {
	body := map[string]any{"inboundIds": inboundIDs}
	return c.do(ctx, http.MethodPost, "/panel/api/clients/"+url.PathEscape(email)+"/detach", body, nil)
}

// ClientLinks returns the share links for every inbound the client is on,
// rendered by the panel itself so host overrides and Reality keys are correct.
func (c *Client) ClientLinks(ctx context.Context, email string) ([]string, error) {
	var links []string
	if err := c.do(ctx, http.MethodGet, "/panel/api/clients/links/"+url.PathEscape(email), nil, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (c *Client) ResetClientTraffic(ctx context.Context, email string) error {
	return c.do(ctx, http.MethodPost, "/panel/api/clients/resetTraffic/"+url.PathEscape(email), nil, nil)
}
