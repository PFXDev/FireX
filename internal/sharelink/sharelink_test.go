package sharelink

import (
	"encoding/base64"
	"testing"
)

func TestParseVLESSReality(t *testing.T) {
	raw := "vless://f0b1d8e2-1111-2222-3333-444455556666@node.example.com:443" +
		"?type=tcp&security=reality&pbk=PUBKEY123&sid=ab12&fp=chrome&sni=www.microsoft.com" +
		"&flow=xtls-rprx-vision#HK-01"

	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Type != "vless" {
		t.Errorf("Type = %q, want vless", p.Type)
	}
	if p.Server != "node.example.com" || p.Port != 443 {
		t.Errorf("endpoint = %s:%d, want node.example.com:443", p.Server, p.Port)
	}
	if p.UUID != "f0b1d8e2-1111-2222-3333-444455556666" {
		t.Errorf("UUID = %q", p.UUID)
	}
	if p.Name != "HK-01" {
		t.Errorf("Name = %q, want HK-01", p.Name)
	}
	if !p.TLS {
		t.Error("TLS = false, want true for reality")
	}
	if p.RealityPublicKey != "PUBKEY123" || p.RealityShortID != "ab12" {
		t.Errorf("reality = %q/%q, want PUBKEY123/ab12", p.RealityPublicKey, p.RealityShortID)
	}
	if p.SNI != "www.microsoft.com" {
		t.Errorf("SNI = %q", p.SNI)
	}
	if p.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q", p.Flow)
	}
	if p.Network != "tcp" {
		t.Errorf("Network = %q, want tcp", p.Network)
	}
}

func TestParseVLESSRealityIgnoresAllowInsecure(t *testing.T) {
	// Reality pins the server key itself; honouring allowInsecure here would
	// emit skip-cert-verify and weaken a connection that does not need it.
	p, err := Parse("vless://uuid-1@h.example:443?security=reality&pbk=K&allowInsecure=1#n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.AllowInsecure {
		t.Error("AllowInsecure = true, want false for reality")
	}
}

func TestParseVLESSWebSocket(t *testing.T) {
	raw := "vless://uuid-2@cdn.example.com:8443?type=ws&security=tls&path=%2Fray&host=front.example.com&sni=front.example.com#WS%20Node"
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Network != "ws" {
		t.Fatalf("Network = %q, want ws", p.Network)
	}
	if p.Path != "/ray" {
		t.Errorf("Path = %q, want /ray", p.Path)
	}
	if p.Host != "front.example.com" {
		t.Errorf("Host = %q", p.Host)
	}
	if p.Name != "WS Node" {
		t.Errorf("Name = %q, want %q", p.Name, "WS Node")
	}
}

func TestParseVMess(t *testing.T) {
	payload := `{"v":"2","ps":"JP-Tokyo","add":"jp.example.com","port":"443","id":"vmess-uuid",` +
		`"aid":"0","scy":"auto","net":"ws","type":"none","host":"jp.example.com","path":"/vm","tls":"tls","sni":"jp.example.com"}`
	raw := "vmess://" + base64.StdEncoding.EncodeToString([]byte(payload))

	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Name != "JP-Tokyo" || p.Server != "jp.example.com" || p.Port != 443 {
		t.Errorf("got %q %s:%d", p.Name, p.Server, p.Port)
	}
	if p.UUID != "vmess-uuid" || p.AlterID != 0 || p.Cipher != "auto" {
		t.Errorf("creds = %q/%d/%q", p.UUID, p.AlterID, p.Cipher)
	}
	if p.Network != "ws" || p.Path != "/vm" || !p.TLS {
		t.Errorf("transport = %q %q tls=%v", p.Network, p.Path, p.TLS)
	}
}

func TestParseVMessNumericPort(t *testing.T) {
	// v2rayN writes port as a JSON number about as often as a string.
	payload := `{"ps":"n","add":"h.example","port":8080,"id":"u","aid":0,"net":"tcp","tls":""}`
	p, err := Parse("vmess://" + base64.RawStdEncoding.EncodeToString([]byte(payload)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Port != 8080 {
		t.Errorf("Port = %d, want 8080", p.Port)
	}
}

func TestParseTrojanGRPC(t *testing.T) {
	raw := "trojan://s3cret@tj.example.com:443?type=grpc&serviceName=grpcsvc&sni=tj.example.com#TJ"
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Password != "s3cret" {
		t.Errorf("Password = %q", p.Password)
	}
	if p.Network != "grpc" || p.GRPCServiceName != "grpcsvc" {
		t.Errorf("transport = %q/%q", p.Network, p.GRPCServiceName)
	}
	if !p.TLS {
		t.Error("TLS = false, want true (trojan is always TLS)")
	}
}

func TestParseShadowsocksSIP002(t *testing.T) {
	userInfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pa55w0rd"))
	p, err := Parse("ss://" + userInfo + "@ss.example.com:8388#SS%20Node")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Cipher != "aes-256-gcm" || p.Password != "pa55w0rd" {
		t.Errorf("creds = %q/%q", p.Cipher, p.Password)
	}
	if p.Server != "ss.example.com" || p.Port != 8388 {
		t.Errorf("endpoint = %s:%d", p.Server, p.Port)
	}
	if p.Name != "SS Node" {
		t.Errorf("Name = %q", p.Name)
	}
}

func TestParseShadowsocksLegacy(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw@ss.example.com:9000"))
	p, err := Parse("ss://" + body + "#Legacy")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Cipher != "chacha20-ietf-poly1305" || p.Password != "pw" || p.Port != 9000 {
		t.Errorf("got %q/%q:%d", p.Cipher, p.Password, p.Port)
	}
}

func TestParseHysteria2(t *testing.T) {
	raw := "hysteria2://authpass@hy.example.com:8443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=obfspw#HY2"
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Password != "authpass" {
		t.Errorf("Password = %q", p.Password)
	}
	if !p.AllowInsecure {
		t.Error("AllowInsecure = false, want true")
	}
	if p.Obfs != "salamander" || p.ObfsPassword != "obfspw" {
		t.Errorf("obfs = %q/%q", p.Obfs, p.ObfsPassword)
	}
}

func TestParseUnsupportedScheme(t *testing.T) {
	if _, err := Parse("socks5://user@h:1080"); err == nil {
		t.Fatal("Parse() error = nil, want unsupported scheme error")
	}
}

func TestParseManyKeepsGoodLinks(t *testing.T) {
	// A single malformed link must not cost the user their whole subscription.
	parsed, errs := ParseMany([]string{
		"vless://u1@a.example:443?security=tls#A",
		"garbage",
		"vless://u2@b.example:443?security=tls#B",
	})
	if len(parsed) != 2 {
		t.Fatalf("len(parsed) = %d, want 2", len(parsed))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if parsed[0].Proxy.Name != "A" || parsed[1].Proxy.Name != "B" {
		t.Errorf("names = %q,%q", parsed[0].Proxy.Name, parsed[1].Proxy.Name)
	}
	// Raw must still line up with its proxy after the bad link was skipped.
	if parsed[1].Raw != "vless://u2@b.example:443?security=tls#B" {
		t.Errorf("Raw = %q", parsed[1].Raw)
	}
}
