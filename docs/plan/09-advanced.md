# Phase 8 — Advanced (W11–W12)

**Goal:** complete the core feature set: preview deployments, teams/RBAC, notifications, metrics.

**Exit criteria (Milestone M8):**
- Open a PR → preview app auto-deploys on a temporary subdomain.
- Teams with roles (owner/admin/read-only) + invites.
- Deploy success/fail notifications via Discord/Slack/Telegram/email.
- Server CPU/RAM/disk/network metrics shown as charts in the UI.

**Rollback:** all add-on features — each has its own env flag to disable. The core deploy flow is untouched.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 9 (see Process & Phase gate in `../process.md`).

---

## BE-8.1 — Preview deployments — `ws/p8-previews`

- **Context brief:** PR webhook (GitHub) → deploy the PR branch on subdomain `{branch}-{pr}.{domain}` (using the Phase 6 wildcard cert), labeled `preview`; PR close/merge → auto-delete. Reuses the Phase 4 orchestrator, differing only in trigger source + domain + TTL.
- **Deliverables:** preview webhook route; `preview_deploys` table; cleanup job on PR close; PR badge comment (preview URL).
- **Verify:** open a PR on the test repo → preview runs → close the PR → deleted.
- **Depends on:** Phase 4 + Phase 6.

### BE-8.1 delivered design

**Schema (migrations `00022_preview_deploys.sql` +
`00023_preview_hardening.sql`).** `preview_deploys(id, application_id →
applications ON DELETE CASCADE, team_id, provider, repo, pr_number, branch,
head_sha, preview_application_id → applications ON DELETE SET NULL, host,
state CHECK IN ('active','deploying','failed','deleted'), created_at,
updated_at, deleted_at)`. The unique `(application_id, pr_number)` pair makes a
redelivered PR event refresh its own row instead of racing a second sibling.
`preview_application_id` is SET NULL, not CASCADE: the normal teardown deletes
the sibling application, and a cascade would erase the audit row
(`state='deleted'`, `deleted_at`) the design asks to keep. `00023` adds
`applications.is_preview` (the marker the teardown and sweep key on) and
`preview_deliveries`, the preview-specific delivery ledger (see below).

**A preview is a sibling application.** `deploy.CreatePreviewApplication`
(system path, no caller) clones the base application's server, provider/repo/
clone URL, build pack, port, plain env vars and team, with `branch = PR head
branch`, `name = "<base>-pr-<n>"` and `base_domain =
"pr-<n>-<slug(base)>.<base_domain>"` (lowercase, `[a-z0-9-]` label, ≤ 63
chars, base name slugified). The base host port is never copied (the agent
assigns one), and the base deploy key is re-registered on the sibling so a
private repository stays cloneable. Sealed secrets and storages are
deliberately **not** copied: a preview builds a PR branch and is served
publicly, so it must not read the base application's production secrets or
write its volumes. The sibling is a normal applications row flagged
`is_preview`, so deploying, routing and deleting reuse the Phase 4
orchestrator and the Phase 6 proxy sync unchanged — the only difference is the
trigger.

**Webhook handling.** `POST /v1/webhooks/{provider}` now also accepts
`pull_request` (GitHub/Gitea) and `Merge Request Hook` (GitLab) deliveries:
`opened`/`synchronize`/`reopened` (GitLab `open`/`update`/`reopen`) create or
refresh the preview and queue a deployment; `closed` (GitLab
`close`/`merge`) delete the sibling application and mark the binding deleted.
A PR whose base branch is not the application's watched branch, a repo no
application watches, an action that is not one of the above, an application
without a base domain, and **a fork head** are acknowledged as ignored. Fork
detection fails closed: a GitHub/Gitea delivery without its `head.repo`
identity (or with the fork flag, or a foreign head repository) and a GitLab
delivery whose source/target project ids are missing or differ are treated as
forks and never previewed. Live previews are capped at 5 per base application;
a PR beyond the cap is ignored and logged, while a synchronize of an
already-previewed PR is never capped — and the cap is enforced inside the
atomic claim, so concurrent distinct-PR deliveries cannot overshoot it.

**PR idempotency uses an atomic lease claim (fix rounds 1–3).** PR deliveries
are claimed in `preview_deliveries`, never in the push `webhook_events`
ledger. The claim (`Store.ClaimPreviewDelivery`) is one transaction per
`(application, PR)`: it locks the base application row, purges that PR's
expired rows, refuses a start while the binding is `closing` (retryable — even
at the delivery's own head, so a same-head reopen waits for the teardown
instead of being acknowledged as a duplicate), dedupes a start **only against
the live binding's current head** or an in-flight lease for exactly that head,
enforces the live-preview quota for a new preview (excluding the claiming PR's
own lease, which already holds its slot), and inserts the in-flight lease
(15-minute expiry; a crashed delivery's lease self-heals). Historical revisions
are therefore not a permanent handled set: a force-push A → B → A deploys
three times. A close claims `(application_id, pr_number)` as a teardown marker.
`delivery_id` is stored for the audit trail only — it is an unsigned header and
never gates a delivery by itself.

Binding promotions are **fenced** (`Store.WritePreviewBinding`): the worker's
binding write runs under the same base-application lock and verifies its claim
lease still exists and is unexpired, refuses a `closing` binding, and re-checks
the quota when the write would make the binding live. Lease and quota validity
use `clock_timestamp()`, so a lease that lapses while the writer waits for the
lock is refused rather than accepted on the transaction-start clock. A worker
whose lease lapsed while it provisioned therefore cannot promote a sixth live
preview, and a close that completed in the meantime can never be overwritten
back to active (the `ErrNotFound` recreate branch included); the refused worker
compensates its freshly provisioned sibling and answers 503. A failed final
promotion is surfaced as a non-2xx (retryable) rather than a 200 with the
revision unrecorded. The commit-based SHA ledger of the push path is untouched.
A start persists the binding before queueing; a failed queue or a conflicting
active deployment releases the lease and answers 503 (retryable). Quota
allocation rides the same claim, so concurrent distinct-PR deliveries cannot
overshoot the cap.

Hooks are installed with `Events: ["push","pull_request"]` (GitLab:
`push_events` + `merge_requests_events`). Hooks installed before the preview
feature keep push-only events: they must be deleted and re-installed — the
documented re-install path, chosen over silently re-registering a hook and
losing the secret deliveries are signed with.

**PR badge comment.** `providers.SourceProvider` gained
`CreatePullRequestComment` (GitHub/Gitea `POST /repos/{repo}/issues/{n}/comments`,
GitLab `POST /projects/{id}/merge_requests/{iid}/notes`), reached through
`Service.CreatePullRequestComment` and the `Commenter` seam. The webhooks
service posts a start/failure/removed comment after a delivery is decided,
best effort with a 5s bound: a Git host that cannot accept a comment never
fails the delivery. The URL renders as `http://<host>` (a certificate-covered
host redirects to HTTPS; without one the HTTP URL is what works).

**Cleanup.** Close persists its intent first (`state='closing'`), then deletes
the sibling (route/container gone, local deploy-key rows removed) and finally
completes atomically: binding deleted and the PR's ledger cleared in one
transaction (`MarkPreviewClosed`, idempotent and used on the already-deleted
retry path too, so a stale reservation can never block a reopen). Every close
path — intent, completion, the no-binding ledger clear, the sweep's retry and
the user-facing sibling delete — takes the **same base-application row lock**
as the claim and the promotion, so a close that starts while a promotion is
writing waits for it and then wins: a completed close can never be overwritten
back to active, and a promotion that wins first is closed by the transition
that follows. The lock is never held across provisioning or teardown network
work. A teardown failure leaves the binding `closing` and returns an error.
Teardown never
removes a deploy key from the Git host: a preview reuses its base
application's remote key, and deleting it (user or system path) would break
the base; the system path additionally refuses a non-preview application.
`DeleteApplication` runs the webhook cleanup before deleting a base row and
**aborts on its failure**, so the base FK can never cascade the bindings away
while a sibling survives. The hourly sweep (`FEATURE_PREVIEWS`, started/stopped
with the server lifecycle) is **orphan-only**: it purges expired leases, closes
bindings whose sibling is already gone, re-attempts the teardown of bindings
left `closing` (older than the grace period) and deletes `is_preview`
applications that no live binding references — it never tears down a preview
that is still bound to a live sibling, so an open PR keeps its preview however
old it is.

**Routes.** Delivery route unchanged; authenticated `GET
/v1/applications/{id}/previews` lists an application's bindings (team-scoped
read) for the FE-8.1 previews screen. `FEATURE_PREVIEWS=false` makes PR
deliveries a no-op, unmounts the listing route and the sweep, and keeps the
hook push-only; push handling is untouched.

**Residuals.** (1) Previews are HTTP-only: the sibling does not get a
`domain_certificates` row, so with a wildcard certificate configured the route
still serves plain HTTP. Copying the base application's wildcard DNS-01 intent
to the sibling (a system-path certificate clone) is the follow-up; issuing a
per-preview certificate is explicitly out of scope. (2) The badge comment
reflects the queue decision, not the terminal deployment state (the deploy
notifier carries team-channel notifications, not PR comments). (3) Secrets and
storages are not shared with previews by default (fix round 1); an operator
who needs shared configuration must move the value into a plain env var
(visible) or configure the preview application explicitly — a per-application
opt-in is deliberately not implemented. (4) A conflicting synchronize answers
503 and waits for a host redelivery: there is no automatic retry queue. A
failed close is covered by the sweep's `closing` retry instead. (5)
`preview_deploys` rows are kept as audit; `preview_deliveries` is transactional
state with a 15-minute lease expiry and is pruned by the sweep and by each
claim. (6) Two previews of one application can still be sequenced by the host
in either order; the claim serializes them but does not reorder events.
(7) If the final (active) promotion write fails, the lease lingers until its
15-minute expiry and the binding keeps the previously queued revision; the next
delivery repairs it, and the live count stays bounded (the binding is already
live).

**Verification.** Unit tests for payload parsing (three providers ×
open/synchronize/close/fork, including the fail-closed missing-identity
cases), host/name derivation, the service lifecycle (create/reuse/redelivery,
reopen at the same SHA, **A→B→A back to an earlier head**, a second PR at the
same SHA, push at the same SHA, busy-deployment retry, binding-write
compensation and recovery, **concurrent distinct-PR cap enforcement**,
**same-head reopen during closing is retryable**, **an expired worker cannot
promote a sixth preview**, **a close wins over a racing synchronize**, the
quota excluding the claiming PR's own lease, fork rejection,
close/close-retry/sweep-retry/comment-failure/flag-off/team scope, orphan-only
sweep), the provider comment endpoints, the GitLab `merge_requests_events`
subscription and the deploy clone/system-delete seam; repository integration
tests against PostgreSQL for the unique pair, `is_preview`, the atomic claim
(current-head dedupe, in-flight duplicate, second PR, historical head approved,
expired-lease purge), the fenced promotion (closing refusal, expired-lease
refusal including a lease that lapses while waiting for the application lock,
closed-binding non-resurrection, and a **real-concurrent close that starts
while the promotion is inside its write** — the close waits for the lock and
wins, the promotion is never overwritten), the closing list, the orphan
queries, the sibling delete clearing the ledger, the deleted→reopened
transition and both cascades, a concurrent claim test proving the cap holds
under real transactions, plus a DB-backed teardown removing the preview's
local private-key row while the base key survives. Gated e2e (`GOTHAM_E2E=1`): `TestP8PreviewLifecycle` opens a PR
over the real delivery route, deploys the head branch through the agent,
proves the anti-spam redelivery and deletes the sibling on close. A real
GitHub PR, a wildcard certificate and the live comment were **not** exercised
(no Git host or TLS in CI).

## BE-8.2 — Teams & roles — `ws/p8-teams`

- **Context brief:** team model: a user belongs to many teams, a resource belongs to 1 team. Roles: `owner`, `admin`, `read-only`. Invite via email + link. Migration adds `team_id` to existing resource tables (applications, databases, services, servers) — **default team** for old data.
- **Deliverables:** `teams`, `team_members`, `invites` migrations; `RequireTeam` middleware + RBAC checker; teams/invites CRUD routes; API filters resources by team.
- **Verify:** user A cannot see team B's resources (RBAC test per role).
- **Depends on:** Phase 1 (auth). Parallel with 8.1/8.3/8.4.

## BE-8.3 — Notifications — `ws/p8-notify`

- **Context brief:** events from deploys (success/fail) and backups (success/fail) → sent via channels: Discord webhook, Slack webhook, Telegram bot, email (SMTP). Configured at team + resource level (override).
- **Deliverables:** `Notifier` interface + 4 implementations; `notification_channels` table + mapping; hook into the Phase 4 event emitter; tests sending to a mock server.
- **Verify:** failed deploy → test Discord webhook receives the correctly formatted message.
- **Depends on:** Phase 4 (event emitter). Parallel with 8.1/8.2/8.4.

## BE-8.4 — Server metrics — `ws/p8-metrics`

- **Context brief:** heartbeats already send CPU/RAM/disk — now persist time-series (Redis TS or an append-style `metrics` table + 30-day retention, pick one), add network I/O (read from `/proc` via the agent), range-query API for charts.
- **Deliverables:** `metrics` table + aggregation (1m/1h/1d rollup); agent reads network + disk I/O too; routes `GET /api/v1/servers/{id}/metrics?from&to&step`.
- **Verify:** charts show correct numbers while running a workload (stress) on the test server.
- **Depends on:** Phase 2. Parallel with 8.1/8.2/8.3.

## FE-8.1 — Combined UI — `ws/p8-misc-ui`

- **Context brief:** 4 small UI groups: (1) previews screen in the app detail; (2) teams page + members/invites management; (3) notifications settings page; (4) metrics charts (ECharts or the Naive UI chart — pick one).
- **Deliverables:** `/teams`, `/settings/notifications` pages; `MetricsChart` component; app detail upgraded with a previews tab.
- **Verify:** e2e per group against the phase exit criteria.
- **Depends on:** BE-8.1..8.4 (contract per part).
