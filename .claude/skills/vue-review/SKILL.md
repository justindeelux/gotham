---
name: vue-review
model: sonnet
description: Review Gotham web/ changes against the repository's Vue skills and references, accessibility, design, API and security requirements. Use for frontend code reviews and before merging Vue, TypeScript, styling or UI-flow changes.
---

# Vue review (gotham web/)

Review read-only; do not fix code, push, merge or post comments unless explicitly asked.
Stack: Vue 3.5+ Composition API, Vite, Pinia + axios, Naive UI, vue-router, vue-i18n,
Zod, Vitest and Playwright; use versions in `web/package.json`. Layout:
`web/src/{app,features/<module>,shared}`. Read `AGENTS.md`. Project rules and acceptance
criteria take precedence over source examples.

## 1. Load the implementation requirements

Read [vue](../../../.opencode/skills/vue/SKILL.md) and
[security-patterns](../../../.opencode/skills/security-patterns/SKILL.md) on every web
review, then load the relevant sources below. Links resolve from this file; command
paths are repository-relative. Apply the source requirements, not only these summaries.
Report missing required sources as coverage gaps. Do not load unrelated references.

| Touched code/behavior | Source | Review focus |
| --- | --- | --- |
| SFCs, props, emits, models, slots, refs | [components.md](../../../.opencode/skills/vue/references/components.md) | Typed contracts, reactive props, model synchronization, template refs |
| Composables, lifecycle, async work | [composables.md](../../../.opencode/skills/vue/references/composables.md) | Reactive inputs/outputs, ownership, cleanup, existing VueUse helpers |
| Refs, computed, watchers | [reactivity.md](../../../.opencode/skills/vue/references/reactivity.md) | Tracking, unwrapping, watch sources/cleanup, pure getters |
| Vue/TS types or generic components | [typescript.md](../../../.opencode/skills/vue/references/typescript.md) | Type-only imports, strict templates, typed refs/events/injection |
| Routes, guards, route-dependent state | [router.md](../../../.opencode/skills/vue/references/router.md) | Param types/reactivity, metadata, redirects and navigation |
| Dependency injection | [provide-inject.md](../../../.opencode/skills/vue/references/provide-inject.md) | InjectionKey/value agreement, defaults, reactive values, provider ownership |
| Custom directives | [directives.md](../../../.opencode/skills/vue/references/directives.md) | Typed bindings, update/unmount behavior, DOM/listener cleanup |
| Client utilities | [utils-client.md](../../../.opencode/skills/vue/references/utils-client.md) | Pure transforms, named exports, stateful logic in composables |
| Changed behavior or tests | [testing.md](../../../.opencode/skills/vue/references/testing.md) | Observable behavior, async updates, API/router mocks, Teleport/Suspense |
| Reactivity/lifecycle/model edge cases | [gotchas.md](../../../.opencode/skills/vue/references/gotchas.md) | Races, proxy identity, null refs, event forwarding, cleanup |
| Interactive UI/forms | [frontend-a11y](../../../.opencode/skills/frontend-a11y/SKILL.md) | Rendered names/labels, keyboard, focus, announcements, reduced motion |
| Layout, styling, theme changes | [design-system](../../../.opencode/skills/design-system/SKILL.md) | Existing tokens/components, hierarchy, responsive/dark states, polish |
| API clients, response/form contracts | [api-design](../../../.opencode/skills/api-design/SKILL.md) | Existing envelopes/errors, pagination, statuses, backend compatibility |
| PR/history review | [git-workflow](../../../.opencode/skills/git-workflow/SKILL.md) | Scope, evidence, CI and conflicts; no history mutations |

This is a Vue SPA: Nuxt/SSR, Pinia Colada, generated-route plugins and alternative UI
libraries in references are not requirements. Reuse Naive UI, axios and installed
VueUse helpers. Do not request dependencies or a design-system replacement just to
match an example. Verify version-sensitive examples against the installed API and a
focused repro/type-check before raising a finding.

## 2. Scope the exact revision

- Record the requested range, head SHA and acceptance criteria. For a PR, get
  `baseRefName,headRefOid,mergeStateStatus,statusCheckRollup` with `gh pr view --json`;
  compare that base to the exact head. Never assume the base is `main`.
- Local review includes staged/unstaged diffs and relevant untracked files under
  `web/`; branch review uses the requested range. Include styles, locales, tests,
  package/lock files and affected API contracts. Missing history is a limitation;
  `git show HEAD` does not prove a whole branch was reviewed. No web changes → N/A.
- Read the component, parent consumers, store/composable, schemas and tests. Use
  GitNexus `query` for flows and `context`/upstream `impact` for named symbols. Run
  `detect_changes` with `scope: compare` and the actual base for branches, `scope: all`
  for local work; bind `repo: gotham` and the reviewed worktree. Report HIGH/CRITICAL
  graph risk; resolve empty/UNKNOWN results with text search. Re-run partial/truncated
  analyses rather than calling them clean.
- Red CI/conflicts prevent approval; continue the review where possible. Judge actual
  reachable behavior and in-spec regressions, not hypothetical multi-tenant features.

## 3. Run the relevant gates

Inspect current `web/package.json` and CI. From `web/`, record commands, exact revision,
results and important failure output:

```bash
npm run lint
npm run type-check
npm run build
npm run test:unit
```

- Run full lint, not changed-file lint. For CI-equivalent web evidence also run
  `npm test` (harness checks, unit tests and CSS/layout Playwright); unit tests alone
  do not cover that gate. CSS/layout changes need browser/layout evidence.
- UI-flow or relevant API changes need `npm run e2e` and focused real-input probes.
  Inspect `web/playwright.config.ts` and setup: provision throwaway PostgreSQL/Redis
  and a dedicated `gotham serve` first; do not assume Playwright creates the server.
  Explicitly set `GOTHAM_TEST_DSN` before any Go tests, configure the test server and
  base URL for that isolated environment, never use shared `gotham-dev-*` data, and
  remove only resources created for the run. Docker unavailable → record unrun checks.
- Build in a disposable copy when npm lifecycle hooks/generated assets would change
  the worker's checkout or index. `internal/server/webdist` stays uncommitted except
  its placeholder `index.html`; release CI builds it before Go. No dist-commit gate.
- Dependency changes: inspect lockfile and `npm audit` results without automatic fixes
  or broad upgrades. Distinguish affected application paths from advisory-only output.
- Backend changes also need `go-review`. An unavailable tool/service is an evidence
  gap, not proof of a code bug; missing mandatory checks still prevent approval.

## 4. Apply the sources to the changed paths

- **Project conventions:** `<script>` → `<template>` → `<style>`, new SFCs using
  `<script setup lang="ts">`, strict TS, `interface` except unions/intersections,
  type-only imports, descriptive names, 2-space/double-quotes/semicolons/trailing commas.
  Preserve feature/shared boundaries and the project's language/i18n conventions.
- **Validation/contracts:** feature schemas in `features/<module>/schemas/`, rules
  through shared `ruleFrom`, envelopes through `parseWith`; no new ad-hoc `validator:`
  closures. Check safe field errors, Enter submission, loading/error recovery and
  duplicate-submit protection where needed. Browser validation does not replace Go
  validation/authorization. Preserve API shapes, pagination and token contracts.
- **Components:** typed props/emits, tuple event signatures, kebab-case listeners,
  explicit shorthand slot templates, `defineModel` for new two-way bindings, parent/
  child defaults and update payloads. Verify event forwarding and `$attrs` do not
  duplicate handlers or attach them to the wrong root; attribute forwarding alone
  does not prove a bug. Keep `defineExpose` narrow, refs typed/null-safe and prefer
  `useTemplateRef` to global selectors. Check stable list identity and `v-if`/`v-for`
  scope. A valid explicit prop/emit model is not a correctness failure by itself.
- **Reactivity:** `.value` in scripts, correct unwrapping in templates vs collections,
  pure computed getters and writable models. Distinguish `reactive`/Pinia state
  destructuring (needs refs when updates matter) from direct Vue 3.5 `defineProps`
  destructuring (supported). Pass a getter for a destructured prop to watchers or
  reactive consumers; ordinary `watch(ref, ...)` is valid. Check `watchEffect` reads
  before `await`, deep-watch identity assumptions, proxy identity and prop mutation.
- **Composables/lifecycle/async:** `useXxx`, reactive inputs via getters/refs where
  changes must propagate, named ref returns and readonly state when mutation is
  provider-owned. Check scope/lifecycle registration, stale-response guards, loading
  and errors; `onWatcherCleanup` registration must precede `await`. Own and clean up
  requests, sockets, EventSource, timers, listeners and reconnect loops; handle
  KeepAlive activation/deactivation without duplicate subscriptions. Do not flag
  compiler-supported top-level SFC await as automatically losing setup context.
- **Stores/router/injection/directives:** Pinia state stays coherent on failure and
  auth changes; preserve existing store patterns. Route params remain reactive and
  handle actual string/array types; guards/redirects use the established auth flow.
  Injection keys match provided value/ref types, missing providers are handled where
  possible, and mutations stay with their owner. Directives handle binding updates,
  native/multiple-root targets and cleanup. Utilities remain pure; Zod owns validation.
- **Security:** trace untrusted logs/files/API data to HTML, URL, style and component
  sinks. Prefer escaped interpolation; rich HTML needs a verified sanitizer before
  rendering, which may live in a shared helper. Check URL schemes for the destination
  (including legitimate relative/blob URLs), not a universal scheme list. Secrets
  stay out of `VITE_*`, bundles and logs. Preserve established token storage/refresh/
  revocation; do not demand cookie migration as an unrelated fix. Client permission
  controls support UX; verify server enforcement before claiming an auth bypass.
  CSP changes must account for Naive UI style injection and WebSocket connections.
- **Accessibility:** test the rendered Naive UI controls, not just component props:
  connected labels/IDs, required/invalid state and linked errors, accessible names
  and tooltips for icon buttons, semantic controls, keyboard operation and visible
  focus. Check dialog focus trap/restore and Escape policy, logical tab order, live
  status/error announcements, image alt/decorative icons and reduced-motion support.
- **Design/interaction:** reuse Naive UI and `shared/styles/tokens.css` for colors,
  typography, spacing, radii and shadows. Check hierarchy, density, consistent states,
  responsive/dark themes, contrast, focus/hover, loading/empty/error states. Modals use
  the shared `.app-modal`/`.wizard-modal` contract: bounded to viewport, fixed header/
  footer, content-only scrolling. Probe changed actions with clicks, double-clicks,
  keyboard and 390/480/1280/1366px viewports; assert the resulting target/action, not
  only visibility. Missing environment proof stays explicit; a screenshot or code
  inspection does not prove interaction correctness.
- **Tests/performance:** non-trivial behavior needs observable regression checks:
  rendered output, props/emits, form errors, route changes, races and cleanup. Await
  updates/`flushPromises`, mount lifecycle-dependent composables with a host, isolate
  Pinia/router/API mocks and clean up wrappers/Teleport targets/timers. Use installed
  test tools; do not require `@pinia/testing` if absent. Use Playwright for affected
  flows; do not weaken assertions to fit a regression. Large replace-only datasets or
  third-party instances may need shallow/raw state; justify optimization/extraction
  with an actual problem. Component size alone is advice, not a blocking defect.

## 5. Output

Report evidence-backed findings grouped by actual impact, not fixed severity for every
reactivity or accessibility issue. Each finding must include:

```text
[SEVERITY] [in-spec | extra] short title
File: web/src/features/x/Foo.vue:42
Requirement: source skill/reference section or project rule.
Issue: input/action and what the user sees, cannot do, or loses/exposes.
Evidence: caller/flow, repro and result, or precise code reasoning.
Fix: smallest specific correction.
```

End with exact head/range, loaded sources/references, severity counts, gate/probe
results and unverified paths. Use `MERGE-GO` (no blocking findings and required evidence
green), `FIX-FIRST` (in-spec HIGH/MEDIUM findings), or `BLOCK` (CRITICAL, failed required
gate, conflicts or missing mandatory evidence). Separate infrastructure gaps from
code failures. Extra-scope suggestions and style preferences do not block merge; skip
nits already caught by tooling. Pure TS/CSS changes only use applicable review lanes.
Write a report file only at the requested path.
