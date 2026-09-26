# Phase 8 — Advanced (W11–W12)

**Goal:** complete the core feature set: preview deployments, teams/RBAC, notifications, metrics.

**Exit criteria (Milestone M8):**
- [ ] Open a PR → preview app auto-deploys on a temporary subdomain.
- [ ] Teams with roles (owner/admin/read-only) + invites.
- [ ] Deploy success/fail notifications via Discord/Slack/Telegram/email.
- [ ] Server CPU/RAM/disk/network metrics shown as charts in the UI.

**Rollback:** all add-on features — each has its own env flag to disable. The core deploy flow is untouched.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 9 (see Model policy & Phase gate in `00-roadmap.md`).

---

## BE-8.1 — Preview deployments — `ws/p8-previews`

- **Context brief:** PR webhook (GitHub) → deploy the PR branch on subdomain `{branch}-{pr}.{domain}` (using the Phase 6 wildcard cert), labeled `preview`; PR close/merge → auto-delete. Reuses the Phase 4 orchestrator, differing only in trigger source + domain + TTL.
- **Deliverables:** preview webhook route; `preview_deploys` table; cleanup job on PR close; PR badge comment (preview URL).
- **Verify:** open a PR on the test repo → preview runs → close the PR → deleted.
- **Depends on:** Phase 4 + Phase 6.

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
