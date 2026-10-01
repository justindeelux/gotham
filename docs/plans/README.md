# Side plans

Implementation plans for owner-approved post-Phase-9 work packages. These sit
outside `docs/plan/` (which holds the per-phase roadmap and milestone reports)
and move into `docs/plan/` reports as they complete.

| File | Work package | Status |
|---|---|---|
| `p-a1-auth-shell.md` | Auth pages: drop the card chrome (strict design port) | planned |
| `p-a2-single-admin.md` | Single admin; registration only via admin invite | planned |
| `p-a3-cli-admin.md` | CLI: `gotham admin create` / `admin reset-password` | planned |
| `p-a4-servers-grid.md` | Servers page: card grid (strict design port) | planned |

Order: P-A2 + P-A3 land as one PR (the one-account invariant must be atomic),
then P-A1, then P-A4.
