# Library Audit (JUS-23)

Scope: `web/src` (validation, formatting, polling/retry, WebSocket backoff,
store merge, clipboard, URL handling, id generation) and Go (config parsing,
validation, retry/backoff, CLI flags, table output, cron/ID parsing, HTTP
middleware, argon2/JWT, SSH). Read-only; no source files changed.

G1 update (2026-10-04): verdicts F1/F2/F3 decided DO. F1 (zod) and F2
(@vueuse/core) landed in the G1 foundation PR; F3 (robfig/cron) was
evaluated in PR #157 and REJECTED (closed): a full implementation passed a
2080-case differential test with zero differences but was net +238/-55, so
the hand-written parser stays. Paths below reflect the post-restructure
layout: `web/src` is now
`app/`, `shared/`, `features/<module>/` (api, stores, pages, utils,
composables, components per feature).

Existing dependencies: `web/package.json` has
axios, naive-ui, pinia, vue, vue-router, zod, @vueuse/core (+dev tooling).
`go.mod` already has chi, pgx, viper, golang-jwt, x/crypto (argon2),
google/uuid, redis, goose, minio; `sethvargo/go-retry` and
`dustin/go-humanize` are indirect-only.

## Ranked findings

| # | Area | Proposed library | Replaces | Verdict |
|---|------|------------------|----------|---------|
| F1 | Form + API validation (web) | zod | ~150 lines hand rules across 8 sites | **DO (landed G1)** |
| F2 | `useMediaQuery`, clipboard copy (web) | @vueuse/core (tree-shaken fns) | ~45 lines in 3 sites | **DO (landed G1)** |
| F3 | Cron parse + schedule (Go) | robfig/cron/v3 | ~200 lines `internal/databases/backup_scheduler.go` | **REJECTED (PR #157)** |
| F4 | Struct validation for new Go endpoints | go-playground/validator/v10 | future code only | **MAYBE** |
| F5 | Relative time / absolute date (web) | date-fns (or native Intl) | `relativeTime`, `formatDate`, `expiryLabel` (~80 lines) | **SKIP** |
| F6 | Byte/duration formatting (web) | filesize / humanize | `formatBytes` (~22 lines), `durationText` x2 (~20 lines each) | **SKIP** |
| F7 | Debounce/throttle (web) | lodash-es / es-toolkit | nothing (no debounce usage found) | **SKIP** |
| F8 | HTTP retry/backoff (web `shared/api/http.ts`) | axios-retry / cockatiel | refresh-and-retry interceptor (~340 lines) | **SKIP** |
| F9 | WebSocket reconnect (web) | reconnecting-websocket / partysocket | `shared/composables/useWebSocket.ts` (510 lines) | **SKIP** |
| F10 | Polling cadences (web) | @vueuse `useIntervalFn` etc. | `shared/utils/polling.ts` (13 lines) + 5 `setInterval` sites | **SKIP** |
| F11 | Merge-by-id store helpers (web) | lodash `unionBy`/`merge` | `features/databases/utils/storeMerge.ts` (49 lines) | **SKIP** |
| F12 | Lease id generation (`shared/api/http.ts:232`) | nanoid | 1 line `Date.now`+`Math.random` | **SKIP** |
| F13 | Open-redirect guard (`features/auth/utils/authRedirect.ts`) | any URL-validation lib | `safeRedirect` (~10 lines) | **SKIP** |
| F14 | Go retry/backoff (`internal/updates/backoff.go:138`) | sethvargo/go-retry (already indirect) | ~5-line `backoffDelay` | **SKIP** |
| F15 | Go password hashing (`internal/auth/password.go`) | alexedwards/argon2id or similar | ~109-line PHC argon2id impl | **SKIP** |
| F16 | Go JWT (`internal/auth/jwt.go`) | already golang-jwt/jwt v5.3.1 | n/a (already a dependency) | **SKIP** |
| F17 | Go CLI flags (`cmd/gotham/*`, `cmd/signer/main.go:279`) | cobra / urfave/cli | stdlib `flag` usage (~4 small CLIs) | **SKIP** |
| F18 | Go table output | tablewriter / lipgloss | nothing (no table output found) | **SKIP** |
| F19 | Go request validation (existing code) | go-playground/validator/v10 | domain `validateEnv*` fns | **SKIP** |
| F20 | validator.js (web, e.g. email/host checks) | validator.js | covered by F1 | **SKIP** |
| F21 | EN/VI UI localization runtime (web) | vue-i18n `^11.4.13` (MIT, active) | hand-rolled locale state + English-only strings | **DO (landed I18N-1)** |

Bundle-size note: zod v3-classic (~14 kB gzip standalone; v4-classic 93 kB and
v4-mini 5.4 kB were rejected on size and API churn) now ships once in the entry
chunk (+54.6 kB raw / +12.7 kB gzip measured in PR #160), accepted as the price of a
single validation source. @vueuse/core adds ~0.04 kB gzip (tree-shaken).

## Details (adopted and rejected libraries)

### F1 — zod for all web validation — DO (landed G1)

- Locations: `web/src/features/auth/pages/LoginPage.vue:37-49`,
  `web/src/features/auth/pages/RegisterPage.vue:94-178`,
  `web/src/features/servers/components/AddServerWizard.vue:129-190,327-337`
  (state in `features/servers/composables/useAddServerWizard.ts`),
  `web/src/features/servers/components/EditServerModal.vue:61-~120`,
  `web/src/features/templates/api/templates.ts:146-223`,
  `web/src/features/applications/utils/wizardValidation.ts:1-33`,
  `web/src/features/databases/pages/DatabaseDetailPage.vue:64,296,619,712-724`
  (state in `features/databases/composables/useDatabaseDetail.ts`),
  `web/src/features/notifications/pages/NotificationsPage.vue:331-345`. Full
  site table in "Validation migration inventory" below.
- What the code does: per-form Naive UI `FormRules` (required/type/email/
  custom validators), regex constants (`NAME_PATTERN`, `HOST_PATTERN`,
  `USER_PATTERN`), password-strength helpers, template-field checks mirroring
  the server, env-key convention warnings, ad-hoc `message.error` guards.
- Library: **zod** `^3.25` (MIT, very active). G1 verdict: v3-classic.
  v4 was evaluated and rejected on bundle size (measured esbuild
  minify+gzip of the validation subset: v4-classic 93 kB, v4-mini 5.4 kB,
  v3-classic ~13 kB). v4-classic drags in the JIT-compile and JSON-schema
  pipeline with no tree-shaking relief; v4-mini meets the budget but uses a
  different schema API (`.check(...)`) that would burden every form-migration
  PR, so v3-classic stands.
- Replaces: ~150 lines of rules/helpers, plus every future form pays no new
  hand-rolled validator.
- Behaviour differences/risks: (1) email check changes from Naive UI
  built-in `type: "email"` to zod `z.string().email()` — edge-case address
  acceptance differs; keep current messages so visible behaviour is
  identical. (2) Template checks must stay a mirror of
  `internal/templates/render.go Field.check`; zod schema is client-side only,
  server remains authoritative — no contract change. (3) Env-key convention
  stays a warning, never a block (API accepts more than the convention) —
  model as `superRefine` warning channel or keep the predicate; see plan.
- Effort: M (one foundation PR + 3-4 mechanical form PRs).
- Verdict: **DO — single validation source replaces 8 divergent hand-rolled
  rule sets and enables runtime API-response parsing.** Foundation landed in
  G1 (`web/src/shared/validation/`); form migrations follow per the plan.

### F2 — @vueuse/core (useMediaQuery, useClipboard) — DO (landed G1)

- Locations: `web/src/shared/composables/useMediaQuery.ts:1-33` (~33 lines
  wrapping `window.matchMedia`); clipboard in
  `web/src/features/databases/composables/useCreateDatabaseWizard.ts:185-191`
  and `web/src/features/databases/composables/useDatabaseDetail.ts:122-128`
  (`navigator.clipboard` + toast).
- Library: **@vueuse/core** (MIT, very active). Size: ~1-3 kB gzip for the
  two imported functions (tree-shaken).
- Replaces: ~45 lines.
- Risks: `useClipboard` returns its own state/toast conventions; the current
  copy helpers pair the write with a Naive UI toast and a fallback error
  path — keep that wrapper either way. `useMediaQuery` is a 1:1 swap.
- Effort: S. Verdict: **DO — landed in G1 with F1.** `useMediaQuery.ts` is a
  thin re-export (5 importers); both database copy helpers share
  `shared/composables/useCopyText.ts`, which uses `useClipboard`
  `{ legacy: true }` (so plain-http pages copy via the textarea+execCommand
  fallback instead of resolving as a silent no-op) and shows the success
  toast only when `copied` is set, keeping both toast strings byte-identical.

### F3 — robfig/cron/v3 for Go cron parsing — REJECTED (PR #157, closed)

- Location: `internal/databases/backup_scheduler.go:49-~150`
  (`parseCron`, `parseCronField`, `cronSpec`, next-run computation).
- Evaluated library: **github.com/robfig/cron/v3** (`v3.0.1`, MIT).
- Evidence: a full implementation passed a 2080-case differential test with
  zero next-run differences but landed net +238/-55 lines: robfig only built
  value sets, so the numeric-only grammar gate and the historical error
  messages still needed the hand-written pre-check, and robfig's
  `Schedule.Next` re-fires the repeated fall-back hour and skips spring-gap
  days, so the wall-clock next-run scan stayed too. The dependency bought no
  deletion and changed DST edge semantics — not worth it. PR #157 closed,
  hand-written parser stays.
- Verdict: **REJECTED — keep the numeric-only parser; do not revisit
  without a grammar requirement change (named months/weekdays).**

### F4 — go-playground/validator for new Go endpoints — MAYBE

- Proposed library: **github.com/go-playground/validator/v10** (`v10.2x`,
  MIT, active).
- Replaces: future code only. Existing validators (`validateEnvKey` in
  `internal/deploy/applications.go:815`, `validateEnv` in
  `internal/services/service.go:753`, template `Field.check`) are
  domain-specific and DB-adjacent; retrofitting struct tags is churn with no
  behaviour gain.
- Verdict: **MAYBE — adopt for new request structs only, no retrofit.**

### F21 — vue-i18n for EN/VI UI localization — DO (landed I18N-1)

- Location: `web/src/shared/i18n/` (composer, locale persistence/sync,
  common en/vi catalogs, catalog parity helpers, stale-chunk copy),
  `web/src/shared/ui/LanguageSelect.vue`, wiring in `app/main.ts`
  (pre-mount preference + localized stale-chunk fallback) and `app/App.vue`
  (Naive `locale`/`date-locale`), invocation-time message resolution in
  `shared/validation/naiveAdapter.ts`, locale-aware `shared/utils/format.ts`
  and `shared/ui/chartModel.ts tickLabel`.
- Library: **vue-i18n** `^11.4.13` (MIT, very active), Vue-3-compatible
  Composition API (`legacy: false`), single global composer,
  `fallbackLocale: "en"`. Chosen over a hand-rolled interpolator because
  named parameters, pluralization and fallback already exist, are tested
  upstream, and every sibling feature package consumes the same contract.
  No date library (native `Intl` in the existing formatters), no new Pinia
  store (a `ref` + storage guards + one `storage` listener), no bundler
  plugin; feature catalogs load synchronously via a literal
  `import.meta.glob` so fallback never depends on a lazy chunk.
- Bundle: measured `vite build` gzip delta in the I18N-1 report
  (`.git/i18n-execution/foundation-report.md`).
- Verdict: **DO — landed in I18N-1 with the npm lockfile.**

## zod design (as built in G1)

Schemas per feature under `web/src/features/<m>/schemas/<name>.ts` (one
module per feature); each module exports schemas plus inferred types
(`export type Connection = z.infer<typeof connectionSchema>`). Shared
primitives (`portSchema`, …) live in `web/src/shared/validation/primitives.ts`
only when two features need the identical shape.

- `features/auth/schemas/auth.ts` — `loginSchema`, `registerSchema` (V1, V2);
  `passwordStrength` stays a separate display helper (not a gate).
- `features/servers/schemas/servers.ts` — `serverNameSchema`, `hostSchema`,
  `portSchema`, `sshUserSchema`, `connectionSchema` with
  `authMode`/`keyMode` discriminants (V3, V4 share this module).
- `features/templates/schemas/templates.ts` — `templateFieldSchema(field)`
  builder + `templateValuesSchema(fields)` (V5); pattern check keeps the
  try/catch-uncompilable-passes behaviour.
- `features/applications/schemas/env.ts` — env-key convention predicate +
  dropped-row counter (V6); warning channel preserved, never blocking.
- `features/databases/schemas/databases.ts`,
  `features/notifications/schemas/notifications.ts` (V7, V8).

`z.infer` types replace hand-written interfaces where the API shape is
identical (e.g. `TemplateValues`, form models for login/register/server
connection). Where the wire shape differs from the form shape (numbers as
strings in `TemplateValues`, conditional auth fields), keep both types and
convert explicitly at the submit boundary — do not force one shape.

Runtime parsing of API responses at the axios boundary:
`parseWith(schema, data, { context })` in
`web/src/shared/validation/parse.ts`, applied in the `list*/get*` functions
of each `features/<m>/api/*.ts` module to the envelope. On parse failure log
`console.warn` with the zod issues and return the raw payload (warn-only
phase); escalate to surfacing an error only after one release of telemetry.
Warn-only is load-bearing: strict parsing at the boundary can break pages on
any server/client skew. A `strict` option exists for unit tests only.

Naive UI form-rules adapter (`web/src/shared/validation/naiveAdapter.ts`):

- `ruleFrom(schema, opts?)` returning a Naive `FormItemRule` validator that
  runs `schema.safeParse(value)` and returns `true` on success or
  `new Error(firstIssueMessage)` on failure, preserving the exact current
  message strings (message catalog per schema, seeded from the inventory
  above). Conditional-required fields (V3 `keyName`/`keyId`/`password`) use
  the `when` predicate reading the same `authMode`/`keyMode` state the
  current computed rules read. `rulesFor(shape)` builds one rule per field;
  `fieldErrors(schema, value)` serves non-Naive callers.
- Triggers stay `["input", "blur"]` (and `["change"]` for `terms`) so
  validation timing does not change.

Error-message strategy: phase 1 is message-preserving (catalogue per feature
schemas module, see `shared/validation/messages.ts`; snapshot the current
strings in unit tests before the swap). Phase 2 (separate PR, optional) may
reword for consistency, with mockup comparison per `docs/design/`.

F5-F20 are SKIP: the hand-written code is small or already covered (see the table).
