# FireX

Management interface for Xray (3X-UI).

FireX is a control plane over a fleet of independent 3x-ui panels. You define
users and plans once; FireX pushes the matching client to every panel a user's
plan covers, aggregates their traffic across panels, and serves each user a
single subscription URL that renders as a Clash (mihomo) profile or a base64
share-link list.

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

FIREX_ADMIN_PASSWORD=change-me-now ./bin/firex
```

Without `FIREX_ADMIN_PASSWORD` a random password is generated and printed once
on first start. Open `http://localhost:8080`, sign in, then:

1. **Panels** — add each 3x-ui with its base URL and API token. FireX pulls its
   inbounds immediately.
2. **Nodes** — newly discovered nodes are disabled on purpose. Give them a name,
   emoji and region, then enable the ones you want to sell.
3. **Plans** — create a plan and tick the nodes it includes.
4. **Users** — create a user on that plan. FireX pushes the client to every
   panel involved and hands you the subscription URL.

## Configuration

All configuration is environment variables; there is no config file.

| Variable                   | Default          | Purpose                                                        |
| -------------------------- | ---------------- | -------------------------------------------------------------- |
| `FIREX_LISTEN`             | `:8080`          | Listen address                                                  |
| `FIREX_DATA_DIR`           | `./data`         | Directory for the SQLite database                               |
| `FIREX_DB`                 | `$DATA_DIR/firex.db` | Explicit database path                                      |
| `FIREX_SUB_BASE_URL`       | derived from request | Public origin for subscription URLs, e.g. `https://sub.example.com` |
| `FIREX_ADMIN_USER`         | `admin`          | Bootstrap admin username (first run only)                       |
| `FIREX_ADMIN_PASSWORD`     | generated        | Bootstrap admin password (first run only)                       |
| `FIREX_SYNC_INTERVAL`      | `2m`             | Full user reconcile interval                                    |
| `FIREX_TRAFFIC_INTERVAL`   | `1m`             | Traffic collection and quota enforcement interval               |
| `FIREX_DISCOVER_INTERVAL`  | `5m`             | Inbound discovery interval                                      |
| `FIREX_DEBUG`              | `false`          | Verbose logging and SQL tracing                                 |

Set `FIREX_SUB_BASE_URL` when running behind a reverse proxy, otherwise the
subscription URLs shown in the UI use whatever `Host` the browser sent.

## Subscriptions

```
GET /sub/<token>              # format chosen from the User-Agent
GET /sub/<token>?target=clash
GET /sub/<token>?target=base64
```

Clash-family user agents (clash, mihomo, meta, stash, …) get a mihomo profile;
everything else gets the base64 share-link list. The response carries
`Subscription-Userinfo` with the aggregate upload, download, quota and expiry,
so clients can show remaining traffic.

Share links come from each panel's own link generator, so Reality keys, host
overrides and external addresses stay correct. FireX only rewrites the display
name and converts them into mihomo proxy entries.

## Clash template

Settings → Clash template holds the profile FireX renders per user. It is a
normal mihomo config; FireX replaces `proxies` and expands these tokens inside
`proxy-groups`:

| Token              | As a group entry                    | Inside a group's `proxies` list      |
| ------------------ | ----------------------------------- | ------------------------------------ |
| `<ALL>`            | —                                   | every node the user may use          |
| `<REGION_GROUPS>`  | one url-test group per region       | those groups' names                  |
| `<REGION:name>`    | —                                   | nodes whose region is exactly `name` |
| `<TAG:name>`       | —                                   | nodes carrying that tag              |
| `<FILTER:regexp>`  | —                                   | nodes whose name matches the regexp  |

A region group is named after the region text you typed on the node, so use
something display-ready like `🇭🇰 香港`.

Groups that expand to nothing — a user whose plan has no node in that region —
are dropped, and any rule pointing at a dropped group is repointed, because
mihomo refuses to load a config with an empty `proxy-group`. An optional
top-level `firex:` block tunes the generated region groups
(`region-group-type`, `test-url`, `interval`, `tolerance`) and is stripped from
the output.

Saving a template renders it against a probe node first, so a broken template is
rejected in the UI instead of at the client.

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
  clash/              # mihomo profile template expansion and rendering
  config/             # environment-driven settings
  model/              # GORM models and migrations
  panel/              # 3x-ui REST client
  paneltest/          # in-process fake 3x-ui used by the tests
  provision/          # discovery, reconciliation, traffic and quota enforcement
  server/             # admin API, subscription endpoint, UI mount
  sharelink/          # share-link parsing and rewriting
  store/              # database open/migrate
  subscription/       # per-user subscription assembly
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
make dev          # Vite dev server on :5173, proxying /api and /sub to :8080
go run ./cmd/firex  # backend on :8080
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
