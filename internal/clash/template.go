package clash

// TemplatePreset is one built-in base config an admin can start from. Presets
// only ever seed the editor: what is served is whatever was saved, or
// DefaultTemplate when nothing was.
type TemplatePreset struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
}

// DefaultTemplate is the stock mihomo profile FireX renders when the admin has
// not supplied one. It carries base settings only — ports, DNS, sniffer. The
// `proxies`, `proxy-groups` and `rules` keys are always generated from the
// routing matrix and appended here, so anything a template puts under those
// names is overwritten.
//
// The DNS layout is the one the stock routing presets are written against:
//
//   - fake-ip for everything but mainland domains, LAN and connectivity
//     probes, so a proxied domain that matches a domain rule reaches the exit
//     unresolved. The exit resolves it, which is both leak-free and gives the
//     CDN answer nearest the exit.
//   - mainland domains resolve through AliDNS DoH and bypass fake-ip. The
//     default resolver also uses AliDNS, so a trailing GEOIP,cn check never
//     depends on an overseas resolver being reachable from the client.
//   - DIRECT dials use the domain policy first. Other domains resolve through
//     Cloudflare DoH via the main proxy selection, while proxied connections
//     still send their domain to the exit for remote resolution.
//   - LAN-only suffixes (.lan, .home.arpa and the like) ask the system
//     resolver, normally the LAN router, since no public resolver knows them.
//     DIRECT dials follow the same policy, so they reach the router too. The
//     rest of geosite:private (router login hosts, localhost.ptlogin2.qq.com)
//     resolves publicly and stays on AliDNS.
//   - every remote upstream is an IP literal, so no bootstrap lookup can
//     leak.
//
// IPv6 is on: domestic traffic leaves through DIRECT, which races both
// families for TCP, or through the opt-in DIRECT-V6 (ipv6-prefer). Proxied
// domains have no AAAA on the client and the exit decides — IPv4 first when
// the panel's freedom outbound uses UseIPv4v6. On a network without IPv6 the
// IPv6 attempts fail at once and everything runs over IPv4.
const DefaultTemplate = dualStackTemplate

// TemplatePresets lists the built-in base configs, the default first.
var TemplatePresets = []TemplatePreset{
	{
		Key:         "dual-stack",
		Name:        "双栈（推荐）",
		Description: "开启 IPv6：境内直连双栈择快（可切 DIRECT-V6 强制优先 IPv6），境外域名交给节点解析；DNS 境内阿里 DoH、境外 Cloudflare DoH 走代理。没有 IPv6 的网络也能正常使用。",
		Template:    dualStackTemplate,
	},
	{
		Key:         "ipv4-only",
		Name:        "仅 IPv4",
		Description: "关闭 IPv6，DNS 分流与双栈版相同。适合本地没有 IPv6 或 IPv6 质量很差的网络。",
		Template:    ipv4OnlyTemplate,
	},
}

const dualStackTemplate = templateHead + `ipv6: true
` + templateBody + `dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: true
` + templateDNS

const ipv4OnlyTemplate = templateHead + `ipv6: false
` + templateBody + `dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
` + templateDNS

const templateHead = `mixed-port: 7890
allow-lan: false
bind-address: '*'
mode: rule
log-level: info
`

const templateBody = `external-controller: 127.0.0.1:9090
unified-delay: true
tcp-concurrent: true
find-process-mode: strict

geodata-mode: true
geo-auto-update: true
geo-update-interval: 24
geox-url:
  geoip: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat
  geosite: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat
  mmdb: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country-lite.mmdb
  asn: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/GeoLite2-ASN.mmdb

profile:
  store-selected: true
  store-fake-ip: true

sniffer:
  enable: true
  force-dns-mapping: true
  parse-pure-ip: true
  override-destination: false
  sniff:
    HTTP:
      ports: [80, 8080-8880]
      override-destination: true
    TLS:
      ports: [443, 8443]
    QUIC:
      ports: [443, 8443]
  skip-domain:
    - 'Mijia Cloud'
    - '+.push.apple.com'

`

const templateDNS = `  prefer-h3: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter-mode: blacklist
  fake-ip-filter:
    - 'geosite:private'
    - 'geosite:cn'
    - '+.lan'
    - '+.local'
    - '+.home.arpa'
    - '+.msftconnecttest.com'
    - '+.msftncsi.com'
    - 'time.*.com'
    - 'ntp.*.com'
    - '+.pool.ntp.org'
    - 'stun.*.*'
    - 'stun.*.*.*'
    - '+.stun.*.*'
    - '+.srv.nintendo.net'
    - '+.stun.playstation.net'
    - 'xbox.*.microsoft.com'
    - '+.xboxlive.com'
    - localhost.ptlogin2.qq.com
  respect-rules: true
  default-nameserver:
    - 223.5.5.5
    - 223.6.6.6
  proxy-server-nameserver:
    - https://223.5.5.5/dns-query
    - https://223.6.6.6/dns-query
  direct-nameserver:
    - 'https://1.1.1.1/dns-query#🚀 节点选择'
    - 'https://1.0.0.1/dns-query#🚀 节点选择'
  direct-nameserver-follow-policy: true
  nameserver-policy:
    '+.lan,+.local,+.home.arpa,+.internal,+.localdomain':
      - system
    'geosite:cn,private':
      - https://223.5.5.5/dns-query
      - https://223.6.6.6/dns-query
  nameserver:
    - https://223.5.5.5/dns-query
    - https://223.6.6.6/dns-query
`
