# Gotham TODO

Open work only. Workflow, invariants and gates: [`../AGENTS.md`](../AGENTS.md).
Plan: [`plans/00-roadmap.md`](plans/00-roadmap.md). Closed items, PR numbers and
merge evidence live in git history and the PRs.

## Status

All phases are complete: 0–9 (roadmap; G0, G1 and G2 passed), 11 UI alignment,
12 user profile, 13 projects and environments, 14 English/Vietnamese i18n,
15 add-resource flow and Git sources, 16 instance settings, 17 update settings.
The retro review of Phases 0–5 closed 2026-10-03
([`sub-plans/retro-review-p0-5-register.md`](sub-plans/retro-review-p0-5-register.md)).
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

- [ ] **LOW-4 agent channel not mutual.** The gRPC listener accepts any peer
  (`VerifyClientCertIfGiven`); mutual TLS waits on the registration bootstrap
  acquiring a client credential. No bootstrap credential exists yet — the
  remaining owner/design decision, not a polishing task. Residuals that still
  need it (details: `deploy/README.md` → Known residuals):
  - **Every** bootstrap Register/heartbeat is self-asserted (metadata
    `node-id`); a caller can impersonate any enrolled node id at any time.
  - **CP-impersonation.** An unauthenticated peer can enroll any node id and
    receive a CA-signed `serverAuth` certificate. The short-term guard (FX-3 R1)
    refuses the CP's own listener ids (`serverHosts`) at Register; the real fix
    is a role-separated CA or a bootstrap credential binding the caller to the
    identity.
  - **Revocation absent.** No CRL/OCSP or revoked-serial set at agent-dial time;
    mitigated by the 90-day agent leaf and early renewal.
  - **Per-IP limiter keying.** Register/heartbeat limits are keyed per peer IP;
    agents behind one NAT/egress IP share a bucket.
  - **Unknown-node Register rows.** Each Register creates a registry row, so an
    unauthenticated peer can grow `servers` within its rate limit; a future
    bootstrap credential should gate row creation.
- [ ] **Teams:** legacy `servers.team_id IS NULL` nodes, their containers and logs
  stay shared with every authenticated caller; a pre-fix self-minted admin-scoped
  API token would need rotation (none shipped); global DNS/proxy management needs
  `PLATFORM_ADMINS` (or an admin-role session / admin-scoped token).
- [ ] Owner-decision follow-ups from the retro register:
  `TestStoreCreateFirstUserSerializes` needs its own DB/schema (or a serial CI
  step) to stop skipping; backfill the `target_id` index on the next migration;
  the `connectionDisplay` DSN masking should move to `new URL()`.

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
