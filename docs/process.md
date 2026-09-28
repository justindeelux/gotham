# Gotham Development Process

How work gets done. The plan itself lives in [`plan/`](plan/) (see the
[roadmap](plan/00-roadmap.md) and [`TODO.md`](TODO.md) for status).

## Work packages & orca workspace

- Each task in a phase file = 1 work package for 1 subagent, running in its
  **own orca workspace**: `ws/p<phase>-<slug>` (e.g. `ws/p4-builds`).
- Each workspace merges to the main branch via its **own PR**; CI must be
  green before merge.
- Tasks may run in parallel only when they touch no common files and depend
  on no each other's output (noted in each phase file).

## Phase gate (continuous mode — owner waiver from Phase 3 onward)

- The owner waived per-phase STOPs: after a phase meets its exit criteria,
  the coordinator continues to the next phase automatically without asking.
  STOP only when blocked, when a decision is needed, or when a review gate
  fails.
- Milestone reports are posted as PR descriptions and chat summaries instead
  of gate approvals.

## Review gates (mandatory, block merge)

| Gate | When | Reviewers |
|---|---|---|
| G0 | End of Phase 0 | subagent `code-reviewer` (whole foundation) |
| G1 | End of Phase 4 | `code-reviewer` + `e2e-runner` (core deploy experience) |
| G2 | End of Phase 9 | `code-reviewer` + `security-reviewer` (release/self-update, signatures, install) |

Design-sensitive packages (contract design, orchestration state machines,
template engines) need `code-reviewer` design review before merge.

## Invariant (run after every task, before PR merge)

1. `go build ./...` passes.
2. `go test ./...` green (all existing tests must pass).
3. `golangci-lint run` clean; `go vet` clean.
4. Migrations are **forward-only** — never edit a merged migration; schema
   changes = new migration.
5. FE: `npm run build` passes, `npm run type-check` clean.
6. No cross-scope imports: `agent/` must not import `internal/`; domain
   packages must not import the HTTP server.

## Rollback

- Each phase file states how to roll back (revert migration, feature flag,
  old image tag) in its own section.
- General rule: every new feature after Phase 0 must be disable-able via an
  env flag if it touches the running deploy flow.

## Minimum dev environment

- Docker (to test agent + docker core), PostgreSQL 16, Redis 7 (dev
  containers in `deploy/compose.dev.yml` — created in Phase 0).
- Node 20+ (for `web/` only).

## E2E suites

```bash
GOTHAM_E2E=1 go test ./internal/e2e/... -count=1 -timeout 20m -v   # API leg
cd web && npm run e2e                                              # Playwright leg
```

## How to resume

Toolchain and repo:

```bash
export PATH=/usr/local/go/bin:$PATH        # Go 1.25.x local; CI runs 1.22.x
cd /Users/ndtpro/Projects/Tools/gotham
git pull --ff-only
```

Test server:

```bash
rsync -az --delete --exclude '.git' --exclude 'web/node_modules' \
  --exclude 'bin' --exclude 'data' ./ gotham:/root/gotham/
ssh gotham 'cd /root/gotham && export PATH=/usr/local/go/bin:$PATH && \
  make migrate && make build && systemctl restart gotham gotham-agent'
```

One work package = one orca worktree = one PR; merge after CI green (and
review for design-sensitive packages). Run GitNexus `impact` before touching
existing symbols and `detect_changes` before committing (see `AGENTS.md`).

## Decision log (owner-approved)

- Continuous phase mode: no STOP between phases; STOP only when blocked, a
  decision is needed, or a review gate fails.
- G1 waivers (2026-09-27): Playwright UI leg deferred to QA-4.1b (merged as
  #49); network clone of a real public repo is verified live on the test box
  instead of in CI (fixtures keep the gate deterministic).
- Accepted deviations (carried from earlier phases): stateless JWT stays valid
  after logout; `internal/servers` domain package; CSR-based agent cert flow;
  no plaintext token logging; dev-mode plaintext agent transport when no CA.
- Migration numbering: `00009` reserved for Traefik/domains, `00010` deploy
  keys, `00011` backups. Never reuse a number.
- BE-6.2 acceptance (2026-09-28): the ACME resolver gains a `caServer` knob
  (empty = Traefik's Let's Encrypt production default) so iteration happens
  against the Let's Encrypt staging directory; the production acceptance is a
  single, explicitly opted-in issuance (`GOTHAM_E2E_ACME_CA=production`).
  Gated acceptance test defaults to staging, so an ambient run cannot consume
  production rate limits.
