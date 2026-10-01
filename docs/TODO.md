# Gotham TODO

Remaining work, checkbox by checkbox. Workflow, invariants and gates: [`../AGENTS.md`](../AGENTS.md).
Plan: [`plan/00-roadmap.md`](plan/00-roadmap.md).

## Phases

- [x] Phase 0 — Foundation (PRs #1–#5)
- [x] Phase 1 — Auth & Users (PRs #6–#9)
- [x] Phase 2 — Server + Agent (PRs #10–#14)
- [x] Phase 3 — Docker Engine Core (PRs #15–#30)
- [x] Phase 4 — Applications (PRs #31–#50; gate G1 passed with waivers)
- [x] Phase 5 core — Database engines + CRUD (#39), databases UI (#43), backups + S3 + restore (#46), backups UI (#50)
- [x] Phase 5 residuals — closed: live smoke, chunk-scale e2e and S3 smoke proven;
  backup LOWs reviewed and merged in PR #54 (`3705260`). Umbrella item done.
- [x] Phase 6 — Proxy, Domains & SSL (BE-6.1 merged #51/#53; BE-6.2 SSL merged #55 at `e0ec802`; real DNS-01 issuance merged #56 at `2788a4e`; FE-6.1 domains UI merged #59 at `4199cca`; BE-6.3 redirects + cert status merged #60 at `15f3da9`; FE follow-up merged #61 at `7ccda27`)
- [x] Phase 7 — Services & Templates (BE-7.1 compose services merged `9832466`; BE-7.2 template engine merged `9fb7741`; FE-7.1 services UI + gallery merged `a7d3340`)
- [x] Phase 8 — Advanced (BE-8.2 teams & roles `8ecdc2c`; BE-8.4 server metrics `cd3840b`; BE-8.3 notifications `8e0f1cb`; BE-8.1 preview deployments `eea683f`; FE-8.1 combined UI `7ba71d3` — all merged; CI on the self-hosted runner)
- [x] Phase 9 — Self-update & Release (gate G2 approved with conditions): BE-9.1 control-plane self-update merged `390a2fa` (PR #71); BE-9.2 agent remote update merged `ae78888` (PR #72); INFRA-9.1 release pipeline + signed installers merged `9a6268d` (PR #73); install hardening merged `e99ef49` (PR #74); G2 conditions closed: e2e determinism (#79 `8928b49`), agent channel TLS by default + release-environment gating + supply-chain pins (#78 `d716734`), download budget + CLI ownership + CP backoff + wrapper health gate (#77 `4220931`), docs/UI + residual register (#80 `0deee90`); the real newer-release `gotham update` + AUTO_UPDATE exercise and the real agent rollout from the GitHub CDN are proven (M9 evidence below); code residuals closed 2026-10-01 (#92, #93, #96)
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
- [x] Backups deferred LOWs: explicit "clear target" semantics on schedule
  update; split `backup_service.go`; require credentials on the update path
  too when switching a target to s3. Closed in PR #54 (`2a89a23` split,
  `bde5ef1` tri-state clear + s3 credential parity); verified on `main`
  2026-10-01 against the review.

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

- [x] **M4 beta-channel binding.** Closed in PR #93 (`19798cf`): offers are
  labelled with the release's channel (stable/prerelease) and the agent applies
  channel eligibility (stable accepted by every configured channel, beta only by
  beta/unset); the offer→signed-manifest binding stays fail-closed. Regression
  covers beta+stable, beta+prerelease, stable-subscriber and mismatch cases.
- [ ] **LOW-4 agent channel not mutual.** The gRPC listener accepts any peer
  (`VerifyClientCertIfGiven`); mutual TLS waits on the registration bootstrap
  acquiring a client credential. No bootstrap credential exists yet — the
  remaining owner/design decision, not a polishing task.
- [x] **Release key rotation/revocation.** Closed in PR #92 (`0e63090`): a
  current + next key ring is embedded in both binaries, verification is
  fail-closed across the set, the release workflow validates/embeds the optional
  next key for every binary, and the operator runbook is
  `deploy/release-key-rotation.md` (ship-before-needed, promotion, emergency
  recovery, installer pins).
- [x] **Shared release runner / key custody.** Resolved: the release job and PR
  CI now run on GitHub-hosted runners, and the signing key stays in the
  approval-gated `release` environment, so untrusted `pull_request` code never
  shares a host with it. The reviewer gate still governs who can build a signed
  release — keep the `v*` tag ruleset and the reviewer list to project owners.
- [x] **I5 `release-verify.sh` temp dir on signal.** Closed in PR #93
  (`19798cf`): the verifier installs EXIT/INT/TERM/HUP cleanup for the duration
  of one verification, chains and restores the caller's traps, and never clobbers
  them; the installer chain test passes under both `sh` and `dash`.
- [x] **`GOTHAM_UPDATE_CURRENT` pin.** Closed in PR #93 (`19798cf`): builds
  with an embedded release key ignore the override (with a warning) and use the
  node-registry version; keyless dev/verify builds keep it. Nothing to set in
  production any more.
- [x] **M9 partial evidence.** The real agent rollout against the GitHub CDN is
  proven — see below.

- [x] **BE-9.1 chain residuals (carried).** Closed in PR #96 (`bdbe862`): the
  cloner now runs `git` in its own process group with Linux `Pdeathsig`, so CP
  `git`/`ssh` children die with the service while the wrapper-survival property
  is unchanged (cross-built, tested); the wrapper lock check-then-open TOCTOU is
  closed with an inode-verified pin (a swap fails closed with `wrapper_failed`,
  no root hang); the theoretical root `mv -T` race and the wrapper-death-during-
  health-window case are documented as non-issues (root-owned-directory
  ownership floor / deliberate fail-closed staging). Two review LOW nits remain
  tracked: `deploy/README.md` staged-outcome wording and a SIGKILL-left stale
  `.update-lock.<pid>` pin (degrades to the documented racy open on PID reuse);
  a follow-up fix round is in flight.

### M9 evidence (Phase 9)

Proven:

- Signed `v0.1.0` and `v0.1.1` GitHub releases, each with exactly 14 assets; all four
  `gotham{,-agent}-manifest-{amd64,arm64}.txt` verify with the pinned key, and the
  GitHub-reported digests equal the signed `sha256=` values.
- Clean Ubuntu 22.04 container install of the control plane, plus re-install with no
  operator additions; the signed manifest/digest chain is covered by
  `deploy/test-release-install.sh`.
- Real two-agent systemd remote-update proof (`deploy/verify-agent-update.sh` C1–C4
  plus NEG1/NEG2: a tampered asset and a broken-but-signed release both refuse and roll
  back).
- **Real newer-release self-update (2026-10-01, container):** `gotham update check`
  reported `v0.1.1 available (current 0.1.0, channel stable)`; `update apply` staged
  v0.1.1, the root wrapper health-checked it and recorded `result=ok
  detail=new binary healthy`; the CP ran **0.1.1** with the new `gotham ca` command
  present; `update rollback` plus the documented restart brought back **0.1.0**; a
  re-apply returned to **0.1.1**. `AUTO_UPDATE=true` (10 s interval) was enabled and the
  loop logged `auto-update enabled` with the durable status, then reverted.

- **Real agent release rollout against the GitHub CDN (2026-10-01, `linux4857`):**
  the released **v0.1.0 agent** was installed through the signed-manifest installer, the
  operator API (`POST /api/v1/servers/agents/update-all`) queued **v0.1.1**, and the agent
  polled, downloaded from
  `github.com/justindeelux/gotham/releases/download/v0.1.1`, verified the signed manifest
  with its embedded key, swapped through the root wrapper and re-registered; the wrapper
  recorded `result=ok`, `version=v0.1.1`, `detail=new binary healthy`, and the registry
  then reported `version: 0.1.1` with `rollout_version: v0.1.1`.

## Phase 8 residuals

- [ ] **Teams:** legacy `servers.team_id IS NULL` nodes, their containers and logs
  stay shared with every authenticated caller; a pre-fix self-minted admin-scoped
  API token would need rotation (none shipped); global DNS/proxy management needs
  `PLATFORM_ADMINS` (or an admin-role session / admin-scoped token).
- [ ] **Metrics:** the Linux `/proc` reads run in Linux CI only; the chart
  stress/workload criterion and production-scale index performance were not
  exercised locally; the container/bridge interface double-count is a documented
  ceiling.
- [ ] **Notifications:** a real Discord/Slack/Telegram/SMTP endpoint was never
  contacted (mock servers + a fake mailer only); invite email delivery stays a stub;
  global proxy/DNS routes are operator-gated.
- [x] **Previews (code follow-ups).** Closed in PR #94 (`8fc69ee`, fix round
  `5a1202f`): the no-binding-close race is fenced even when the ledger clear fails
  (F-1), `UpsertPreviewDeploy` is serialized under the application lock with a
  discriminating regression (F-2), terminal deploy outcomes update the PR badge
  comment, and the base wildcard certificate intent is cloned onto the sibling so
  previews are served over HTTPS.
- [ ] **Previews (live-environment evidence):** a real Git-host PR/comment exchange
  and real wildcard issuance were not exercised; run them against a live provider when
  owner credentials/infra allow.
- [ ] **Combined UI:** per-channel event editing, per-resource notification
  overrides and metric auto-refresh are not exposed; the mockups' full permission
  matrix / member fields and the plan's chart-library choices were intentionally
  adapted (dependency-free SVG chart, lean footprint).

## Phase 4 residuals

- [ ] Wire webhook lifecycle into applications CRUD (apps do not
  create/delete provider hooks automatically yet).
- [ ] Deploy keys FE: decide whether the FE should send an SSH clone URL
  instead of relying on the https → ssh rewrite; cover a private-repo deploy
  e2e.
- [ ] GitHub OAuth manual verification (blocked on owner credentials).
- [ ] Second-node SSH validation e2e on real hardware.

## Cross-cutting

- [x] Go 1.27 toolchain: `go.mod` `go 1.27`, CI pins `1.27.x`, golangci-lint
  `v2.14.0` (built with Go 1.27; older linters refuse a 1.27 target). Supersedes
  the closed Dependabot bumps #75 (grpc) and #76 (x/crypto), which required
  Go 1.25.
- [ ] GitHub required status checks: enable manually in repo settings if
  enforcement is wanted (API returns 403 on this plan).
- [ ] GitNexus re-index before larger refactors; stats drift shows up in
  `AGENTS.md`/`CLAUDE.md`.
- [x] E2E hygiene: closed in PR #95 (`3572b0a`): opted-in runs (`GOTHAM_E2E=1`)
  fail fast when Docker/Postgres/Redis are absent instead of skipping green,
  deliberate opt-ins (DNS-01) still skip, and build images are cleaned up on
  success and failure; the shared-daemon dangling-sweep caveat is documented
  in-code.
