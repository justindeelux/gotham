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

**Schema (migration `00022_preview_deploys.sql`).** `preview_deploys(id,
application_id → applications ON DELETE CASCADE, team_id, provider, repo,
pr_number, branch, head_sha, preview_application_id → applications ON DELETE
SET NULL, host, state CHECK IN ('active','deploying','failed','deleted'),
created_at, updated_at, deleted_at)`. The unique `(application_id, pr_number)`
pair makes a redelivered PR event refresh its own row instead of racing a
second sibling. `preview_application_id` is SET NULL, not CASCADE: the normal
teardown deletes the sibling application, and a cascade would erase the audit
row (`state='deleted'`, `deleted_at`) the design asks to keep.

**A preview is a sibling application.** `deploy.CreatePreviewApplication`
(system path, no caller) clones the base application's server, provider/repo/
clone URL, build pack, port, env vars, sealed secrets, storages and team, with
`branch = PR head branch`, `name = "<base>-pr-<n>"` and `base_domain =
"pr-<n>-<slug(base)>.<base_domain>"` (lowercase, `[a-z0-9-]` label, ≤ 63
chars, base name slugified). The base host port is never copied (the agent
assigns one), and the base deploy key is re-registered on the sibling so a
private repository stays cloneable. The sibling is a normal applications row,
so deploying, routing and deleting reuse the Phase 4 orchestrator and the
Phase 6 proxy sync unchanged — the only difference is the trigger.

**Webhook handling.** `POST /v1/webhooks/{provider}` now also accepts
`pull_request` (GitHub/Gitea) and `Merge Request Hook` (GitLab) deliveries:
`opened`/`synchronize`/`reopened` (GitLab `open`/`update`/`reopen`) create or
refresh the preview and queue a deployment; `closed` (GitLab
`close`/`merge`) delete the sibling application and mark the binding deleted.
A PR whose base branch is not the application's watched branch, a repo no
application watches, an action that is not one of the above, and an
application without a base domain are acknowledged as ignored. The delivery is
claimed in `webhook_events` like a push; a start delivery dedupes by head SHA,
a close delivery deliberately does **not** (its SHA is the same as the
synchronize that deployed the preview) — close redeliveries are idempotent
through the binding's state. Hooks are installed with
`Events: ["push","pull_request"]` (GitLab: `merge_requests_events`). Hooks
installed before this release keep push-only events: they must be deleted and
re-installed — the documented re-install path, chosen over silently
re-registering a hook and losing the secret deliveries are signed with.

**PR badge comment.** `providers.SourceProvider` gained
`CreatePullRequestComment` (GitHub/Gitea `POST /repos/{repo}/issues/{n}/comments`,
GitLab `POST /projects/{id}/merge_requests/{iid}/notes`), reached through
`Service.CreatePullRequestComment` and the `Commenter` seam. The webhooks
service posts a start/failure/removed comment after a delivery is decided,
best effort with a 5s bound: a Git host that cannot accept a comment never
fails the delivery. The URL renders as `http://<host>` (a certificate-covered
host redirects to HTTPS; without one the HTTP URL is what works).

**Cleanup.** Close is the normal teardown (delete sibling → route/container
gone, mark binding deleted). `DeleteApplication` calls the webhooks service
before deleting a base row so preview siblings are never orphaned, and an
hourly sweep (`FEATURE_PREVIEWS`, TTL 7d, started/stopped with the server
lifecycle) tears down previews whose close delivery never arrived.

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
notifier carries team-channel notifications, not PR comments). (3) Reusing the
base application's storages means base and preview mount the same host path —
acceptable for a preview, risky for stateful apps. (4) `preview_deploys` rows
of a closed PR are kept as audit; pruning them is deferred.

**Verification.** Unit tests for payload parsing (three providers ×
open/synchronize/close), host/name derivation, the service lifecycle
(create/reuse/redelivery/close/comment-failure/flag-off/team scope), the
provider comment endpoints and the deploy clone/system-delete seam;
repository integration tests against PostgreSQL for the unique pair, the
deleted→reopened transition, the stale list and both cascades. Gated e2e
(`GOTHAM_E2E=1`): `TestP8PreviewLifecycle` opens a PR over the real delivery
route, deploys the head branch through the agent, proves the anti-spam
redelivery and deletes the sibling on close. A real GitHub PR, a wildcard
certificate and the live comment were **not** exercised (no Git host or TLS
in CI).

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
