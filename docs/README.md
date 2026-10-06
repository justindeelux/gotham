# Gotham Docs

Index of the project documentation. Start here.

## Development plan

The plan lives in [`plans/`](plans/) — one file per phase, each with goal,
exit criteria, work packages, dependencies, and rollback. Start with the
[roadmap](plans/00-roadmap.md).

| Phase | File |
|---|---|
| 0 — Foundation | [01-foundation.md](plans/01-foundation.md) |
| 1 — Auth & Users | [02-auth-users.md](plans/02-auth-users.md) |
| 2 — Server + Agent | [03-server-agent.md](plans/03-server-agent.md) |
| 3 — Docker Engine Core | [04-docker-core.md](plans/04-docker-core.md) |
| 4 — Applications | [05-applications.md](plans/05-applications.md) |
| 5 — Databases & Backups | [06-databases-backups.md](plans/06-databases-backups.md) |
| 6 — Proxy, Domains & SSL | [07-proxy-domains.md](plans/07-proxy-domains.md) |
| 7 — Services & Templates | [08-services-templates.md](plans/08-services-templates.md) |
| 8 — Advanced | [09-advanced.md](plans/09-advanced.md) |
| 9 — Self-update & Release | [10-self-update-release.md](plans/10-self-update-release.md) |
| 11 — UI Alignment (side track) | [11-ui-alignment.md](plans/11-ui-alignment.md) |
| 12 — User profile management | [12-user-profile.md](plans/12-user-profile.md) |
| 13 — Projects and environments | [13-projects-environments.md](plans/13-projects-environments.md) |
| 14 — English/Vietnamese UI localization | [14-i18n.md](plans/14-i18n.md) |

Phase status and remaining work live only in [`TODO.md`](TODO.md).

### Where a plan goes

| Folder | Holds |
|---|---|
| [`plans/`](plans/) | Medium to large features and phases: goal, scope, backend + web design, work packages, risks |
| [`sub-plans/`](sub-plans/) | Small, focused work: one UI fix or a single work package |
| [`design-plans/`](design-plans/) | UI only: what each screen must let a user do, for the design agent; no backend or delivery detail |

## How we work

[`../AGENTS.md`](../AGENTS.md) — workflow, invariants, review gates and the phase gate.

## Library decisions

[`library-audit.md`](library-audit.md) — which third-party libraries replace
hand-rolled code and which stay hand-written (JUS-23): zod for all web
validation, @vueuse/core for media-query/clipboard only, everything else
stays as-is.

## Test server

[`test-server.md`](test-server.md) — the long-lived all-in-one test box
(CP + Postgres + Redis + node agent): connection, provisioning, test
workflow, Playwright UI smoke, and what has been verified on real hardware.

## Install & release

[`install.md`](install.md) — control-plane and node-agent install, first login,
updates/rollback, and the signed release/signing flow (keypair, assets,
GoReleaser workflow).

## One-click service templates

[`../templates/README.md`](../templates/README.md) — the template format
(`template.yaml` + `compose.yaml`), the field types and validation rules, the
render guarantees, and the HTTP contract the services gallery consumes.

## UI design

[`design/`](design/) is the UI source of truth: one `*.html` mockup per page
plus shared tokens in `assets/gotham-ui.css` (+ page CSS in
`assets/gotham-views.css`).
