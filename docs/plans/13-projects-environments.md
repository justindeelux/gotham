# Phase 13 — Projects and environments

Status: implemented and deployed to the test box 2026-10-05 (Linear JUS-30..JUS-37). Written from a read of the schema, routes and web layout; the delivery log is in section 9.
layout; handler-level details are to be confirmed by each package's spec.

## 1. Where we are

- `applications`, `databases` and `services` each carry `team_id` (scope) and `user_id` (creator)
  and a nullable `server_id` (`ON DELETE SET NULL`). Picking the deploy node already exists.
- Names are unique per creator: `(user_id, name)`. Domains are unique per node:
  `(server_id, lower(base_domain))`.
- There is no grouping layer. The web router is flat (`/applications`, `/services`, `/databases`).
- A PR preview is a sibling `applications` row (`is_preview`) cloned from its base application
  (`preview_deploys`).
- The product has no real data yet, so this phase wipes resource data instead of backfilling.

## 2. Target model

```
team ─┬─ project ── environment ─┬─ application ─► server (node)
      │                          ├─ service     ─► server (node)
      └─ server (shared)         └─ database    ─► server (node)
```

- A project belongs to a team and has 1..n environments (`production` is created with the
  project; users add `staging`, etc.).
- Applications, services and databases belong to exactly one environment and are each pinned to
  one server. The server stays a team-level resource shared by every project.
- Roles stay team-level. No per-project permissions in this phase.

## 3. Decisions

| # | Decision |
|---|---|
| 1 | Databases live in an environment, like applications and services. Backups and restores follow their database. |
| 2 | Shared variables at project and environment level. Precedence at deploy time: application > environment > project. |
| 3 | URLs are nested: `/projects/:projectId/environments/:envId/<kind>/:id`. The flat list pages go away. |
| 4 | A PR preview is created in the same environment as its base application, marked `is_preview`, hidden from the default list as today. No per-PR environments. (Owner did not weigh in; this is the default, revisit if it gets noisy.) |
| 5 | Authorization stays at team role (viewer / member / admin as today). |
| 6 | No data migration. The migration truncates resource tables. |

## 4. Backend design

### Migration `00034_projects_environments.sql`
- `projects(id, team_id → teams ON DELETE CASCADE, name, description, created_at, updated_at)`,
  unique `(team_id, lower(name))`.
- `environments(id, project_id → projects ON DELETE CASCADE, name, created_at, updated_at)`,
  unique `(project_id, lower(name))`.
- Truncate `applications, databases, services` (cascade to deployments, env vars, backups,
  previews, webhooks, proxy versions that hang off them) in the same migration, with a comment
  that this is deliberate because no production data exists.
- Add `environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT` to the three
  resource tables. Deleting an environment requires it to be empty; deleting a project requires
  all its environments to be empty (409 with the blocking count).
- Make `server_id NOT NULL ... ON DELETE RESTRICT` on the three tables: a server with resources
  cannot be deleted (409 listing them). Replaces the silent `SET NULL`.
- Replace `(user_id, name)` unique indexes with `(environment_id, name)`. Keep `user_id` as
  creator. Keep the per-server domain uniqueness unchanged.
- `shared_env_vars(id, project_id, environment_id NULL, key, value / secret flag, ...)`: a row
  with `environment_id NULL` is project-level. Reuse the encryption and masking pattern of the
  existing `env_vars` / `secrets`; the package spec must read that code first.

### API (team scope via the existing `RequireTeam`)
| Route | Notes |
|---|---|
| `GET/POST /projects`, `GET/PATCH/DELETE /projects/{id}` | create adds a `production` environment in the same tx |
| `GET/POST /projects/{id}/environments`, `PATCH/DELETE /environments/{id}` | |
| `GET/PUT/DELETE /projects/{id}/variables`, `/environments/{id}/variables` | shared variables |
| `GET .../environments/{id}/resources` | one call returns the apps, services and databases of an environment for the environment page |
| existing create routes | `environment_id` and `server_id` required in the body; the environment must belong to the caller's team |
| `PATCH /applications/{id}` (and services, databases) | `server_id` change and `environment_id` move, both within the team. Moving a database re-checks its backup targets. Changing the server is rejected while a deploy is in flight. |

Resource IDs stay globally unique, so existing id-based routes (`/applications/{id}`, deploys,
logs, webhooks) keep working; only the web URLs and list/create entry points change.

### Deploy merge
The deploy spec merges shared variables beneath application variables
(`project < environment < application`). Secret values keep their masking in every API response.

### Preview
`CreatePreview` copies `environment_id` and `server_id` from the base application.

## 5. Web design

- Sidebar: **Projects** replaces Applications / Services / Databases. Servers, Domains,
  Templates, Teams, Settings stay.
- Pages (UI brief goes to `docs/design-plans/`, one mockup per page in `docs/design/`):
  1. `/projects`: project cards (name, environments, resource counts), create dialog.
  2. `/projects/:p`: environment tabs, project shared variables.
  3. `/projects/:p/environments/:e`: tabs or sections for applications, services, databases, an
     "Add resource" menu, environment shared variables.
  4. Resource detail pages keep their current content under the nested URL, with breadcrumbs
     `Project / Environment / Resource`.
- Create flows (application, service, database, template deploy): pick project and environment
  (preselected from the current URL) and **server** (required, defaults to the only node).
  Resource settings let the user change server and move environment.
- Old flat routes redirect once to the Projects page, no per-route compatibility.
- Follow the lessons from Phase 12: per-mount state, barrels without `.vue`, `ruleFrom` schemas
  in `features/projects/schemas/`, one submit guard, container queries for narrow layouts,
  Playwright MCP check at 1280 and 480 widths, rebuild `webdist`.

## 6. API contract (fixed; backend and web follow it exactly)

All routes under `/api/v1`, same auth and `X-Team-Id` scope as today. Errors keep the existing
`{"message": "..."}` body. Ids are uuid strings. Timestamps are RFC 3339.

```
Project     {id, name, description, created_at, updated_at, environment_count, resource_counts:{applications,services,databases}}
Environment {id, project_id, name, created_at, updated_at, resource_counts:{applications,services,databases}}
Variable    {key, value?: string, secret: boolean}   // value omitted when secret; never returned
```

| Route | Body → result |
|---|---|
| `GET /projects` | → `{projects: Project[]}` |
| `POST /projects` | `{name, description?}` → 201 `{project, environments:[production]}`; name 1-64 chars, unique per team (case-insensitive) else 409 `project name already exists` |
| `GET /projects/{id}` | → `{project, environments: Environment[]}` |
| `PATCH /projects/{id}` | `{name?, description?}` → `{project}` |
| `DELETE /projects/{id}` | 204; 409 `project still has resources (including previews)` when any environment has resources or live previews; otherwise removes its environments and shared variables |
| `POST /projects/{id}/environments` | `{name}` → 201 `{environment}`; 1-64 chars, unique per project (case-insensitive) |
| `PATCH /environments/{id}` | `{name}` → `{environment}` |
| `DELETE /environments/{id}` | 204; 409 `environment still has resources (including previews)`; the last environment of a project cannot be deleted (409 `a project needs at least one environment`) |
| `GET /environments/{id}/resources` | → `{environment, project, applications: [...], services: [...], databases: [...]}` using the existing list item shapes of each resource, each with `server_id` and `server_name`; previews excluded unless `?previews=1`; application items carry `is_preview` and `preview_of` (the base application id, empty when none) so the page can nest them |
| `GET /projects/{id}/variables`, `GET /environments/{id}/variables` | → `{variables: Variable[]}` |
| `PUT /projects/{id}/variables`, `PUT /environments/{id}/variables` | `{variables: [{key, value, secret}]}` replaces the whole set; omitted `value` for an existing secret key keeps its sealed value; key `^[A-Za-z_][A-Za-z0-9_]*$`, max 128 keys |
| `POST /applications`, `/services`, `/databases` | existing body plus required `environment_id` (400 `environment is required`), required `server_id` (400 `server is required`, 404 if not in the team, 409 if the server is offline/unusable as today) |
| `PUT /applications/{id}`, `PATCH /services/{id}`, `PATCH /databases/{id}` | optional `environment_id` (move within the team, 409 on name collision in the target) and `server_id` (change node; 409 `a deploy is in progress` while one runs; a base application with open previews answers 409 `close the open previews first`; a deployed service answers 409 `a deployed service cannot change server`; a created database answers 409 `a database cannot change server once created`); both optional so existing edits keep working |
| `GET /applications`, `/services`, `/databases` | existing list plus optional `?environment_id=` and `?project_id=` filters; every item gains `environment_id`, `environment_name`, `project_id`, `project_name` (the Gotham project; services carry the compose project name separately as `compose_project`) |
| `GET /applications/{id}`, `/services/{id}`, `/databases/{id}` | same extra fields (`project_name` is the Gotham project everywhere; `compose_project` is services-only) |

Service items (list, get and the resources envelope) always include
`domains` alongside the grouping fields, reusing the services response
shape so the environment page can render service cards unchanged.
`resource_counts` exclude previews (they match the default listing);
previews still block project and environment deletes, whose 409 bodies
read `project still has resources (including previews)` and
`environment still has resources (including previews)`.

Roles: viewers read; members and admins write (same `Scope.CanWrite()` rule as the other resources).
Team isolation: an id from another team answers 404, as elsewhere.

Deploy-time variable precedence for applications: project < environment < application. Services
and databases do not read shared variables in v1 (applications only; revisit when services expose an
env surface).

## 7. Work packages

| Id | Package | Size | Depends on |
|---|---|---|---|
| PE-1 | Migration, `internal/projects` (model, repository, service, routes), sqlc, tests | M | |
| PE-2 | Attach apps / services / databases: `environment_id` + required `server_id`, new unique indexes, move and server-change routes, 409 guards, preview inheritance, tests | L | PE-1 |
| PE-3 | Shared variables: table, routes, deploy-spec merge, masking, tests | M | PE-1, PE-2 |
| PE-4 | Web foundation: `features/projects` (api, store, schemas), router, sidebar, Projects and Project pages, breadcrumbs | M | PE-1 |
| PE-5 | Web environment page and nested resource routes, create flows with project/environment/server pickers, templates deploy, move/server-change UI, old routes removed | L | PE-2, PE-4 |
| PE-6 | Web shared variables UI (project and environment) | S | PE-3, PE-4 |
| PE-7 | e2e flows, docs, test-box redeploy and live check (create project → environment → app on a node → deploy) | M | all |

PE-1 first; PE-4 can run in parallel with PE-2 because the API contract above is already frozen.
Package specs: [`../sub-plans/13-projects-packages.md`](../sub-plans/13-projects-packages.md).
UI brief: [`../design-plans/projects-environments.md`](../design-plans/projects-environments.md);
mockups: `docs/design/{projects,project-detail,environment}.html`.

## 8. Risks

- Truncating in a migration is irreversible by design; the migration must state it, and the test
  box is the only place it runs for now.
- Required `server_id` breaks any flow that created resources without a node (templates, previews,
  e2e fixtures). PE-2 must grep every creator.
- Moving a resource between environments must keep domain uniqueness (per server, unaffected) and
  name uniqueness (per environment, can collide: 409).
- Nested URLs touch every link, breadcrumb and Playwright spec in `web/`.
- Shared variables must never leak secret values in list responses or deploy logs.

## 9. Delivery log

Implemented 2026-10-05 and deployed to the test box (`main` `8d4d2f7`, migrations 00034-00036). Worker: default variant;
every package went through independent review at high effort (Claude Code CLI) until MERGE-GO.

| Package | PR | Review rounds | Linear |
|---|---|---|---|
| Plan, UI brief, mockups, specs | #174 | docs only | JUS-30..36 |
| PE-1 projects and environments backend | #175 | 2 (+1 CI allowlist fix) | JUS-30 |
| PE-4 web foundation | #176 | 3 (+1 smoke-spec fix) | JUS-33 |
| PE-2 resources attach to environment and server | #177 | 2 (+1 e2e/smoke fixture fix) | JUS-31 |
| PE-3 shared variables backend | #178 | 3 | JUS-32 |
| PE-5 environment page, nested routes, create flows | #179 | 2 + merge pass | JUS-34 |
| PE-6 shared variables UI | #180 | 2 | JUS-35 |
| PE-7 e2e flow and docs | #181 | 2 | JUS-36 |
| PE-8 fixes from the live check | #182 | 1 | JUS-37 |

What review caught before shipping (all fixed): last-environment delete race (59/60 concurrent runs), name length counted
in bytes, FK races answering 500, soft-deleted services/databases pinning environment/project/server delete forever,
changing the server of a database/service orphaning its workload and data, unlocked service/database updates losing
moves, `project_name` clash with the compose project (now `compose_project`), previews left behind when their base app
moves, concurrent shared-variable PUT 500s and a lost secret update, a lock-order deadlock introduced by that fix
(project row first, then environment), stale create-wizard scope, template deploy ignoring the environment, missing
accessible names, a duplicate-key Save with no explanation, wrong project-vs-environment override display.

Live check on the box (Playwright MCP at 1280 and 480): create project, add environment, environment page, create a
database through the wizard, database Settings (server change refused with the reason shown, environment move panel),
shared variable saved and persisted, 480px layout. It found three defects that review and CI had missed, fixed in PE-8:
empty red alerts in the "Change" panel (Ref used without `.value`), a database named `pg-...` failing initdb because
its generated user began with `pg_` (now never), and a blank Version select. Re-verified after redeploy: no empty alerts,
`pg-shop` reaches running and healthy.

Process lessons: CI jobs the reviewers did not run (Playwright smoke, gated e2e) failed on three PRs (flat pages gone,
fixtures without `environment_id`/`server_id`); from PE-5 on the review prompt required running the smoke job exactly as
`ui-e2e.yml` does. A worker's local question TUI blocks it until answered (answer or have it use the orchestration ask).

Follow-ups (not scheduled; the detailed lists are in `docs/TODO.md`): PE-2 R2-R4 (service update waits up to the deploy
timeout for the lifecycle lock, error-message precedence, preview-creation sliver), PE-5 residuals (404 page for a missing
application/database detail, tab reset after a move, database node pinned in the UI for never-provisioned error rows),
PE-6 residuals (unsaved-changes guard), shared variables for services (applications only in v1), per-project roles.
Test data left on the box: project `storefront` (production, staging) with two databases (`pg-orders` in error from the
old naming bug, `pg-shop` running).
