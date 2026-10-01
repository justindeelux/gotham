# Phase 9 — Self-update & Release (W13)

**Goal:** signed automatic updates + the official release pipeline — completing design priority #3 of the project.

**Exit criteria (Milestone M9):**
- `gotham update` downloads the new version from GitHub Releases, verifies the Ed25519 signature, replaces the binary itself, rolls back to the old version on failure.
- Agents self-update remotely via the CP (new binary pushed over gRPC).
- GoReleaser publishes: linux amd64/arm64 binaries, checksums, signatures; a one-line install script sets up a clean VPS.
- **Gate G2**: `code-reviewer` + `security-reviewer` approve the whole update chain (signatures, distribution channel, runtime privileges).

**Rollback:** rollback IS the feature — the old binary is kept as `<binary>.old`, `update rollback` switches back. If a release is broken: publish a fixed release, users update again; no extra mechanism needed.

**Phase gate:** after the exit criteria are met and Gate G2 passes, STOP and report to the project owner for final acceptance (see Process & Phase gate in `../process.md`).

---

## BE-9.1 — Self-update CP — `ws/p9-update`

- **Context brief:** signed self-update with a built-in atomic swap + Ed25519
  signed manifest. Flow: query the GitHub Releases API (`stable`/`beta`
  channels) → download the signed manifest → verify it with the public key
  **embedded in the binary** → download the artifact → verify its digest against
  the manifest → hardlink-swap the binary (keep `.old`) → restart service. Must
  work when Gotham is installed via systemd (`deploy/gotham.service`). A "check
  update" button in the UI + `AUTO_UPDATE=true` env.
  - **Deviation (owner-approved, fix round 1/2):** the brief named
    `minio/selfupdate`; its fixed staging path and two-rename commit interleave
    and can lose the target on a crash, so the swap is implemented in
    `internal/updates` instead (flock-serialized, unique staging, hardlink
    backup, startup recovery). The plan's intent — a signed, verifiable,
    rollback-capable swap — is met without that dependency.
  - **Deployment:** the binary lives in `/var/lib/gotham/bin/gotham`, the
    privileged wrapper is root-owned at `/usr/libexec/gotham/gotham-update`
    (argument-free, config in `/etc/gotham/updater.conf`), the authoritative
    status is root-owned under `/var/lib/gotham-updater/`, and the staged gate
    is the control-plane-owned `/var/lib/gotham/update.pending`. See
    `deploy/README.md`.
- **Deliverables:**
  - `internal/updates/`: `Checker`, `Applier`, `Signer` (dedicated `cmd/signer` CLI tool for signing releases), rollback.
  - systemd unit + safe restart script (healthcheck after update, auto-rollback on failure).
  - Routes: `GET /api/v1/updates/check`, `POST /api/v1/updates/apply`.
  - Tests: sign/verify roundtrip; fake-release e2e (local file server) → update → new version → rollback → old version.
- **Verify:** install v1.0.0 on a test VM → update to signed v1.0.1 → runs OK → rollback → v1.0.0.
- **Depends on:** Phase 0 (binary structure), shares `cmd/signer` with INFRA-9.1.

## BE-9.2 — Agent remote update — `ws/p9-agent-update`

- **Context brief:** the CP keeps an agent version map; on a new release: CP sends the `RequestUpdate` RPC (Phase 2 contract) with URL + checksum + signature → agent downloads, verifies, replaces its own binary, restarts, reports the new version. Safety: agents only update from an mTLS-authenticated CP.
- **Deliverables:** `UpdateService` implemented on both sides; "Update all agents" flow from UI/API; tests: push a fake new version → agent reports the new version after reconnect.
- **Verify:** run 2 agents (2 versions) → update all → heartbeats report the new version together.
- **Depends on:** BE-9.1 (shared verify mechanism).
- **Implementation note (BE-9.2):** the transport-agnostic verify/download/swap
  engine was extracted to a neutral top-level package `updatecore/` so `agent/`
  can reuse it without importing `internal/`; `internal/updates` keeps the CP
  release checker and HTTP surface. The CP offers updates over the agent's
  authenticated `RequestUpdate` call and an operator `update-all` endpoint
  records a rollout target that agents pick up on their next poll (the contract
  is pull-based; the request never dials a node). The channel is authenticated
  with the CA the control-plane installer provisions (`gotham ca init`); the
  agent installer requires that certificate (`--ca`) and dials TLS, so a fresh
  install never runs the agent channel in plaintext (client-certificate mTLS
  remains a follow-up).

## INFRA-9.1 — Release pipeline — `ws/p9-release`

- **Context brief:** GoReleaser: build 2 binaries (gotham, gotham-agent) × linux amd64/arm64, generate checksums, sign with Ed25519 (using `cmd/signer`), attach to the GitHub Release. Install script `deploy/install.sh`: download the right-arch build → verify checksum + signature → install systemd → open the web UI. Install + update guide docs.
- **Deliverables:** `.goreleaser.yaml`, `release.yml` workflow (runs on `v*` tags), `deploy/install.sh`, guides (in README or `docs/install.md`).
- **Verify:** tag `v0.1.0` on the test repo → release has all assets → run the install script on a clean VPS (Ubuntu 22.04) → CP runs + login works.
- **Depends on:** BE-9.1 (signer), Phase 1 (login UI for testing).

## M9 evidence

Proven today:

- Signed `v0.1.0` GitHub release with exactly 14 assets; all four
  `gotham{,-agent}-manifest-{amd64,arm64}.txt` verify with the pinned key, and
  the GitHub-reported digests equal the signed `sha256=` values.
- Clean Ubuntu 22.04 container install of the control plane, plus re-install with
  no operator additions; the signed manifest/digest chain is covered end to end
  by `deploy/test-release-install.sh`.
- Real two-agent systemd remote-update proof (`deploy/verify-agent-update.sh`
  C1–C4 plus NEG1/NEG2: a tampered asset and a broken-but-signed release both
  refuse and roll back).
- Ed25519 verify → digest → hardlink swap → health-check → rollback on the CP
  (`updatecore` unit/e2e suites and `deploy/verify-systemd.sh` on the box).

Pending (the coordinator tags `v0.1.1` next):

- Apply a real *newer* GitHub-hosted release through `gotham update apply` and
  roll back once (closes the real-release gap for the control plane).
- Apply an agent release rollout against the real CDN.
- Exercise the unattended `AUTO_UPDATE` loop.

The non-blocking residual register (M4 beta-channel binding, LOW-4 mutual agent
channel, key rotation/revocation, the shared release runner, I5, the
`GOTHAM_UPDATE_CURRENT` pin) is tracked in `docs/TODO.md` → Phase 9 residuals and
`deploy/README.md` → Known residuals.
