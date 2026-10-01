# Phase 7 — Services & Templates (W10)

**Goal:** `docker-compose` service deploys + a "one-click" template system (WordPress, Nextcloud...).

**Exit criteria (Milestone M7):**
- Deploy any compose file (parse + validate + run via agent).
- YAML templates in `templates/` render to compose with user-supplied variables.
- Gallery in the UI: pick WordPress → fill domain/admin → 1-click deploy → reachable.

**Rollback:** a service is a compose project — rollback = redeploy the old compose (compose file versioned per deploy). Templates are static renders — editing a template does not affect already-deployed services.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to Phase 8 (see the phase gate in [`../../AGENTS.md`](../../AGENTS.md)).

---

## BE-7.1 — Compose services — `ws/p7-compose`

- **Context brief:** a service = a docker-compose project running on 1 server. Either use `docker/compose/v2` or call agent RPCs (agent with compose CLI installed) — decision: **agent side** uses the compose CLI to leverage exact compose behavior; the CP stores the compose yaml in the DB (`services` table, `compose_yaml` column, versioned per deploy).
- **Deliverables:**
  - Agent RPCs: `ComposeValidate`, `ComposeUp`, `ComposeDown`, `ComposeLogs`, `ComposePs` (added to the Phase 2 proto).
  - `internal/services/`: parse + validate (schema check), env var substitution, domain map (service labels → Traefik via Phase 6), storage mount.
  - `services`, `service_deploys` migrations.
  - CRUD routes + deploy/stop/restart.
  - Tests: deploy a sample compose (2 services + volume) → ps correct → per-service logs.
- **Verify:** sample compose runs, domain maps to the right service, restart keeps state.
- **Depends on:** Phase 6 (domains), Phase 3. Parallel with BE-7.2.

## BE-7.2 — Template engine — `ws/p7-templates`

- **Context brief:** design the template format: `templates/{slug}/` directory with `template.yaml` (metadata: name, icon, description, form fields: input type, default, required) + `compose.yaml` (Go template or `{{ .field }}` placeholders). The engine renders fields → compose → deploys via BE-7.1. Catalog shown in the UI.
- **Deliverables:**
  - Template schema + validator; render engine (templates must not execute code — string substitution only).
  - First template set: **WordPress** (wp + mysql, volumes, domain), **Nextcloud** (nextcloud + postgres/redis), **n8n**, **Uptime Kuma** (4 total to prove the format).
  - Routes: `GET /api/v1/templates`, `GET /api/v1/templates/{slug}`.
  - Tests: render each template → valid compose → deployable (reuse BE-7.1 tests).
- **Verify:** render WordPress from sample input → compose contains the right domain/env; 1-click deploy succeeds.
- **Depends on:** BE-7.1 (deploys through it). Parallel with the gallery part of FE-7.1.

## FE-7.1 — Services UI + template gallery — `ws/p7-services-ui`

- **Context brief:** 2 screens: (1) services list + detail (compose editor, env, per-service logs, start/stop/restart); (2) template gallery: cards (icon, name, description) → form rendered from the template schema (dynamic form by field type) → deploy button → deploy logs screen.
- **Deliverables:** `/services`, `/services/{id}`, `/templates` pages; `DynamicForm` component (renders from schema), `ComposeEditor`.
- **Verify:** e2e: gallery → WordPress → fill form → deploy → reach the WordPress domain → finish setup.
- **Depends on:** BE-7.1 (contract), BE-7.2 (gallery schema).
