# Phase 17 — Update settings (Settings → Updates)

**Goal:** operators drive self-update from the UI: see version / channel / latest release and notes, check on demand, apply with progress and failure state, and manage a persisted check / auto-apply schedule that hot-reloads without restart. Linear: JUS-93.

## Current state (verified)
- `internal/updates`: `GET /v1/updates/check` (any caller), `POST /v1/updates/apply` (platform admin). `AUTO_UPDATE` / `AUTO_UPDATE_INTERVAL` are env-only; the loop is a fixed ticker started once in `Server.Run`.
- `web/src/features/version` only renders the sidebar version tag.
- No instance timezone setting exists on `main` (JUS-92 is in flight) → schedules run in **UTC**; the response carries `timezone: "UTC"` so the UI label switches when the instance setting lands.

## Design decisions
- **Schedule model** (`update_schedule` single-row table, migration 00047, forward-only): `check_enabled`, `auto_apply`, `channel` (`stable|beta`), `frequency` (`interval|daily|weekly`), `interval_minutes` (60..43200), `at_time` (`HH:MM`), `weekday` (0=Sunday..6). Auto-apply requires check enabled.
- **Env stays the default and kill switch.** No row → defaults come from `AUTO_UPDATE` (check + apply), `AUTO_UPDATE_INTERVAL`, `GOTHAM_UPDATE_CHANNEL`. `FEATURE_UPDATES=false` removes all routes and the loop. Saving a schedule writes the row; env is not rewritten.
- **Hot reload:** the scheduler goroutine always runs (when the feature is on) and re-reads the schedule from an in-memory holder; `PUT` stores to the DB then calls `Reload`, which wakes the loop (channel signal) to recompute the next run. Survives restart because the row is loaded at startup.
- **Check results** are cached by the loop (`last_checked_at`, `last_error`, latest release) so the page shows them without a network call; the Check button forces a refresh.
- **Apply** reuses `Service.Apply` and the existing backoff semantics. The status response exposes `backoff` (version, until) from the existing backoff store and `last_update` (result, detail) so a failed / rolled-back update shows its error. After apply returns `staged`, the UI polls status until the version changes or the result settles, tolerating connection loss while the service restarts.
- **Server-side validation:** channel / frequency enums, interval bounds, `HH:MM`, weekday range; unknown JSON fields rejected; 422 with field messages.

## API (all under `/api/v1`)
| Route | Auth | Purpose |
|---|---|---|
| `GET /updates/check` | any authenticated | existing; now also returns `last_checked_at`, `check_error`, `channel` of the running config |
| `POST /updates/check` | platform admin | force a refresh, same body |
| `POST /updates/apply` | platform admin | existing |
| `GET /updates/schedule` | any authenticated | schedule + next run + backoff |
| `PUT /updates/schedule` | platform admin | validate, persist, hot reload |

## Web
`web/src/features/updates/` (api, schemas (zod), composables, components, pages, locales en + vi). Route `settings/updates` (name `updates`), sidebar System → Updates entry becomes live. Non-admins see status read-only (actions hidden). Mockup: `docs/design/updates.html` (written before the port). Naive UI themed with existing tokens; destructive/irreversible apply uses a confirm modal following the `.app-modal` contract.

## Work packages
1. Migration + sqlc + store adapter. 2. `updates` schedule type, validation, holder, scheduler rewrite, tests. 3. Routes. 4. Mockup. 5. Vue port + i18n + rebuilt `webdist`. 6. Verify + PR.

## Verify
`go build ./...`, `go test ./...`, `golangci-lint run`, `go vet`, `npm run build`, `npm run type-check`, rebuilt `webdist` committed.

## Out of scope
Instance timezone (JUS-92), per-agent update schedules, cron expressions beyond the presets.
