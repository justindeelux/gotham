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
| Self-update | minio/selfupdate + Ed25519 signing | GitHub Releases as the update channel |
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
│   ├── updates/              # self-update & remote agent update
│   └── store/                # persistence (sqlc repositories)
├── agent/                    # agent implementation (must not import `internal/`)
├── proto/                    # protobuf contracts (buf-managed)
├── web/                      # Vue 3 SPA (Vite)
├── templates/                # one-click service templates (YAML)
├── deploy/                   # install scripts, systemd units, compose
└── docs/
    └── plan/                 # development plan per phase (see below)
```

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

## 4. Development Plan

Development is split into phases, one file per phase in [`docs/plan/`](docs/plan/) — each file contains: goal, exit criteria (milestone), work package list for subagents (orca workspace), dependencies, and rollback.

| Phase | File | Week* |
|---|---|---|
| Overall roadmap | [docs/plan/00-roadmap.md](docs/plan/00-roadmap.md) | — |
| 0 — Foundation | [docs/plan/01-foundation.md](docs/plan/01-foundation.md) | W1 |
| 1 — Auth & Users | [docs/plan/02-auth-users.md](docs/plan/02-auth-users.md) | W2 |
| 2 — Server + Agent | [docs/plan/03-server-agent.md](docs/plan/03-server-agent.md) | W3–W4 |
| 3 — Docker Engine Core | [docs/plan/04-docker-core.md](docs/plan/04-docker-core.md) | W5 |
| 4 — Applications | [docs/plan/05-applications.md](docs/plan/05-applications.md) | W6–W8 |
| 5 — Databases & Backups | [docs/plan/06-databases-backups.md](docs/plan/06-databases-backups.md) | W8–W9 |
| 6 — Proxy, Domains & SSL | [docs/plan/07-proxy-domains.md](docs/plan/07-proxy-domains.md) | W8–W9 |
| 7 — Services & Templates | [docs/plan/08-services-templates.md](docs/plan/08-services-templates.md) | W10 |
| 8 — Advanced | [docs/plan/09-advanced.md](docs/plan/09-advanced.md) | W11–W12 |
| 9 — Self-update & Release | [docs/plan/10-self-update-release.md](docs/plan/10-self-update-release.md) | W13 |

\* Relative week (dev-week) from the start, assuming 1 coordinator + 3–4 subagents running in parallel in an orca workspace.

Review gates: end of Phases 0, 4, 9 require subagent review (`code-reviewer` + `e2e-runner` / `security-reviewer`, review model `deepseek v4 pro (deepseek)`) before merge.

Phase gate: after each phase, stop and ask the project owner before continuing. See [`docs/plan/00-roadmap.md`](docs/plan/00-roadmap.md).