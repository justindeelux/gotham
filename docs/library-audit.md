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

Bundle-size evidence (measured `vite build` gzip, G1; baseline index chunk
441.80 kB / 140.00 gzip, after index chunk 441.93 kB / 140.04 gzip):

- F1 zod: the foundation ships no production importer yet (no form migrated),
  so Rollup tree-shakes it to zero in the page bundle; the shipped delta is
  F2 only. Standalone esbuild minify+gzip of the validation subset:
  v4-classic 93 kB (rejected), v4-mini 5.4 kB (rejected on API churn),
  v3-classic ~14.1 kB (chosen). Expect ~10-14 kB gzip added when the first
  form migration (PR2) imports schemas in production code.
- F2 @vueuse/core, importing only `useMediaQuery` + `useClipboard`: +0.13 kB
  raw / +0.04 kB gzip on the index chunk (tree-shaken; unused exports are
  dropped by Vite/Rollup).

Bundle (templates-env-apps-services migration, PR #160): zod now ships once
in the entry chunk — index 441,967 B / 139,868 B gzip before, 496,560 B /
152,544 B gzip after (+54.6 kB raw / +12.7 kB gzip). It lands in the entry
chunk rather than a lazy shared chunk because the gating schemas are
reachable from the entry graph (template wizard, app wizard, import dialog
all render from first interaction). Accepted: single copy, no duplication,
matches the G1 ~14 kB estimate; lazy-splitting would only defer a schema
needed on first use.

## Details

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

### F5 — date-fns (or native Intl) for relative/absolute dates — SKIP

- Locations: `relativeTime` (`web/src/shared/utils/format.ts:137-180`),
  `formatDate` (`format.ts:183-196`), `expiryLabel` (`format.ts:199-218`).
- date-fns (`^4.x`, MIT, active) `formatDistanceToNow` renders "5 minutes
  ago", not the app's compact "5m ago" / "just now" / "in a moment" dialect
  used across 8+ pages; switching changes visible copy everywhere and still
  needs wrappers for `expiryLabel` and the `never`/`unknown` sentinels.
  Native `Intl.RelativeTimeFormat` + `Intl.DateTimeFormat` cover the absolute
  side with zero bytes but not the compact dialect either.
- Verdict: **SKIP — a library buys ~80 lines at the cost of changing
  user-visible copy; the 15 kB+ budget is not justified.**

### F6 — filesize/humanize for bytes/durations — SKIP

- Locations: `formatBytes` (`format.ts:10-31`, binary KiB/MiB dialect with
  custom 1/2-decimal rule), `durationText` in
  `web/src/features/applications/pages/ApplicationDetailPage.vue:198-216` and
  `web/src/features/services/pages/ServiceDetailPage.vue:155-~175` (`45s` /
  `3m12s` / `2h05m` dialect).
- A library (`filesize`, `humanize-duration`) renders different dialects
  (decimal KB, "3 minutes") and each site is ~20 lines already covered by
  unit tests.
- Verdict: **SKIP — trivial code with an intentional display dialect.**

### F7 — debounce/throttle library — SKIP

- A repo-wide search finds **no** debounce/throttle usage: search inputs
  filter synchronously (`matchesSearch` in `ServersPage.vue:70`,
  `DatabasesPage.vue:61`), timers are polling cadences, not input
  debouncing. There is nothing to replace and no dependency to prefer
  (none installed).
- Verdict: **SKIP — no code to replace; if search inputs ever need it, add
  a 10-line `setTimeout` helper, not a dependency.**

### F8 — axios-retry/cockatiel for `shared/api/http.ts` — SKIP

- Location: `web/src/shared/api/http.ts:90-341` (401 refresh-and-retry,
  single-flight promise, Web Locks cross-tab serialisation, localStorage
  lease fallback).
- This is domain logic (single-use rotating refresh tokens, stale-session
  protection), not generic retry: axios-retry (~2-3 kB) retries failures but
  knows nothing about token rotation or cross-tab locks; adopting it would
  wrap, not replace, the ~340 lines.
- Verdict: **SKIP — the complexity is the product requirement, not the
  mechanism.**

### F9 — reconnecting-websocket/partysocket for `useWebSocket.ts` — SKIP

- Location: `web/src/shared/composables/useWebSocket.ts:1-510`
  (`computeBackoffDelay` `:138-151`, `buildWebSocketUrl` `:158-177`,
  resubscribe-on-open, dual frame-count + byte-count buffer caps).
- Generic clients cover backoff but not the BE-3.2 wire contract
  (subscribe/unsubscribe frames, `log`/`disconnect`/`subscribed` kinds),
  the per-connect token getter (post-refresh rotation), or the 2000-frame /
  2 MiB buffer policy. Extracting `computeBackoffDelay` into a retry lib
  saves ~15 lines while forfeiting the jitter unit tests.
- Verdict: **SKIP — domain protocol code, not a generic socket.**

### F10 — polling abstraction for `setInterval` sites — SKIP

- Locations: `web/src/shared/utils/polling.ts:1-13` (two constants +
  selector), `web/src/features/servers/stores/servers.ts:25,86`,
  `web/src/features/databases/stores/databases.ts:37,108`,
  `web/src/features/applications/stores/applications.ts:28,203`,
  `web/src/features/servers/pages/ContainersPage.vue:103,380`,
  `web/src/features/databases/pages/DatabaseDetailPage.vue:361,528`,
  `web/src/features/servers/pages/ServerDetailPage.vue:112`.
- Each site is `setInterval` + `clearInterval` with domain cadences; a
  composable (`useIntervalFn`) saves ~3 lines per site at the cost of a
  dependency and a lifecycle-semantics review of each poller.
- Verdict: **SKIP — 13-line module plus idiomatic timers; no library earns
  its place.**

### F11 — lodash merge for `storeMerge.ts` — SKIP

- Location: `web/src/features/databases/utils/storeMerge.ts:1-49`.
- `mergeBackupsById` keeps local `running` rows the server has not reported
  yet; `mergeDatabasesById` honours a `deletedIds` tombstone set so a stale
  list cannot resurrect a deleted row. `unionBy`/`merge` express neither
  semantic; a rewrite around lodash would be longer and less clear.
- Verdict: **SKIP — the value is the domain semantics, which no generic
  merge provides.**

### F12 — nanoid for the refresh-lease id — SKIP

- Location: `web/src/shared/api/http.ts:232`
  (`` `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}` ``).
- nanoid (`^5.x`, MIT, <1 kB) non-secure variant is the same `Math.random`
  mechanism with the same collision profile; the crypto variant needs a
  secure context, which is exactly why the hand-rolled fallback exists
  (plain-HTTP support). One line either way.
- Verdict: **SKIP — 1 line with a documented insecure-context rationale.**

### F13 — URL-validation library for `safeRedirect` — SKIP

- Location: `web/src/features/auth/utils/authRedirect.ts:27-37`
  (open-redirect guard: local absolute path, rejects `//host` and `/\host`).
- A library cannot know the app's redirect policy; the check is 10 lines,
  security-critical, and unit-tested. Any replacement must be re-audited
  anyway.
- Verdict: **SKIP — security boundary code stays hand-written and reviewed.**

### F14 — sethvargo/go-retry for `backoffDelay` — SKIP

- Location: `internal/updates/backoff.go:138-140`
  (`base * 2^(count-1)`, capped). `sethvargo/go-retry` is already an
  indirect dependency (`go.mod:43`).
- Promoting it to direct to compute a 5-line exponential delay adds API
  surface (fibonacci/jitter helpers nobody else needs) for zero behaviour
  gain; persistence/seed logic around the delay is the real code and stays.
- Verdict: **SKIP — strict 5-10-line rule applies.**

### F15 — argon2 wrapper library — SKIP

- Location: `internal/auth/password.go:1-109` (PHC `$argon2id$` encode,
  params-from-hash decode, constant-time verify, RFC 9106 parameters).
- Correct, tested (`password_test.go`), and already on `golang.org/x/crypto`
  (a first-party dependency). Swapping hash libraries forces a hash-format
  migration and a security re-review for negative line savings.
- Verdict: **SKIP — never churn a working password-hash implementation to
  save lines.**

### F16 — JWT — SKIP (already a dependency)

- `internal/auth/jwt.go:12` uses `github.com/golang-jwt/jwt/v5 v5.3.1`
  (EdDSA, issuer + expiry enforcement). Nothing hand-rolled. No action.

### F17 — cobra/urfave-cli for Go CLIs — SKIP

- Locations: `cmd/gotham/admin.go:72,134`, `cmd/gotham/update.go:101`,
  `cmd/signer/main.go:173,279` — stdlib `flag.FlagSet` subcommands.
- The CLI surface is 3-4 admin/operator commands; cobra adds command-tree
  machinery, help generation and shell completion nobody asked for.
- Verdict: **SKIP — stdlib `flag` is the right size for this surface.**

### F18 — table output library — SKIP

- No ASCII-table rendering found in `cmd/` or `internal/` (output is
  `log/slog` + JSON API). Nothing to replace.
- Verdict: **SKIP — no code to replace.**

### F19 — go-playground/validator retrofit for existing Go validation — SKIP

- `validateEnvKey` (`internal/deploy/applications.go:815`), `validateEnv`
  (`internal/services/service.go:753`), template `Field.check`
  (`internal/templates/*`), cron field checks — all encode container-domain
  constraints, not struct shapes. A tag-based retrofit touches every call
  site and splits each rule across struct tag + custom function.
- Verdict: **SKIP — retrofit churn with no behaviour gain (see F4 for new
  code).**

### F20 — validator.js for web field checks — SKIP

- Email/host/username checks are subsumed by F1 (zod). validator.js is
  CommonJS with poor tree-shaking; importing it for `isEmail`/`isIP` pulls
  far more than used.
- Verdict: **SKIP — covered by the zod migration at a third of the weight.**

## Validation migration inventory (mechanical migration list)

Current rule at each site; first pass preserves every message string verbatim
so the migration has zero user-visible diff.

| # | Site | Current rule(s) |
|---|------|-----------------|
| V1 | `features/auth/pages/LoginPage.vue:37-49` | `email`: required "Email is required" + Naive `type: "email"` "Enter a valid email address"; `password`: required "Password is required" |
| V2 | `features/auth/pages/RegisterPage.vue:143-178` + helpers `:94-137` | `email`: required + `type: "email"`; `password`: required + custom `len >= 10 && countCharClasses >= 2` (classes = lower/upper/digit/symbol); `confirmPassword`: required + `=== form.password`; `terms`: `=== true`. Strength meter `scorePassword` (`features/auth/utils/passwordStrength.ts`, display-only via `PasswordStrengthMeter.vue`) is not a gate |
| V3 | `features/servers/components/AddServerWizard.vue:129-190` + `:327-337` (state in `features/servers/composables/useAddServerWizard.ts`) | consts `HOST_PATTERN`, `NAME_PATTERN`, `USER_PATTERN`; `name`: required + NAME_PATTERN; `ip`: required + `isValidHost` (IPv4 literal or DNS hostname); `port`: `type: "number"` required + integer 1-65535; `sshUser`: required + USER_PATTERN (Unix username); `keyName`/`privateKey`/`keyId`/`password`: conditional required by `authMode`/`keyMode` |
| V4 | `features/servers/components/EditServerModal.vue:61-~120` | same shapes as V3 (`name`, `ip` + host check, `port` 1-65535, `sshUser`, conditional `keyId`); must share schemas with V3 after migration, not duplicate them |
| V5 | `features/templates/api/templates.ts:146-223` | `checkTemplateValue` per `TemplateFieldType`: text/secret (required-trim, `max_length` in code points via `[...value]`, anchored `pattern` mirror of server `^(?:pattern)$`, uncompilable pattern = pass); number (trim, `/^-?\d+$/`, min/max); bool (`"true"`/`"false"`); select (must be in `options`, required-emptiness). `validateTemplateValues` collects first message per field |
| V6 | `features/applications/utils/wizardValidation.ts:1-33` | `isRecommendedEnvKey` (`/^[A-Z][A-Z0-9_]*$/` on trimmed key) = warning only; `hasEnvKeyWarnings`; `countDroppedEnvRows` (nameless row with value). Warning-not-block is load-bearing (API accepts more) |
| V7 | `features/databases/pages/DatabaseDetailPage.vue:64,296,619,712-724,1624` (state in `features/databases/composables/useDatabaseDetail.ts`) | rename: `NAME_PATTERN` test + toast "Name must be 1-63 characters…", submit disabled unless match; schedule: cron non-empty ("Cron expression is required, e.g. 0 2 * * *.") — no grammar check client-side; target: name/endpoint+bucket/keys required toasts |
| V8 | `features/notifications/pages/NotificationsPage.vue:331-345` (dialog state in `features/notifications/composables/useChannelDialog.ts`) | `canSubmit`: name non-empty + `events.length > 0` + (`resourceType === ""` or `resourceId !== ""`); `parseRecipients`: split `/[\s,]+`, trim, drop empties |
| V9 | Sweep targets (confirm during migration) | `EnvEditor.vue`, `ComposeEditor.vue`, `DomainEditor.vue`, `CertificateForm.vue`, `features/*/pages` env/compose editors, `features/teams` invite form, `useCreateDatabaseWizard.ts` — grep `message.error\|:rules\|validator` in each; any ad-hoc check found becomes a schema in the matching feature module |

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

## Migration plan (ordered; PR2-5 parallel after PR1)

- PR1 (foundation, S): add `zod` dependency; create
  `web/src/shared/validation/` with `naiveAdapter.ts` + message catalog +
  unit tests snapshotting current messages; no form touched. **Done in G1,
  plus the @vueuse/core swaps.**
- PR2 (auth forms, S, needs PR1): V1 + V2 to `features/auth/schemas/auth.ts`;
  replace `countCharClasses` inline rule with schema refine; keep
  `scorePassword` meter untouched.
- PR3 (server forms, S, needs PR1): V3 + V4 to
  `features/servers/schemas/servers.ts` (single shared module; delete the
  duplicated patterns in the modal).
- PR4 (templates + env, M, needs PR1): V5 to
  `features/templates/schemas/templates.ts` (differential-test against
  `Field.check` vectors), V6 to `features/applications/schemas/env.ts`
  keeping warning-not-block.
- PR5 (misc + boundary parsing, M, needs PR1): V7 + V8 + V9 sweep;
  warn-only `parseWith` on `features/templates/api/templates.ts` and
  `features/servers/api/*` envelopes first, remaining `features/<m>/api`
  modules after.
- Each PR: `npm run build`, `npm run type-check`, `npm test` green;
  bundle-size check (`vite build` report) attached to PR1 proving the gzip
  delta.

## Verification notes

- G0 sizes were published figures, not measured deltas. G1 measured the
  before/after `vite build` comparison; figures are in the G1 report.
- Impact analysis on G1 edits: `useMediaQuery` LOW (4 direct callers in the
  graph index, 5 importers by grep — index paths predate the restructure);
  `copyText` LOW (2 candidates, max 1 impacted). No HIGH/CRITICAL findings.
- The load-bearing coupling found by inspection is V5's mirror of
  `Field.check` in `internal/templates/render.go`, noted in F1/PR4.
