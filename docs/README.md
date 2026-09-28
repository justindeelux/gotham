# Gotham Docs

Index of the project documentation. Start here.

## Development plan

The plan lives in [`plan/`](plan/) — one file per phase, each with goal,
exit criteria, work packages, dependencies, and rollback. Start with the
[roadmap](plan/00-roadmap.md).

| Phase | File |
|---|---|
| 0 — Foundation | [01-foundation.md](plan/01-foundation.md) |
| 1 — Auth & Users | [02-auth-users.md](plan/02-auth-users.md) |
| 2 — Server + Agent | [03-server-agent.md](plan/03-server-agent.md) |
| 3 — Docker Engine Core | [04-docker-core.md](plan/04-docker-core.md) |
| 4 — Applications | [05-applications.md](plan/05-applications.md) |
| 5 — Databases & Backups | [06-databases-backups.md](plan/06-databases-backups.md) |
| 6 — Proxy, Domains & SSL | [07-proxy-domains.md](plan/07-proxy-domains.md) |
| 7 — Services & Templates | [08-services-templates.md](plan/08-services-templates.md) |
| 8 — Advanced | [09-advanced.md](plan/09-advanced.md) |
| 9 — Self-update & Release | [10-self-update-release.md](plan/10-self-update-release.md) |
| 11 — UI Alignment (side track) | [11-ui-alignment.md](plan/11-ui-alignment.md) |

Phase status and remaining work live only in [`TODO.md`](TODO.md).

## How we work

[`process.md`](process.md) — work packages, phase gates, review gates,
invariants, rollback, dev environment, decision log, and how to resume work.

## Test server

[`test-server.md`](test-server.md) — the long-lived all-in-one test box
(CP + Postgres + Redis + node agent): connection, provisioning, test
workflow, Playwright UI smoke, and what has been verified on real hardware.

## One-click service templates

[`../templates/README.md`](../templates/README.md) — the template format
(`template.yaml` + `compose.yaml`), the field types and validation rules, the
render guarantees, and the HTTP contract the services gallery consumes.

## UI design

[`design/`](design/) is the UI source of truth: one `*.html` mockup per page
plus shared tokens in `assets/gotham-ui.css` (+ page CSS in
`assets/gotham-views.css`).
