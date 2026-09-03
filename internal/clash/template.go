package clash

// DefaultTemplate is the stock mihomo profile FireX renders when the admin has
// not supplied one. It carries base settings only — ports, DNS, sniffer. The
// `proxies`, `proxy-groups` and `rules` keys are always generated from the
// routing matrix and appended here, so anything a template puts under those
// names is overwritten.
const DefaultTemplate = `mixed-port: 7890
allow-lan: false
bind-address: '*'
mode: rule
log-level: info
ipv6: false
external-controller: 127.0.0.1:9090
unified-delay: true
tcp-concurrent: true
find-process-mode: strict
global-client-fingerprint: chrome

profile:
  store-selected: true
  store-fake-ip: true

sniffer:
  enable: true
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

dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: false
  prefer-h3: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter:
    - '*.lan'
    - '*.local'
    - '+.msftconnecttest.com'
    - '+.msftncsi.com'
    - localhost.ptlogin2.qq.com
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://doh.pub/dns-query
  fallback:
    - https://1.1.1.1/dns-query
    - https://dns.google/dns-query
  fallback-filter:
    geoip: true
    geoip-code: CN
    ipcidr:
      - 240.0.0.0/4
`
