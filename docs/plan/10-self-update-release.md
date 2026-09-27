# Phase 9 — Self-update & Release (W13)

**Goal:** signed automatic updates + the official release pipeline — completing design priority #3 of the project.

**Exit criteria (Milestone M9):**
- [ ] `gotham update` downloads the new version from GitHub Releases, verifies the Ed25519 signature, replaces the binary itself, rolls back to the old version on failure.
- [ ] Agents self-update remotely via the CP (new binary pushed over gRPC).
- [ ] GoReleaser publishes: linux amd64/arm64 binaries, checksums, signatures; a one-line install script sets up a clean VPS.
- [ ] **Gate G2**: `code-reviewer` + `security-reviewer` approve the whole update chain (signatures, distribution channel, runtime privileges).

**Rollback:** rollback IS the feature — the old binary is kept as `<binary>.old`, `update rollback` switches back. If a release is broken: publish a fixed release, users update again; no extra mechanism needed.

**Phase gate:** after the exit criteria are met and Gate G2 passes, STOP and report to the project owner for final acceptance (see Process & Phase gate in `../process.md`).

---

## BE-9.1 — Self-update CP — `ws/p9-update`

- **Context brief:** use `minio/selfupdate` + Ed25519 signatures. Flow: query the GitHub Releases API (`stable`/`beta` channels) → download binary → verify signature with the public key **embedded in the binary** → selfupdate (swap file, keep `.old`) → restart service. Must work when Gotham is installed via systemd (`deploy/gotham.service`). A "check update" button in the UI + `AUTO_UPDATE=true` env.
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

## INFRA-9.1 — Release pipeline — `ws/p9-release`

- **Context brief:** GoReleaser: build 2 binaries (gotham, gotham-agent) × linux amd64/arm64, generate checksums, sign with Ed25519 (using `cmd/signer`), attach to the GitHub Release. Install script `deploy/install.sh`: download the right-arch build → verify checksum + signature → install systemd → open the web UI. Install + update guide docs.
- **Deliverables:** `.goreleaser.yaml`, `release.yml` workflow (runs on `v*` tags), `deploy/install.sh`, guides (in README or `docs/install.md`).
- **Verify:** tag `v0.1.0` on the test repo → release has all assets → run the install script on a clean VPS (Ubuntu 22.04) → CP runs + login works.
- **Depends on:** BE-9.1 (signer), Phase 1 (login UI for testing).
