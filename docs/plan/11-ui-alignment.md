# Phase 11 — UI Alignment with docs/design (side track)

**Goal:** bring the Vue SPA in line with the `docs/design/` mockups without
changing any backend API: port the dark-theme design tokens, re-theme Naive UI,
and close the layout/copy gap on every page that exists today.

**Scope:** pages that exist in `web/src` (login/register, dashboard, servers,
app shell). Mockups for future-phase pages (applications, databases, domains,
services, files, team-settings) are references only — they are implemented when
their backend phase lands, not here. No new backend route is required by any
package in this phase; backend-dependent widgets ship as explicit stubs.

**Exit criteria:**
- [ ] Dark-theme token layer from `assets/gotham-ui.css` ported into `web/`;
  Naive UI renders the dark theme everywhere (no light-theme remnants).
- [ ] Auth screens match `login.html`: two-column shell with brand aside,
  tab switch, divider, GitHub outline button, 10-char/2-class password rule
  with strength meter, terms checkbox.
- [ ] App shell matches the 72/260/48 geometry: server rail, grouped sidebar
  with counts, brand lockup + version chip, composed topbar.
- [ ] Dashboard matches `dashboard.html` structure; widgets without backend
  data render as explicit empty states (never fabricated numbers).
- [ ] Servers list matches `servers.html`: filter chips, search, threshold
  meters, metadata columns, empty state; status tag gains dot/pulse semantics.
- [ ] Wizard matches `servers.html` steps: 4th Finish step, Back + step
  counter, passphrase field, install progress area.
- [ ] New `/servers/:id` route renders header + Overview/Settings from the
  existing `getServer` API; Containers/Metrics/Proxy tabs stay stubbed until
  Phases 3/8/6.
- [ ] All UI copy in English; no Vietnamese strings remain in `web/src`.
- [ ] CI green on every PR (including the webdist dist-drift check).

**Rollback:** pure frontend — revert the PR. `internal/server/webdist` is
committed, so rollback is a single revert with no migration involved.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner
before scheduling further UI work. This track runs in parallel with the paused
backend pipeline and merges to main via its own PRs.

**Recorded decisions:**
- Keep separate `/login` and `/register` routes; add a tab-style switch UI
  linking them (mockup implies one page, but routes already exist and the
  OAuth callback flow depends on them).
- Register keeps its GitHub button (the product has OAuth; the mockup omits it).
- Servers list keeps `n-data-table`; the mockup card grid is deferred unless
  the owner asks for it.
- Wizard install progress/log is client-side only until a backend install
  stream exists; never fake a completion state.

---

## UI-0 — Design tokens + Naive UI dark theme — `ws/p11-tokens`

- **Context brief:** the foundation every later package builds on. Extract the
  token contract from `docs/design/assets/gotham-ui.css` (`:root` blocks:
  surfaces, foreground, borders, accent `#5865f2`, semantic success/warn/
  danger, type scale, 4px spacing grid, radii, elevation, motion, layout
  widths) into `web/src/styles/tokens.css`, imported by `main.css`. Replace
  the light globals in `main.css` (currently `color-scheme: light`, Inter).
  Self-host the fonts: download Google Sans + JetBrains Mono woff2 into
  `web/` and reference them locally — do NOT hotlink Google Fonts (violates
  the self-hosted principle and the CSP `style-src 'self'`). Set
  `color-scheme: dark` in `web/index.html`. Wire `darkTheme` +
  `themeOverrides` (accent, surfaces, radii, mono font) into the
  `NConfigProvider` in `web/src/App.vue`, with per-component overrides for
  Input/Button/Alert/Card/Form.
- **Deliverables:** `tokens.css`, font assets, themed `App.vue`, dark `main.css`.
- **Verify:** `npm run build` + `npm run type-check` clean; dist rebuilt;
  every existing page renders dark with no light remnants (screenshot check).
- **Depends on:** none. Blocks UI-1..UI-10.

## UI-1 — Shared AuthLayout — `ws/p11-auth-layout`

- **Context brief:** build the `.auth` two-column shell from `login.html` +
  `gotham-views.css` as a reusable layout: `.auth-aside` (brand lockup with
  mark, eyebrow/h1/lede in English, 4 value props with icons, aside foot)
  + centered `.auth-main` slot; hide the aside at ≤940px. Provide the icon
  source for the `#i-*` sprite (Naive UI icon components or a ported sprite —
  pick one, document it). Wrap login/register/callback routes; keep the route
  structure unchanged.
- **Deliverables:** `web/src/layouts/AuthLayout.vue` (+ icon module), router
  wiring, English aside copy.
- **Verify:** type-check + build clean; `/login` shows the aside on desktop,
  hides it on narrow viewports.
- **Depends on:** UI-0.

## UI-2 — Auth forms parity — `ws/p11-auth-forms`

- **Context brief:** inside the new layout, align `LoginPage.vue` /
  `RegisterPage.vue` with the mockup: tab switch UI wired to the existing
  routes, "or" divider, "Forgot password?" link (inert — no backend; link to
  nothing or a tooltip), GitHub button restyled to outline with icon,
  register 10-char/2-class rule with 4-segment strength meter + hint, terms
  checkbox, footnote. Unify error presentation (pick inline field errors vs
  `NAlert`, apply consistently); keep `describeAuthError` as message source.
  Polish `OAuthCallbackPage.vue` against the token layer (no mockup exists).
- **Deliverables:** restructured login/register pages, new validation rules,
  strength meter, terms checkbox.
- **Verify:** type-check + build clean; register rejects 8-char passwords;
  e2e register → dashboard still works.
- **Depends on:** UI-0, UI-1.

## UI-3 — Shell geometry + server rail — `ws/p11-shell`

- **Context brief:** reshape `AppLayout.vue` to the 72/260/48 grid from
  `gotham-ui.css` + `gotham-ui.js` shell renderer: add the 72px server rail
  (brand-mark home, one avatar per server from the `servers` store with status
  dot + tooltip, add-server button, footer map/alerts), fix sidebar 260px and
  topbar 48px, `.page` max-width 1360px content wrapper, port the ≤1024px
  responsive behavior.
- **Deliverables:** rail component (inline or `ServerRail.vue`), geometry fixes.
- **Verify:** type-check + build clean; rail shows live servers with status.
- **Depends on:** UI-0.

## UI-4 — Sidebar sections + topbar — `ws/p11-sidebar`

- **Context brief:** replace the flat 2-entry menu with the three grouped
  sections from the mockup shell (Operations / Team / System) with labels,
  icons, count pills; brand lockup + version chip in the header; sticky
  `me-card` footer (avatar, email, role). Register disabled placeholder entries
  for not-yet-built sections (no route, tooltip "coming in Phase N"). Compose
  the topbar: environment status chip, `CP :8000` / `gRPC :9442` chips, search
  field (inert stub), bell/doc/account cluster.
- **Deliverables:** grouped sidebar, me-card, composed topbar.
- **Verify:** type-check + build clean; Dashboard/Servers still route; stubs
  are visibly inert, not dead links.
- **Depends on:** UI-0, UI-3. Parallel with UI-5..UI-8 after UI-0.

## UI-5 — Dashboard page — `ws/p11-dashboard`

- **Context brief:** implement `dashboard.html` structure in `DashboardPage.vue`:
  page head (eyebrow, h1, description, two action buttons), 4-card KPI row,
  recent-deploys table, server-health node cards, context aside (heartbeat
  feed, control-plane components, team activity, alerts). Only two widgets may
  read live data: "Servers ready" KPI and node cards from the `servers` store.
  Everything without a backend renders as an explicit empty state — never
  fabricated numbers. Leave a typed seam for a future deployments store.
- **Deliverables:** full dashboard page with real + stub widgets clearly marked.
- **Verify:** type-check + build clean; node cards reflect live server data.
- **Depends on:** UI-0. Parallel with UI-4, UI-6..UI-8.

## UI-6 — Servers list parity — `ws/p11-servers-list`

- **Context brief:** align `ServersPage.vue` with `servers.html`: page head
  (eyebrow/h1/description + Check SSH + Add server actions), All/Ready/
  Offline/Agent-update-needed filter chips with counts, name/IP/OS search,
  threshold-colored meters (accent CPU / success RAM / warn disk, danger over
  80%), OS/arch/SSH-user/container-count columns from existing `Server`
  fields, tabular numerals, mockup empty state.
- **Deliverables:** chips, search, meters, columns, empty state (client-side
  filtering only — no backend change).
- **Verify:** type-check + build clean; filters + search work against live data.
- **Depends on:** UI-0. Parallel with UI-4, UI-5, UI-7, UI-8.

## UI-7 — Status tag + row actions + install card — `ws/p11-servers-actions`

- **Context brief:** realign `ServerStatusTag.vue` with mockup semantics: status
  dot + pulse, `offline → danger` (red, not warning yellow), "agent update
  needed" state. Row actions: Re-validate, "Open node" (links to UI-9 route),
  Update agent (visible only when a backend route exists — otherwise omit, do
  not fake it), keep Delete. Add the static agent-install reference card from
  the mockup (one-liner + mTLS/heartbeat callouts).
- **Deliverables:** tag realignment, row actions, install card.
- **Verify:** type-check + build clean; offline rows render red with pulse.
- **Depends on:** UI-0. Parallel with UI-4..UI-6, UI-8.

## UI-8 — Wizard parity — `ws/p11-wizard`

- **Context brief:** align `AddServerWizard.vue` with the 4-step mockup rail:
  4th Finish step (node avatar, ready tags, resource summary), Back button +
  "Step X / 4" counter, key passphrase field, per-field hints and client-side
  pattern errors, fixed-check list with idle/running/ok/fail states + SSH
  status line + summary, install screen with progress area (client-side only).
  English copy throughout. Deferred (no backend): saved-key dropdown (no list
  API — keep paste-new-key), SSH-connection probe name (backend exposes only
  docker/cpu/ram/disk), real install progress stream.
- **Deliverables:** 4-step wizard with copy + state parity where the API allows.
- **Verify:** type-check + build clean; full e2e against a real node still
  validates; 422 checklist renders.
- **Depends on:** UI-0. Parallel with UI-4..UI-7.

## UI-9 — Server detail route (static parts) — `ws/p11-server-detail`

- **Context brief:** add `/servers/:id` route rendering breadcrumb, head
  (avatar, title, status tag, actions), tabs (Overview / Containers / Metrics /
  Proxy & Traefik / Node settings), node info + labels placeholder + danger
  zone — all from the existing `getServer` API (currently dead code; wire it).
  Containers/Metrics/Proxy tabs render as explicit stubs blocked on Phases
  3/8/6; do not invent data. Wire "Open node" from the list (UI-7).
- **Deliverables:** detail route + static tabs + stubs.
- **Verify:** type-check + build clean; direct URL load works (SPA fallback).
- **Depends on:** UI-0. Parallel with UI-4..UI-8.

## UI-10 — English copy pass + responsive polish — `ws/p11-copy`

- **Context brief:** final sweep over every file touched in UI-0..UI-9: apply
  the English copy tables from the gap analysis, remove any remaining
  Vietnamese strings in `web/src` (grep `[\u00C0-\u1EF9]` must be empty
  outside comments), port the ≤1180/1024/860/640px responsive behaviors and
  verify empty states. Last PR of the track.
- **Deliverables:** copy + responsive parity, grep-clean.
- **Verify:** type-check + build clean; full click-through of all routes.
- **Depends on:** UI-1..UI-9.
