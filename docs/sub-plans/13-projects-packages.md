# Phase 13 work-package specs

Plan: `docs/plans/13-projects-environments.md` (API contract in section 6, decisions in section 3).
UI: `docs/design-plans/projects-environments.md` and `docs/design/{projects,project-detail,environment}.html`.
Each package = one worktree, one branch, one PR. Owner approval is required before the first
package starts; worker and reviewer variants are chosen by the owner each session.

## Common rules

- Read `AGENTS.md`, `docs/ORCHESTRATION.md` and the plan first. English only in code, comments,
  commits and PR text.
- GitNexus: `impact` before editing any symbol (note the risk in the report); `detect-changes`
  before committing (partial/truncated is not clean).
- Invariants before pushing: `go build ./...`, `go test ./...`, `golangci-lint run`, `go vet`
  (Go); `npm run build`, `npm run type-check`, `npm test` in `web/` (web). Rebuilt
  `internal/server/webdist` is committed whenever `web/` changes (`git clean -fdq
  internal/server/webdist` first).
- Web: compare with the mockups first, keep Naive UI, per-mount state (no module-scope refs),
  barrels without `.vue`, zod schemas in `features/<m>/schemas/` through `ruleFrom`, single submit
  guard, container queries for narrow layouts, verify with Playwright MCP at 1280 and 480 widths
  (screenshots under `.playwright-mcp/`).
- Migrations are forward-only: only add new ones. Private scratch databases in tests.
- Work only inside the ownership list; anything else is an escalation. Draft PR against `main`;
  never commit `opencode.json` or scratch files.
- Finish with `worker_done`: PR URL, head SHA, files changed, commands with pass/fail, GitNexus
  summary, what could not be verified.

## PE-1 Backend: projects and environments (branch `feat/pe1-projects-backend`) — Linear JUS-30

1. Migration `00034_projects_environments.sql`: `projects`, `environments` (plan section 4), unique
   indexes on lower(name), cascade from teams/projects. In the same migration truncate
   `applications, databases, services` with `CASCADE` and say in a comment that this is deliberate
   (no production data). Do not add `environment_id` to the resource tables here (PE-2 does).
   Down drops the two tables.
2. `internal/projects`: model, repository (sqlc queries in `internal/store/queries/projects.sql`),
   service, routes mounted like the other packages (team scope, `CanWrite`), `doc.go`. Implements
   the project and environment routes of the contract. Resource counts come from a repository
   interface that PE-2 later satisfies; until then return zeros behind a clearly named stub and a
   test that fails when PE-2 forgets to wire it (e.g. the counter is a required constructor arg).
3. Creating a project and its `production` environment is one transaction. Deletion rules per the
   contract (the "still has resources" checks go through the counter interface).
4. Wire into `internal/server`; follow the feature-flag pattern of the other mounts if one applies.
5. Tests: service, repository against a scratch DB, HTTP (viewer 403, cross-team 404, duplicate
   names 409, last environment, error bodies exact), migration up/down.

Ownership: `internal/projects/**`, `internal/store/**` (migration, queries, sqlc output),
`internal/server/**` wiring, docs route mention. No web.

## PE-2 Backend: attach resources to environments and servers (branch `feat/pe2-resources-backend`, after PE-1) — Linear JUS-31

1. Migration `00035_resources_environment.sql`: `environment_id uuid NOT NULL REFERENCES
   environments(id) ON DELETE RESTRICT` on applications, databases, services; `server_id NOT NULL
   ... ON DELETE RESTRICT`; drop the three `(user_id, name)` unique indexes and add
   `(environment_id, name)` (keep the `WHERE deleted_at IS NULL` partial form for databases and
   services). Tables are empty after PE-1's truncate. Keep per-server domain uniqueness.
2. Create and update paths of `internal/deploy`, `internal/services`, `internal/databases` and
   `internal/templates` take and validate `environment_id` (same team) and `server_id` (required,
   same team, usable) per the contract. Move and server-change updates with the 409 guards. Grep
   every creator (`CreateApplication`, preview creation, template render/deploy, e2e fixtures).
3. Previews copy `environment_id` and `server_id` from the base application; previews stay out of
   the default environment listing.
4. List/get responses gain `environment_id, environment_name, project_id, project_name` and the
   `environment_id` / `project_id` filters. Implement `GET /environments/{id}/resources`.
5. Implement the resource counter used by PE-1 and make server deletion answer 409 with the
   blocking resources.
6. Tests: each creator rejects missing/foreign environment and server; move collision 409; server
   change blocked mid-deploy; preview inherits; delete blocks; update existing tests and fixtures.

Ownership: `internal/deploy/**`, `internal/services/**`, `internal/databases/**`,
`internal/templates/**`, `internal/servers/**` (delete guard), `internal/projects/**` (counter),
`internal/store/**`, `internal/e2e/**` fixtures, `internal/server/**` wiring.

## PE-3 Backend: shared variables (branch `feat/pe3-shared-variables`, after PE-2) — Linear JUS-32

1. Migration `00036_shared_variables.sql`: `shared_variables(id, project_id, environment_id NULL,
   key, value, ciphertext, secret, created_at, updated_at)`, cascade from project/environment,
   unique `(project_id, coalesce(environment_id, nil uuid), key)`. Secret values are sealed with the
   same AES-GCM helper as application `secrets` (read `internal/deploy` env/secret code first).
2. Routes `GET/PUT` for project and environment variables per the contract, secrets never returned,
   `PUT` keeps a secret's existing ciphertext when `value` is omitted.
3. Deploy spec: merge project < environment < application variables when building the payload for
   an application deploy (preview deploys too). Services and databases are untouched.
4. Tests: masking in every response and log path, precedence, replace semantics, key validation,
   viewer 403, cross-team 404, preview merge.

Ownership: `internal/projects/**` (variables), `internal/deploy/**` (spec merge), `internal/store/**`,
`internal/server/**` wiring.

## PE-4 Web foundation (branch `feat/pe4-projects-web`, after the contract only) — Linear JUS-33

1. `web/src/features/projects/` with `api/`, `schemas/` (zod for every response and form), a store
   or composables, `index.ts` barrel. Types and calls follow the contract exactly; mock against it
   until PE-1 is merged, then run against the real API.
2. Pages: Projects, Project (environments tab; the shared-variables tab is a placeholder filled by
   PE-6), breadcrumb component, create/rename/delete dialogs with the blocked-delete messages.
3. Router and sidebar: **Projects** replaces Applications, Services and Databases. The new routes
   are added; the old flat routes are removed in PE-5, not here.
4. Match `docs/design/projects.html` and `project-detail.html`; unit tests for the schemas,
   composables and page states (empty, loading, error, viewer); Playwright layout spec at 1280 and
   480 widths in the style of `web/tests/profile-layout.spec.ts`.

Ownership: `web/src/features/projects/**`, `web/src/app/router/**`, the sidebar component,
`web/tests/**`, `internal/server/webdist` rebuild.

## PE-5 Web: environment page, nested resources, create flows (branch `feat/pe5-environment-web`, after PE-2 and PE-4) — Linear JUS-34

1. Environment page per `docs/design/environment.html`: unified resource table with type tabs and
   preview switch, Add resource dialog.
2. Nested routes `/projects/:p/environments/:e/{applications,services,databases}/:id`; move the
   existing resource pages under them with breadcrumbs; delete the flat list pages and routes
   (redirect `/applications`, `/services`, `/databases` to `/projects`). Update every internal link.
3. Create flows (application wizard, service, database, template deploy) take project/environment
   from the route (read-only summary with a change link) and a required server picker
   (`GET /servers`; offline disabled with a reason; preselected when single).
4. Resource settings: change server and move environment with the contract's errors rendered
   inline. Dashboard and any other place that linked to the flat pages follow.
5. Tests: unit and layout specs for the new pages and dialogs at 1280/480; existing specs updated.

Ownership: `web/src/features/{applications,services,databases,templates,projects,dashboard}/**`,
`web/src/app/router/**`, `web/tests/**`, `internal/server/webdist`.

## PE-6 Web: shared variables UI (branch `feat/pe6-shared-variables-web`, after PE-3 and PE-4) — Linear JUS-35

1. Shared-variables editor (project tab and environment page section), secret write-only
   behaviour, precedence hint, per `docs/design/project-detail.html`.
2. Application variable editor lists inherited rows read-only with their origin and allows
   overriding a key.
3. Tests and layout spec at 1280/480.

Ownership: `web/src/features/{projects,applications}/**`, `web/tests/**`, `internal/server/webdist`.

## PE-7 Integration, docs, test box (branch `docs/pe7-delivery`, after all) — Linear JUS-36

1. e2e flow in `internal/e2e` (or the existing e2e harness): create project, environment, an
   application on a node, deploy, variables precedence.
2. Update `docs/README.md`, `docs/TODO.md`, `docs/test-server.md` (wipe note) and
   `docs/plans/13-projects-environments.md` section 9 (delivery log, follow-ups).
3. Redeploy the test box (recipe in `docs/test-server.md`; note that migrations 00034/00035 wipe
   resource data) and run the live check through Playwright MCP: create a project, add an
   environment, create an application and a database on the node, deploy, change server, move
   environment, set shared variables and see them applied.

Ownership: `internal/e2e/**`, `docs/**`.
