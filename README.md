# Gotham — Self-Hosted PaaS

Gotham is a self-hosted Platform-as-a-Service (PaaS), built with **Go** (backend + agent) and **Vue 3** (frontend). One `gotham` binary runs the control plane; one `gotham-agent` binary runs on each managed node.

Design priorities (in order):

1. **Compatibility** — wide OS / Docker / database support matrix, graceful degradation on older nodes.
2. **Extensibility** — modular monolith with clean boundaries, versioned gRPC contracts, template-driven services.
3. **Self-updating** — signed automatic updates for both the control plane and node agents, with rollback.
4. **Lean footprint** — comfortable on small servers (≥ 1 GB RAM, 1 vCPU), but not at the cost of the three priorities above.

---

## 1. Final Technology Stack

| Layer | Technology | Notes |
|---|---|---|
| Backend | Go 1.22+ | Single binary, cross-compiled for `linux/amd64` + `linux/arm64` |
| HTTP framework | Chi router | stdlib `net/http` compatible, easy middleware composition |
| Data access | sqlc + pgx | Type-safe generated queries, no reflection |
| Database | PostgreSQL | Control-plane state (single node or managed cluster) |
| Migrations | goose | Versioned, embeddable schema migrations |
| Agent | Go (same repository) | One small binary per managed node |
| CP ↔ Agent | gRPC + protobuf (buf) | Versioned contracts, mTLS |
| Realtime | WebSocket + Redis pub/sub | Live build/deploy/container logs |
| Reverse proxy | Traefik 3.x | Docker provider, automatic Let's Encrypt SSL |
| Build engines | Dockerfile, Railpack, Buildpacks | Multi-language builds without Dockerfile |
| Frontend | Vue 3 + Vite + TypeScript | SPA embedded into the Go binary via `embed.FS` |
| State / HTTP | Pinia + axios | |
| UI kit | Naive UI | Flat design style |
| Auth | JWT + OAuth2 (GitHub, GitLab) | argon2id password hashing, TOTP 2FA later |
| Config | viper | YAML / ENV, hot reload |
| Self-update | Built-in atomic swap + Ed25519 signed manifest | GitHub Releases as the update channel |
| CI/CD | GitHub Actions + GoReleaser | Build, sign, publish, attach checksums |

---

## 2. Architecture at a Glance

```
                        ┌───────────────────────────────────────┐
                        │   Control Plane (single Go binary)    │
                        │  ┌──────────────┐  ┌────────────────┐ │
 Users ──── HTTPS ─────►│  │ REST API v1  │  │ Web UI (Vue 3) │ │
                        │  │ (Chi)        │  │ embedded SPA   │ │
                        │  └──────┬───────┘  └────────────────┘ │
                        │         │                             │
                        │  ┌──────▼───────┐  ┌────────────────┐ │
                        │  │ Services     │  │ gRPC server    │ │
                        │  │ (deploy,     │  │ (agent         │ │
                        │  │  build,      │  │  gateway)      │ │
                        │  │  proxy, …)   │  └───────┬────────┘ │
                        │  └──────┬───────┘          │          │
                        │  ┌──────▼───────┐  ┌───────▼────────┐ │
                        │  │ PostgreSQL   │  │ Redis pub/sub  │ │
                        │  └──────────────┘  └────────────────┘ │
                        └───────────────────────────┬───────────┘
                                                    │ gRPC (mTLS)
                        ┌───────────────────────────▼───────────┐
                        │  Agent (Go binary, one per node)      │
                        │   ├─ Docker Engine ──► containers     │
                        │   └─ Traefik (labels, Let's Encrypt)  │
                        └───────────────────────────────────────┘
```

- The **control plane** is a modular monolith: each domain (servers, applications, databases, services, proxy, updates) is a package with its own service interface and store. Boundaries are drawn so any module can be extracted later.
- The **agent** calls the Docker Engine API on its node and streams logs/status to the control plane.
- **Traefik** runs on one or more nodes; containers are wired to it via Docker labels managed by the control plane.

---

## 3. Repository Layout

```
.
├── cmd/
│   ├── gotham/               # Gotham control-plane entrypoint (`gotham serve`, `gotham update`, ...)
│   └── gotham-agent/         # node agent entrypoint
├── internal/                 # CP-only packages (modular monolith)
│   ├── server/               # REST API, middleware, WebSocket hub
│   ├── deploy/               # deploy orchestration
│   ├── builds/               # build engines (dockerfile, railpack, buildpacks, static)
│   ├── proxy/                # Traefik integration
│   ├── databases/            # managed databases & backups
│   ├── services/             # one-click service templates
│   ├── auth/                 # JWT, OAuth2, RBAC, 2FA
│   ├── updates/              # self-update & remote agent update (CP side; uses updatecore)
│   └── store/                # persistence (sqlc repositories)
├── agent/                    # agent implementation (must not import `internal/`)
├── updatecore/               # shared self-update engine: verify, download, atomic swap (CP + agent)
├── proto/                    # protobuf contracts (buf-managed)
├── web/                      # Vue 3 SPA (Vite)
├── templates/                # one-click service templates (YAML)
├── deploy/                   # install scripts, systemd units, compose
└── docs/                     # docs index, process, plan, test server
    ├── README.md             # start here: plan status, process, test server
    ├── process.md            # how we work (packages, gates, invariants)
    ├── TODO.md               # remaining work, checkbox by checkbox
    ├── test-server.md        # shared all-in-one test box
    ├── plan/                 # development plan per phase
    └── design/               # UI mockups (*.html) + design tokens
```

## Agent remote update (BE-9.2)

Node agents self-update from the control plane over the authenticated mTLS
channel:

1. The agent calls `UpdateService/RequestUpdate` on reconnect and on a poll
   interval, reporting its `agent_version`, `os` and `arch`.
2. The control plane resolves the newest agent release, fetches its **signed
   manifest** and verifies the Ed25519 signature with the release public key
   before answering, and returns the asset URL, manifest URL, signature URL,
   `sha256` and channel. An unsigned or unverifiable release is never offered.
3. The agent downloads the asset through the shared safe client in
   `updatecore` (https-only, bounded size/redirects, no link-local dials),
   re-verifies the signature with the key **embedded in the agent binary**,
   binds version/arch/file/digest, then swaps atomically (hardlink backup,
   gap-free commit, `<binary>.old` rollback) and restarts via a root-owned
   wrapper outside its writable directory.
4. After the restart the agent reports the new version on its next heartbeat;
   the control plane keeps an in-memory agent version map (`GET
   /api/v1/servers/agents`). A failed update rolls back and keeps the old
   version.

A plain offer is applied only when `GOTHAM_AGENT_AUTO_UPDATE=true`. A platform
operator can force a fleet rollout with `POST /api/v1/servers/agents/update-all`,
which records the target and lets agents converge on their next poll — the
request never dials or blocks on a node. The control plane only serves update
material over the mTLS agent channel; the HTTP API only triggers.

## Naming conventions

| Item | Convention | Example |
|---|---|---|
| Product | Gotham | — |
| Control-plane binary / CLI | `gotham` | `gotham serve`, `gotham update` |
| Node agent binary | `gotham-agent` | `bin/gotham-agent` |
| Go module | `github.com/<org>/gotham` | — |
| Config file / env prefix | `gotham.yaml` / `GOTHAM_` | `GOTHAM_LOG_LEVEL` |
| Container / volume prefix | `gotham-` | `gotham-traefik`, `gotham-db-{id}` |
| Image tags (built apps) | `gotham/{appID}:{deployID}` | — |
| Systemd units | `gotham.service`, `gotham-agent.service` | `deploy/` |

---

## Environment variables (selected)

Beyond the `GOTHAM_*` configuration keys loaded by viper, the control plane reads
a few operational knobs directly from the environment:

| Variable | Default | Purpose |
|---|---|---|
| `FEATURE_APPLICATIONS` | on | Mount the applications/deploy surface. |
| `FEATURE_DATABASES` | on | Mount the databases and backup surface. |
| `FEATURE_SERVICES` | on | Mount the compose-services and template surface. |
| `FEATURE_PREVIEWS` | on | Handle `pull_request` deliveries: deploy each PR as a sibling application under the base domain and remove it when the PR closes. Previews copy plain env vars only — sealed secrets and storages stay with the base application — and fork pull requests are never previewed. Off: PR deliveries are acknowledged without action, the preview listing route is unmounted and the orphan sweep does not run; push deliveries are unaffected. Hooks installed before the feature (or with it off) subscribe to `push` only and must be deleted and re-installed to receive `pull_request` events. |
| `FEATURE_PROXY` | on | Mount the Traefik/SSL/redirect surface. |
| `FEATURE_TEAMS` | on | Mount the teams and invites routes. Off: every request resolves to the caller's personal team and the team-management routes are unmounted. |
| `PLATFORM_ADMINS` | unset | Comma-separated account emails allowed to use the **platform-global** proxy operations (node-wide `POST /v1/proxy/sync`, DNS-provider CRUD) with a session. Unset denies every session; an admin-scoped API token always passes. **Operators must list themselves here** (or mint an admin-scoped token) to manage global DNS providers — per-application certificate and redirect management stays with the owning team. |

---

## 4. Development Plan

Development is phased — one file per phase in `docs/plan/`. See
[`docs/README.md`](docs/README.md) for the plan index with per-phase status,
[`docs/process.md`](docs/process.md) for how we work, and
[`docs/TODO.md`](docs/TODO.md) for what is left.