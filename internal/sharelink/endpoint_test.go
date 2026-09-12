package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestWithEndpointRewritesVLESSAuthorityOnly(t *testing.T) {
	raw := "vless://uuid-1@127.0.0.1:9443?type=tcp&security=reality&pbk=K&sid=ab&sni=irsa.ipac.caltech.edu&path=%2Fa%2Fb#HK%2001"
	got := WithEndpoint(raw, "edge.example.com", 443)
	want := "vless://uuid-1@edge.example.com:443?type=tcp&security=reality&pbk=K&sid=ab&sni=irsa.ipac.caltech.edu&path=%2Fa%2Fb#HK%2001"
	if got != want {
		t.Fatalf("WithEndpoint() =\n%s\nwant\n%s", got, want)
	}
	p, err := Parse(got)
	if err != nil {
		t.Fatalf("rewritten link no longer parses: %v", err)
	}
	if p.Server != "edge.example.com" || p.Port != 443 {
		t.Errorf("endpoint = %s:%d, want edge.example.com:443", p.Server, p.Port)
	}
	if p.SNI != "irsa.ipac.caltech.edu" || p.RealityPublicKey != "K" {
		t.Errorf("reality params changed: sni=%q pbk=%q", p.SNI, p.RealityPublicKey)
	}
}

func TestWithEndpointHalves(t *testing.T) {
	raw := "trojan://pw@10.0.0.1:8443?security=tls#n"
	if got := WithEndpoint(raw, "", 443); got != "trojan://pw@10.0.0.1:443?security=tls#n" {
		t.Errorf("port only = %q", got)
	}
	if got := WithEndpoint(raw, "edge.example.com", 0); got != "trojan://pw@edge.example.com:8443?security=tls#n" {
		t.Errorf("server only = %q", got)
	}
	if got := WithEndpoint(raw, "", 0); got != raw {
		t.Errorf("no override must be a no-op, got %q", got)
	}
}

func TestWithEndpointBracketsIPv6(t *testing.T) {
	got := WithEndpoint("hysteria2://pw@h.example:443?sni=h.example#n", "2001:db8::1", 8443)
	if got != "hysteria2://pw@[2001:db8::1]:8443?sni=h.example#n" {
		t.Errorf("got %q", got)
	}
	if _, err := Parse(got); err != nil {
		t.Errorf("rewritten link no longer parses: %v", err)
	}
}

func TestWithEndpointVMess(t *testing.T) {
	payload := `{"v":"2","ps":"JP","add":"jp.example.com","port":443,"id":"vmess-uuid","aid":0,"net":"ws","tls":"tls"}`
	raw := "vmess://" + base64.StdEncoding.EncodeToString([]byte(payload))
	got := WithEndpoint(raw, "cdn.example.com", 2053)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "vmess://"))
	if err != nil {
		t.Fatalf("output is not base64: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded, &obj); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if obj["add"] != "cdn.example.com" || obj["port"] != float64(2053) {
		t.Errorf("add/port = %v/%v", obj["add"], obj["port"])
	}
	if obj["ps"] != "JP" || obj["id"] != "vmess-uuid" {
		t.Errorf("unrelated fields changed: %v", obj)
	}
	p, err := Parse(got)
	if err != nil {
		t.Fatalf("rewritten link no longer parses: %v", err)
	}
	if p.Server != "cdn.example.com" || p.Port != 2053 {
		t.Errorf("endpoint = %s:%d", p.Server, p.Port)
	}
}

func TestWithEndpointLegacyShadowsocks(t *testing.T) {
	raw := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret@10.0.0.1:8388")) + "#SS"
	got := WithEndpoint(raw, "ss.example.com", 443)
	p, err := Parse(got)
	if err != nil {
		t.Fatalf("rewritten link no longer parses: %v", err)
	}
	if p.Server != "ss.example.com" || p.Port != 443 {
		t.Errorf("endpoint = %s:%d", p.Server, p.Port)
	}
	if p.Cipher != "aes-256-gcm" || p.Password != "secret" || p.Name != "SS" {
		t.Errorf("credentials or name changed: %+v", p)
	}
}

func TestWithEndpointLeavesUnparsableLinksAlone(t *testing.T) {
	for _, raw := range []string{"not a link", "vless://uuid@nohostport?x=1", "vmess://!!!"} {
		if got := WithEndpoint(raw, "edge.example.com", 443); got != raw {
			t.Errorf("WithEndpoint(%q) = %q, want unchanged", raw, got)
		}
	}
}
