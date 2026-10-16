# Gotham TODO

Open work only. Workflow, invariants and gates: [`../AGENTS.md`](../AGENTS.md).
Plan: [`plans/00-roadmap.md`](plans/00-roadmap.md). Closed items, PR numbers and
merge evidence live in git history and the PRs.

## Status

All phases are complete: 0–9 (roadmap; G0, G1 and G2 passed), 11 UI alignment,
12 user profile, 13 projects and environments, 14 English/Vietnamese i18n,
15 add-resource flow and Git sources, 16 instance settings, 17 update settings,
18 remote file manager (hardening/e2e/docs done; step 9b archives built; optional step 9a chmod deferred).
The retro review of Phases 0–5 closed 2026-10-03.
Releases `v0.1.0` and `v0.1.1` are signed and published; the real `gotham update`
and the real agent rollout from the GitHub CDN are proven.

## Live verification still owed

- [ ] Git sources: real GitHub, GitLab and a private registry (tests use fakes).
- [ ] GitHub OAuth manual verification (blocked on owner credentials).
- [ ] Second-node SSH validation e2e on real hardware.
- [ ] **Previews:** a real Git-host PR/comment exchange and real wildcard issuance
  (run against a live provider when owner credentials/infra allow).
- [ ] **Notifications:** a real Discord/Slack/Telegram/SMTP endpoint was never
  contacted (mock servers + a fake mailer only); invite email delivery stays a stub.
- [ ] **Metrics:** the Linux `/proc` reads run in Linux CI only; chart
  stress/workload and production-scale index performance were not exercised
  locally; the container/bridge interface double-count is a documented ceiling.

## Security / design decisions

- [x] **LOW-4 agent channel is now mutual (JUS-194 / #327).** The TLS listener
  requests a client certificate and the gateway verifies it per RPC: the agent
  presents the node leaf issued at enrollment (`ExtKeyUsage{serverAuth,
  clientAuth}`), the identity must match the claimed node id, and the presented
  key must match the key bound to the node. Register, Heartbeat (re-checked on
  every message) and RequestUpdate all require it; removing or re-keying a node
  invalidates the credential and closes its live stream. First-ever enrollment is
  the one certificate-less registration and is gated by
  `GOTHAM_GRPC_ENROLLMENT_TOKEN` when configured. Details and the upgrade path:
  `deploy/README.md` → Known residuals.
  - The two directions are separate trust roles: a node leaf authenticates only
    to the CP gateway (bound to node-id + stored key fingerprint), and an agent
    listener accepts only the control-plane credential (`spiffe://gotham/control-plane`
    URI SAN, checked in the handshake and in a gRPC interceptor), so a node leaf
    cannot act as the CP toward another agent.
  - **Residual: open enrollment.** First enrollment fails closed: the CP uses
    `GOTHAM_GRPC_ENROLLMENT_TOKEN` or generates `<GOTHAM_CA_DIR>/enrollment-token`.
    `GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true` is the explicit, loudly-logged
    opt-in for trust-on-first-use. A per-node, single-use enrollment token
    surfaced in the add-server wizard would be the end state (future work).
  - **Residual: no CRL/serial set.** Revocation is key-based (delete the node and
    re-enroll; there is no in-place rekey) plus the 90-day leaf; a superseded leaf
    of the *same* key stays valid until it expires.
  - **Legacy-leaf sunset (implemented).** The gateway keeps accepting a pre-mTLS
    (serverAuth-only) node leaf so enrolled nodes are not bricked, but only until
    `GOTHAM_GRPC_LEGACY_LEAF_UNTIL` (default `servers.DefaultLegacyLeafDeadline`);
    serve logs the date and the number of nodes still presenting one, each accept
    is warned, and after the date the leaf is refused. The agent's matching
    rollout grace (a URI-less control-plane credential) is logged per accept and
    is bounded and non-exploitable — only the CA mints such a credential — but it
    has no date gate yet: drop it in the same release that removes the gateway
    branch, and tighten `VerifyNodeClientCert` to clientAuth-only.
  - **Per-IP limiter keying.** Register/heartbeat limits are keyed per peer IP;
    agents behind one NAT/egress IP share a bucket.
  - **Unknown-node Register rows.** A certificate-less bootstrap still creates a
    registry row, so a peer holding the enrollment token (or an open first
    enrollment) can grow `servers` within its rate limit.
- [ ] **Teams:** legacy `servers.team_id IS NULL` nodes, their containers and logs
  stay shared with every authenticated caller; a pre-fix self-minted admin-scoped
  API token would need rotation (none shipped); global DNS/proxy management needs
  `PLATFORM_ADMINS` (or an admin-role session / admin-scoped token).
- [ ] Owner-decision follow-ups from the retro register:
  `TestStoreCreateFirstUserSerializes` needs its own DB/schema (or a serial CI
  step) to stop skipping; backfill the `target_id` index on the next migration;
  the `connectionDisplay` DSN masking should move to `new URL()`.

## Phase 18 residuals (file manager; none blocking)

The full triage lives in [`plans/18-file-manager.md`](plans/18-file-manager.md) (step 10 results
+ "Known residuals").

- [x] **Flags (owner sign-off 2026-10-13).** `FEATURE_FILES` (CP) defaults on and the file
  surface is platform-operator only. `GOTHAM_AGENT_FILES` (agent) also defaults on and, with
  `GOTHAM_AGENT_FILE_FULL_ACCESS` (default on), exposes the whole filesystem (`/`) — risk
  accepted by the owner. `install-agent.sh` asks (default no) and on yes runs the agent as root
  through the `file-manager.conf` drop-in.
- [x] **Container-created volumes.** Solved for operators who answer yes: the agent runs as root.
- [ ] **`agent/files.go` root-stat symlink swap.** Capture the root identity fd-first
  (`O_PATH`/`openat` + `fstat`, or `Lstat`) instead of `EvalSymlinks` then a fresh `os.Stat`.
- [ ] **Per-team upload staging cap.** `FILES_SPOOL_CONCURRENCY` is a single global semaphore, so
  one team can starve others; add a per-team cap or fair queueing.
- [x] **Step 9a chmod (JUS-113)** built: agent `FileService.Chmod` (0o777 bits only;
  setuid/setgid/sticky refused; no symlink follow; read-only roots refused), CP
  `POST /files/chmod`, and the web Permissions dialog.
- [x] **Step 9b archives (JUS-114) built.** The agent `Compress`/`Extract` RPCs,
  the CP `/files/compress` + `/files/extract` routes and the web Compress…/Extract
  here… dialogs ship.

## Phase 4 residuals

- [ ] **FX-10a — reconcile stuck unlinked webhook claims.** A push claim is
  durable only once `webhook_events.deployment_id` is linked. A crash between the
  claim insert and the link leaves a row with `deployment_id IS NULL`, so every
  redelivery of that commit answers 503 forever. Needs a bounded sweep of old
  unlinked claims, or a re-claim after an age threshold.
- [ ] **FX-10a — document that 503 recovery is host-driven.** A conflict/retryable
  delivery is recovered only when the Git host redelivers (GitHub/GitLab/Gitea
  "Redeliver", or an automatic retry); there is no in-process requeue. Add this
  to the operations guide.
- [ ] **FX-10a — sealed deploy-key row under double failure.** If the C3-10 orphan
  purge and the fenced mapping delete both fail and the application delete then
  succeeds, an inert sealed `private_keys` row is stranded. Same reconciliation
  follow-up: sweep `private_keys` named `deploy-key:%` with no
  `application_deploy_keys` row.

## Phase 13 residuals (from the PE-2/PE-5/PE-6 reviews; none blocking)

- [ ] **PE-2 R2 — `PATCH /services/{id}` can wait up to the deploy timeout** for a
  running deploy (lock acquire is not context-aware). Consider a try-lock
  answering the contract's 409 `a deploy is in progress`.
- [ ] **PE-2 R3 — message precedence.** A service with deploy history answers
  `a deployed service cannot change server` even while a deploy runs, where the
  contract names `a deploy is in progress` first.
- [ ] **PE-2 R4 — preview-creation sliver.** `CreatePreviewApplication` reads the
  base then inserts the sibling without the app lock: a base move in that window
  leaves the preview on the old environment/node. Tiny window; the next PR close
  cleans it up.
- [ ] **PE-5 M-A — detail ghost shell on 404.** Unknown application/database ids
  render the red "not found" alert under the full detail shell instead of the
  NEmpty not-found state the environment/service pages use. UX-only.
- [ ] **PE-5 L-A — a successful move resets the application page to Overview.**
  Keep the tab when the id is unchanged.
- [ ] **PE-5 L-B — "Show previews" label text does not toggle the switch.**
  Add a click handler or id/for.
- [ ] **PE-5 L-C — database node is always pinned in the UI.** A failed
  never-provisioned database cannot be re-pinned from the UI (matches the stated
  "always for databases" decision).
- [ ] **PE-5 L-D — duplicate reads around the canonical redirect.** Harmless.
- [ ] **PE-6 N1 — server-error text sticks after the row is fixed.** Clear
  `saveError` on draft edit.
- [ ] **PE-6 N2 — raw inline server message.** Extend `stripErrorPrefix` for the
  `replace shared variables: projects: validation: …` prefix.
- [ ] **PE-6 N3 — saving drops keyboard focus.** Restore focus after save.
- [ ] **PE-6 N4 — no unsaved-changes guard in the variables editor.**

## Cross-cutting

- [ ] GitHub required status checks: enable manually in repo settings if
  enforcement is wanted (API returns 403 on this plan).
- [ ] **FX-9: reap backup artifacts (local files, node volumes, S3 objects) for
  purged databases.** The grace-window sweep removes a deleted database's volume
  and row, but the `backups`/`backup_schedules` cascade does not remove stored
  artifacts; they are orphaned at soft-delete time. Reaping needs the backup
  service's object-store/target resolution (cascade documented in
  `internal/databases/repository.go` and `retention.go`).
