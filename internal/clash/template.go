package clash

// DefaultTemplate is the stock mihomo profile FireX renders when the admin has
// not supplied one. Rules use built-in GEOSITE/GEOIP databases so a client
// needs no external rule-provider fetch to come up.
//
// Tokens expanded at render time, inside proxy-groups only:
//
//	<ALL>            every node the user may use
//	<REGION_GROUPS>  as a group entry: one url-test group per region;
//	                 inside a proxies list: those groups' names
//	<REGION:name>    nodes whose region is exactly name
//	<TAG:name>       nodes carrying that tag
//	<FILTER:regexp>  nodes whose display name matches the Go regexp
const DefaultTemplate = `firex:
  region-group-type: url-test
  test-url: https://www.gstatic.com/generate_204
  interval: 300
  tolerance: 50

mixed-port: 7890
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

proxies: []

proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies:
      - ♻️ 自动选择
      - <REGION_GROUPS>
      - DIRECT
      - <ALL>

  - name: ♻️ 自动选择
    type: url-test
    url: https://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
    proxies:
      - <ALL>

  - <REGION_GROUPS>

  - name: 📺 国外媒体
    type: select
    proxies:
      - 🚀 节点选择
      - ♻️ 自动选择
      - <REGION_GROUPS>
      - DIRECT

  - name: 🤖 AI 服务
    type: select
    proxies:
      - 🚀 节点选择
      - <REGION_GROUPS>
      - DIRECT

  - name: 📱 电报消息
    type: select
    proxies:
      - 🚀 节点选择
      - <REGION_GROUPS>
      - DIRECT

  - name: Ⓜ️ 微软服务
    type: select
    proxies:
      - DIRECT
      - 🚀 节点选择

  - name: 🍎 苹果服务
    type: select
    proxies:
      - DIRECT
      - 🚀 节点选择

  - name: 🎯 全球直连
    type: select
    proxies:
      - DIRECT
      - 🚀 节点选择

  - name: 🛑 广告拦截
    type: select
    proxies:
      - REJECT
      - DIRECT

  - name: 🐟 漏网之鱼
    type: select
    proxies:
      - 🚀 节点选择
      - DIRECT
      - ♻️ 自动选择

rules:
  - GEOSITE,category-ads-all,🛑 广告拦截
  - GEOSITE,private,🎯 全球直连
  - GEOSITE,openai,🤖 AI 服务
  - GEOSITE,anthropic,🤖 AI 服务
  - GEOSITE,google-gemini,🤖 AI 服务
  - GEOSITE,telegram,📱 电报消息
  - GEOSITE,youtube,📺 国外媒体
  - GEOSITE,netflix,📺 国外媒体
  - GEOSITE,disney,📺 国外媒体
  - GEOSITE,spotify,📺 国外媒体
  - GEOSITE,bilibili,🎯 全球直连
  - GEOSITE,apple-cn,🍎 苹果服务
  - GEOSITE,microsoft@cn,Ⓜ️ 微软服务
  - GEOSITE,geolocation-!cn,🚀 节点选择
  - GEOSITE,cn,🎯 全球直连
  - GEOIP,telegram,📱 电报消息,no-resolve
  - GEOIP,private,🎯 全球直连,no-resolve
  - GEOIP,cn,🎯 全球直连
  - MATCH,🐟 漏网之鱼
`
