---
name: vue-review
model: sonnet
description: Review Vue 3 / TypeScript changes in gotham's web/ SPA (src/app, src/features/<module>, src/shared; Pinia, vue-router, Naive UI, vue-i18n, zod validation) for reactivity bugs, XSS, component contracts, project style and UI-design rules. Use whenever the user asks to review, check or audit .vue/.ts code under web/, a frontend PR or diff, or a UI change — and proactively before merging any change that touches web/, even if they don't say "review".
---

# Vue review (gotham web/)

Report findings only; do not rewrite code. Stack: Vue 3.5 `<script setup lang="ts">`, Vite, Pinia 3 + axios, Naive UI, vue-router 4, vue-i18n, zod, Vitest, Playwright. Layout: `web/src/{app,features/<module>,shared}`. Project rules live in `AGENTS.md`.

## 1. Scope the diff

- PR: `gh pr view --json baseRefName,mergeStateStatus,statusCheckRollup`; red checks/conflicts → stop and report. Never hard-code `main`.
- Local: `git diff --staged -- 'web/**'` then `git diff -- 'web/**'`; shallow → `git show --patch HEAD -- web`.
- Read the whole component and its store/composable before commenting. Use GitNexus `impact`/`context` for who uses a changed composable or store; `UNKNOWN` ≠ safe.
- No `web/` changes → nothing to do here; say so.

## 2. Run the gates (from `web/`)

```bash
npm run lint && npm run type-check && npm run build
npm run test:unit
npm run e2e      # whole Playwright suite if UI or its API changed (see .local-workflow/docs/test-server.md: own throwaway Postgres+Redis, never the shared gotham-dev-* containers)
```
Red gate = BLOCK. If `web/` changed, also confirm `internal/server/webdist` was rebuilt and committed (CI drift check).

## 3. Project conventions (flag deviations — these are enforced here, not generic Vue advice)

- SFC block order is always `<script>` → `<template>` → `<style>`; omit unused blocks.
- `interface` over `type` (except unions/intersections); strict TS; 2-space, double quotes, semicolons, trailing commas, ~100 cols; descriptive names (`isActive`, not `act`).
- **Validation = zod** via `web/src/shared/validation` (`ruleFrom`, `parseWith`), schemas in `features/<m>/schemas/`. A new ad-hoc `validator:` closure is a finding.
- Feature code stays in `features/<module>/`; reusable pieces go to `shared/`. Cross-feature deep imports (feature A reaching into feature B internals) are a finding.
- **UI**: Naive UI is the component base — don't rebuild components from raw CSS; colors/spacing/radii come from the design tokens in `web/src/shared/styles/tokens.css`, not hard-coded hex/px.
- **Modals**: header and footer fixed, only the content region scrolls, height bounded to viewport (`.app-modal` / `.wizard-modal` contract in `shared/styles/main.css`).
- **Copy/i18n**: user-facing strings go through vue-i18n in the project language; no hard-coded literals in templates.
- Icon-only buttons need a tooltip and an accessible name (recent JUS-156 work).

## 4. Review priorities

**CRITICAL — security**
- `v-html` / `innerHTML` / `h(..., { innerHTML })` with non-constant data and no sanitizer at the same site. Log/terminal output from builds or containers is untrusted — it must be escaped or rendered as text (ANSI → safe spans).
- `:href`/`:src`/router targets from API data without scheme allowlist (`http`, `https`, `mailto`); user-controlled `:is`; `:style` from raw strings.
- Tokens/secrets in `localStorage`/`sessionStorage`/`VITE_*` env or logged to console; session must rely on httpOnly cookies or the existing auth store pattern.
- Missing client-side authz is not a bug by itself (server enforces) — but UI that hides a control as its only protection should be noted.

**CRITICAL — reactivity**
- Destructuring a `reactive` object or Pinia store without `toRefs`/`storeToRefs`; reassigning a whole `reactive`; `reactive()` on primitives.
- `watch(() => ref, …)` instead of `() => ref.value`; watching a destructured prop bare (3.5 compile error — wrap in getter); `watchEffect` reading state after an `await` (tracking stops).
- Impure `computed` (side effects, async, DOM); props mutated in the child.

**HIGH — lifecycle, realtime, async (this app is WebSocket-heavy)**
- WebSocket/EventSource/timers/listeners/subscriptions/`axios` requests not cleaned up in `onUnmounted`/`onScopeDispose` or via `AbortController`; reconnect loops without backoff or without stopping on unmount.
- Stale-response races: route/param change fires a new fetch but an older response overwrites newer state (guard with request id/abort).
- Composables that capture `.value` once instead of accepting `MaybeRefOrGetter` + `toValue`; module-scope side effects; not named `useXxx`; lifecycle hooks registered after an `await` or conditionally.
- Pinia: mutating multi-field business state outside actions; non-serializable state; actions that leave state half-updated on failure; no error handling/loading flag where the UI depends on it. Prefer setup stores; custom `$reset` if needed.
- Router: guards returning `false` with no feedback; `route.params` destructured at setup top level (use `computed(() => route.params.id)`); lazy routes with no error/loading fallback.

**HIGH — templates & components**
- `v-for` without a stable `:key` or keyed by index/object; `v-if` with `v-for` on one element; `v-model` on a computed with no setter; `v-bind="$attrs"` without `inheritAttrs: false`.
- Props without types / emits not declared (tuple form); camelCase event names in templates; `defineModel` not used for two-way binding in new code; `defineExpose` leaking internals.
- Options API, mixins, or `<script>` without `setup`/`lang="ts"` in new code; raw `document.querySelector` instead of `useTemplateRef`.
- SFC >300 lines (template + script): suggest extraction — but only as MEDIUM unless it hides a real bug.

**MEDIUM**
- Accessibility: non-semantic clickable `div`, missing labels/`aria-*`, focus lost after modal/drawer close, color-only state.
- Forms without `<form>` + `@submit.prevent`; debounce via manual `setTimeout` without cleanup.
- Performance: `ref` on huge replace-only data (`shallowRef`), `KeepAlive` without `max`, heavy work in `computed`, `v-show` vs `v-if` misuse, un-lazy heavy routes.
- Tests: new behavior without a Vitest spec (props/emits/rendered output, not private state); Pinia tests need `createTestingPinia({ createSpy: vi.fn })` or a fresh `setActivePinia(createPinia())`; UI-flow changes need a Playwright spec.

## 5. Output

Group by severity. Each finding:

```
[SEVERITY] short title
File: web/src/features/x/Foo.vue:42
Issue: one sentence.
Why: concrete failure (what the user sees / what leaks).
Fix: specific change.
```
Cite file:line for every finding; skip anything `eslint`/`vue-tsc` already reports unless the gate is red. For pure `.ts` changes with no Vue involvement, still apply the TS/style rules in §3 but skip Vue-specific lanes. End with:

```
## Review Summary
| Severity | Count | Status |
| CRITICAL | n | pass/block |
| HIGH     | n | pass/block |
| MEDIUM   | n | info |
Verdict: APPROVE | WARNING (MEDIUM only) | BLOCK (any CRITICAL/HIGH or red gate)
```
