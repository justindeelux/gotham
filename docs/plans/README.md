# Side plans

Implementation plans for owner-approved post-Phase-9 work packages. These sit
outside `docs/plan/` (which holds the per-phase roadmap and milestone reports)
and move into `docs/plan/` reports as they complete.

| File | Work package | Status |
|---|---|---|
| `p-a1-auth-shell.md` | Auth pages: drop the card chrome (strict design port) | done (#89) |
| `p-a2-single-admin.md` | Single admin; registration only via admin invite | done (#88) |
| `p-a3-cli-admin.md` | CLI: `gotham admin create` / `admin reset-password` | done (#88) |
| `p-a4-servers-grid.md` | Servers page: card grid (strict design port) | done (#90) |
| `p-a5-session-versioning.md` | Credential versioning: a password reset ends every live chain | done (#91) |
| `retro-review-p0-5.md` | Retro review Phases 0–5 + UI side track (parallel Codex + subagent) | planned |

Order: P-A2 + P-A3 land as one PR (the one-account invariant must be atomic),
then P-A1, then P-A4. P-A5 is carried from the PR #88 review: it closes the
reset/login vs session-issuance race that P-A3 deliberately narrowed rather
than solved.
