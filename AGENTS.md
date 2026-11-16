# Gotham — PaaS Platform

Self-hosted Platform-as-a-Service: control plane (Go, modular monolith) + node agent (Go) + embedded Vue 3 SPA. Design priorities: ease of use for non-developers → compatibility → extensibility → self-updating → lean footprint.

## Product Principle

Gotham is self-hosted and aimed at people with little server experience, not only developers. Installing, configuring and updating it must not require running shell commands by hand or setting Linux environment variables.

- A fresh install works out of the box with sensible defaults; anything optional (domain, HTTPS, extra nodes) is a prompt in the installer or a setting in the UI.
- Environment variables and CLI flags are escape hatches for automation, never the primary path.
- Installation is a single script that needs no repository checkout.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.
- Use the `no-ai-slop` skill for the writing style, and avoid overusing the `·` character.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## Development Workflow

- If you are an orchestrator, worker or reviewer, read the file for your role first: `.workflow/orchestration.md`, `.workflow/worker.md`, `.workflow/reviewer.md`.
- `docs/` is the user guide, not a place for internal documentation.
- Small changes (typos, wording, CSS, minor fixes, config, docs) are batched: related changes share one branch/PR, one commit per change. Real logic changes, migrations, security fixes and changes touching many modules get their own branch/PR.
- Branch name: `<type>/<slug>`, e.g. `feat/files-icon-tooltips`. `<type>` is one of `feat` (new capability), `fix` (bug fix), `refactor` (no behaviour change), `perf`, `docs`, `test`, `ci`, `build` (build system, deps, packaging), `chore` (housekeeping).
- Commits follow Conventional Commits: `<type>(<scope>): <subject>`, imperative, no trailing period, ~50 chars, e.g. `fix(files): show owner / mode separately`. The squash-merge title is the PR title; for a batch, use the theme. For branching, merge vs rebase and conflicts, see the `/git-workflow` skill.
- PRs must be self-explanatory: give the context and list every item in a batch. If two PRs touch the same file, merge them in sequence and rebase the second onto the new `main`.
- Before pushing, run the invariants below that match the paths you touched. CI must be green before merge.
- Invariants (run after every task, before merge):
  1. `go build ./...` clean
  2. `go test ./...` green
  3. `golangci-lint run` + `go vet` clean
  4. Migrations are **forward-only**: never edit a merged migration; a schema change is a new migration
  5. `npm run build` + `npm run type-check` clean (`web/`)
  6. No cross-scope imports: `agent/` must not import `internal/`; domain packages must not import the HTTP server
  7. `cd docs && npm ci && npm run docs:build` clean (`docs/` only; no Go/`web/` impact)

## Code Style

- Vue SFC blocks: **always** `<script>` → `<template>` → `<style>` (omit unused blocks; when present, keep this order)
- Prefer `interface` over `type` (except unions/intersections)
- 2-space indent, LF, UTF-8, max line length ~100
- Semicolons, double quotes, trailing commas
- Strict TypeScript; clear names (`isActive`, `getRoomById`) over abbreviations
- Validation = zod schemas via `web/src/shared/validation` (adapter `ruleFrom`, envelope `parseWith`); feature schemas live in `features/<m>/schemas/`, no new ad-hoc `validator:` closures

## UI Design

- Any frontend (`web/`) work follows the design system in `DESIGN.md` (repo root): read it first and match its rules. Never edit `DESIGN.md`; if a change seems to need a deviation, stop and ask.
- Keep Naive UI as the component base — do not rebuild components from raw CSS. Colors, fonts, spacing and radii come from the design tokens in `web/src/shared/styles/tokens.css`, not hard-coded hex/px.
- UI copy follows the project's language policy (see Language below); rewrite from the mockups where they differ.
- i18n: every user-facing string goes through vue-i18n, never hardcoded in components, schemas or toasts. Each feature's `locales/` ships both `en.ts` and `vi.ts` with identical keys, registered like the other features. A new or changed key is added to both files in the same PR; a missing `vi` key is a bug. Before opening the PR, switch the UI to Vietnamese and check that no English text or raw key shows.
- Modals: header and footer stay fixed, only the content region scrolls, max-height is bounded to the viewport and a modal never exceeds it (shared `.app-modal`/`.wizard-modal` contract in `web/src/shared/styles/main.css`).
- `internal/server/webdist` is not committed except a placeholder `index.html`; `make web-build` (or `npm run build` in `web/`) fills it, and release CI builds it before `go build`.

## Language

Use one consistent language across the project (code, comments, commit messages, docs, UI copy and i18n files).
