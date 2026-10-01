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
- [ ] Phase 5 residuals — live smoke, chunk-scale e2e and S3 smoke are proven (below); the deferred backups LOWs keep this umbrella open
- [ ] Phase 6 — Proxy, Domains & SSL (BE-6.1 #51 and BE-6.2 #55/#56 merged; FE-6.1 in review; phase gate pending)
- [ ] Phase 7 — Services & Templates
- [ ] Phase 8 — Advanced
- [ ] Phase 9 — Self-update & Release (gate G2 approved with conditions; residuals below)
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

- [x] BE-6.1 Traefik file provider: control-plane sync, `gotham-traefik`
  bootstrap and the agent proxy RPC (#51).
- [x] BE-6.2 SSL: `dns_providers` + certificate intents, HTTP-01/DNS-01
  resolvers, HTTPS activation (#55, #56).
- [x] FE-6.1 domains UI: `/domains` page (DNS provider + certificate CRUD),
  domain editor in app detail, honest backend-pending stubs for router list,
  redirects and cert status/expiry (in review, `feat/p6-domains-ui`).

## Phase 9 residuals

Gate G2 signed off with conditions; the update/release product logic is merged
(`feat/p9-g2-update`, `feat/p9-g2-channel`). These are the tracked, non-blocking
residuals — full detail in `deploy/README.md` → Known residuals:

- [ ] **M4 beta-channel binding.** A `beta`-configured subscriber rejects a
  newer `stable` manifest (the signed manifest says `channel=stable`, the checker
  labels the offer `beta`); fix before any beta channel is offered. Availability
  only, fail-closed.
- [ ] **LOW-4 agent channel not mutual.** The gRPC listener accepts any peer
  (`VerifyClientCertIfGiven`); mutual TLS waits on the registration bootstrap
  acquiring a client credential.
- [ ] **Release key rotation/revocation.** One embedded key, no key ring: a
  compromise or loss means reinstalling the fleet. Embed a current + next key set
  and write a runbook.
- [ ] **Shared release runner / key custody (HIGH, carried).** If any untrusted
  workflow ever runs on the signing runner, treat `GOTHAM_UPDATE_SIGNING_KEY` as
  compromised — rotate the key and re-release — and move signing to an
  isolated/ephemeral runner before the next public release.
- [ ] **I5 `release-verify.sh` temp dir on signal.** Public material only; add
  `_GOTHAM_VERIFY_WORK` to the installers' `EXIT` cleanup.
- [ ] **`GOTHAM_UPDATE_CURRENT` pin.** A test-only env the server honours; do not
  set it in production.
- [ ] **M9 partial evidence.** See below.

### M9 evidence (Phase 9)

Proven today:

- Signed `v0.1.0` GitHub release with exactly 14 assets; all four
  `gotham{,-agent}-manifest-{amd64,arm64}.txt` verify with the pinned key, and
  the GitHub-reported digests equal the signed `sha256=` values.
- Clean Ubuntu 22.04 container install of the control plane, plus re-install with
  no operator additions; the signed manifest/digest chain is covered by
  `deploy/test-release-install.sh`.
- Real two-agent systemd remote-update proof (`deploy/verify-agent-update.sh`
  C1–C4 plus NEG1/NEG2: a tampered asset and a broken-but-signed release both
  refuse and roll back).

Pending (the coordinator tags `v0.1.1` next):

- Apply a real *newer* GitHub-hosted release through `gotham update apply` and
  roll back once (closes the real-release gap for the control plane).
- Apply an agent release rollout against the real CDN.
- Exercise the unattended `AUTO_UPDATE` loop.

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
