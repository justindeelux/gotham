# Gotham TODO

Remaining work, checkbox by checkbox. Process: [`process.md`](process.md).
Plan: [`plan/00-roadmap.md`](plan/00-roadmap.md).

## Phases

- [x] Phase 0 — Foundation (PRs #1–#5)
- [x] Phase 1 — Auth & Users (PRs #6–#9)
- [x] Phase 2 — Server + Agent (PRs #10–#14)
- [x] Phase 3 — Docker Engine Core (PRs #15–#30)
- [x] Phase 4 — Applications (PRs #31–#50; gate G1 passed with waivers)
- [x] Phase 5 core — Database engines + CRUD (#39), databases UI (#43), backups + S3 + restore (#46), backups UI (#50)
- [ ] Phase 5 residuals — live backup/restore smoke, chunk-scale backup e2e, S3 smoke, backups LOWs (see below)
- [ ] Phase 6 — Proxy, Domains & SSL (BE-6.1 paused on `feat/p6-traefik`; BE-6.2 and FE-6.1 not started)
- [ ] Phase 7 — Services & Templates
- [ ] Phase 8 — Advanced
- [ ] Phase 9 — Self-update & Release (gate G2)
- [x] Phase 11 — UI Alignment side track

## Phase 5 residuals

- [ ] Live smoke: create a managed Postgres via the API, local backup, drop a
  table, restore, verify rows (BE-5.2 exit criterion).
- [ ] Backup e2e for the review finding class: artifact larger than one staging
  chunk (> ~97 KB) through the real Docker path.
- [ ] S3 target smoke against MinIO (endpoint must carry `http://` explicitly —
  scheme-less endpoints default to TLS).
- [ ] Backups deferred LOWs: explicit "clear target" semantics on schedule
  update; split `backup_service.go`; require credentials on the update path
  too when switching a target to s3.

## Phase 6

- [ ] **BE-6.1 resume** (`feat/p6-traefik` @ `884b1ed`, no PR): sqlc generate +
  store wrapper, agent RPC handler, control-plane sync + `gotham-traefik`
  bootstrap, `server.go` wiring, tests, then PR.
- [ ] BE-6.2 SSL: Let's Encrypt HTTP-01 + DNS-01 (Cloudflare + one more),
  `dns_providers` table, acme generator.
- [ ] FE-6.1 domains UI: domain editor in app detail, redirects, cert status,
  DNS provider settings.

## Phase 4 residuals

- [ ] Wire webhook lifecycle into applications CRUD (apps do not
  create/delete provider hooks automatically yet).
- [ ] Deploy keys FE: decide whether the FE should send an SSH clone URL
  instead of relying on the https → ssh rewrite; cover a private-repo deploy
  e2e.
- [ ] GitHub OAuth manual verification (blocked on owner credentials).
- [ ] Second-node SSH validation e2e on real hardware.

## Cross-cutting

- [ ] GitHub required status checks: enable manually in repo settings if
  enforcement is wanted (API returns 403 on this plan).
- [ ] GitNexus re-index before larger refactors; stats drift shows up in
  `AGENTS.md`/`CLAUDE.md`.
- [ ] E2E hygiene: dangling images from builds; skip-as-green when
  Docker/Postgres are absent.
