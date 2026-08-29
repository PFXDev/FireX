package clash

import (
	"strings"

	"github.com/PFXDev/FireX/internal/sharelink"
)

// ProxyEntry renders a parsed share link as a mihomo proxy mapping. The second
// return is false when mihomo has no equivalent for the proxy's transport, in
// which case the caller must omit it rather than emit a broken entry.
func ProxyEntry(p *sharelink.Proxy) (*Ordered, bool) {
	o := NewOrdered()
	o.Set("name", p.Name)
	o.Set("type", p.Type)
	o.Set("server", p.Server)
	o.Set("port", p.Port)

	switch p.Type {
	case "vless":
		if !clashNetwork(p.Network) {
			return nil, false
		}
		o.Set("uuid", p.UUID)
		o.Set("udp", p.UDP)
		if p.Flow != "" {
			o.Set("flow", p.Flow)
		}
		applyTLS(o, p)
		applyTransport(o, p)
	case "vmess":
		if !clashNetwork(p.Network) {
			return nil, false
		}
		o.Set("uuid", p.UUID)
		o.Set("alterId", p.AlterID)
		o.Set("cipher", orDefault(p.Cipher, "auto"))
		o.Set("udp", p.UDP)
		applyTLS(o, p)
		applyTransport(o, p)
	case "trojan":
		if !clashNetwork(p.Network) {
			return nil, false
		}
		o.Set("password", p.Password)
		o.Set("udp", p.UDP)
		if sni := orDefault(p.SNI, p.Server); sni != "" {
			o.Set("sni", sni)
		}
		if len(p.ALPN) > 0 {
			o.Set("alpn", p.ALPN)
		}
		if p.Fingerprint != "" {
			o.Set("client-fingerprint", p.Fingerprint)
		}
		if p.AllowInsecure {
			o.Set("skip-cert-verify", true)
		}
		applyTransport(o, p)
	case "ss":
		// A plugin (obfs / v2ray-plugin) needs its own option block; emitting
		// the proxy without it would silently produce a non-working entry.
		if p.Obfs != "" {
			return nil, false
		}
		o.Set("cipher", p.Cipher)
		o.Set("password", p.Password)
		o.Set("udp", p.UDP)
	case "hysteria2":
		o.Set("password", p.Password)
		if sni := orDefault(p.SNI, p.Server); sni != "" {
			o.Set("sni", sni)
		}
		if len(p.ALPN) > 0 {
			o.Set("alpn", p.ALPN)
		}
		if p.Obfs != "" {
			o.Set("obfs", p.Obfs)
			o.Set("obfs-password", p.ObfsPassword)
		}
		if p.AllowInsecure {
			o.Set("skip-cert-verify", true)
		}
	case "tuic":
		o.Set("uuid", p.UUID)
		o.Set("password", p.Password)
		if sni := orDefault(p.SNI, p.Server); sni != "" {
			o.Set("sni", sni)
		}
		if len(p.ALPN) > 0 {
			o.Set("alpn", p.ALPN)
		}
		o.Set("congestion-controller", "bbr")
		o.Set("udp-relay-mode", "native")
		if p.AllowInsecure {
			o.Set("skip-cert-verify", true)
		}
	default:
		return nil, false
	}
	return o, true
}

func applyTLS(o *Ordered, p *sharelink.Proxy) {
	if !p.TLS {
		return
	}
	o.Set("tls", true)
	if sni := orDefault(p.SNI, p.Server); sni != "" {
		o.Set("servername", sni)
	}
	if len(p.ALPN) > 0 {
		o.Set("alpn", p.ALPN)
	}
	if p.Fingerprint != "" {
		o.Set("client-fingerprint", p.Fingerprint)
	}
	if p.RealityPublicKey != "" {
		reality := NewOrdered()
		reality.Set("public-key", p.RealityPublicKey)
		if p.RealityShortID != "" {
			reality.Set("short-id", p.RealityShortID)
		}
		o.Set("reality-opts", reality)
	}
	if p.AllowInsecure {
		o.Set("skip-cert-verify", true)
	}
}

func applyTransport(o *Ordered, p *sharelink.Proxy) {
	network := p.Network
	if network == "" {
		network = "tcp"
	}
	o.Set("network", network)
	switch network {
	case "ws", "httpupgrade":
		opts := NewOrdered()
		opts.Set("path", orDefault(p.Path, "/"))
		if p.Host != "" {
			headers := NewOrdered()
			headers.Set("Host", p.Host)
			opts.Set("headers", headers)
		}
		o.Set(network+"-opts", opts)
	case "grpc":
		opts := NewOrdered()
		opts.Set("grpc-service-name", p.GRPCServiceName)
		o.Set("grpc-opts", opts)
	case "h2":
		opts := NewOrdered()
		if p.Host != "" {
			opts.Set("host", splitHosts(p.Host))
		}
		opts.Set("path", orDefault(p.Path, "/"))
		o.Set("h2-opts", opts)
	case "http":
		opts := NewOrdered()
		if p.Host != "" {
			opts.Set("headers", map[string][]string{"Host": splitHosts(p.Host)})
		}
		opts.Set("path", []string{orDefault(p.Path, "/")})
		o.Set("http-opts", opts)
	}
}

// clashNetwork reports whether mihomo can carry this transport. xhttp and any
// unknown transport have no mapping.
func clashNetwork(network string) bool {
	switch network {
	case "", "tcp", "ws", "grpc", "h2", "http", "httpupgrade":
		return true
	default:
		return false
	}
}

func splitHosts(host string) []string {
	parts := strings.Split(host, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
