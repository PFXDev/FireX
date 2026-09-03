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
  clients as a single proxy. FireX owns the display name and emoji; rediscovery
  never overwrites them.
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
`<username>@firex` so its own clients are always distinguishable from ones you
created by hand.

## Requirements

- A 3x-ui panel exposing `/panel/api` with an **admin**-scope API token
  (Settings → API tokens). Monitor and node-sync scopes are not enough — FireX
  creates and edits clients.
- Go 1.24+ and Node 20+ to build.

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
| `adminUser`            | Bootstrap admin username (first run only)                         |
| `adminPassword`        | Bootstrap admin password (first run only); empty generates one    |
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
file. `adminUser` and `adminPassword` are read only while no admin exists — the
file is written `0600` because of that one setting, and editing it later changes
nothing. Set `subBaseUrl` when running behind a reverse proxy, otherwise the
subscription URLs shown in the UI use whatever `Host` the browser sent.

Changes take effect on restart; FireX never reloads the file underneath itself.
Everything an admin can change while it runs — the mihomo template, the routing
matrix, panels, plans, users — lives in the database instead, edited from the UI.

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
Asset names are `firex-{goos}-{goarch}[.exe]` and must stay identical to
`targetName()` in `internal/updater` — a name only one side knows about is a
permanent 404 for those machines.

## Subscriptions

```
GET /sub/<token>              # mihomo by default
GET /sub/<token>?target=clash
GET /sub/<token>?target=mihomo
GET /sub/<token>?target=base64
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
🤖 AI 服务        all groups     CN2GIA        —
📺 国外媒体       all groups     —             IEPL only
```

A policy owns its rule list, and that list is the same for everybody: `AI 服务`
matches `GEOSITE,openai` whichever tier you are on. What differs per tier is the
egress — which node groups the traffic actually leaves through.

Row order is both the rule precedence and the order the groups appear in a
client, so ad blocking leads and the broad CN rules sit at the end.

A profile column stores only what differs from the default; a cell is one of
three states — follow the default, override, or hidden (the policy is not
emitted at all for that tier, and its traffic falls through). The
`all-node-groups` member expands to **the profile's own whitelist**, which is why
one default column serves every tier and most cells stay empty.

### Invariants

Everything references a node group or policy by its **bare name**, never by the
name clients see, so changing an emoji cannot orphan a reference. Renaming a
node group rewrites every egress member in the same request; deleting one drops
those members and un-whitelists it from every profile.

Saving the matrix runs in one transaction and validates before it commits:
unknown references, a comma in a name (rules are comma-separated), a name that
collides with a node group, an unknown matcher, more or fewer than one final
policy, and loops between policies are all rejected with the reason and roll the
whole save back.

Groups that end up empty — a profile that grants no inbound in that group — are
dropped at render time, and any rule pointing at a dropped or unknown group is
repointed, because mihomo refuses to load a config with an empty `proxy-group`
or a missing target. An expired user still receives something loadable.

```
GET  /api/inbounds               # inbounds with their panel and group count
GET  /api/node-groups            # groups with tags and member inbound ids
GET  /api/profiles               # profiles with their node-group whitelist
GET  /api/routing                # the whole matrix plus editor options
PUT  /api/routing                # validate and store the whole matrix
GET  /api/routing/preview?profileId=
```

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
want merging by hand.

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
