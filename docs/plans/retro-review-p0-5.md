# Retro review — Phases 0–5 + UI side track

Owner-approved pass to re-review, under the dual-reviewer regime (Codex +
subagent), all code merged up to Phase 5 (PRs #1–#54). Everything is already
merged and in use, so findings land as follow-up PRs (owner-approved per
cluster) or as register entries. Status: planned, 2026-10-02.

## Scope

PRs ≤ #54 still live on `main`; review the code as it exists on `main`
(`9db58de`) — the historical diffs are only the map of what to inspect.
Excluded: post-Phase-9 PRs #88–#99 (already dual-reviewed in the 2026-10-01
pass).

| Group | PRs | Insertions / files |
|---|---|---|
| P0 Foundation | #1–#5 | 6.3k / 74 |
| P1 Auth | #6–#9 | 7.8k / 67 |
| P2 Server + Agent | #10–#14, #44 | ~15k / 96 |
| P3 Docker core + UI-0..10 | #15–#30, #34 | 27.7k / 196 |
| P4 Applications | #31–#50 (P5 PRs excluded) | 31.2k / 228 |
| P5 Databases + backups | #39, #43, #46, #50, #52, #54 | 24k / 186 |

Total ≈ 112k insertions / 847 file-changes.

## Review units

| Unit | Scope (PRs) | Reviewers |
|---|---|---|
| A1 | P0 Foundation #1–#5 | Codex + code-reviewer |
| A2 | P1 Auth #6–#9 | Codex + code-reviewer + security-reviewer |
| A3 | P2 proto + agent #10, #11, #44 | Codex + code-reviewer |
| A4 | P2 CP gRPC/CA/registry + FE + e2e #12–#14 | Codex + code-reviewer + security-reviewer |
| B1 | P3 backend #15, #17, #18, #29 | Codex + code-reviewer |
| B2 | P3 web #21, #24, #27 | Codex + vue-reviewer |
| B3 | UI shell/theme #16, #19, #20, #22 | Codex + vue-reviewer |
| B4 | UI pages #23, #25, #26, #28, #30, #34 | Codex + vue-reviewer |
| C1 | P4 providers + builds #31–#33, #35, #36 | Codex + code-reviewer |
| C2 | P4 orchestration + CRUD #37, #41, #48 | Codex + code-reviewer |
| C3 | P4 webhooks + deploy keys #40, #47 | Codex + code-reviewer + security-reviewer |
| C4 | P4 web + QA #38, #42, #45, #49 | Codex + vue-reviewer |
| D1 | P5 databases BE #39 | Codex + code-reviewer |
| D2 | P5 backups BE + fixes #46, #52, #54 | Codex + code-reviewer |
| D3 | P5 web #43, #50 | Codex + vue-reviewer |

## Wave schedule (2 units per wave)

| Wave | Units |
|---|---|
| 1 | A2, C3 |
| 2 | A4, D2 |
| 3 | C2, A3 |
| 4 | B1, D1 |
| 5 | C1, A1 |
| 6 | C4, B2 |
| 7 | B3, D3 |
| 8 | B4 |

Triage of a finished wave runs while the next wave is in flight.

## Review contract

Both reviewers of a unit get the same scope and output contract:

- Findings: `[SEVERITY] file:line — problem — evidence — suggested fix —
  verified-live-on-main: yes/no`.
- End with `VERDICT: RETRO-GO` or `VERDICT: FINDINGS(n: C/H/M/L)`.
- Focus: correctness, security, concurrency/races, resource leaks, silent
  failures, migration safety, test quality, API contracts, cross-scope imports.
- Read-only; unit tests allowed; do not modify files.
- Do not re-report documented residuals (`docs/TODO.md`); mark findings that
  later phases already rewrote as `superseded`.
- Codex recipe: `codex exec --sandbox read-only "<prompt>"` from the repo root,
  scope given as commit SHAs plus `gh pr view <n>`.
- Subagent reviewers load the project `code-reviewer` skill
  (`.opencode/skills/code-reviewer`) for the checklist and analyzers;
  `vue-reviewer` for web units, `security-reviewer` on the security-critical
  units.

Evidence: `/Users/ndtpro/orca/workspaces/gotham/retro-p0-5/`
(`<unit>-codex.txt`, `<unit>-subagent.md`).

## Triage and disposition

1. Normalize every finding into one table (dedupe across reviewers, verify
   against current `main`).
2. Classify: live defect / superseded / documented residual / false positive.
3. Owner approves the fix list per cluster.
4. Each fix cluster ships as its own work package (Orca workspace + PR + dual
   review + green CI).
5. Outcomes recorded in `docs/TODO.md` under "Retro review Phases 0–5".
6. Final report: findings, dispositions, fix PRs.

## Estimates and risks

- 15 units × 2–3 reviewers = 30–45 review runs; 8 waves ≈ 3–4 h wall clock;
  triage is pipelined; fixes follow the findings.
- Risks: false positives from superseded code (verify-on-main step); context
  overload on large units (units kept ≤ ~11k insertions); reviewers touching
  files (read-only instruction); Codex/subagent rate limits (2 units per wave,
  retry).
