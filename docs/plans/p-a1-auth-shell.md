# P-A1 — Auth pages: drop the card chrome (strict design port)

## Requirement
Sign-in / create-account content must NOT sit inside a card (no border, no
background). Strictly follow `docs/design/login.html` — the UI source of truth.

## What the mockup actually specifies (verified)
`docs/design/assets/gotham-views.css`:

```css
.auth-main  { display: grid; place-items: center; padding: var(--space-8) var(--space-6); background: var(--bg); }
.auth-card  { width: min(420px, 100%); }          /* width container ONLY — no border, no bg, no shadow */
.auth-switch { ... background: var(--surface-warm); border-radius: var(--radius-sm); padding: 3px; margin-bottom: var(--space-5); }
```

So in the design the "card" is a bare width limiter; the only filled element is
the segmented tab switch (`surface-warm` pill). The current Vue port wraps
everything in `NCard`, which draws Naive UI card chrome → the deviation.

## Current code (verified)
- `web/src/layouts/AuthLayout.vue` — shell (aside + main), fine, keep.
- `web/src/pages/LoginPage.vue` — `<NCard class="auth-card">` wrapping all content.
- `web/src/pages/RegisterPage.vue` — same pattern.
- Scoped styles already port `.auth-switch`, `.auth-title`, divider, footnote.

## Changes (2 files + maybe shared css)
1. LoginPage.vue / RegisterPage.vue: replace `<NCard class="auth-card">` with a
   plain `<div class="auth-card">`; remove `NCard` from imports.
2. Move/keep `.auth-card { width: min(420px, 100%); }` — port the mockup rule
   verbatim (width only). If a `:deep` NCard selector exists, delete it.
3. Diff against mockup end-to-end: switch pill (surface-warm, 3px padding,
   radius-sm), tab colors (muted / fg-2 + selected-row when active), h2 title,
   muted subtitle, field layout, divider "or", footnote text sizes. Port any gap.

## Side-effects to check
- `RegisterPage` / `LoginPage` both rely on NCard default padding; add the
  equivalent spacing via the mockup's utilities (the mockup panels have no
  extra padding — content sits directly on `.auth-main`'s background).
- AuthLayout `.auth-main` already `place-items: center` — unchanged.

## Verification
- `npm run type-check` + `npm run build` (webdist rebuild — dist drift gate).
- Playwright: existing auth.spec must still pass; eyeball via screenshot against
  `docs/design/login.html` opened side by side.
- e2e locators unaffected (no role/text changes).

## PR
1 PR `fix/auth-cardless` → Codex review → CI → merge.