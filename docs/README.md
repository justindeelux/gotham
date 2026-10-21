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
| 15 — Add resource flow & automatic Git sources | [15-add-resource-git-sources.md](plans/15-add-resource-git-sources.md) |
| 16 — Instance settings | [16-instance-settings.md](plans/16-instance-settings.md) |
| 17 — Update settings | [17-update-settings.md](plans/17-update-settings.md) |
| 18 — Remote file manager & code editor | [18-file-manager.md](plans/18-file-manager.md) |

Phase 18 is complete (step 10 hardening/e2e/docs, JUS-115): the agent `FileService`, the
control-plane `internal/files` module and the web browser/ops/editor ship behind the
`FEATURE_FILES` (CP, on by default, platform operators only) and the per-node opt-in
`GOTHAM_AGENT_FILES` (agent) flags. Step 9a chmod (JUS-113) and step 9b archives
(JUS-114) shipped as optional phase-2 adds. Phase status and remaining work
live only in [`TODO.md`](TODO.md).

### Where a plan goes

| Folder | Holds |
|---|---|
| [`plans/`](plans/) | Medium to large features and phases: goal, scope, backend + web design, work packages, risks |

## How we work

[`../AGENTS.md`](../AGENTS.md) — workflow and invariants.

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
