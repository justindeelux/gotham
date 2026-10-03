# Retro review Phases 0–5 — register

Register for the owner-approved retro review of all code merged up to Phase 5
(PRs #1–#54). Review ran 2026-10-02/03 under a dual-reviewer regime; every fix
cluster shipped as its own PR with dual review and green CI. **Campaign
complete 2026-10-03: 16 clusters, PRs #100–#130, `main` @ `2219d78`.**

Plan and scope: `retro-review-p0-5.md`. Detailed triage tables, per-cluster fix
rounds and reviewer outputs live in the campaign workspace
`/Users/ndtpro/orca/workspaces/gotham/retro-p0-5/` (`WAVE*-TRIAGE.md`,
`FX*-spec.md`, `FX*-fixround*.md`, `FX*-report.md`, reviewer logs).

## Review units

15 units, dual-reviewed (external reviewer + built-in subagent):
A1–A4 (P0–P2), B1–B4 (P3 + UI shell/pages), C1–C4 (P4), D1–D3 (P5).

| Wave | Unit | Findings |
|---|---|---|
| 1 | A2 P1 Auth | 20 (4H/10M/6L) |
| 1 | C3 P4 webhooks/keys | 11 (2H/7M/2L) |
| 2 | A4 P2 CP/CA/registry | 19 (3H/13M/3L) |
| 2 | D2 P5 backups | 14 (1C/6H/5M/2L) |
| 3 | A3 P2 proto/agent | 12 (1H/9M/2L) |
| 3 | C2 P4 orchestration | 16 (6H/7M/3L) |
| 4 | B1 P3 backend | 13 (2H/7M/4L) |
| 4 | D1 P5 databases | 13 (6H/6M/1L) |
| 5 | A1 P0 foundation | 11 (6M/5L) |
| 5 | C1 P4 providers/builds | 16 (4H/8M/4L) |
| 6 | B2 P3 web | 14 (1H/7M/6L) |
| 6 | C4 P4 web+QA | 21 (2H/10M/9L) |
| 7 | B3 UI shell | 19 (9M/10L) |
| 7 | D3 P5 web | 17 (1H/9M/7L) |
| 8 | B4 UI pages | 20 (2H/8M/10L) |

≈236 deduped findings: 1 CRITICAL, 40 HIGH, ~120 MED, ~75 LOW. Cross-unit
duplicates (empty secret ×7, host trust ×2, rail poll ×3, log streaming ×3) were
merged into the clusters below.

## Fix clusters and delivery

Each cluster shipped as one work package (Orca worktree + PR + dual review +
green CI). Tier order: 0 → 4. Clusters marked a/b were split on size.

| Cluster | Scope | PR | Merge |
|---|---|---|---|
| FX-1 | Secrets at rest (systemic) | #100 | `37ca99d` |
| FX-2a | OAuth hardening | #101 | `8d3ab58` |
| FX-2b | Session lifecycle | #102 | `03980ee` |
| FX-2e | FE session robustness | #103 | `42c2ed4` |
| FX-2f | Logout replay | #104 | `3cee7d4` |
| FX-2c | API-token containment | #105 | `8a1f215` |
| FX-2d | HTTP hardening | #106 | `ba2ca01` |
| FX-4 | Git/SSH host trust | #107 | `1b7c169` |
| FX-3 | gRPC/CA | #108 | `cf7541c` |
| FX-5a | Storage host binds | #109 | `2a2cb03` |
| FX-5b | Registry isolation + remote builds | #110 | `cb24ba7` |
| FX-6a | Deploy state machine | #111 | `7bf1bea` |
| FX-6b | Config atomicity + redaction | #112 | `5370bf4` |
| FX-7a | Agent runtime / gRPC taxonomy | #113 | `511b5d5` |
| FX-7b | Installer + backoff | #114 | `0b8bda2` |
| FX-8a | DB lifecycle/fencing | #115 | `8fe63ae` |
| FX-8b | DB engine/retention/readiness | #116 | `93d2ec0` |
| FX-9a | Restore execution | #117 | `fc4eb69` |
| FX-9b | Backup targets/store/scheduler | #118 | `708b22d` |
| FX-10a | Webhooks / deploy keys | #119 | `6f5a68e` |
| FX-10b | Providers | #120 | `395ab30` |
| FX-11 | Build pipeline | #121 | `dd51285` |
| FX-12a | Realtime backend | #122 | `fe56d84` |
| FX-13a | Foundation | #123 | `32d5563` |
| FX-12b | Realtime frontend | #124 | `8396ccb` |
| FX-14a | App/wizard web | #125 | `111f9da` |
| FX-13b | Servers hardening | #126 | `0748dcf` |
| FX-14b | Databases web | #127 | `2e60fa3` |
| FX-15a | Shell/nav/a11y | #128 | `a8776e2` |
| FX-15b | Theme/design-port | #129 | `434098f` |
| FX-16 | CI/test/tooling gaps | #130 | `2219d78` |

### Cluster scope

- **FX-1** — one empty-secret resolution + audit of every `SealSecret` caller + regressions + `gotham.yaml` ignore.
- **FX-2** — OAuth verified email, fragment→exchange, state cap; token scopes; session family/pruning/atomic rotation; timing; trusted-proxy rate limit; HTTP timeouts; HSTS/CSP; FE refresh races.
- **FX-3** — SAN validation, CA fail-closed, registration identity binding, gRPC rate limits, error logging, cert renewal, dev plaintext bind.
- **FX-4** — pinned/configurable `known_hosts` + `StrictHostKeyChecking=yes` for cloner and servers SSH validation.
- **FX-5** — storage host binds confined to a managed root; registry auth + network isolation; railpack/buildpacks remote-node builds.
- **FX-6** — delete/start fencing, retirement verification, prep-before-stop, transition ordering, idempotent retry, node move, collection transactions; config atomicity + redaction.
- **FX-7** — 404/Unavailable mapping, created-container cleanup, log-stream errors, gRPC conn/deadline management; installer env preservation, restart, backoff jitter.
- **FX-8** — image pull, start-failure cleanup, `ServerID` preservation, provisioning/deletion fencing, rename clobber, PG18 mount, retention sweep, port conflicts; engine/readiness.
- **FX-9** — transactional restores, restore records/sweep, orphan-job removal, lifecycle lease, MariaDB binaries, DST, fsync, pagination, target integrity.
- **FX-10** — 503/pending delivery, dedupe atomicity, branch exact match, decrypt logging, conditional deletes, detached rollback, orphan sweep; provider connect route, token refresh persistence, cache atomicity, pagination bounds, timeouts.
- **FX-11** — `.dockerignore` enforcement, symlink preservation, bounded context, forcerm, image/registry retention.
- **FX-12** — container log streaming end to end, connection reuse, bridge supervision, shutdown, reader/write leaks, subscription caps, token refresh on reconnect, denied frames; FE reconnect/edit races.
- **FX-13** — config watcher race, migrate session lock, dev-compose binding, Redis timeout, panic logging, hot-reload docs; offline sweep, duplicate registration, passphrase keys, SSH hang, ready semantics, servers poll races.
- **FX-14** — app draft races/destructive save, pipeline failure display, `logServerId`, repo errors, env wizard contract, create&deploy, secret mislabel, polling lifecycle; DB schedule/restore/downtime/UX states; wizard feedback/validation.
- **FX-15** — auth scroll, drawer focus, nav active, rail links/colors, contrast/focus, popover token, order/copy/landmarks, chips/aria, empty states, design-port gaps.
- **FX-16** — proto drift job, DB-backed auth/servers tests, sqlc untracked check, web `mock-ws-check` + `eslint-plugin-vue`, e2e hygiene/determinism, skipped-test masking.

## Accepted residuals (register-only, not code fixes)

Owner-approved deferrals, recorded rather than fixed:

- **R register-only:** A2-18, A2-20, D1-10, D3-17 subset, B2-13, C4-21 — access-JWT post-logout window, localStorage refresh TTL, documented design adaptations/deferred follow-ups.

Accepted LOWs per cluster (details in the workspace `FX*-report.md`):

- **FX-6b** — one-character comment typo.
- **FX-7b** — 4 fail-closed installer parity LOWs (trailing dot, leading-zero test, stricter IPv6 forms, header index).
- **FX-8a** — unconditional image-pull tradeoff; error rows keeping ports.
- **FX-8b** — `docs/TODO.md` wording note.
- **FX-9a** — test-harness lease wiring note; artifact reaping tracked below.
- **FX-11** — comment about a glued `**` (Docker treats it as recursive; CP does not).
- **FX-12a** — batch id absent, `replay` key assertion, stale start callbacks, seq-based upgrade path.
- **FX-12b** — per-channel buffer eviction (LRU when it matters).
- **FX-13b** — `npm test` not in CI (closed by FX-16); enrollment association when node id ≠ address.
- **FX-14a** — 4 LOWs (stale `loading` clear, `fetchAll` teardown guard, `reposError` late failure, wizard call-site coverage).
- **FX-14b** — page-wiring Vue-scheduling test for restore toasts.
- **FX-15a** — docstring wording.
- **FX-15b** — `connectionDisplay` regex masks only the first `:…@` (use `new URL()` later).
- **FX-16** — `TestStoreCreateFirstUserSerializes` skips on a fresh CI DB (own DB/schema or serial step); Redis 500ms ping may need ~2s on a cold runner.

## Reviewer tooling

- **External slot priority:** Claude Code CLI first
  (`cat prompt.md | claude -p --allowedTools "Read,Grep,Glob,Bash" --add-dir <main repo>`,
  loading `.claude/skills/code-reviewer/SKILL.md`), then Codex if Claude is
  unavailable (`codex exec --dangerously-bypass-approvals-and-sandbox -C <worktree> -`),
  then **stop** — no silent substitution. Built-in subagents are the second
  reviewer alongside it.
- One Claude CLI session limit (02:45–03:10 +07) and one Codex quota halt
  (02:27–09:27) occurred; Codex covered the Claude gap.

## Notable campaign outcomes

- **FX-16 earned its keep:** enabling the DB-backed and untracked-file gates
  surfaced real defects that had been hidden by silent skips — two auth tests
  that fail on a fresh database, a webdist drift gate blind spot, and Redis
  tests that skipped despite a provisioned Redis. It also caught two of its own
  environment gaps (web job needed Node 22 for the global `WebSocket` harness;
  the untracked gate proved itself on a real drift).
- **FX-15b:** CI caught a real regression — Naive UI cannot parse
  `var(--float)`/`color-mix()` for `popoverColor`, so the theme now passes the
  literal `#121315`.
