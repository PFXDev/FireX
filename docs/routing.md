# Stock routing, DNS and IPv6

This is the reasoning behind FireX's built-in routing presets
(`internal/routing/seed.go`) and template presets (`internal/clash/template.go`),
plus the panel-side settings that complete them. The README has the short
version.

## Goals

- **Bypass every mainland address.** Mainland traffic should leave directly,
  including mainland sites that no list names.
- **Handle IPv6 properly.** Domestic traffic may use IPv6, and can be told to
  prefer it. Foreign traffic should prefer IPv4. A network without IPv6 must
  lose nothing.
- **Keep DNS split and leak-free.** Mainland names should resolve through AliDNS
  DoH. Foreign names should be resolved by the exit, or failing that by
  Cloudflare DoH over the proxy.
- **Give OpenAI and Claude their own rows**, so each can be pinned to a region
  on its own.
- **Separate Google, Steam and overseas bulk downloads.** File/model/package
  sources get an independent proxy switch; game CDNs default to DIRECT.
  Google and Steam's storefront/community have their own selectable exits.
- **Stay small.** Built-in GEOSITE/GEOIP data, no third-party rule-providers,
  and sixteen rows in the standard preset. The one list that changes often,
  大流量下载, is a rule set FireX serves itself.

## How a connection is decided

```
app ──DNS──▶ mihomo (fake-ip)            every name gets 198.18.x.x, no AAAA
app ──TCP──▶ 198.18.x.x ─▶ mihomo maps it back to the domain, then the rules:

  广告拦截                                           GEOSITE list
  大流量下载   RULE-SET,大流量下载 (served by FireX)   → proxy
  OpenAI / Claude / AI 服务 / 电报 / 媒体              GEOSITE lists
  谷歌服务     GEOSITE,google / GEOIP,google,no-resolve → proxy
  游戏下载     game download CDNs                     → DIRECT
  Steam / 苹果                                       GEOSITE lists
  国外网站     GEOSITE,geolocation-!cn            → proxy, domain sent as is
  全球直连     GEOSITE,private / GEOSITE,cn        → DIRECT (or DIRECT-V6)
               GEOIP,private,no-resolve
               GEOIP,cn   (resolves: Cloudflare DoH via proxy)
  漏网之鱼     MATCH                               → proxy
```

- A **proxied** domain is never resolved on the client. The exit node resolves
  it, so nothing leaks to a local resolver, and the CDN answer is the one
  nearest the exit. That is why `enhanced-mode` is pinned to `fake-ip` at
  render time (`forceFakeIP` in `internal/clash/render.go`).
- A **direct** domain is resolved by `direct-nameserver` (AliDNS DoH) when the
  connection is dialled. The default, builtin `DIRECT`, uses `dual`. For TCP
  it dials both families at once and takes whichever connects first; for UDP
  it picks an IPv4 address. The opt-in `DIRECT-V6` (`ipv6-prefer`) gives IPv6
  TCP a 300 ms head start before falling back to IPv4
  (`reference/mihomo/component/dialer/dialer.go`, `dualStackDialContext`).
  Its UDP, however, always takes an IPv6 address when one exists
  (`ResolveIPPrefer6WithResolver` in
  `reference/mihomo/component/resolver/resolver.go`).
- **Unlisted domains** reach the resolving `GEOIP,cn` after both
  `geolocation-!cn` and `cn` have missed. They are looked up with the default
  `nameserver`, which is Cloudflare DoH. `respect-rules` routes that query
  through the proxy, so it never reaches a domestic resolver.
- **Node addresses** resolve through `proxy-server-nameserver`, which is AliDNS
  DoH dialled directly. This is also required whenever `respect-rules` is on.
  Generated nodes use `ip-version: ipv6-prefer`: TCP gives IPv6 up to 300 ms
  to connect before accepting IPv4. A literal IP endpoint keeps using that
  address, and the `ipv4-only` template still disables IPv6 DNS resolution.
  UDP-based transports follow mihomo's address selection; an IPv6 handshake
  failure does not guarantee IPv4 fallback.

- **LAN names** (`+.lan`, `+.local`, `+.home.arpa`, `+.internal`,
  `+.localdomain`) are exempt from fake-ip and resolve through `system`, the
  resolver in the OS configuration, which is normally the LAN router. No
  public resolver knows `nas.lan`, so AliDNS would answer NXDOMAIN. mihomo
  skips its own TUN DNS address when it reads the system list
  (`reference/mihomo/dns/system_common.go`), so this cannot loop back. When
  the OS resolver is something else (a GUI that points system DNS at the TUN,
  or another VPN), the lookup fails just as it did on AliDNS, so nothing
  regresses. The rest of `geosite:private`, such as router login hosts and
  `localhost.ptlogin2.qq.com`, resolves publicly and stays on AliDNS.
  `direct-nameserver-follow-policy` makes DIRECT dials use the same policy,
  so a LAN name that reaches DIRECT unresolved, for example through the mixed
  port, also asks the router. Other names still dial through
  `direct-nameserver`.

Every remote upstream is an IP literal (`https://223.5.5.5/dns-query`,
`https://1.1.1.1/dns-query`), so no DoH hostname ever needs a bootstrap lookup.
`fake-ip-filter` only exempts names that must resolve for real: LAN suffixes,
NTP, STUN, console NAT tests and OS connectivity probes. The DNS listener is
bound to `127.0.0.1`; TUN's DNS hijack does not need it, and `allow-lan` is
off.

## The rows

| Row | Rules | Default egress | Why |
| --- | ----- | -------------- | --- |
| 🚀 节点选择 | — | 自动选择, node groups, inbounds, DIRECT | Manual switch every other row falls back to |
| ♻️ 自动选择 | — | url-test over every inbound | |
| 🛑 广告拦截 | `category-ads-all` | REJECT | Before anything that could route the same name elsewhere |
| 📥 大流量下载 | Rule set `大流量下载`, described below | 节点选择 | Before OpenAI, AI and Google; choose a high-bandwidth or low-cost proxy independently |
| 🤖 OpenAI | `openai` | node groups first | Region-locked; keep it on a fixed exit |
| ✳️ Claude | `anthropic` | node groups first | Same, and stricter about IP changes |
| ✨ AI 服务 | `category-ai-!cn` | 节点选择 | Gemini, Copilot, Perplexity and the rest |
| 📱 电报消息 | `telegram`, GEOIP `telegram` | 节点选择 | Telegram dials IP literals |
| 📺 国外媒体 | YouTube, Netflix, Disney+, HBO, Prime Video, Spotify, TikTok | 节点选择 | One switch for unlock regions |
| 🔎 谷歌服务 | `google`, GEOIP `google` with `no-resolve` | 节点选择 | Google Search, FCM and other services; earlier download/AI/media rows keep Drive/Gemini/YouTube |
| 🕹️ 游戏下载 | `category-game-platforms-download`, Steam CDN suffixes | DIRECT | Before Steam, keeping game downloads off the overseas bulk proxy |
| 🎮 Steam | `steam` | 节点选择 | Store, community and login; DIRECT is also offered |
| 🍎 苹果服务 | `apple` | DIRECT | Apple's mainland CDN is fast; flip it for foreign storefronts |
| 🌐 国外网站 | `geolocation-!cn` | 节点选择 | Catch-all for known foreign names |
| 🎯 全球直连 | `private`, `cn`, GEOIP `private`, GEOIP `cn` | DIRECT, DIRECT-V6 offered | Mainland catch-all, ending on a resolving IP check |
| 🐟 漏网之鱼 | MATCH | 节点选择 | |

The `lite` preset keeps 节点选择, 自动选择, OpenAI, Claude, 国外网站 (with
Telegram folded in), 全球直连 and 漏网之鱼.

大流量下载 is for large downloads from sources that are blocked or slow when
reached directly. Every foreign name already goes through the proxy by way of
国外网站, so this row exists only to put those downloads on a different exit,
such as a high-bandwidth or low-cost node. Its hosts live in the 大流量下载
rule set, which can be edited under **Routing → 规则集**. The stock list
prefers upstream GEOSITE lists and names hosts only where no list is narrow
enough:

| Source | Rule |
| ------ | ---- |
| Hugging Face | `GEOSITE,huggingface` |
| GitHub releases, raw files, source archives | `DOMAIN-SUFFIX,githubusercontent.com`, `DOMAIN,codeload.github.com` (`GEOSITE,github` would also take the main site, Copilot and npm) |
| Google Drive downloads | `drive.usercontent.google.com`, `doc-*-docs.googleusercontent.com` |
| MEGA | `GEOSITE,mega` |
| ChatGPT installers | `DOMAIN,persistent.oaistatic.com`, ahead of OpenAI's list |
| Debian, Ubuntu, Arch and AUR | `GEOSITE,debian`, `GEOSITE,ubuntu`, `GEOSITE,archlinux` |

These rules match hosts, not response sizes, so a host such as
`huggingface.co` sends its web pages through this exit as well. Domestic
mirrors such as TUNA or USTC are not on the list and keep going direct, but
`cn.archive.ubuntu.com` and `archlinuxcn.org` fall under the GEOSITE lists.
Game CDNs go to 游戏下载 instead: Steam, Epic, Battle.net and the other
platforms in the built-in list, plus explicit `steamcontent.com` and
`steamcdn-a.akamaihd.net` suffixes for new Steam CDN hosts.

### Rule sets

A rule set is a named list of classical rule lines without a target, such as
`DOMAIN-SUFFIX,hf.co`. A policy uses one through a `RULE-SET,<name>` rule. The
subscription then declares it under `rule-providers` as an `http`,
`classical`, `text` provider at `/sub/<token>/rules/<id>`, and only for sets
the rendered rules actually reference. Clients fetch it every six hours, so
list edits arrive without a subscription refresh.

- Rule sets are saved with the matrix. Renaming one rewrites the RULE-SET
  rules that name it, and validation rejects a rule that points at a missing
  set, a line that carries a target, and a nested `RULE-SET`, `SUB-RULE` or
  `MATCH`.
- Applying a preset keeps existing rule sets and only adds the ones it
  references that do not exist yet. An edited list survives a preset swap.
- A RULE-SET rule may also name a provider the Clash template declares
  itself. The template then cannot drop that provider while a rule still
  uses it.

The AI rows list node groups before 节点选择 so that a client's first pick is
a named region rather than whatever the manual switch happens to hold. Set a
profile override when the first node group is in a region these services
refuse, such as Hong Kong or the mainland.

### Without IPv6

| Path | On a network without IPv6 |
| ---- | ------------------------- |
| App DNS | fake-ip returns IPv4 placeholders and no AAAA, so apps never try IPv6 |
| Proxied traffic | The domain goes to the exit unresolved, so the client's IPv6 is irrelevant |
| DNS upstreams, node dialling | IPv4 literals; node names use `dual` and fall back to IPv4 |
| `DIRECT` | IPv6 attempts fail at once with "network unreachable", so there is no added delay; UDP uses IPv4 |
| `DIRECT-V6` | TCP falls back immediately. UDP to a name with an AAAA record is sent over IPv6 and fails: QUIC drops to TCP, and some UDP apps break |
| Broken IPv6 (address but no route) | `DIRECT` is unaffected; `DIRECT-V6` adds up to 300 ms per new TCP connection |

`DIRECT` is therefore the default. `DIRECT-V6` suits a client that sits on
known-good IPv6. The `ipv4-only` template removes IPv6 for the whole install.

Microsoft has no row on purpose. Its `geosite:microsoft` list includes GitHub,
which would then go direct. Microsoft's mainland names are already in
`geosite:cn`.

## Panel-side settings (3x-ui → Xray 配置)

FireX owns inbounds, not the panel's outbound and DNS configuration, so these
settings are applied by hand on each panel. They complete the client-side
layout: the client sends foreign names unresolved, so the panel decides which
address family they use.

```json
{
  "dns": {
    "servers": [
      "https+local://1.1.1.1/dns-query",
      "https+local://8.8.8.8/dns-query",
      "localhost"
    ],
    "queryStrategy": "UseIP"
  },
  "outbounds": [
    { "tag": "direct", "protocol": "freedom", "settings": { "domainStrategy": "UseIPv4v6" } }
  ]
}
```

- `UseIPv4v6` resolves with the built-in DNS and dials IPv4 first, falling
  back to IPv6. That is the "foreign IPv4 first" half. Use `UseIPv4` on a
  server without IPv6.
- `https+local://` sends DoH straight from the server instead of through
  Xray's routing, and Cloudflare/Google return answers local to the exit.
- If an exit's IP is refused by OpenAI or Claude, route `geosite:openai` and
  `geosite:anthropic` to a cleaner outbound (WARP, or a residential relay) in
  the panel's routing rules. Clients need no change for this.

## Upstream references

Clones live in `reference/`, which is git-ignored. Only this note is tracked.
Re-create them with:

```bash
git clone --depth 1 https://github.com/Aethersailor/Custom_OpenClash_Rules.git      reference/Custom_OpenClash_Rules
git clone --depth 1 https://github.com/Aethersailor/Custom_OpenClash_Rules.wiki.git reference/Custom_OpenClash_Rules.wiki
git clone --depth 1 -b Meta https://github.com/MetaCubeX/mihomo.git                 reference/mihomo
```

| Source | Revision read | What was taken from it |
| ------ | ------------- | ---------------------- |
| [Custom_OpenClash_Rules](https://github.com/Aethersailor/Custom_OpenClash_Rules) `cfg/yaml/Custom_Clash*.yaml` | `88a9879` (2026-10-11) | Rule order (private → services → `geolocation-!cn`/`gfw` → `cn` → MATCH), `no-resolve` on IP rules, the separate ChatGPT/AI rows |
| [Custom_OpenClash_Rules wiki](https://github.com/Aethersailor/Custom_OpenClash_Rules/wiki) "OpenClash 设置方案", "OpenWrt IPv6 设置方案" | `830a4ef` (2026-08-29) | fake-ip only, a domestic DoH `nameserver` with no `fallback`, foreign names resolved by the exit, IPv4 transport for DNS |
| [mihomo](https://github.com/MetaCubeX/mihomo) `Meta` | `88dcbf7` (2026-09-30) | `type: direct` + `ip-version`, `direct-nameserver`, the `respect-rules` requirements, fake-ip AAAA behaviour, and the removal of `global-client-fingerprint` |
| [meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat) `geo/geosite/*.list` | checked 2026-10-11 | Every GEOSITE/GEOIP name in the presets exists in the default geodata |

Where FireX differs from the wiki:

- **No Microsoft or GitHub rows, no third-party rule-providers.** Fewer moving
  parts. The only provider is FireX's own rule set, fetched from the same
  origin as the subscription.
- **The trailing `GEOIP,cn` resolves.** The wiki targets a router, where
  OpenClash bypasses mainland IPs in the firewall before mihomo sees them.
  FireX serves device clients, which have no such bypass.

Every combination of routing preset and template preset loads in mihomo
`88dcbf7` with `mihomo -t`. The whole check can be re-run with the program
kept in `.tmp/render` (git-ignored), together with a mihomo binary built from
`reference/mihomo`.
