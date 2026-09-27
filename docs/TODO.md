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
- [x] Phase 5 residuals — live backup/restore smoke, chunk-scale backup e2e, S3 smoke (backups LOWs remain, see below)
- [ ] Phase 6 — Proxy, Domains & SSL (BE-6.1 paused on `feat/p6-traefik`; BE-6.2 and FE-6.1 not started)
- [ ] Phase 7 — Services & Templates
- [ ] Phase 8 — Advanced
- [ ] Phase 9 — Self-update & Release (gate G2)
- [x] Phase 11 — UI Alignment side track

## Phase 5 residuals

- [x] Live smoke: create a managed Postgres via the API, local backup, drop a
  table, restore, verify rows (BE-5.2 exit criterion). Proven on the
  disposable local stack: `internal/e2e/p5_backup_test.go`
  (`TestP5BackupLocalRoundTrip`), 200 rows, identical md5 checksum before and
  after (run5: `200:3dfbedd249eae27832cc4181ee42a6c7`).
- [x] Backup e2e for the review finding class: artifact larger than one staging
  chunk (> ~97 KB) through the real Docker path. Proven: 473 411-byte artifact
  over six 90 000-byte staging chunks, 400-row checksum
  `400:ee0967651ce32132e90861116d721f43` restored identical.
- [x] S3 target smoke against MinIO (endpoint must carry `http://` explicitly —
  scheme-less endpoints default to TLS). Proven: target check ok, object
  `databases/<db>/<backup>.dump.gz` (1629 bytes) verified in the bucket,
  restore reproduced `150:536d8ea0f682d75eff6602e3ce38e970`; the smoke also
  found and fixed three latent BE-5.2 path defects (binary dump bytes mangled
  by the Docker log driver, restore staging not base64-decoding the chunk,
  `pg_restore -` opened as a file name).
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
