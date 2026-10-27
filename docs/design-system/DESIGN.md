---
title: Design system
---

# Design system

Gotham's console is a dark, dense operations surface for people who may never
have run a server command. This page records the design system that ships
today: the tokens, the Naive UI mapping, the layout and component rules, and the
places where the code and the ideal do not yet agree. It is written for
contributors and for owners who want to change the interface without breaking
its rhythm.

The page describes shipped behaviour. Where a value and this page disagree, the
runtime CSS is right:

| What | Where |
| --- | --- |
| Tokens (authoritative) | `web/src/shared/styles/tokens.css` |
| Global styles, modal contract | `web/src/shared/styles/main.css` |
| Naive UI theme overrides | `web/src/app/App.vue` |
| Console shell layout | `web/src/app/layouts/AppLayout.vue` |
| Portable token artifacts | `/design-system/design-tokens.json`, `/design-system/design-tokens.css` |
| Interactive catalogue | [`/design-system/design-preview.html`](/design-system/design-preview.html) |

## Principles

1. **Calm density.** The console shows a lot at once — projects, nodes,
   deployments, logs. Hierarchy comes from spacing, weight and surface colour,
   not from decoration.
2. **One dark identity.** There is no light app theme and no theme switch. The
   light palette in the tokens belongs to the terminal's own appearance menu.
3. **Naive UI is the component base.** Buttons, inputs, modals, tables, tags and
   messages come from Naive UI with theme overrides and small global rules. Do
   not rebuild controls from raw CSS; extend the theme instead.
4. **Plain language first.** Labels name the object, actions start with a verb,
   and errors say what to do next. Jargon belongs in the reference, not in a
   form.
5. **Progressive disclosure.** Wizards, tabs and side panels keep the first
   screen small; advanced settings stay reachable without crowding the default
   path.
6. **Accessible by default, not certified.** Focus rings, keyboard paths, labels
   and reduced-motion handling are part of the system. Gotham has not been
   audited against WCAG, so do not claim conformance (see
   [Accessibility](#accessibility)).

## Tokens

`tokens.css` declares two `:root` blocks. The second block deliberately
overrides and extends the first, so the **effective** value is the last
duplicate. Today that is **144 custom properties**. The downloadable
`design-tokens.json`, `design-tokens.css` and the preview's inline token blocks
are generated from those effective values and checked against the runtime file
by a script (see [Refresh and check](#refresh-and-check)); the prose and tables
on this page are hand-written and can lag the code.

Values are copied verbatim, including `color-mix()` aliases. Do not flatten a
`color-mix()` into a hex value in documentation, or the two will drift.

### Colour

| Token | Value | Role |
| --- | --- | --- |
| `--bg` | `#313338` | App and page background |
| `--surface` | `#2b2d31` | Cards, sidebar, modals, table bodies |
| `--surface-warm` | `#1e1f22` | Inputs, embedded surfaces, terminal background |
| `--fg` | `#dbdee1` | Default body text |
| `--fg-2` | `#f2f3f5` | Headings and emphasised text |
| `--muted` | `#949ba4` | Secondary labels, descriptions |
| `--meta` | `#80848e` | Metadata, hints, placeholders, disabled text |
| `--border` | `rgba(255, 255, 255, 0.06)` | Dividers, card rings, table rules |
| `--border-soft` | `#3f4147` | Hover borders on inputs and controls |
| `--accent` | `#5865f2` | Primary action, links, focus, active state |
| `--accent-on` | `#ffffff` | Text and icons on an accent fill |
| `--accent-hover` | `#4752c4` | Accent hover |
| `--accent-active` | `color-mix(in oklab, var(--accent), black 14%)` | Accent pressed state |
| `--success` | `#23a55a` | Success fill, status dot |
| `--warn` | `#f0b232` | Warning fill, status dot |
| `--danger` | `#f23f43` | Error fill, status dot |

The second block adds soft and text-safe variants:

| Token | Role |
| --- | --- |
| `--fg-soft` | Barely-there foreground wash (6% of `--fg`) |
| `--hover-row`, `--selected-row` | Table row hover and selection |
| `--accent-soft`, `--accent-softer` | Selected backgrounds, inset accent washes |
| `--accent-ink`, `--accent-ink-hover`, `--link` | Accent used as **text or icon** on dark surfaces |
| `--success-soft`, `--warn-soft`, `--danger-soft` | Alert and tag fills |
| `--success-ink`, `--warn-ink`, `--danger-ink` | Status text and icons on dark surfaces |
| `--float` | Floating surfaces (popovers, floating windows) |

**Rule:** use the base colours (`--accent`, `--success`, `--warn`, `--danger`)
for fills, dots, borders and decoration. Use the `*-ink` variants when the
colour carries small text or an icon that must stay readable. A status dot can
be `--success`; the word next to it should be `--success-ink`, not the raw
green.

#### Measured contrast

These ratios were measured on 2026-10-27 with the WCAG relative-luminance
formula against `--bg` (`#313338`). Treat them as measurements, not a
certification.

| Token | Value | Contrast vs `--bg` | Guidance |
| --- | --- | --- | --- |
| `--fg-2` | `#f2f3f5` | 11.39:1 | Headings and key values |
| `--fg` | `#dbdee1` | 9.36:1 | Body text |
| `--muted` | `#949ba4` | 4.50:1 on `--bg`; 4.92:1 on `--surface` | Secondary text; little margin |
| `--meta` | `#80848e` | 3.38:1 | Metadata only, never essential small text |
| `--accent` | `#5865f2` | 2.74:1 | Fill, dot, border — not text |
| `--success` | `#23a55a` | 3.97:1 | Fill, dot — not text |
| `--warn` | `#f0b232` | 6.69:1 | Passes as text, but prefer `--warn-ink` for consistency |
| `--danger` | `#f23f43` | 3.35:1 | Fill, dot — not text |

Consequences that are easy to miss:

- `--meta`, `--accent`, `--success` and `--danger` fail the 4.5:1 threshold for
  normal text. Do not use them for error details, hints that carry meaning, or
  small status labels. `--warn` (~6.69:1) passes, but the design rule still
  prefers the `*-ink` variants for status text so the colour roles stay
  consistent.
- `--muted` measures 4.50:1 on `--bg` and 4.92:1 on the darker `--surface`, so
  it passes the 4.5:1 threshold for normal text with little margin. The ratio
  does not change with font size; the threshold does (large text may use 3:1).
  Re-measure if the token changes.
- Naive's tag theme derives its own colours from the common palette, so a tag
  label is not automatically the raw status token. If you need a custom tag,
  measure the computed text colour, not just the dot.
- Ink variants are mixed with white at runtime. Measure them in the browser
  rather than assuming a fixed ratio.

References: [WCAG 2.2 contrast
minimum](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html) and
[target size
minimum](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).

### Typography

| Token | Value | Use |
| --- | --- | --- |
| `--font-display` | `"Google Sans", "Helvetica Neue", Helvetica, Arial, sans-serif` | Headings, brand |
| `--font-body` | same stack as display | Body and controls |
| `--font-mono` | `"JetBrains Mono", Consolas, "Andale Mono", "Courier New", monospace` | IDs, addresses, ports, numbers |
| `--text-xs` | `12px` | Metadata, eyebrows, hints |
| `--text-sm` | `14px` | Default body size in the console |
| `--text-base` | `16px` | Long-form reading, auth copy |
| `--text-lg` | `18px` | Section headings |
| `--text-xl` | `20px` | Page section titles |
| `--text-2xl` | `24px` | Page titles |
| `--text-3xl` | `32px` | Marketing/auth display |
| `--text-4xl` | `56px` | Landing display only |
| `--leading-body` | `1.375` | Body copy |
| `--leading-tight` | `1.1` | Compact display lines |
| `--leading-title` | `1.25` | Headings |
| `--tracking-display` | `-0.02em` | Large display headings |
| `--tracking-eyebrow` | `0.08em` | Uppercase eyebrow labels |

Usage rules:

- The console body is `14px` (`main.css` sets `font-size: var(--text-sm)` on
  `body`). Do not raise it locally to make a page feel bigger; use spacing and
  hierarchy.
- `h1` is `--text-2xl`/700, `h2` is `--text-xl`/700, `h3` is `--text-lg`/600.
  Page-level headings go through `PageHeader`/`.page-head`, not a bare `<h1>`.
- `--text-3xl` and `--text-4xl` are for auth and marketing surfaces. A console
  page that uses them looks like a landing page.
- Use `.mono` (or `--font-mono`) for anything a user might copy or compare
  character by character: container IDs, image tags, IP addresses, ports,
  domains, token prefixes. Use `.num` when digits must align.
- Uppercase eyebrows are mono, 12px, with `--tracking-eyebrow` and `--muted`;
  use them sparingly to label a group, not every card.

**Offline font fallback.** The console serves Google Sans and JetBrains Mono
from `/fonts/*.woff2` through `@font-face` in `tokens.css`. If those files are
missing, blocked or the page is opened from the filesystem, text falls back to
Helvetica/Arial and the local monospace font. Avoid layouts that depend on
exact glyph widths; the fallback changes wrapping slightly.

### Spacing, radius, elevation and motion

| Group | Tokens | Notes |
| --- | --- | --- |
| Spacing | `--space-1` 4px, `--space-2` 8px, `--space-3` 12px, `--space-4` 16px, `--space-5` 20px, `--space-6` 24px, `--space-8` 32px, `--space-12` 40px | Use the scale. `--space-12` is **40px**, not 48px; do not "fix" it to a 4/8 grid that skips it. |
| Section rhythm | `--section-y-desktop` 80px, `--section-y-tablet` 48px, `--section-y-phone` 32px | Declared but currently unreferenced; console pages use `--space-*` and page padding instead. |
| Radius | `--radius-sm` 4px, `--radius-md` 8px, `--radius-lg` 16px, `--radius-pill` 9999px | Controls use 4px, cards and modals 8px; `lg` is rare (hero/illustration). Pills are for status dots, count chips and segmented controls. |
| Elevation | `--elev-flat`, `--elev-ring`, `--elev-raised`, `--elev-floating` | Rings are `1px` light borders; floating surfaces use the deep shadow plus a ring. Prefer `--elev-ring` over inventing a border colour. |
| Motion | `--motion-fast` 80ms, `--motion-base` 200ms, `--ease-standard` `cubic-bezier(0.2, 0, 0, 1)`, `--ease-morph` `cubic-bezier(0.215, 0.61, 0.355, 1)` | Hover/press feedback uses fast; panels and layout shifts use base. No decorative loops. |
| Focus | `--focus-ring` = `0 0 0 3px var(--accent-ink)` | Global `:focus-visible` also sets a 2px outline with 2px offset. Use one pattern per control, not both. |

`prefers-reduced-motion: reduce` is declared in `AppLayout.vue` (scoped styles)
and collapses animation and transition durations for the shell. Coverage
through feature component internals has not been verified; a new animation
should include its own reduced-motion behaviour. The token preview ships such a
rule for its specimen transitions.

### Layout and responsive behaviour

The shell is a three-column grid in `AppLayout.vue`:

- `--rail-w` 72px server rail (owner only; hidden for non-owners),
- `--sidebar-w` 260px navigation,
- the remaining width for the page, with a middle column of `minmax(0, 1fr)` so
  long tables and logs do not force the shell wider.

The topbar is sticky at `--topbar-h` 48px. Page content sits in `.page` with
`--space-6` padding and `--space-12` bottom padding, **max-width 1360px**. That
1360px value is hardcoded in the layout, not driven by `--container-max`
(1200px), which is currently unreferenced. Do not assume `--container-max`
governs the console shell; it does not.

| Width | Behaviour |
| --- | --- |
| `> 1024px` | Rail + sidebar + page; full page width up to 1360px |
| `≤ 1024px` | Shell becomes one column; the server rail is hidden entirely (`v-if` in `AppLayout.vue`); navigation moves to the sidebar drawer; page padding drops to 20px |
| `≤ 860px` | `.page-actions` wrap below the heading and lose their auto margin |
| `≤ 640px` | Page padding drops to 16px; full-height pages use 8px vertical padding |
| Container `≤ 480px` | `.form-row` (a two-column form grid) collapses to one column via a container query, independent of viewport |

`page--wide` removes the max-width for log/terminal-style pages. `page--fill`
makes the page a fixed-height flex column for viewers that manage their own
scroll (terminal, files). Use them through route meta (`wide`, `fillViewport`)
rather than ad-hoc classes.

## Components

### Page header

`web/src/shared/ui/PageHeader.vue` is the contract for a page's identity and
actions: optional identity slot (icon/brand), breadcrumb, title, badges, mono
meta line, description, and actions. It is built on `.page-head`,
`.page-desc` and `.page-actions` in `main.css`.

- Title is a single `h1`; badges sit next to it, not in the description.
- Meta is mono and `--muted`: project/environment, IDs, timestamps.
- Actions wrap at 860px and move below the heading; keep them to one primary
  action plus secondary buttons.

### Forms

- Use `NForm`/`NFormItem`; labels via i18n keys, not literals.
- Validation uses zod schemas through `web/src/shared/validation`
  (`ruleFrom`, `rulesFor`, `fieldErrors`). Feature schemas live in
  `features/<module>/schemas/`. Do not add ad-hoc `validator:` closures in a
  component.
- API responses are parsed at the trust boundary with `parseWith` from
  `web/src/shared/validation/parse`; form rules use `ruleFrom`. Both stop a
  malformed shape before it reaches the UI.
- `field-hint` is a 12px `--meta` hint under a control; it hides when the field
  is in error (`n-form-item-blank--error`), so the error message replaces it
  rather than stacking with it.
- `.form-row` is a two-column grid that collapses at a 480px **container**
  width. Prefer it over viewport media queries inside a modal, which may be
  narrower than the viewport.
- Feedback text is 12px across all Naive sizes; labels are `--muted` (Naive
  override), errors use `#f67c7c`.

### Modals and wizards

The modal contract is shared in `main.css` and applies to `.app-modal`,
`.wizard-modal` and `.edit-server-modal`:

- The card never exceeds `calc(100vh - 64px)` and is a flex column.
- Header, footer and action areas stay fixed; the content region scrolls
  (`flex: 1 1 auto; overflow-y: auto; min-height: 0`).
- The wizard variant keeps the content as a flex column with `overflow: hidden`
  and lets the `.wizard` child own the scroll, because its step body and footer
  need separate behaviour.

**Current short-height exception.** Below a 480px viewport height, `.app-modal`
stops pinning the header/footer: the card itself scrolls
(`overflow-y: auto`) and the content becomes `flex: 0 0 auto; overflow-y:
visible`. This is a deliberate fallback for small landscape windows, not the
general pattern. Keep it when refactoring; if you change it, test a 480px-tall
viewport, not just a narrow one.

`CreateAppWizard.vue` is the shared-contract example: `NModal preset="card"`
with `class="wizard-modal"`, `width: 880px; max-width: 96vw`, a fixed
`.wizard-foot`, and a `.wizard-body` that scrolls (`overflow-y: auto;
flex: 1 1 auto; min-height: 0`). Its two-column rail collapses at 720px. Take
that structure rather than inventing a new modal skeleton.

`CreateDatabaseWizard.vue` is a current exception: it uses
`class="form-container"` and a 640px card, so it does **not** participate in
the `wizard-modal` contract. Do not copy it as the modal pattern; moving it
onto the shared contract would be a separate behaviour change.

Use [`NModal`](https://www.naiveui.com/en-US/dark/components/modal) behaviour
and follow the [WAI-ARIA dialog
pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/) for manual
dialogs: labelled title, focus moved in, Escape cancels and focus returns to
the trigger. Do not block Escape merely because the dialog contains a
destructive action; block close during an unsafe in-flight operation only when
that is explicitly justified.

### Status, tables and feedback

- Status is a tag with a dot and a word
  (`DatabaseStatusTag.vue`): creating = accent + pulse, running = success,
  stopped = warning, error = danger, deleting = muted. Colour is never the only
  signal; the word carries the meaning.
- Tables are `NDataTable` with `--surface` bodies and `--border` rules. Long
  values break inside cells (`.mono`/`.error-text` use `overflow-wrap: anywhere`)
  instead of widening the table.
- Empty, loading and error states are part of the component, not an afterthought:
  a table that can be empty needs a sentence telling the user what to do next.
- Destructive actions use an error-styled confirm and name the object.

### Tooltips

`AppTooltip.vue` wraps Naive's tooltip: 500ms hover delay, 120ms leave grace,
keyboard activation after a delay, Escape closes, touch taps do not open it.
Provide a real `aria-label`; when it differs from the tooltip text, the tooltip
also emits a screen-reader-only copy. Do not put critical information only in a
tooltip.

## Naive UI mapping: current and recommended

`App.vue` passes `darkTheme` plus `GlobalThemeOverrides`. The mapping is close,
but several values are duplicated literals rather than token references:

| Naive key | Current value | Token equivalent | Alignment target and caution |
| --- | --- | --- | --- |
| `common.primaryColor` | `#5865f2` | `--accent` | Keep the literal; align it when the mapping is revised |
| `common.primaryColorHover` | `#4752c4` | `--accent-hover` | Keep the literal |
| `common.primaryColorPressed` | `#4c57d0` | `--accent-active` (`color-mix(…, black 14%)`) | Keep the literal; it currently drifts from the CSS pressed state |
| `common.bodyColor` | `#313338` | `--bg` | Keep the literal |
| `common.cardColor`, `modalColor`, `tableColor` | `#2b2d31` | `--surface` | Keep the literals |
| `common.popoverColor` | `#121315` | `--float` (`color-mix(surface-warm 78%, black)`) | Two sources for floating surfaces; consolidate on one value |
| `common.inputColor` | `#1e1f22` | `--surface-warm` | Keep the literal |
| `common.textColor1/2/3` | `#f2f3f5`, `#dbdee1`, `#949ba4` | `--fg-2`, `--fg`, `--muted` | Keep the literals |
| `common.placeholderColor` | `#80848e` | `--meta` | Keep the literal |
| `common.dividerColor`, `borderColor` | `rgba(255,255,255,0.06)` | `--border` | Keep the literal |
| `common.fontFamily` | Google Sans stack | `--font-body` | Keep the literal; a font stack is not a colour parse |
| `common.fontFamilyMono` | JetBrains Mono stack | `--font-mono` | Keep the literal |
| `common.borderRadius`, `borderRadiusSmall` | `4px` | `--radius-sm` | Keep the literal |
| `Card.borderRadius` | `8px` | `--radius-md` | Keep the literal |
| `Alert.colorSuccess/Warning/Error/Info` | 16%/10% translucent fills | `--success-soft`, `--warn-soft`, `--danger-soft`, `--accent-soft` | Duplicates; consolidate only with a concrete check |
| `Alert.iconColor*` | `#61be88`, `#f3c057`, `#f67c7c`, `#97a0f7` | approximates `--success-ink`, `--warn-ink`, `--danger-ink`, `--accent-ink` | Hardcoded values do not follow `color-mix` changes |
| `Button.colorPrimary*` | accent hexes | `--accent`, `--accent-hover`, `--accent-active` | Target accent tokens; validate the derived states before converting fills |
| `Input.borderFocus`, `boxShadowFocus` | `var(--accent-ink)` + `var(--bg)` ring | already tokenised | Component-level `var()` works; `main.css` removes the state-border shadow, so the border carries focus |

Why this matters: a token change currently updates most of the console but not
the pressed primary button, the popover surface or the alert fills, because
those are literals in `App.vue`.

**Do not blanket-replace `common` colours with `var()`.** Naive's tag dark
theme runs `changeColor` over common colours: passing
`common.primaryColor: "var(--accent)"` throws
`Invalid color value var(--accent)` (verified against the installed package).
Component-level overrides that already use `var()` — Button text colours,
Input borders — are direct CSS properties and do not prove `common` accepts
them. Treat the tokens as the alignment target, resolve concrete colours for
`common`, and verify derived hover, pressed and tag colours in the browser
before changing the mapping.

## Motion

- 80ms for hover/press colour and small transforms; 200ms for panels, drawers,
  row expansion and layout shifts; 200ms+ for modal entry only if it does not
  delay the form.
- Use `--ease-standard` for standard movement and `--ease-morph` for
  size/shape morphs. Do not add new cubic-beziers.
- Status dots can pulse to show work in progress or a node needing attention;
  see `DatabaseStatusTag`, `DeploymentStatusTag` and `ServerStatusTag` for the
  exact state sets. `AppLayout` declares reduced-motion handling, but its
  coverage through feature components is unverified, so a new animation should
  include its own reduced-motion behaviour.
- Loading states should be honest: a spinner for unknown duration, a progress
  value when the backend reports one, and a text line for work that outlives a
  page navigation.

## Accessibility

Shipped behaviour:

- `:focus-visible` uses a 2px `--accent-ink` outline with 2px offset.
  `main.css` removes Naive's input state-border glow
  (`.n-input--focus .n-input__state-border`), so the visible input indicator is
  the `--accent-ink` focus **border**.
- The shell exposes landmarks: sidebar `<aside aria-label>`, nav list,
  topbar header, main view. The mobile navigation drawer is labelled and
  dismissible.
- Tooltips have a keyboard path and Escape; touch avoids hover-only content.
- `AppLayout.vue` declares reduced-motion handling for the shell; coverage
  through feature component internals is unverified, so add your own
  reduced-motion rule for new animation.
- The modal contract keeps content scrollable inside a bounded card, which
  preserves the dialog title and actions within the viewport.

Requirements to verify in the page you touch (not audited here):

- Icon-only buttons need an `aria-label`; decorative icons should be
  `aria-hidden`. Check the component you change rather than assuming it.
- Measure pointer targets you add. WCAG 2.2 asks for at least 24×24 CSS pixels
  for pointer targets; static status tags are not pointer targets.
- Test the flow with a keyboard, and with a screen reader where feasible. There
  has been no screen-reader audit.
- There has been no WCAG audit, and no page claims conformance. Contrast values
  in this document are measurements of specific token pairs, not a verdict on
  the product.

## Copy and language

- UI copy is bilingual: English and Vietnamese live in
  `web/src/shared/i18n/locales/`. A new key needs both locales; a missing key
  falls back, but the fallback is not a translation.
- Write for a person who has never opened a terminal: say "Add node", not
  "Provision agent enrolment"; explain what a field does in one sentence; put
  the consequence of a destructive action in the confirm dialog.
- Sentence case for labels and buttons. No exclamation marks. No blame
  ("Invalid input" → "Enter a name with letters, numbers or dashes").
- Keep the same noun everywhere: "environment", "node", "application" — not
  "env", "server", "app" in the same screen.
- These docs are English with Vietnamese under `/vi/`; keep page structure and
  token names identical so the two versions can be compared.

## What we borrow from comparable consoles

These observations were made on the public homepages on 2026-10-27. They are
not a full product audit, and no logged-in flow was tested. Gotham does not copy
brand colours, illustrations or marketing animation.

- [Coolify](https://coolify.io/) — a dark resource table that puts status,
  domain and server together, plus one clearly placed "new resource" action.
  Takeaway: keep status and context in the same row; make the next action
  obvious.
- [Dokploy](https://dokploy.com/) — capabilities grouped by Applications &
  Databases, Compose, Logs, Monitoring, Backups and Templates. Takeaway: group
  by the job the user is doing, not by internal service name.
- [Railway](https://railway.com/) — one prominent Deploy action and visible
  project/environment context; a screenshot shows a contextual memory warning.
  Takeaway: show the scope (project/environment) near the action, and warn in
  context when a resource limit matters.

All three are marketing surfaces, so their density is lower than the console's.
Use them for information architecture, not for sizing or colour.

## Do and do not

Do:

- use the spacing scale and the existing tokens; add a token only when a value
  repeats and needs a name;
- compose Naive components and shared UI (`PageHeader`, `AppTooltip`,
  `GothamIcon`) instead of new primitives;
- keep the modal contract: fixed header/footer, scrolling content, bounded
  height;
- put scope and status near the action that needs them;
- test at 1024px, 640px and a 480px-tall viewport, with reduced motion on;
- run the token check after touching `tokens.css`.

Do not:

- add a light theme switch or a second palette for the app;
- use `--meta` or a base status colour for essential small text;
- hardcode a hex value in a component when a token exists;
- claim WCAG conformance or "accessible" without an audit;
- grow a page past 1360px by default, or use the marketing display sizes in the
  console;
- recreate Naive controls as bespoke CSS specimens in product pages.

## Refresh and check

The documentation artifacts are generated from the runtime CSS; do not edit
`design-tokens.json`, `design-tokens.css` or the generated blocks inside
`design-preview.html` by hand.

```sh
cd docs

# after changing web/src/shared/styles/tokens.css
node scripts/design-tokens.mjs --write

# verify runtime CSS, JSON, CSS snapshot and preview inline tokens match
node scripts/design-tokens.mjs --check

# verify the documentation site still builds
npm ci
npm run docs:build
```

`--check` regenerates in memory and compares: the JSON, the CSS snapshot and the
preview's inline `:root` and token data must equal what the runtime CSS
produces; every token must classify into a preview category; the portable CSS
and preview must not load fonts, imports or remote resources; and the preview's
inline script must compile. It does not test filtering behaviour or pin any
token value — parity follows `tokens.css`. It prints the effective token count
(144 today). Run it manually before pushing; the docs CI job runs
`npm run docs:build` only.

Rounding of values is not allowed: the script normalizes whitespace only, so
`color-mix()` expressions and alias chains survive.

## File map

| Path | Purpose |
| --- | --- |
| `docs/design-system/DESIGN.md` | This page (English) |
| `docs/vi/design-system/DESIGN.md` | Vietnamese translation |
| `docs/public/design-system/design-tokens.json` | Flat token map plus preview categories |
| `docs/public/design-system/design-tokens.css` | Portable `:root` snapshot, no font faces |
| `docs/public/design-system/design-preview.html` | Offline interactive catalogue |
| `docs/scripts/design-tokens.mjs` | Generator and parity check |
