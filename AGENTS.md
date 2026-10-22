# Gotham — PaaS Platform

Self-hosted Platform-as-a-Service: control plane (Go, modular monolith) + node agent (Go) + embedded Vue 3 SPA. Design priorities: compatibility → extensibility → self-updating → lean footprint.

## Development Workflow

- Docs index: `docs/README.md` (plan status, process, test server, design).
- Public docs: `site/` (VitePress, EN + `/vi/`); `docs/` is internal reference, never `docs/plans/`.
- Each task = 1 work package in its own worktree/branch (naming: `docs/ORCHESTRATION.md` → Naming rule), merged via its own PR; CI must be green before merge. `docs/plans/` and `docs/TODO.md` are reference only.
- Git operations (branching, commit messages, merge vs rebase, conflict resolution): use the `/git-workflow` skill. Commits and PR titles follow Conventional Commits, `<type>(<scope>): <subject>` (e.g. `fix(files): show owner / mode separately`). Branch and worktree names keep the `<type>/<slug>` rule in `docs/ORCHESTRATION.md`.
- Invariants (run after every task, before merge):
  1. `go build ./...` clean
  2. `go test ./...` green
  3. `golangci-lint run` + `go vet` clean
  4. Migrations are **forward-only** — never edit a merged migration; schema change = new migration
  5. `npm run build` + `npm run type-check` clean (web/)
  6. No cross-scope imports: `agent/` must not import `internal/`; domain packages must not import the HTTP server
  7. `cd site && npm ci && npm run docs:build` clean (`site/` only; Node 20+; no Go/`web/` impact)
- Orchestration rules (worker dispatch, recipe, lessons): see `docs/ORCHESTRATION.md`.
- UI e2e: run the Playwright smoke suite (`npm run e2e` in `web/`) when needed, against throwaway Postgres + Redis containers and its own `gotham serve` (see `docs/test-server.md`); never reuse the shared `gotham-dev-postgres` / `gotham-dev-redis`, and remove the containers when the run ends.
- Minimum dev environment: Docker, PostgreSQL 16, Redis 7, Node 20+ (web/ only).

## Code Style

- Vue SFC blocks: **always** `<script>` → `<template>` → `<style>` (omit unused blocks; when present, keep this order)
- Prefer `interface` over `type` (except unions/intersections)
- 2-space indent, LF, UTF-8, max line length ~100
- Semicolons, double quotes, trailing commas
- Strict TypeScript; clear names (`isActive`, `getRoomById`) over abbreviations
- Validation = zod schemas via `web/src/shared/validation` (adapter `ruleFrom`, envelope `parseWith`); feature schemas live in `features/<m>/schemas/`, no new ad-hoc `validator:` closures

## UI Design

- Keep Naive UI as the component base — do not rebuild components from raw CSS. Colors, fonts, spacing and radii come from the design tokens in `web/src/shared/styles/tokens.css`, not hard-coded hex/px.
- UI copy follows the project's language policy (see Language below); rewrite from the mockups where they differ.
- Modals: header and footer stay fixed, only the content region scrolls, max-height is bounded to the viewport and a modal never exceeds it (shared `.app-modal`/`.wizard-modal` contract in `web/src/shared/styles/main.css`).
- Rebuilt `internal/server/webdist` stays committed; the CI dist-drift check must pass.

## Language

Use one consistent language across the project (code, comments, commit messages, docs, UI copy and i18n files).

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **gotham** (32964 symbols, 101271 relationships, 1448 execution flows).

> Index stale? Run `node .gitnexus/run.cjs analyze --index-only` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? Bootstrap with `npx`, `bunx`, or `pnpm dlx` — e.g. `bunx gitnexus@latest analyze` (npm 11 npx crash; #1939).

## Always Do

- **MUST run impact before editing.** Use `impact({target: "symbolName", direction: "upstream"})` or `node .gitnexus/run.cjs impact "symbolName" --direction upstream --repo .`; report callers, processes, and risk. Never substitute grep for graph analysis.
- **MUST analyze graph changes before committing.** Use `detect_changes({scope: "all"})` (MCP) or `node .gitnexus/run.cjs detect-changes --scope all --repo .` (CLI fallback). `partial: true` or `truncated: true` is not a clean check — a zero means unseen, not unaffected; re-run it. For regression review: `detect_changes({scope: "compare", base_ref: "main"})` or `node .gitnexus/run.cjs detect-changes --scope compare --base-ref "main" --repo .`.
- MUST warn on HIGH/CRITICAL `risk` pre-edit; never use `riskSharedAxes` to waive a HIGH/CRITICAL `risk` warning. Compare File/symbol: MCP File omits axes; Graph-RAG expands File.
- **MUST treat `risk: UNKNOWN` as unresolved, not as low.** An empty caller set is not evidence the symbol is unused — it can also mean the callers are not resolvable by the index (plain-object property access, dynamic dispatch, cross-language calls). `impact` pairs `UNKNOWN` with a `riskNote` saying so. Confirm with a text search before treating the symbol as safe to change or delete; do not proceed on the strength of a zero.
- **MUST use `query({search_query: "concept"})` for concepts/flows, `context({name: "symbolName"})` for a named symbol, or `impact` for blast radius, on read-only callers, dependencies, imports, or execution flow.** Graph first; text search only for empty/`UNKNOWN`/literals.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method before MCP/CLI impact analysis.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis, and never read `UNKNOWN` as an all-clear — it means the walk could not answer, which is the one verdict that requires confirming by other means.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit before MCP/CLI graph change analysis.

## Resources

| Resource | Use for |
| --- | --- |
| `gitnexus://repo/gotham/context` | Codebase overview, check index freshness |
| `gitnexus://repo/gotham/clusters` | All functional areas |
| `gitnexus://repo/gotham/processes` | All execution flows |
| `gitnexus://repo/gotham/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
| --- | --- |
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
