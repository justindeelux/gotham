# Gotham — PaaS Platform

Self-hosted Platform-as-a-Service: control plane (Go, modular monolith) + node agent (Go) + embedded Vue 3 SPA. Design priorities: compatibility → extensibility → self-updating → lean footprint.

## Tech Stack

| Layer | Technology |
|---|---|
| Backend / Agent | Go 1.22+, single binary, `linux/amd64` + `linux/arm64` |
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
| Config | viper (YAML / ENV, hot reload) |
| Self-update | minio/selfupdate + Ed25519 signing (GitHub Releases) |
| CI/CD | GitHub Actions + GoReleaser |

## Repository Layout (planned)

```
cmd/             # control-plane + agent entrypoints
internal/        # CP packages (server, deploy, builds, proxy, databases, services, auth, updates, store)
agent/           # node agent implementation
proto/           # protobuf contracts (buf-managed)
web/             # Vue 3 SPA (Vite)
templates/       # one-click service templates (YAML)
deploy/          # install scripts, systemd units, compose
docs/plan/       # per-phase development plans
docs/design/     # UI mockups (*.html) + design tokens (assets/gotham-ui.css)
```

## Development Workflow

- Docs index: `docs/README.md` (plan status, process, test server, design).
- 10 phases defined in `docs/plan/00-roadmap.md`; each task = 1 work package in an orca workspace (`ws/p<phase>-<slug>`), merged via its own PR; CI must be green before merge.
- Invariants (run after every task, before merge):
  1. `go build ./...` clean
  2. `go test ./...` green
  3. `golangci-lint run` + `go vet` clean
  4. Migrations are **forward-only** — never edit a merged migration; schema change = new migration
  5. `npm run build` + `npm run type-check` clean (web/)
  6. No cross-scope imports: `agent/` must not import `internal/`; domain packages must not import the HTTP server
- Mandatory review gates: G0 (end of Phase 0), G1 (Phase 4), G2 (Phase 9) — see `docs/plan/00-roadmap.md`.
- Phase gate: after each phase meets its exit criteria, STOP and ask the project owner before starting the next phase.
- Minimum dev environment: Docker, PostgreSQL 16, Redis 7, Node 20+ (web/ only).

## Code Style

- Vue SFC blocks: **always** `<script>` → `<template>` → `<style>` (omit unused blocks; when present, keep this order)
- Prefer `interface` over `type` (except unions/intersections)
- 2-space indent, LF, UTF-8, max line length ~100
- Semicolons, double quotes, trailing commas
- Strict TypeScript; clear names (`isActive`, `getRoomById`) over abbreviations

## UI Design

- `docs/design/` is the UI source of truth: one `*.html` mockup per page + shared tokens in `assets/gotham-ui.css` (+ page CSS in `assets/gotham-views.css`).
- Any task touching `web/` MUST compare against the matching mockup first and port the gap: extract design tokens (colors, fonts, spacing, radii) from `gotham-ui.css` into the Vue app, theme Naive UI to match — keep Naive UI as the component base, do not rebuild components from raw CSS.
- UI copy is English (rewrite from the mockups where they differ); code/docs stay English-only per Language below.
- Mockups for future-phase pages (applications, databases, domains, services, files, team-settings) are references only — implement them when their phase lands, not before.
- Rebuilt `internal/server/webdist` stays committed; the CI dist-drift check must pass.

## Language

**English only** in source code, comments, commit messages and docs, replies, and multilingual (i18n) files.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **gotham** (13710 symbols, 38199 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/gotham/context` | Codebase overview, check index freshness |
| `gitnexus://repo/gotham/clusters` | All functional areas |
| `gitnexus://repo/gotham/processes` | All execution flows |
| `gitnexus://repo/gotham/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
