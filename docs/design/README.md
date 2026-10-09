# Gotham UI Design Source of Truth

One `*.html` mockup per page plus the shared token contract in
`assets/gotham-ui.css` (+ screen composites in `assets/gotham-views.css`).
The Vue app in `web/` ports these mockups: extract tokens (colors, fonts,
spacing, radii) from `gotham-ui.css`, theme Naive UI to match, keep Naive UI
as the component base.

## Modals (JUS-69)

Every modal follows the same anatomy, in the mockups (`.modal` in
`gotham-ui.css`, `.wizard` in `gotham-views.css`) and in the Vue port
(`web/src/shared/styles/main.css`):

- The modal never exceeds the viewport: `max-height` is bounded to the
  viewport (`calc(100vh - 64px)` in the Vue port, `86vh`/`92vh` in the
  mockups).
- The header (title and close) and any footer actions stay fixed; only the
  content region scrolls.
- Wizards pin their own footer: the card body is a fixed flex column and
  only the step body (`.wizard-body`) scrolls, so Back/Continue/Create stay
  visible on tall steps such as the Dockerfile and Docker Compose editors.

Naive UI renders `NModal preset="card"` as `.n-card-header` +
`.n-card-content` + `.n-card__footer`/`.n-card__action` siblings with no
height bound of its own, so the bound and the scroll split live in the
shared stylesheet, not in per-modal styles. New card modals reuse the
`.app-modal` class; wizard modals reuse `.wizard-modal`.
