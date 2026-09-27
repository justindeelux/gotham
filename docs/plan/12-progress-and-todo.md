# Gotham — Progress & TODO (snapshot 2026-09-27)

Single source of truth for where the project stands and what is left. Written
at the owner's request when the Phase 4/5 workstreams were wrapped up and the
Phase 6 package was paused mid-implementation. Update this file at every phase
boundary; keep the roadmap (`00-roadmap.md`) as the plan and this file as the
status.

## 1. Snapshot

| Item | Value |
|---|---|
| `main` | `5a7c6af` (PR #50), CI green |
| Open PRs | none |
| Latest migration | `00011_backups.sql` (00009 reserved for Traefik, 00010 deploy keys) |
| Test server | `http://103.176.22.225:8000` — demo@gotham.dev / GothamDemo123, node `test-node-1` ready |
| Paused branch | `feat/p6-traefik` @ `884b1ed` (`wip(p6)`, pushed, no PR) |
| GitNexus | indexed as `gotham`; stats live in `AGENTS.md` / `CLAUDE.md` |

## 2. Phase status

| Phase | Status | Notes |
|---|---|---|
| 0 Foundation | ✅ done | PRs #1–#5 (scaffold, config, store, SPA, CI) |
| 1 Auth | ✅ done | PRs #6–#9 (JWT, GitHub OAuth, API tokens, auth UI) |
| 2 Server + agent | ✅ done | PRs #10–#14 (proto, agent, gateway/registry, servers UI, CSR certs) |
| 3 Docker core | ✅ done | PRs #15–#30 (dial-out client, realtime WS, containers service/UI, LogViewer, M3 e2e #29) |
| 4 Applications | ✅ done | PRs #31–#50, see §3; gate G1 passed with two owner-approved waivers (§6) |
| 5 Databases & backups | 🟡 core done | BE-5.1 #39, FE-5.1a #43, BE-5.2 #46, FE-5.1b #50; live backup/restore smoke still open (§5) |
| 6 Proxy & domains | 🟡 paused | BE-6.1 paused mid-way (`feat/p6-traefik`); BE-6.2 SSL and FE-6.1 domains UI not started |
| 7 Services/templates | ⚪ not started | needs Phase 4 + 6 |
| 8 Advanced | ⚪ not started | previews, teams, notifications, metrics |
| 9 Self-update/release | ⚪ not started | gate G2 |
| UI-11 alignment | ✅ done | tokens #16, auth #19/#23, shell #20/#22, dashboard #26, servers #25/#27/#28/#30, copy #34, plus FE-4.1/FE-5.1 pages |

## 3. Phase 4 work packages (all merged)

| Package | PR | What landed |
|---|---|---|
| BE-4.1 source providers | #32, #33 | GitHub/GitLab/Gitea OAuth connections, repos/branches |
| BE-4.2 build engines | #31, #35, #36 | dockerfile/static/railpack/buildpacks engines; agent `BuildImage` + node registry bootstrap |
| BE-4.3 deploy orchestration ⭐ | #37 | state machine, worker pool, retry, rollback, sealed secrets, cloner allow-list; boot sweep + `Close()` + dev clone gate after review |
| BE-4.3b applications CRUD | #41 | create/list/get/update/delete, env/storage collections, manual stop/start |
| BE-4.3c PORT injection | #48 | `PORT` defaults to the app port when unset (explicit env/secret wins) |
| BE-4.4 webhooks | #40 | signed deliveries, commit-SHA dedupe, rate limit, hook lifecycle, `RemoveContainer`-adjacent work |
| BE-4.4b SSH deploy keys | #47 | ed25519 per app, provider add/remove, `GIT_SSH_COMMAND` clone, 409 on duplicates |
| FE-4.1 / FE-4.1b | #38, #42 | applications wizard, detail, deploy logs, rollback/redeploy, stop/start, env/storage editors |
| QA-4.1 API e2e gate | #45 | 4 scenarios on a real stack, dedicated `e2e.yml`, owner-approved waivers |
| QA-4.1b Playwright UI leg | #49 | auth + navigation + seeded application, `ui-e2e.yml` |
| Fixes | #44 | agent serves plaintext DockerService in dev mode (no CA) — unblocked all agent RPCs |

## 4. Live test server — verified on real hardware

All-in-one Ubuntu box: CP + Postgres 16 + Redis 7 + agent (`gotham.service`,
`gotham-agent.service`), deploy via rsync → `make migrate && make build` →
restart.

Environment prerequisites installed for the current feature set:

- `railpack` 0.40 on PATH and a `buildkit` container with
  `BUILDKIT_HOST=docker-container://buildkit` in
  `/etc/systemd/system/gotham.service.d/buildkit.conf` (Railpack apps).
- `servers.ip = 127.0.0.1` for `test-node-1` (the all-in-one registration has
  no operator address; the CP dials the agent on the node port 9443).
- Dev mode: no CA configured, so CP and agent speak plaintext (PR #44).

Verified live (beyond CI):

- Deploy `docker/welcome-to-docker` (Dockerfile) → build on the agent →
  container running → HTTP 200 via host port; update + redeploy; rollback →
  previous image runs → HTTP 200; manual stop/start.
- Deploy `heroku/node-js-getting-started` (no Dockerfile, Railpack) →
  running → HTTP 200; `PORT=3000` auto-injected (BE-4.3c); env vars applied;
  persistent `/data` mount present.
- Containers list through the agent (`/api/v1/servers/{id}/containers`).
- Migrations `00010_deploy_keys`, `00011_backups` applied; new routes mounted
  (`/api/v1/databases/backup-targets` → 401 unauthenticated).

Not yet verified live: webhook delivery end-to-end (needs a provider
connection or a seeded row), managed database create/backup/restore, S3
targets, Traefik/domains, GitHub OAuth (owner credentials).

## 5. TODO

### Phase 5 residuals

1. Live smoke: create a managed Postgres via the API, local backup, drop a
   table, restore, verify rows (BE-5.2 exit criterion).
2. Backup e2e for the review finding class: artifact larger than one staging
   chunk (> ~97 KB) through the real Docker path (unit bound test exists;
   Docker-gated e2e recommended by the BE-5.2 review).
3. S3 target smoke against MinIO (endpoint must carry `http://` explicitly —
   scheme-less endpoints now default to TLS).
4. Backups deferred LOWs: explicit "clear target" semantics on schedule
   update; split `backup_service.go` (≈1.3k lines); require credentials on the
   update path too when switching a target to s3.

### Phase 6

5. **BE-6.1 resume** (`feat/p6-traefik` @ `884b1ed`, no PR): the generator
   (`internal/proxy/{model,build,generate}.go`), `proxy.sql` query and the
   agent `ProxyService.WriteProxyConfig` proto are done. Remaining: sqlc
   generate + store wrapper, agent RPC handler (write files under the config
   dir, reject path escapes, ping Traefik), control-plane sync (applications
   with `base_domain` → generate → write) and bootstrap of the
   `gotham-traefik` container on server validation, `server.go` wiring,
   tests, then PR. Keep migration `00009` reserved for the domains table if
   one is needed.
6. BE-6.2 SSL: Let's Encrypt HTTP-01 + DNS-01 (Cloudflare + one more),
   `dns_providers` table, acme generator.
7. FE-6.1 domains UI: domain editor in app detail, redirects, cert status,
   DNS provider settings.

### Phase 4 residuals

8. Wire webhook lifecycle into applications CRUD: BE-4.4 exposes
   `webhooks.Service.CreateWebhook/DeleteWebhook`, but the CRUD path does not
   call it yet (apps currently do not create/delete hooks automatically).
9. Deploy keys FE: the cloner rewrites `https://…` → `ssh://git@…` when a key
   exists; decide whether the FE should send an SSH clone URL instead, and
   cover a private-repo deploy e2e.
10. GitHub OAuth manual verification (blocked on owner credentials), plus a
    real provider repo list/branch check.
11. Second-node SSH validation e2e on real hardware (test box has capacity).

### Phases 7–9

12. Phase 7 services/templates: compose services + template gallery
    (`templates/`), depends on Phase 4 + 6.
13. Phase 8 advanced: previews, teams, notifications, metrics.
14. Phase 9 self-update + release: Ed25519 signing, GitHub Releases, gate G2.

### Cross-cutting

15. GitHub required status checks: the free private plan hides branch
    protection via the API (403); the E2E and UI-E2E workflows run but cannot
    be proven "required" — enable manually in repo settings if enforcement is
    wanted.
16. GitNexus re-index before larger refactors (`node .gitnexus/run.cjs
    analyze`); stats drift shows up in `AGENTS.md`/`CLAUDE.md`.
17. E2E hygiene follow-ups from the G1 review: dangling images from builds,
    skip-as-green when Docker/Postgres are absent (workflow has explicit
    preflight steps).

## 6. Decisions & waivers (owner-approved)

- Continuous phase mode: no STOP between phases; STOP only when blocked, a
  decision is needed, or a review gate fails (`00-roadmap.md`).
- Model policy: review `openrouter/z-ai/glm-5.3-prime`; implementation
  `opencode-go/mimo-v2.6-flash` (Go), `opencode-go/deepseek-v4.1-flash`
  (spec-driven), `opencode-go/muse-spark-1.3-contributor` (Vue/UI).
- G1 waivers (2026-09-27): Playwright UI leg deferred to QA-4.1b (now merged
  as #49); network clone of a real public repo is verified live on the test
  box instead of in CI (fixtures keep the gate deterministic).
- Accepted deviations (carried from earlier phases): stateless JWT stays valid
  after logout; `internal/servers` domain package; CSR-based agent cert flow;
  no plaintext token logging; dev-mode plaintext agent transport when no CA.
- Migration numbering: `00009` reserved for Traefik/domains, `00010` deploy
  keys, `00011` backups. Never reuse a number.

## 7. How to resume

Toolchain and repo:

```bash
export PATH=/usr/local/go/bin:$PATH        # Go 1.25.x local; CI runs 1.22.x
cd /Users/ndtpro/Projects/Tools/gotham
git pull --ff-only
```

Test server:

```bash
rsync -az --delete --exclude '.git' --exclude 'web/node_modules' \
  --exclude 'bin' --exclude 'data' ./ gotham:/root/gotham/
ssh gotham 'cd /root/gotham && export PATH=/usr/local/go/bin:$PATH && \
  make migrate && make build && systemctl restart gotham gotham-agent'
```

Gates and invariants per work package (see `AGENTS.md` and
`00-roadmap.md`): `go build ./...`, `go test ./...`, `golangci-lint run`,
`go vet`, `make sqlc-generate` drift, forward-only migrations, `npm run
type-check && npm run build` + webdist drift, no cross-scope imports. Run
GitNexus `impact` before touching existing symbols and `detect_changes` before
committing. One work package = one orca worktree = one PR; merge after CI
green (and review for design-sensitive packages).

E2E suites:

```bash
GOTHAM_E2E=1 go test ./internal/e2e/... -count=1 -timeout 20m -v   # API leg
cd web && npm run e2e                                              # Playwright leg
```
