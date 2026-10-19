# FireX

Management interface for Xray (3X-UI).

FireX is a control plane over a fleet of independent 3x-ui panels. You define
users, plans and routing once; FireX pushes the matching client to every panel a
user's plan reaches, aggregates their traffic across panels, and serves each user
a single subscription URL that renders as a mihomo (Clash-compatible) profile by
default, with a base64 share-link list available for legacy clients.

It is built for a single operator: there is no self-service signup, no billing,
and no user-facing portal — just an admin UI and a subscription endpoint.

## How it fits together

```
                    ┌──────────────┐
   admin UI ───────▶│              │──── /panel/api ───▶  3x-ui panel A
                    │    FireX     │──── /panel/api ───▶  3x-ui panel B
   client app ─────▶│              │──── /panel/api ───▶  3x-ui panel C
      /sub/<token>  └──────────────┘
```

- **Panel** — one remote 3x-ui install, reached over its REST API with an
  admin-scoped API token. Panels stay ordinary independent installs; FireX
  never needs them clustered.
- **Inbound** — one inbound on one panel, discovered automatically and shown to
  clients as a single proxy. FireX owns the display name, emoji and, when the
  panel's own address or port is not the one clients should dial, the public
  endpoint; rediscovery never overwrites them.
- **Node group** (节点组) — hand-picked inbounds from any number of panels
  presented as one proxy-group, normally one region on one line
  (`🇭🇰 香港 IEPL`). It carries key/value tags (地区, 线路, 落地) for filtering,
  and one inbound may sit in several groups. This is the finest unit anything
  else addresses.
- **Policy** (分流策略) — a reusable rule list plus the identity it takes in the
  client's group list (`🤖 AI 服务`). Policies are global and globally ordered:
  every user sees the same rules in the same order.
- **Profile** (分流方案) — one tier's routing: the node groups its users may
  reach, plus the column of egress overrides that differ from the defaults.
- **Egress** (出口) — one cell of the matrix: which node groups policy P uses
  for profile F, and how it picks between them.
- **Plan** — quota, duration, device limit, and the profile it binds.
- **User** — one identity with one UUID reused across every inbound, one
  subscription token, and traffic totalled across all panels.

```
user ─ plan ─ profile ─┬─ node group whitelist ─ inbound ─ panel   ← what is pushed
                       └─ egress (per policy) ──────────────────   ← how traffic splits
```

**Only the profile's whitelist decides which inbounds a user is provisioned
onto.** Editing rules or egresses changes what a client renders and never writes
to a panel.

Provisioning state is tracked per (user, panel) because 3x-ui keys a client and
its traffic counters by a panel-unique email. FireX writes clients as
`<username>@FireX` so its own clients are always distinguishable from ones you
created by hand. Clients left over from the earlier `<username>@firex` suffix are
renamed in place on the next sync.

## Requirements

- A 3x-ui panel exposing `/panel/api` with an **admin**-scope API token
  (Settings → API tokens). Monitor and node-sync scopes are not enough — FireX
  creates and edits clients.
- Go 1.26+ and Node 22+ to build.

## Quick start

```bash
make ui-deps          # once
make build            # builds the UI, embeds it, produces ./bin/firex

./bin/firex
```

First start writes `./data/config.json` from the built-in template and, because
that file has no `adminPassword` yet, generates one and prints it once. Open
`http://localhost:8080`, sign in, then:

1. **Panels** — add each 3x-ui with its base URL and API token. FireX pulls its
   inbounds immediately.
2. **Inbounds** — newly discovered inbounds are disabled on purpose. Give them a
   name and emoji, then enable the ones you want to sell.
3. **Node groups** — bundle those inbounds into the proxy-groups clients will
   see, one per region and line, and tag them. Nothing reaches a user until it
   is in a group.
4. **Routing** — the matrix. Rows are policies, columns are profiles. Create a
   profile per tier and tick the node groups it may use; the default column
   usually says "every node group", which each profile narrows to its own
   whitelist automatically.
5. **Plans** — create a plan, set quota and duration, bind a profile.
6. **Users** — create a user on that plan. FireX pushes the client to every
   panel involved and hands you the subscription URL.

## Configuration

All configuration lives in one JSON file, `./data/config.json`, next to the
database. Nothing is read from the environment. `-config` points the binary at a
different file, and an update re-executes with the same argument:

```bash
./bin/firex -config /etc/firex/config.json
```

The file is released from a template compiled into the binary on first start,
and completed on every start after that: a setting it never mentions — because
an older build wrote it, or because it was typed by hand — is filled in with its
default and written back, so the file always lists everything the running build
understands. A value FireX cannot act on (a blank listen address, a zero
interval, an unknown update source) is replaced the same way, and a key this
build does not know — a typo, or a setting from a newer version — is dropped on
the way through. What it will not do is guess: a malformed file, or a duration
written as anything but a string, aborts startup.

```json
{
  "listen": ":8080",
  "dataDir": "./data",
  "dbPath": "",
  "subBaseUrl": "",
  "debug": false,
  "adminUser": "admin",
  "adminPassword": "",
  "syncInterval": "2m0s",
  "trafficInterval": "1m0s",
  "discoverInterval": "5m0s",
  "update": {
    "enabled": false,
    "channel": "stable",
    "checkInterval": "1h0m0s",
    "source": "github",
    "proxyBaseUrl": "https://dl.repo.chycloud.top",
    "repo": "PFXDev/FireX"
  }
}
```

| Setting                | Purpose                                                          |
| ---------------------- | ---------------------------------------------------------------- |
| `listen`               | Listen address                                                    |
| `dataDir`              | Directory for the database and staged updates                     |
| `dbPath`               | Explicit database path; empty follows `dataDir`                   |
| `subBaseUrl`           | Public origin for subscription URLs; empty derives it per request |
| `debug`                | Verbose logging and SQL tracing                                   |
| `adminUser`            | Admin username; created on first start, reset target after        |
| `adminPassword`        | One-shot: sets the admin's password on the next start, then blanks |
| `syncInterval`         | Full user reconcile interval                                      |
| `trafficInterval`      | Traffic collection and quota enforcement interval                 |
| `discoverInterval`     | Inbound discovery interval                                        |
| `update.enabled`       | Check for new releases periodically                               |
| `update.channel`       | `stable` (installs automatically) or `dev` (waits for approval)   |
| `update.checkInterval` | Update check interval, floored at one minute                      |
| `update.source`        | `github` for direct downloads, `proxy` for a mirror               |
| `update.proxyBaseUrl`  | Mirror used when the source is `proxy`                            |
| `update.repo`          | `owner/name` of the repository releases come from                 |

Durations are anything `time.ParseDuration` accepts: `"90s"`, `"2m"`, `"1h30m"`.
Relative paths resolve against the working directory, not against the config
file. `adminPassword` is consumed rather than kept: whatever is there on start
becomes the admin's password — the new admin's on first start, a reset of
`adminUser`'s on any later one — and the setting is then blanked from the file,
which is written `0600` for the moments it holds one. That is also the way back
in from a lost password: write one, restart, sign in, and find the setting
empty again. Every session open under the old password is signed out. Leave it
empty on first start and FireX generates one and prints it to the log once. Set
`subBaseUrl` when running behind a reverse proxy, otherwise the subscription
URLs shown in the UI use whatever `Host` the browser sent.

The **System → Server configuration** section edits this same file from the web
UI, including a custom `-config` path. It shows the saved values and whether a
restart is needed. Saves validate addresses, intervals and update settings,
replace the file atomically with mode `0600`, and reject stale edits if another
browser or an operator has changed the settings since they were loaded.

Changes take effect on restart; restart FireX through your deployment's service
manager after saving. The listener, background jobs and updater keep their
startup settings until then. Changing storage paths does not move the existing
database. The version section continues to show the active update settings.

The password field is write-only: leaving it blank preserves any pending reset;
the cancellation switch clears a previously saved reset. A reset requires an
existing admin username and takes effect on the next start, when the password
is consumed and its old sessions are signed out. Use **Settings → Account
security** for an immediate password change.

```
GET /api/settings/server   # saved config, path, revision and restart status
PUT /api/settings/server   # { "revision": "...", "config": { ...edited fields } }
```

Both endpoints require an admin session. Omitted fields are preserved, including
`adminPassword`; an explicit empty password cancels a pending reset. A stale
revision returns `409`, invalid values return `400` and failed writes return
`500` without changing the file.

The mihomo template, routing matrix, panels, plans and users live in the database
and continue to take effect immediately when edited from the UI.

## Managed inbound configurations

**Inbound configurations** lets FireX own an inbound's complete panel
configuration, without clients, counters or panel IDs. **Take over** saves the last
discovered configuration. Discovery then only compares the panel with it. The
inbound shows **drifted** when the panel was changed outside FireX, **pending** after
an edit, and **missing** when the panel no longer has it.

**Preview and deploy** writes the owned configuration back after showing the diff.
It updates the inbound and keeps its clients and counters. It recreates a missing
inbound. If an overwritten panel database renumbered the inbound, it can rebind to
the new copy. The FireX inbound keeps its identity, node groups and users either way.
Each write is sent once and read back. A stale preview is rejected. Before every
write, FireX saves the panel's configuration so it can be loaded and deployed
again. FireX's client address and port stay on the inbound and are never touched
by a deployment.

**Inbound templates** hold the parameters nodes share, such as a REALITY setup with
its keys, target and fallback limits. **Save as template** drops listen, port, tag,
PROXY and fallback routes. **Create inbound from template** fills in one node's
values, creates the panel inbound and takes it over. Like any new inbound it stays
disabled and ungrouped until reviewed. Later template edits do not change inbounds
created from it.

```
GET    /api/inbounds/:id/config            # owned parameters, state, deployment history
POST   /api/inbounds/:id/config            # take over: current config, or { deploymentId } backup
PUT    /api/inbounds/:id/config            # revision + parameters, or revision + deploymentId
DELETE /api/inbounds/:id/config            # stop managing; the panel is untouched
POST   /api/inbounds/:id/config/preview    # revision, optional rebindInboundId
POST   /api/inbounds/:id/config/deploy     # same + targetRevision from the preview
GET/POST /api/inbound-templates            # POST: name + inboundId or parameters
GET/PUT/DELETE /api/inbound-templates/:id
POST   /api/inbound-templates/:id/instantiate  # revision, panelId, listen, port, remark,
                                               # acceptProxyProtocol, publicAddress, publicPort
```

## Agent sessions

A local agent (Claude Code, a script, anything that speaks HTTP) can edit the
configuration, but nothing it sends takes effect until an admin has reviewed
the diff in the UI and merged it.

1. Under **Agent 修改**, describe the task, pick how long the session lives
   (30 minutes to 24 hours) and click **生成提示词**. FireX returns a short
   prompt with the background, the API address and a single-use `fxa_` token.
   FireX stores only the token's hash, and the prompt stays in this browser
   until the session ends.
2. Paste the prompt into the agent. It reads `GET /api/agent/guide`, fetches a
   snapshot, checks its edits with the preview endpoint and submits them with
   a summary. The page tracks it live: connected, read the config, submitted.
3. Review the diff: every changed row, field by field. **合并** applies it,
   **退回修改** sends it back with feedback (the agent keeps its token and
   resubmits), **取消会话** discards it. A newer submission replaces the one
   under review, and a merge only applies the version the reviewer saw.
4. Merging, cancelling or expiry ends the session. The token stops working on
   the next request.

There is one live session at a time. If the configuration changed after the
agent submitted, the submission cannot be merged: a merge attempt returns it
to the agent, which rebuilds on the current configuration.

```
# Admin (session cookie)
GET  /api/agent-sessions               # history: the last 100 sessions
POST /api/agent-sessions               # { task, ttlMinutes } → { session, token, prompt }
GET  /api/agent-sessions/current       # the live session, or null
GET  /api/agent-sessions/:id           # state, diff preview, merge result
POST /api/agent-sessions/:id/merge     # { submissions, allowPanelRemoval }
POST /api/agent-sessions/:id/return    # { feedback }
POST /api/agent-sessions/:id/cancel

# Agent (Authorization: Bearer fxa_…; reaches nothing else)
GET  /api/agent                        # state, task, feedback
GET  /api/agent/guide                  # the format and rules, in markdown
GET  /api/agent/snapshot               # { revision, ...sections }
POST /api/agent/preview                # validates and returns the diff, writes nothing
PUT  /api/agent/submission             # { revision, ...sections, summary }
```

A **snapshot** is the configuration as one JSON document keyed by name rather
than row id, so the same document means the same thing on any install. The
top-level sections are `panels`, `inbounds`, `inboundTemplates`, `nodeGroups`,
`profiles`, `routing` and `clashTemplate`.

- **Sections are optional; a present section is the whole truth.** A section
  left out is untouched. A section that is present replaces what is stored:
  rows it does not list are deleted, and a field left out of a row takes the
  value a new row would get.
- **One transaction.** The whole document is written and run through the same
  routing validation as a matrix save, then committed or rolled back as a
  unit. Preview and submission run exactly the same path and roll back, so a
  submission that is accepted is a merge that will succeed, unless the
  configuration has moved since.
- **Optimistic concurrency.** `revision` is required and must match the
  current configuration, both when the agent submits and when the admin
  merges.
- **References are names.** Inbounds are addressed as `{ panel, remoteId }`;
  node groups, profiles, policies and rule sets by bare name, as everywhere
  else in FireX.
- **Panels.** `apiToken` is never exported. It is required for a new panel and
  optional for an existing one (empty keeps the stored token). Dropping a panel
  deletes every FireX client on it, so the review page lists it under
  `removedPanels` and the reviewer must tick a confirmation before merging. New
  panels are discovered after the merge, so their inbounds can only be
  referenced by the next session.
- **Inbounds** are discovered, not created. A snapshot sets FireX's own fields
  and the managed configuration (`managedConfig`, `null` to stop managing).
  Leaving out an inbound that still exists on its panel is refused; one
  marked missing is deleted. Discovery-owned fields (`tag`, `port`, …) are
  shown for reading and ignored on merge.
- **Panels are written only for clients.** If the merge changed which inbounds
  a profile reaches, the affected users are pushed before the merge returns
  (`reconciledPlans`, `syncError`). A changed managed configuration is **not**
  deployed: it becomes pending, and the result page links each one to its
  inbound config page to preview and deploy.
- **Users and plans are out of scope.** They carry live traffic counters and
  subscription tokens that a configuration push must never rewind. A profile
  still bound by a plan cannot be deleted by a snapshot.

The Agent page lists past sessions under **历史记录**. Opening one shows the
task, the agent's summary, the last feedback and the diff that was merged (or,
for an unmerged session, its last submission).

## Admins and roles

FireX has two admin roles. A **super admin** has unrestricted access: panels,
inbounds, node groups, routing, templates, server settings, self-update, agent
sessions, the audit log, and other admin accounts. An **operator** manages
users only — creating, editing and deleting them, resetting traffic,
resyncing, and previewing subscriptions — and may read the overview, plan
list and profiles.

The role is checked on every request by the server; the UI only hides what
would be refused. Operators are signed in normally and see a smaller sidebar.

```
GET    /api/admins                     # id, username, role, isSelf
POST   /api/admins                     # { username, password, role }
PUT    /api/admins/:id                 # { username, role }
DELETE /api/admins/:id                 # also signs the account out
POST   /api/admins/:id/resetPassword   # { password }; signs the account out
```

The bootstrap admin created on first start is a super admin, and admins that
predate roles are promoted to super admin on upgrade. The last super admin
cannot be demoted or deleted, and an admin cannot delete their own account or
reset their own password through this API — use **设置 → 账号安全** for that,
which requires the current password.

## Audit log

Every successful write through the admin API is recorded on the **操作日志**
page with the time, admin, action, target and client IP. Agent sessions are
recorded too: created, returned, cancelled and merged. The target is the
object's name as it was before the change, so deletes and renames still read
correctly. Request bodies are never stored, so passwords and panel tokens stay
out of the log. Reads, previews, connection tests and rejected requests are not
recorded. The newest 10,000 entries are kept.

```
GET /api/audit-logs?before=<id>        # { items, more }, newest first, 50 per page
```

## Updates

FireX updates itself from its own GitHub releases. The **System** page shows the
running build and drives the whole flow; `update.enabled` only controls
the periodic check, so an admin can always trigger one by hand.

```
GET  /api/version          # running build plus the update settings
GET  /api/update/status    # state, progress, last check, error
POST /api/update/check     # look for a newer release, download nothing
POST /api/update/apply     # install a pending build, or run the whole pass
POST /api/update/dismiss   # discard a downloaded pre-release
```

All five sit behind the admin session, like the rest of `/api`. Sessions live in
the database, so they survive the restart an update causes.

Two channels, because they answer different questions:

- **stable** compares semver against the newest `v*` tag and installs it
  unattended — you opted into tracking releases.
- **dev** follows the rolling `dev` prerelease, compares the build's commit
  rather than its version string, and stops after downloading. An admin
  confirms the restart from the System page.

**How a release is verified.** Each release ships one `SHA256SUMS` covering all
of its binaries. The updater downloads the binary, fetches the manifest, selects
its own line by exact file name, and refuses to install if the manifest is
missing, has no entry for this platform, or disagrees with the bytes on disk.
There is no fallback to installing unverified — an optional check is no check.

There is deliberately **no release signature**. The consequence is real and
accepted: an attacker who controls the download channel can serve a poisoned
binary alongside a matching `SHA256SUMS`. What holds that off is HTTPS and the
default of pulling from GitHub directly; only point `update.source` at a
mirror you run yourself.

**Applying an update** waits for in-flight panel work to drain (up to ten
minutes), shuts down the listener, closes the database, replaces the binary in
place and re-executes it. On Unix the PID is unchanged, so a supervisor never
sees the service leave; on Windows a detached PowerShell script performs the
swap. FireX keeps no job table, so nothing needs recovering afterwards: the next
discovery and reconcile cycle re-converges every panel.

Releases come from `.github/workflows/cross-compile.yml`: pushes to `main`
refresh the rolling `dev` prerelease, and a `v*` tag publishes a stable release.
CI builds Linux and macOS binaries for amd64 and arm64; Windows artifacts are
not published. Asset names are `firex-{goos}-{goarch}` and must stay identical to
`targetName()` in `internal/updater` — a name only one side knows about is a
permanent 404 for those machines.

## Subscriptions

```
GET /sub/<token>              # mihomo by default
GET /sub/<token>?target=clash
GET /sub/<token>?target=mihomo
GET /sub/<token>?target=base64
GET /sub/<token>/rules/<id>   # a rule set as plain-text classical rules
```

The default response is a mihomo profile. Known legacy clients such as v2rayN
still receive the base64 share-link list automatically, and callers can select
either format explicitly with `target`. sing-box output is not enabled and an
unsupported target is rejected instead of silently returning another format.
The response carries
`Subscription-Userinfo` with the aggregate upload, download, quota and expiry,
so clients can show remaining traffic.

Share links come from each panel's own link generator, so Reality keys, host
overrides and external addresses stay correct. FireX only rewrites the display
name and converts them into mihomo proxy entries.

The one exception is the endpoint. A panel can only advertise where xray
listens, and that is not always where clients should knock: an inbound parked
on `127.0.0.1:9443` behind an SNI router that owns 443, a relay, a CDN. Set
**public address** and **public port** on the inbound and FireX substitutes
them into the link and the mihomo entry after matching; everything else in the
link, Reality keys included, is left as the panel wrote it. Leave a field empty
to keep the panel's value for that half.

**Inbound parameters and Vision.** Click an inbound's name or **参数** to inspect
its last discovered configuration, grouped by connection, transport/security,
protocol and sniffing, or as JSON. Unknown panel options are retained; client
accounts and their traffic counters are excluded. Keys are hidden by default
and can be revealed and copied. **同步参数** refreshes the panel snapshot.

In **编辑入站**, choose **开启 Vision** for VLESS over TCP/RAW with REALITY or TLS.
Newly discovered inbounds default to off. Inbounds from versions without this
setting show **保留现有流控**: upgrades, quota edits and rediscovery preserve each
existing user's flow, even when users on the same inbound differ. New users
and new attachments start without flow while this mode is retained. Choosing
on or off explicitly takes ownership of the inbound's flow and survives
rediscovery; ordinary edits leave the preserved setting unchanged. Enabling it pushes
`xtls-rprx-vision` to this inbound's FireX users and invalidates cached links;
clients must refresh their subscriptions. Turning it off explicitly clears
the flow. New users and new attachments inherit the inbound's setting.
Other transports cannot enable Vision, and a panel-side `disableFlow` must
be cleared in 3X-UI first. Per-inbound updates require 3X-UI's
`POST /panel/api/clients/update/:email?inboundIds=...` support. FireX reads the
flows back before recording a successful sync and retries failures on the
next sync; it never fabricates Vision in a subscription after a failed push.
Removed inbound permissions are revoked before validating the remaining
inbounds, so an incompatible Vision setting cannot block that revocation.

## Routing

A mihomo profile is two things: a base config (DNS, sniffer, ports) and a policy
layout (`proxy-groups` plus `rules`). The YAML template owns the base and only
the base — the policy layout always comes from the routing matrix, compiled per
user.

### The matrix

Rows are **policies**, columns are **profiles**, and each cell is an **egress**.

```
                 default        VIP           VVIP
🛑 广告拦截       REJECT         —             —
🤖 OpenAI         all groups     CN2GIA        —
📺 国外媒体       all groups     —             IEPL only
```

A policy owns its rule list, and that list is the same for everybody: `OpenAI`
matches `GEOSITE,openai` whichever tier you are on. What differs per tier is the
egress — which node groups the traffic actually leaves through.

Row order is both the rule precedence and the order the groups appear in a
client, so ad blocking leads and the broad CN rules sit at the end.

A profile column stores only what differs from the default; a cell is one of
three states — follow the default, override, or hidden (the policy is not
emitted at all for that tier, and its traffic falls through). The
`all-node-groups` member expands to **the profile's own whitelist**, which is why
one default column serves every tier and most cells stay empty.

Each egress also has an independent **hide in client** switch (`clientHidden`).
It emits `hidden: true` on the mihomo proxy-group while keeping its members,
rules and references intact, including the final `MATCH` policy. Profiles
inherit this switch with the default egress and can override it per cell.
It is off by default. Hiding requires a client that supports mihomo's `hidden`
flag; the group remains readable in the downloaded config and the controller
API. This differs from **do not emit** (`hidden`), which removes the policy
group and its rules for that profile.

### Invariants

Everything references a node group or policy by its **bare name**, never by the
name clients see, so changing an emoji cannot orphan a reference. Renaming a
node group or a policy rewrites every egress member that pointed at it in the
same request; deleting one drops those members (and, for a node group,
un-whitelists it from every profile).

Saving the matrix runs in one transaction and validates before it commits:
unknown references, a comma in a name (rules are comma-separated), a name that
collides with a node group or with a mihomo builtin such as `DIRECT`, an unknown
matcher, more or fewer than one final policy, a profile column that hides the
final policy, and loops between policies are all rejected with the reason and
roll the whole save back.

Edits that change which inbounds a user holds — node group membership, a group
or inbound being disabled, a profile's whitelist, a plan's profile — are pushed
to the panels before the request returns. The row is saved either way; if a
panel refused the push, the response carries `syncError` and the UI shows it
next to the success toast, so a panel that disagrees with FireX is never
silent.

Groups that end up empty — a profile that grants no inbound in that group — are
dropped at render time, and any rule pointing at a dropped or unknown group is
repointed, because mihomo refuses to load a config with an empty `proxy-group`
or a missing target. An expired user still receives something loadable.

```
GET  /api/inbounds               # inbounds with their panel and group count
GET  /api/node-groups            # groups with tags and member inbound ids
GET  /api/profiles               # profiles with their node-group whitelist
GET  /api/routing                # the whole matrix plus editor options
PUT  /api/routing                # validate and store the whole matrix, rule sets included
GET  /api/routing/preview?profileId=
POST /api/routing/presets/:key   # replace every policy with a built-in layout
```

### Presets, DNS and IPv6

Both halves of a profile have built-in starting points, and neither is ever
applied behind your back. **Routing → 套用预设** swaps every policy for a
built-in layout in one validated transaction; profiles, whitelists and any
profile override whose policy name survives are kept. **Settings → 载入预设**
fills the template editor and waits for you to save.

| Routing preset | Rows |
| -------------- | ---- |
| `standard` (fresh installs) | 节点选择, 自动选择, 广告拦截, 大流量下载, OpenAI, Claude, AI 服务, 电报消息, 国外媒体, 谷歌服务, 游戏下载, Steam, 苹果服务, 国外网站, 全球直连, 漏网之鱼 |
| `lite` | 节点选择, 自动选择, OpenAI, Claude, 国外网站, 全球直连, 漏网之鱼 |

| Template preset | Use |
| --------------- | --- |
| `dual-stack` (built-in default) | IPv6 on; the right choice wherever clients have working IPv6 |
| `ipv4-only` | Same DNS layout with IPv6 off |

The stock layouts are built on these choices:

- **fake-ip, always.** The renderer pins `dns.enhanced-mode` to `fake-ip`
  whatever the template says. A proxied domain is handed to the exit node
  unresolved, so the client never looks it up — no leak, and the CDN answer
  is the one nearest the exit.
- **Split DNS.** Mainland and private domains, node addresses and every
  DIRECT dial resolve through AliDNS DoH (`direct-nameserver`,
  `proxy-server-nameserver`, `nameserver-policy`). Anything else that needs a
  client-side answer goes to Cloudflare DoH, which `respect-rules` sends
  through the proxy. Every upstream is an IP literal, so nothing needs a
  bootstrap lookup.
- **IPv6-first node connections.** Generated nodes carry
  `ip-version: ipv6-prefer`. TCP prefers IPv6 with IPv4 fallback; literal IP
  endpoints keep using that address. The `ipv4-only` template still disables
  IPv6 DNS resolution. UDP-based transports follow mihomo's address selection
  and do not guarantee fallback after an IPv6 handshake failure.
- **Every mainland address bypasses.** `GEOSITE,cn` catches known domains;
  the last direct rule is a resolving `GEOIP,cn`, so a mainland site no list
  knows still goes direct. It sits after `geolocation-!cn`, so only domains in
  neither list pay for the lookup.
- **IPv6 that degrades safely.** Direct rows default to the builtin
  `DIRECT`, which races both families for TCP and sends UDP over IPv4, so a
  network without IPv6 loses nothing. 全球直连 also offers `DIRECT-V6`, a
  `type: direct` outbound with `ip-version: ipv6-prefer` that FireX adds to
  `proxies` whenever a group lists it. A client on good IPv6 can switch to it
  for domestic IPv6 first. It is not the default, because its UDP never falls
  back to IPv4. Proxied domains get no AAAA on the client and are resolved by
  the exit; set the panel's freedom outbound to `UseIPv4v6` for IPv4-first
  exits.
- **OpenAI and Claude have their own rows**, separate from other AI services.
  They offer node groups first and are plain `select` groups, because both
  services refuse some regions and flag sessions whose exit keeps changing.
- **Google and Steam have their own proxy switches.** YouTube and Gemini
  keep their earlier media and AI rules. 大流量下载 puts large downloads from
  blocked or slow overseas sources (Hugging Face, GitHub downloads, Drive,
  MEGA, `persistent.oaistatic.com`, Debian/Ubuntu/Arch/AUR) on an exit of
  their own. It precedes OpenAI, AI and Google so those broader rules cannot
  capture its hosts. Game CDNs use 游戏下载, defaulting to DIRECT before
  Steam.
- **Long, changing lists are rule sets.** 大流量下载's hosts live in a rule
  set edited under **Routing → 规则集** and referenced by a `RULE-SET` rule.
  FireX serves it to clients as a `rule-providers` entry at
  `/sub/<token>/rules/<id>`, which clients re-fetch every six hours. Applying
  a preset never overwrites an existing rule set.

Background, references and the recommended server-side Xray settings are in
[`docs/routing.md`](docs/routing.md).

### Migrating from the pre-matrix schema

A database from before this model is migrated on first start, after a
`VACUUM INTO` snapshot is written next to it as `firex.db.bak-<timestamp>`;
FireX refuses to start rather than migrate without one. `nodes` becomes
`inbounds`, the group's region/line columns become tags, inbounds in no group
are grouped by their old region text, and the stored routing blob is split into
policies and their default egresses.

Each plan gets a profile granting **exactly** the inbounds it granted before: a
node group is whitelisted only when every one of its members was already in the
plan, and whatever that leaves uncovered gets a group of its own. Nothing is
ever widened — a cheap plan silently gaining a premium line would be worse than
some migration clutter. Two things cannot be restored faithfully and are worth
reviewing afterwards: rules that interleaved between policies keep their
relative order but move as a block, and the generated leftover groups usually
want merging by hand. The routing page shows a review banner after a migration
until the matrix is saved once.

## Quota enforcement

FireX polls each panel's per-client counters and accumulates deltas into the
user's global total; a counter that moves backwards is treated as a panel-side
reset rather than negative usage. When the aggregate crosses the limit — or the
expiry passes — the user is disabled on every panel.

Each panel-side client also carries the user's full quota as its own cap. That
bounds how far a user can overshoot between polls, while FireX's aggregate
remains the authority across panels.

## Development

### Layout

```
cmd/firex/            # main package: config, background loops, HTTP server
internal/
  clash/              # mihomo rendering: template plus resolved groups and rules
  config/             # the JSON config file: template, completion, validation
  model/              # GORM models and migrations
  panel/              # 3x-ui REST client
  paneltest/          # in-process fake 3x-ui used by the tests
  provision/          # discovery, reconciliation, traffic and quota enforcement
  server/             # admin API, subscription endpoint, UI mount
  sharelink/          # share-link parsing and rewriting
  routing/            # the matrix: profile whitelists, egress resolution, seed
  store/              # database open, schema migration
  subscription/       # per-user subscription assembly from the panels' links
  updater/            # self-update: release discovery, checksum, restart
  version/            # build identity stamped in by the release workflow
  web/                # go:embed of the built UI (dist/ is generated)
web/                  # React 19 + TypeScript + Vite frontend sources
bin/                  # build output (generated)
data/                 # SQLite database at runtime (generated)
```

The management UI is React 19 + TypeScript + Vite, styled with
[shadcn/ui](https://ui.shadcn.com) (Base UI primitives, Tailwind v4) under
`web/`. Components live in `web/src/components/ui/` as ordinary source — add
more with `cd web && npx shadcn@latest add <component>`. `npm run build` writes
the bundle into `internal/web/dist/`, which the Go build embeds.

```bash
make ui-deps      # once: install the frontend dependencies
make run          # build the frontend and backend, then start FireX on :8080
make dev          # Vite dev server on :5173, proxying /api and /sub to :8080
make test         # Go tests
make verify       # vet + test + typecheck + full build
```

The Go tests drive a fake 3x-ui (`internal/paneltest`) end to end: discovery,
provisioning, traffic accounting and subscription rendering all run against it
without a real panel.

A fresh clone has no UI bundle, only the tracked `internal/web/dist/.gitkeep`
placeholder that satisfies the `go:embed` in `internal/web/embed.go` — so plain
`go build ./...` and `go test ./...` work, and the resulting binary serves the
API while reporting that the UI is missing. Run `make ui` (or `make build`) for
the real thing; `make dist-stub` restores the placeholder after a `make clean`.

## License

GPL-3.0. See [LICENSE](LICENSE).
