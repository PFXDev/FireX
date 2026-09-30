// Package paneltest provides an in-memory stand-in for a 3x-ui panel's
// /panel/api surface, so FireX's sync and subscription paths can be driven
// end to end without a real panel.
package paneltest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type Inbound struct {
	ID             int
	Port           int
	Protocol       string
	Remark         string
	Tag            string
	Enable         bool
	Listen         string
	Settings       string
	StreamSettings string
	Sniffing       string
	DisableFlow    bool
}

type Client struct {
	Email      string
	ID         string
	Enable     bool
	TotalGB    int64
	ExpiryTime int64
	LimitIP    int
	Flow       string
	Up         int64
	Down       int64
}

// Panel is a fake 3x-ui instance. Every field is guarded by mu because the
// server goroutine and the test body both touch it.
type Panel struct {
	mu       sync.Mutex
	inbounds []Inbound
	clients  map[string]*Client
	// members maps lowercase email to the inbound ids it is attached to.
	members map[string]map[int]bool
	flows   map[string]map[int]string
	calls   []string

	Token  string
	Server *httptest.Server
	// FailNext makes the next matching request fail, for error-path tests.
	FailNext map[string]bool
}

func New(token string, inbounds ...Inbound) *Panel {
	p := &Panel{
		inbounds: inbounds,
		clients:  map[string]*Client{},
		members:  map[string]map[int]bool{},
		flows:    map[string]map[int]string{},
		Token:    token,
		FailNext: map[string]bool{},
	}
	p.Server = httptest.NewServer(http.HandlerFunc(p.handle))
	return p
}

func (p *Panel) Close() { p.Server.Close() }

func (p *Panel) URL() string { return p.Server.URL }

// Calls returns the API paths hit so far, so a test can assert that a no-op
// reconcile really did nothing.
func (p *Panel) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func (p *Panel) ResetCalls() {
	p.mu.Lock()
	p.calls = nil
	p.mu.Unlock()
}

func (p *Panel) Client(email string) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.clients[strings.ToLower(email)]
	if !ok {
		return nil
	}
	copied := *c
	return &copied
}

// Members returns the inbound ids a client is attached to, sorted ascending.
func (p *Panel) Members(email string) []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := p.members[strings.ToLower(email)]
	out := make([]int, 0, len(set))
	for _, ib := range p.inbounds {
		if set[ib.ID] {
			out = append(out, ib.ID)
		}
	}
	return out
}

// SetTraffic overwrites a client's counters the way a live panel's stats poll
// would.
func (p *Panel) SetTraffic(email string, up, down int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[strings.ToLower(email)]; ok {
		c.Up, c.Down = up, down
	}
}

func (p *Panel) ClientFlow(email string, inboundID int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.flows[strings.ToLower(email)][inboundID]
}

func (p *Panel) SetClientFlow(email string, inboundID int, flow string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flows[strings.ToLower(email)][inboundID] = flow
}

// AddInbound simulates an admin creating an inbound on the panel.
func (p *Panel) AddInbound(ib Inbound) {
	p.mu.Lock()
	p.inbounds = append(p.inbounds, ib)
	p.mu.Unlock()
}

// RemoveInbound simulates an admin deleting an inbound on the panel.
func (p *Panel) RemoveInbound(id int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := p.inbounds[:0]
	for _, ib := range p.inbounds {
		if ib.ID != id {
			kept = append(kept, ib)
		}
	}
	p.inbounds = kept
}

func (p *Panel) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+p.Token {
		writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Msg: "unauthorized"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/panel/api")

	p.mu.Lock()
	p.calls = append(p.calls, r.Method+" "+path)
	if p.FailNext[path] {
		delete(p.FailNext, path)
		p.mu.Unlock()
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "induced failure"})
		return
	}
	p.mu.Unlock()

	switch {
	case path == "/server/status":
		writeJSON(w, http.StatusOK, envelope{Success: true, Obj: map[string]any{
			"xray":         map[string]any{"state": "running", "version": "25.1.1"},
			"panelVersion": "3.0.0-test",
		}})
	case path == "/inbounds/list", path == "/inbounds/list/slim":
		p.mu.Lock()
		defer p.mu.Unlock()
		writeJSON(w, http.StatusOK, envelope{Success: true, Obj: p.inboundPayload()})
	case path == "/clients/add":
		p.addClient(w, r)
	case strings.HasPrefix(path, "/clients/update/"):
		p.updateClient(w, r, strings.TrimPrefix(path, "/clients/update/"))
	case strings.HasPrefix(path, "/clients/del/"):
		p.deleteClient(w, strings.TrimPrefix(path, "/clients/del/"))
	case strings.HasPrefix(path, "/clients/resetTraffic/"):
		p.resetTraffic(w, strings.TrimPrefix(path, "/clients/resetTraffic/"))
	case strings.HasPrefix(path, "/clients/links/"):
		p.clientLinks(w, strings.TrimPrefix(path, "/clients/links/"))
	case strings.HasSuffix(path, "/attach"), strings.HasSuffix(path, "/detach"):
		p.attachDetach(w, r, path)
	default:
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Msg: "no such route"})
	}
}

// inboundPayload mirrors 3x-ui v3.7's list shape: JSON-backed fields are
// nested objects on the wire, while traffic lives in clientStats.
func (p *Panel) inboundPayload() []map[string]any {
	out := make([]map[string]any, 0, len(p.inbounds))
	for _, ib := range p.inbounds {
		var clients []map[string]any
		var stats []map[string]any
		for email, set := range p.members {
			if !set[ib.ID] {
				continue
			}
			c := p.clients[email]
			clients = append(clients, map[string]any{"email": c.Email, "id": c.ID, "flow": p.flows[email][ib.ID]})
			stats = append(stats, map[string]any{
				"inboundId": ib.ID, "email": c.Email, "up": c.Up, "down": c.Down,
				"enable": c.Enable, "total": c.TotalGB, "expiryTime": c.ExpiryTime,
			})
		}
		settingsMap := map[string]any{}
		_ = json.Unmarshal([]byte(ib.Settings), &settingsMap)
		settingsMap["clients"] = clients
		settings, _ := json.Marshal(settingsMap)
		stream := ib.StreamSettings
		if stream == "" {
			stream = `{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.example.com"],"privateKey":"test-private-key","shortIds":["ab"],"settings":{"publicKey":"PBK","fingerprint":"chrome"}}}`
		}
		sniffing := ib.Sniffing
		if sniffing == "" {
			sniffing = `{"enabled":false}`
		}
		out = append(out, map[string]any{
			"id": ib.ID, "port": ib.Port, "protocol": ib.Protocol,
			"remark": ib.Remark, "tag": ib.Tag, "enable": ib.Enable,
			"settings":       json.RawMessage(settings),
			"streamSettings": json.RawMessage(stream),
			"sniffing":       json.RawMessage(sniffing),
			"listen":         ib.Listen, "disableFlow": ib.DisableFlow,
			"clientStats": stats,
		})
	}
	return out
}

func (p *Panel) addClient(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Client     Client `json:"client"`
		InboundIDs []int  `json:"inboundIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: err.Error()})
		return
	}
	key := strings.ToLower(body.Client.Email)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.clients[key]; exists {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "duplicate email"})
		return
	}
	c := body.Client
	p.clients[key] = &c
	p.members[key] = map[int]bool{}
	p.flows[key] = map[int]string{}
	for _, id := range body.InboundIDs {
		p.members[key][id] = true
		p.flows[key][id] = c.Flow
	}
	writeJSON(w, http.StatusOK, envelope{Success: true})
}

func (p *Panel) updateClient(w http.ResponseWriter, r *http.Request, email string) {
	var incoming Client
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: err.Error()})
		return
	}
	key := strings.ToLower(email)
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.clients[key]
	if !ok {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "record not found"})
		return
	}
	c.ID = incoming.ID
	c.Enable = incoming.Enable
	c.TotalGB = incoming.TotalGB
	c.ExpiryTime = incoming.ExpiryTime
	c.LimitIP = incoming.LimitIP
	c.Flow = incoming.Flow
	filter := map[int]bool{}
	for _, raw := range strings.Split(r.URL.Query().Get("inboundIds"), ",") {
		if id, err := strconv.Atoi(raw); err == nil {
			filter[id] = true
		}
	}
	for id := range p.members[key] {
		if len(filter) == 0 || filter[id] {
			p.flows[key][id] = incoming.Flow
		}
	}
	writeJSON(w, http.StatusOK, envelope{Success: true})
}

func (p *Panel) deleteClient(w http.ResponseWriter, email string) {
	key := strings.ToLower(email)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.clients[key]; !ok {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "record not found"})
		return
	}
	delete(p.clients, key)
	delete(p.members, key)
	delete(p.flows, key)
	writeJSON(w, http.StatusOK, envelope{Success: true})
}

func (p *Panel) resetTraffic(w http.ResponseWriter, email string) {
	key := strings.ToLower(email)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		c.Up, c.Down = 0, 0
	}
	writeJSON(w, http.StatusOK, envelope{Success: true})
}

func (p *Panel) attachDetach(w http.ResponseWriter, r *http.Request, path string) {
	trimmed := strings.TrimPrefix(path, "/clients/")
	idx := strings.LastIndex(trimmed, "/")
	email, action := trimmed[:idx], trimmed[idx+1:]
	var body struct {
		InboundIDs []int `json:"inboundIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: err.Error()})
		return
	}
	key := strings.ToLower(email)
	p.mu.Lock()
	defer p.mu.Unlock()
	set, ok := p.members[key]
	if !ok {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "record not found"})
		return
	}
	for _, id := range body.InboundIDs {
		if action == "attach" {
			set[id] = true
			p.flows[key][id] = p.clients[key].Flow
		} else {
			delete(set, id)
			delete(p.flows[key], id)
		}
	}
	writeJSON(w, http.StatusOK, envelope{Success: true})
}

// clientLinks emits one reality vless link per attached inbound, matching the
// shape a real panel produces.
func (p *Panel) clientLinks(w http.ResponseWriter, email string) {
	key := strings.ToLower(email)
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.clients[key]
	if !ok {
		writeJSON(w, http.StatusOK, envelope{Success: false, Msg: "record not found"})
		return
	}
	set := p.members[key]
	links := []string{}
	for _, ib := range p.inbounds {
		if !set[ib.ID] {
			continue
		}
		flow := ""
		if value := p.flows[key][ib.ID]; value != "" {
			flow = "&flow=" + url.QueryEscape(value)
		}
		links = append(links, fmt.Sprintf(
			"vless://%s@host%d.example.com:%d?type=tcp&security=reality&pbk=PBK%d&sid=ab&fp=chrome&sni=www.example.com%s#%s",
			c.ID, ib.ID, ib.Port, ib.ID, flow, ib.Remark))
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Obj: links})
}

type envelope struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Obj     any    `json:"obj"`
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
