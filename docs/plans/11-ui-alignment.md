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
- Dark-theme token layer from `assets/gotham-ui.css` ported into `web/`;
  Naive UI renders the dark theme everywhere (no light-theme remnants).
- Auth screens match `login.html`: two-column shell with brand aside,
  tab switch, divider, GitHub outline button, 10-char/2-class password rule
  with strength meter, terms checkbox.
- App shell matches the 72/260/48 geometry: server rail, grouped sidebar
  with counts, brand lockup + version chip, composed topbar.
- Dashboard matches `dashboard.html` structure; widgets without backend
  data render as explicit empty states (never fabricated numbers).
- Servers list matches `servers.html`: filter chips, search, threshold
  meters, metadata columns, empty state; status tag gains dot/pulse semantics.
- Wizard matches `servers.html` steps: 4th Finish step, Back + step
  counter, passphrase field, install progress area.
- New `/servers/:id` route renders header + Overview/Settings from the
  existing `getServer` API; Containers/Metrics/Proxy tabs stay stubbed until
  Phases 3/8/6.
- All UI copy in English; no Vietnamese strings remain in `web/src`.
- CI green on every PR (including the webdist dist-drift check).

**Rollback:** pure frontend — revert the PR. `internal/server/webdist` is
committed, so rollback is a single revert with no migration involved.

**Phase gate:** continuous mode applies (see 00-roadmap.md) — this track runs in parallel with the backend pipeline and merges to main via its own PRs without stopping.

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

## Delivery

Complete. Eleven work packages (UI-0 design tokens + Naive UI dark theme, UI-1 auth layout,
UI-2 auth forms, UI-3 shell geometry + server rail, UI-4 sidebar + topbar, UI-5 dashboard,
UI-6 servers list, UI-7 status tag + row actions, UI-8 wizard, UI-9 server detail,
UI-10 English copy + responsive polish) merged as separate PRs; see git history.
