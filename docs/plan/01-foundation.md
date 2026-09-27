# Phase 0 — Gotham Foundation (W1)

**Goal:** set up the Gotham repo foundation per the `README.md` structure, so all later phases can develop in parallel.

**Exit criteria (Milestone M0):**
- [ ] `make build` produces 2 binaries: `bin/gotham`, `bin/gotham-agent` (agent is a stub, see task 0.1).
- [ ] `./bin/gotham serve` → `GET /healthz` returns 200 (DB check OK).
- [ ] `make migrate` runs goose, creating the schema on local Postgres.
- [ ] UI opens: Vue SPA served from `embed.FS`.
- [ ] GitHub Actions CI: lint + test + build green.
- [ ] **Gate G0**: subagent `code-reviewer` approves the whole phase before the foundation is locked.

**Rollback:** this phase is the starting point — if anything breaks, fix directly on main (no users, no data yet). No rollback mechanism needed.

**Phase gate:** after the exit criteria are met and Gate G0 passes, STOP and ask the project owner before continuing to Phase 1 (see Process & Phase gate in `../process.md`).

---

## INFRA-0.1 — Scaffold repo + Makefile + CI — `ws/p0-scaffold`

- **Context brief:** the repo currently only has `README.md` and `docs/plan/`. Create the full directory structure per section 3 of the README: `cmd/gotham/`, `cmd/gotham-agent/`, `internal/{server,deploy,builds,proxy,databases,services,auth,updates,store}/`, `agent/`, `proto/`, `web/`, `templates/`, `deploy/`. Each internal package gets a `doc.go` with a one-sentence responsibility description. `cmd/gotham-agent/main.go` temporarily prints the version and exits (the real agent lands in Phase 2).
- **Deliverables:**
  - `go.mod` (module `github.com/<org>/gotham`, Go 1.22), Makefile with targets: `build`, `test`, `lint`, `migrate`, `dev`.
  - `.golangci.yml`, `.gitignore`, `.editorconfig`.
  - GitHub Actions: `.github/workflows/ci.yml` (linux matrix, jobs: fmt-check, vet, test, build 2 binaries).
  - `deploy/compose.dev.yml` (Postgres 16 + Redis 7 for dev).
- **Verify:** `make build && make test && make lint` green on the dev machine; push 1 dummy PR and see CI run.
- **Depends on:** none.

## BE-0.2 — Config + logging + HTTP server — `ws/p0-config`

- **Context brief:** work on `cmd/gotham/` and `internal/server/`. Configure via viper: read `gotham.yaml` (default), then override with ENV (prefix `GOTHAM_`), support hot-reload (watch file, log on reload). Logging uses `log/slog` with JSON handler, level from config.
- **Deliverables:**
  - `internal/config/` (may temporarily live in `internal/server/config.go` if a separate package is not needed yet): config structs `Server{Addr, Port}`, `Database{DSN}`, `Redis{Addr}`, `Log{Level}`.
  - Chi router with middleware: request log, recover, request ID; route `GET /healthz` (ping DB + Redis, return JSON).
  - `cmd/gotham/main.go`: read config → init logger → serve (graceful shutdown on SIGTERM/SIGINT).
- **Verify:** `go run ./cmd/gotham serve` → `curl localhost:8000/healthz` returns 200; change the level in config + send SIGHUP → log level changes.
- **Depends on:** INFRA-0.1.

## BE-0.3 — Store: goose + sqlc + pgx — `ws/p0-store`

- **Context brief:** the persistence foundation for the whole project. Use goose (migrations embedded into the binary via `embed.FS`), sqlc for type-safe generated queries, pgx pool. Convention: all repositories live in `internal/store/`, one query file `<table>.sql` per table in `internal/store/queries/`.
- **Deliverables:**
  - `internal/store/migrations/` + first migration: `schema_migrations_meta` table (or let goose manage itself); minimal `users` table (id uuid, email, created_at) — full schema lands in Phase 1.
  - `sqlc.yaml`, `internal/store/sqlc/` (generated), pool wrapper with retry + timeout.
  - `make migrate` (up/down/status) using the embedded goose library.
  - A `Store` interface aggregating the child repositories.
- **Verify:** `make migrate` on local Postgres → `sqlc generate` shows no diff; integration test pings the DB.
- **Depends on:** BE-0.2 (config for the DSN).

## FE-0.4 — Scaffold Vue 3 + embed into binary — `ws/p0-web`

- **Context brief:** Vue 3 + Vite + TypeScript + Naive UI + Pinia + axios SPA in `web/`. The build output is embedded by Go via `embed.FS` and served by Gotham (SPA routes fall back to `index.html`).
- **Deliverables:**
  - `web/` scaffold: Vite config, strict TS, router, Pinia, Naive UI provider, basic layout (sidebar + topbar), empty "Dashboard" page.
  - axios instance with baseURL `/api/v1` + error interceptor (preparation for Phase 1).
  - `internal/server/spa.go`: embed + handler; `/*` route falls back to index.html; dev mode (env `WEB_DEV=1`) proxies to the Vite dev server.
- **Verify:** `make build` → run the binary, open the UI and see the layout; `cd web && npm run build && npm run type-check` clean.
- **Depends on:** BE-0.2 (for the server routes). Fully parallel with BE-0.3.
