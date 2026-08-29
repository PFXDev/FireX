// Package sharelink parses the vless/vmess/trojan/ss/hysteria2/tuic URIs a
// 3x-ui panel emits into a protocol-neutral Proxy, and renders that Proxy as a
// mihomo (Clash.Meta) proxy entry.
package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Proxy struct {
	Name   string
	Type   string
	Server string
	Port   int

	UUID     string
	Password string
	Cipher   string
	AlterID  int
	Flow     string

	Network         string // tcp | ws | grpc | h2 | http
	Path            string
	Host            string
	GRPCServiceName string

	TLS           bool
	SNI           string
	ALPN          []string
	Fingerprint   string
	AllowInsecure bool

	RealityPublicKey string
	RealityShortID   string

	Obfs         string
	ObfsPassword string

	UDP bool
}

// Parse turns one share URI into a Proxy. The scheme decides the shape; an
// unsupported scheme is an error so callers can count what they skipped.
func Parse(raw string) (*Proxy, error) {
	raw = strings.TrimSpace(raw)
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return nil, fmt.Errorf("sharelink: not a URI: %q", truncate(raw))
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESS(raw)
	case "vmess":
		return parseVMess(raw)
	case "trojan":
		return parseTrojan(raw)
	case "ss":
		return parseShadowsocks(raw)
	case "hysteria2", "hy2":
		return parseHysteria2(raw)
	case "tuic":
		return parseTUIC(raw)
	default:
		return nil, fmt.Errorf("sharelink: unsupported scheme %q", scheme)
	}
}

// Parsed pairs a proxy with the link it came from, so callers can rewrite the
// original URI without having to find it again.
type Parsed struct {
	Raw   string
	Proxy *Proxy
}

// ParseMany parses a batch, returning what parsed and the errors for what did
// not. One bad link must not sink a whole subscription.
func ParseMany(raws []string) ([]Parsed, []error) {
	parsed := make([]Parsed, 0, len(raws))
	var errs []error
	for _, raw := range raws {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		p, err := Parse(raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		parsed = append(parsed, Parsed{Raw: raw, Proxy: p})
	}
	return parsed, errs
}

func parseVLESS(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("sharelink: vless: %w", err)
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("sharelink: vless: %w", err)
	}
	q := u.Query()
	p := &Proxy{
		Name:   fragmentName(u, host),
		Type:   "vless",
		Server: host,
		Port:   port,
		UUID:   u.User.Username(),
		Flow:   q.Get("flow"),
		UDP:    true,
	}
	applyTransport(p, q)
	applySecurity(p, q)
	if p.UUID == "" {
		return nil, fmt.Errorf("sharelink: vless: missing uuid in %q", truncate(raw))
	}
	return p, nil
}

// vmessJSON is the v2rayN base64 payload. Numeric fields ship as either JSON
// numbers or strings depending on the generator, hence json.Number.
type vmessJSON struct {
	PS   string      `json:"ps"`
	Add  string      `json:"add"`
	Port json.Number `json:"port"`
	ID   string      `json:"id"`
	Aid  json.Number `json:"aid"`
	Scy  string      `json:"scy"`
	Net  string      `json:"net"`
	Type string      `json:"type"`
	Host string      `json:"host"`
	Path string      `json:"path"`
	TLS  string      `json:"tls"`
	SNI  string      `json:"sni"`
	ALPN string      `json:"alpn"`
	FP   string      `json:"fp"`
}

func parseVMess(raw string) (*Proxy, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	if i := strings.IndexByte(payload, '#'); i >= 0 {
		payload = payload[:i]
	}
	decoded, err := decodeBase64(payload)
	if err != nil {
		return nil, fmt.Errorf("sharelink: vmess: %w", err)
	}
	var v vmessJSON
	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, fmt.Errorf("sharelink: vmess: %w", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(v.Port.String()))
	if err != nil {
		return nil, fmt.Errorf("sharelink: vmess: bad port %q", v.Port.String())
	}
	alterID, _ := strconv.Atoi(strings.TrimSpace(v.Aid.String()))
	cipher := v.Scy
	if cipher == "" {
		cipher = "auto"
	}
	name := v.PS
	if name == "" {
		name = v.Add
	}
	p := &Proxy{
		Name:        name,
		Type:        "vmess",
		Server:      v.Add,
		Port:        port,
		UUID:        v.ID,
		AlterID:     alterID,
		Cipher:      cipher,
		Network:     normalizeNetwork(v.Net),
		Path:        v.Path,
		Host:        v.Host,
		TLS:         v.TLS == "tls" || v.TLS == "reality",
		SNI:         v.SNI,
		ALPN:        splitCSV(v.ALPN),
		Fingerprint: v.FP,
		UDP:         true,
	}
	if p.Network == "grpc" {
		// v2rayN carries the gRPC service name in path for historical reasons.
		p.GRPCServiceName = v.Path
	}
	if p.TLS && p.SNI == "" {
		p.SNI = v.Host
	}
	return p, nil
}

func parseTrojan(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("sharelink: trojan: %w", err)
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("sharelink: trojan: %w", err)
	}
	password := u.User.Username()
	if password == "" {
		return nil, fmt.Errorf("sharelink: trojan: missing password in %q", truncate(raw))
	}
	q := u.Query()
	p := &Proxy{
		Name:     fragmentName(u, host),
		Type:     "trojan",
		Server:   host,
		Port:     port,
		Password: password,
		Flow:     q.Get("flow"),
		TLS:      true, // trojan is TLS by definition
		UDP:      true,
	}
	applyTransport(p, q)
	applySecurity(p, q)
	p.TLS = true
	return p, nil
}

func parseShadowsocks(raw string) (*Proxy, error) {
	body := strings.TrimPrefix(raw, "ss://")
	name := ""
	if i := strings.IndexByte(body, '#'); i >= 0 {
		name = decodeFragment(body[i+1:])
		body = body[:i]
	}
	query := url.Values{}
	if i := strings.IndexByte(body, '?'); i >= 0 {
		query, _ = url.ParseQuery(body[i+1:])
		body = body[:i]
	}

	var userInfo, hostPortPart string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		userInfo, hostPortPart = body[:at], body[at+1:]
		if decoded, err := decodeBase64(userInfo); err == nil {
			userInfo = string(decoded)
		}
	} else {
		// Legacy form: the whole "method:password@host:port" is base64.
		decoded, err := decodeBase64(body)
		if err != nil {
			return nil, fmt.Errorf("sharelink: ss: %w", err)
		}
		at := strings.LastIndexByte(string(decoded), '@')
		if at < 0 {
			return nil, fmt.Errorf("sharelink: ss: malformed legacy uri")
		}
		userInfo, hostPortPart = string(decoded)[:at], string(decoded)[at+1:]
	}

	method, password, ok := strings.Cut(userInfo, ":")
	if !ok {
		return nil, fmt.Errorf("sharelink: ss: malformed userinfo")
	}
	host, port, err := hostPort(hostPortPart)
	if err != nil {
		return nil, fmt.Errorf("sharelink: ss: %w", err)
	}
	if name == "" {
		name = host
	}
	// url.PathUnescape covers passwords percent-encoded in the SIP002 form.
	if unescaped, err := url.PathUnescape(password); err == nil {
		password = unescaped
	}
	p := &Proxy{
		Name:     name,
		Type:     "ss",
		Server:   host,
		Port:     port,
		Cipher:   method,
		Password: password,
		UDP:      true,
	}
	if plugin := query.Get("plugin"); plugin != "" {
		p.Obfs = plugin
	}
	return p, nil
}

func parseHysteria2(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("sharelink: hysteria2: %w", err)
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("sharelink: hysteria2: %w", err)
	}
	q := u.Query()
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password += ":" + pw
	}
	if password == "" {
		password = q.Get("auth")
	}
	p := &Proxy{
		Name:          fragmentName(u, host),
		Type:          "hysteria2",
		Server:        host,
		Port:          port,
		Password:      password,
		TLS:           true,
		SNI:           firstNonEmpty(q.Get("sni"), q.Get("peer")),
		ALPN:          splitCSV(q.Get("alpn")),
		AllowInsecure: isTrue(firstNonEmpty(q.Get("insecure"), q.Get("allowInsecure"))),
		Obfs:          q.Get("obfs"),
		ObfsPassword:  firstNonEmpty(q.Get("obfs-password"), q.Get("obfsParam")),
		Fingerprint:   q.Get("fp"),
		UDP:           true,
	}
	return p, nil
}

func parseTUIC(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("sharelink: tuic: %w", err)
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("sharelink: tuic: %w", err)
	}
	q := u.Query()
	password, _ := u.User.Password()
	p := &Proxy{
		Name:          fragmentName(u, host),
		Type:          "tuic",
		Server:        host,
		Port:          port,
		UUID:          u.User.Username(),
		Password:      password,
		TLS:           true,
		SNI:           q.Get("sni"),
		ALPN:          splitCSV(q.Get("alpn")),
		AllowInsecure: isTrue(firstNonEmpty(q.Get("insecure"), q.Get("allow_insecure"))),
		UDP:           true,
	}
	return p, nil
}

// applyTransport fills the stream-layer fields shared by vless and trojan.
func applyTransport(p *Proxy, q url.Values) {
	p.Network = normalizeNetwork(firstNonEmpty(q.Get("type"), q.Get("network")))
	switch p.Network {
	case "ws", "httpupgrade":
		p.Path = firstNonEmpty(q.Get("path"), "/")
		p.Host = q.Get("host")
	case "grpc":
		p.GRPCServiceName = q.Get("serviceName")
	case "h2":
		p.Path = firstNonEmpty(q.Get("path"), "/")
		p.Host = q.Get("host")
	case "http":
		p.Path = firstNonEmpty(q.Get("path"), "/")
		p.Host = q.Get("host")
	}
}

func applySecurity(p *Proxy, q url.Values) {
	security := strings.ToLower(q.Get("security"))
	p.TLS = security == "tls" || security == "reality" || security == "xtls"
	p.SNI = firstNonEmpty(q.Get("sni"), q.Get("peer"), q.Get("host"))
	p.ALPN = splitCSV(q.Get("alpn"))
	p.Fingerprint = q.Get("fp")
	p.AllowInsecure = isTrue(firstNonEmpty(q.Get("allowInsecure"), q.Get("insecure")))
	if security == "reality" {
		p.RealityPublicKey = q.Get("pbk")
		p.RealityShortID = q.Get("sid")
		// Reality pins the certificate itself; skip-cert-verify would be wrong.
		p.AllowInsecure = false
	}
}

func normalizeNetwork(net string) string {
	switch strings.ToLower(strings.TrimSpace(net)) {
	case "", "tcp", "raw":
		return "tcp"
	case "ws", "websocket":
		return "ws"
	case "grpc", "gun":
		return "grpc"
	case "h2", "http2":
		return "h2"
	case "http":
		return "http"
	case "httpupgrade":
		return "httpupgrade"
	case "xhttp", "splithttp":
		return "xhttp"
	default:
		return strings.ToLower(net)
	}
}

func hostPort(hostport string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, fmt.Errorf("bad host:port %q: %w", hostport, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("bad port %q", portStr)
	}
	return strings.Trim(host, "[]"), port, nil
}

func fragmentName(u *url.URL, fallback string) string {
	if u.Fragment != "" {
		return u.Fragment
	}
	return fallback
}

func decodeFragment(s string) string {
	if unescaped, err := url.PathUnescape(s); err == nil {
		return unescaped
	}
	return s
}

// decodeBase64 accepts every padding/alphabet combination panels emit.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if out, err := enc.DecodeString(s); err == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("not base64: %q", truncate(s))
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func truncate(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
