package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"strconv"
	"strings"
)

// WithEndpoint rewrites the server and port a share link points at, leaving
// every other byte of the URI alone. An empty server or a non-positive port
// keeps the link's own value for that half. The endpoint lives in the
// authority for every scheme except vmess, which keeps it inside its base64
// JSON. A link that cannot be rewritten is returned unchanged, so a bad
// override degrades to the panel's endpoint rather than to a broken entry.
func WithEndpoint(raw, server string, port int) string {
	raw = strings.TrimSpace(raw)
	if server == "" && port <= 0 {
		return raw
	}
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return raw
	}
	switch strings.ToLower(scheme) {
	case "vmess":
		return rewriteVMessEndpoint(raw, server, port)
	case "ss":
		if !strings.ContainsAny(authorityOf(rest), "@") {
			return rewriteLegacySSEndpoint(scheme, rest, server, port)
		}
	}
	return rewriteAuthorityEndpoint(scheme, rest, server, port)
}

// authorityOf is everything after the scheme up to the first path, query or
// fragment delimiter.
func authorityOf(rest string) string {
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// rewriteAuthorityEndpoint handles the URL-shaped schemes. The authority is
// spliced by hand instead of round-tripping through net/url so percent-encoded
// query values and the fragment come back exactly as the panel emitted them.
func rewriteAuthorityEndpoint(scheme, rest, server string, port int) string {
	authority := authorityOf(rest)
	tail := rest[len(authority):]

	userinfo := ""
	hostport := authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userinfo, hostport = authority[:at+1], authority[at+1:]
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return scheme + "://" + rest
	}
	host = strings.Trim(host, "[]")
	if server != "" {
		host = server
	}
	if port > 0 {
		portStr = strconv.Itoa(port)
	}
	return scheme + "://" + userinfo + net.JoinHostPort(host, portStr) + tail
}

// rewriteLegacySSEndpoint handles ss://base64(method:password@host:port). The
// whole authority is one opaque blob, so it is decoded, spliced and re-encoded.
func rewriteLegacySSEndpoint(scheme, rest, server string, port int) string {
	body := authorityOf(rest)
	tail := rest[len(body):]
	decoded, err := decodeBase64(body)
	if err != nil {
		return scheme + "://" + rest
	}
	at := strings.LastIndexByte(string(decoded), '@')
	if at < 0 {
		return scheme + "://" + rest
	}
	userinfo, hostport := string(decoded)[:at+1], string(decoded)[at+1:]
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return scheme + "://" + rest
	}
	host = strings.Trim(host, "[]")
	if server != "" {
		host = server
	}
	if port > 0 {
		portStr = strconv.Itoa(port)
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(userinfo + net.JoinHostPort(host, portStr)))
	return scheme + "://" + encoded + tail
}

func rewriteVMessEndpoint(raw, server string, port int) string {
	payload := raw[len("vmess://"):]
	if i := strings.IndexByte(payload, '#'); i >= 0 {
		payload = payload[:i]
	}
	decoded, err := decodeBase64(payload)
	if err != nil {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded, &obj); err != nil {
		return raw
	}
	if server != "" {
		obj["add"] = server
	}
	if port > 0 {
		obj["port"] = port
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(encoded)
}
