# FireX

Management interface for Xray (3X-UI).

FireX is a control plane over a fleet of independent 3x-ui panels. You define
users and plans once; FireX pushes the matching client to every panel a user's
plan covers, aggregates their traffic across panels, and serves each user a
single subscription URL that renders as a mihomo (Clash-compatible) profile by
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
- **Node** — one inbound on one panel, discovered automatically and shown to
  clients as a single proxy. FireX owns the display name, emoji, region and
  tags; rediscovery never overwrites them.
- **Node group** — hand-picked nodes from any number of panels presented as one
  proxy-group, normally one region on one line (`🇭🇰 香港 IEPL`). Membership is
  explicit, and one node may sit in several groups.
- **Plan** — a set of nodes plus default quota, duration and device limit.
- **User** — one identity with one UUID reused across every node, one
  subscription token, and traffic totalled across all panels.

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
2. **Nodes** — newly discovered nodes are disabled on purpose. Give them a name,
   emoji and region, then enable the ones you want to sell.
3. **Groups** — bundle those nodes into the proxy-groups clients will see, one
   per region and line. Optional: with no groups, nodes are grouped by region.
4. **Plans** — create a plan and tick the nodes it includes.
5. **Users** — create a user on that plan. FireX pushes the client to every
   panel involved and hands you the subscription URL.
6. **Routing** — compose the policy groups and rules that decide which group a
   given kind of traffic uses.

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

## Grouping and routing

A mihomo profile is two things: a base config (DNS, sniffer, ports) and a policy
layout (`proxy-groups` plus `rules`). FireX always takes the base from the YAML
template, and offers two ways to own the policy layout. Which one applies is the
**mode** on the Routing page.

### Visual mode (default)

Groups and rules are edited as data, not YAML.

- **Nodes → Groups** creates a node group: a name, an emoji, the region and line
  it stands for, its proxy-group type (`select`, `url-test`, `fallback`,
  `load-balance`) with its own probe URL, interval and tolerance, and the nodes
  it contains. Membership is ticked by hand across every panel, so a group is a
  deliberate product decision rather than a side effect of how someone typed a
  region.
- **Routing** composes *policy groups* (`🚀 节点选择`, `🤖 AI 服务`, …) out of
  node groups, other policy groups, `DIRECT`/`REJECT`, and two dynamic entries:
  every node group, or every node the user may use. The rule list underneath is
  an ordered set of `matcher, value, target` rows, with `no-resolve` offered
  only on the IP matchers that read it, and a final MATCH target.

Everything references a group by its **bare name**, never by the name clients
see, so changing an emoji cannot orphan a rule. Renaming or deleting a node
group rewrites the stored routing in the same request, and the UI reports how
many references moved.

With **no node groups defined at all**, groups are derived from the nodes'
region text instead — one url-test group per region, exactly as before. The
Groups page offers to materialise that same split as real rows to start from.

Saving validates before it stores: unknown references, a policy group with no
members, a comma in a name (rules are comma-separated), a name that collides
with a node group, an unknown matcher, and loops between policy groups are all
rejected with the reason. The Routing page's preview renders the whole profile
against the real node groups so an admin can read what a client would receive.

### YAML mode

The template keeps `proxy-groups` and `rules`, and FireX expands these tokens
inside `proxy-groups`:

| Token              | As a group entry                    | Inside a group's `proxies` list      |
| ------------------ | ----------------------------------- | ------------------------------------ |
| `<ALL>`            | —                                   | every node the user may use          |
| `<REGION_GROUPS>`  | one url-test group per region       | those groups' names                  |
| `<REGION:name>`    | —                                   | nodes whose region is exactly `name` |
| `<TAG:name>`       | —                                   | nodes carrying that tag              |
| `<FILTER:regexp>`  | —                                   | nodes whose name matches the regexp  |

A region group is named after the region text you typed on the node, so use
something display-ready like `🇭🇰 香港`. An optional top-level `firex:` block
tunes the generated region groups (`region-group-type`, `test-url`, `interval`,
`tolerance`) and is stripped from the output. Node groups are ignored in this
mode.

### Both modes

`proxies` is always replaced with the user's own nodes. Groups that end up empty
— a user whose plan holds no node in that group — are dropped, and any rule
pointing at a dropped group is repointed, because mihomo refuses to load a
config with an empty `proxy-group`. Saving a template renders it against a probe
node first, so a broken template is rejected in the UI instead of at the client.

```
GET  /api/node-groups            # groups with their member node ids
POST /api/node-groups/generate   # materialise the region split as groups
GET  /api/settings/routing       # mode, model, built-in default, editor options
PUT  /api/settings/routing       # validate and store
POST /api/settings/routing/reset # back to the built-in default
POST /api/settings/routing/preview
```

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
  clash/              # mihomo rendering: routing model, groups, template tokens
  config/             # the JSON config file: template, completion, validation
  model/              # GORM models and migrations
  panel/              # 3x-ui REST client
  paneltest/          # in-process fake 3x-ui used by the tests
  provision/          # discovery, reconciliation, traffic and quota enforcement
  server/             # admin API, subscription endpoint, UI mount
  sharelink/          # share-link parsing and rewriting
  store/              # database open/migrate
  subscription/       # per-user subscription assembly, node group resolution
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
