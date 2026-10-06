# Gotham — PaaS Platform

Self-hosted Platform-as-a-Service: control plane (Go, modular monolith) + node agent (Go) + embedded Vue 3 SPA. Design priorities: compatibility → extensibility → self-updating → lean footprint.

## Tech Stack

| Layer | Technology |
|---|---|
| Backend / Agent | Go 1.27+, single binary, `linux/amd64` + `linux/arm64` |
| HTTP framework | Chi router |
| Data access | sqlc + pgx |
| Database | PostgreSQL 16 |
| Migrations | goose (forward-only) |
| CP ↔ Agent | gRPC + protobuf (buf), mTLS |
| Realtime | WebSocket + Redis pub/sub |
| Reverse proxy | Traefik 3.x |
| Frontend | Vue 3 + Vite + TypeScript (SPA embedded via `embed.FS`) |
| State / HTTP | Pinia + axios |
| UI kit | Naive UI |
| Auth | JWT + OAuth2 (GitHub, GitLab), argon2id, TOTP later |
| Config | viper (YAML / ENV, hot reload of log level) |
| Self-update | Built-in atomic swap + Ed25519 signed manifest (GitHub Releases) |
| CI/CD | GitHub Actions + GoReleaser |

## Repository Layout

```
cmd/             # control-plane + agent entrypoints
internal/        # CP packages (server, deploy, builds, proxy, databases, services, auth, updates, store)
agent/           # node agent implementation
updatecore/      # shared, transport-agnostic self-update engine (verify + swap), imported by internal/updates and agent/
proto/           # protobuf contracts (buf-managed)
web/             # Vue 3 SPA (Vite; src/: app/ shell, features/<module>/, shared/)
templates/       # one-click service templates (YAML)
deploy/          # install scripts, systemd units, compose
docs/plans/      # feature/phase plans (medium to large work)
docs/sub-plans/  # small focused plans (single UI fix, one-off work package)
docs/design-plans/ # UI-only design briefs (what each screen must do)
docs/design/     # UI mockups (*.html) + design tokens (assets/gotham-ui.css)
```

## Development Workflow

- Docs index: `docs/README.md` (plan status, process, test server, design).
- 10 phases defined in `docs/plans/00-roadmap.md`; each task = 1 work package in an orca workspace (`ws/p<phase>-<slug>`), merged via its own PR; CI must be green before merge.
- Invariants (run after every task, before merge):
  1. `go build ./...` clean
  2. `go test ./...` green
  3. `golangci-lint run` + `go vet` clean
  4. Migrations are **forward-only** — never edit a merged migration; schema change = new migration
  5. `npm run build` + `npm run type-check` clean (web/)
  6. No cross-scope imports: `agent/` must not import `internal/`; domain packages must not import the HTTP server
- Mandatory review gates: G0 (end of Phase 0), G1 (Phase 4), G2 (Phase 9) — see `docs/plans/00-roadmap.md`.
- Phase gate: after each phase meets its exit criteria, STOP and ask the project owner before starting the next phase.
- Minimum dev environment: Docker, PostgreSQL 16, Redis 7, Node 20+ (web/ only).

## Code Style

- Vue SFC blocks: **always** `<script>` → `<template>` → `<style>` (omit unused blocks; when present, keep this order)
- Prefer `interface` over `type` (except unions/intersections)
- 2-space indent, LF, UTF-8, max line length ~100
- Semicolons, double quotes, trailing commas
- Strict TypeScript; clear names (`isActive`, `getRoomById`) over abbreviations
- Validation = zod schemas via `web/src/shared/validation` (adapter `ruleFrom`, envelope `parseWith`); feature schemas live in `features/<m>/schemas/`, no new ad-hoc `validator:` closures

## UI Design

- `docs/design/` is the UI source of truth: one `*.html` mockup per page + shared tokens in `assets/gotham-ui.css` (+ page CSS in `assets/gotham-views.css`).
- Any task touching `web/` MUST compare against the matching mockup first and port the gap: extract design tokens (colors, fonts, spacing, radii) from `gotham-ui.css` into the Vue app, theme Naive UI to match — keep Naive UI as the component base, do not rebuild components from raw CSS.
- UI copy is English (rewrite from the mockups where they differ); shipped English copy is the
  English i18n catalog baseline. Vietnamese lives as translated message values in
  `web/src/shared/i18n/locales/vi.ts` and `web/src/features/*/locales/vi.ts`, plus three
  deliberate non-catalog spots: parameterized day-count sentences in
  `web/src/shared/utils/format.ts` (consume static `time.*` labels from the catalog),
  the dependency-free stale-chunk copy in `web/src/shared/i18n/staleFallback.ts`
  (must render without the Vue/i18n runtime), and proper-noun autonyms
  (`language.names`) consumed by `LanguageSelect.vue`. Code/docs stay English-only
  per Language below.
- Mockups for future-phase pages (applications, databases, domains, services, files, team-settings) are references only — implement them when their phase lands, not before.
- Rebuilt `internal/server/webdist` stays committed; the CI dist-drift check must pass.

## Language

**English only** in source code, comments, commit messages and docs, replies, and multilingual (i18n) files —
with one narrow UI-language exception: translated Vietnamese message *values* in
`web/src/shared/i18n/locales/vi.ts` and `web/src/features/*/locales/vi.ts` may be Vietnamese.
Message keys, identifiers, comments, docs and replies stay English.

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
