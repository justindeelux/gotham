# Development workflow

## Delivery boundaries

Track issues separately in Plane, but deliver related small changes in one
short-lived branch and PR when they share a review and rollback boundary. Link
every issue in the PR and attach the PR to every included issue. Keep each issue
open until its work is merged. A PR should normally fit one working session;
split it when review, risk or independent release needs require separate changes.
Migrations, permission changes and independent features get separate PRs.

Use Conventional Commits for commits and PR titles: `<type>(<scope>): <subject>`.
Branches use `<type>/<slug>` with a short kebab-case slug. Allowed types are
`feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `ci`, `build`, and `chore`.
Use `Refs: GOTHAM-<n>` footers for all included Plane issues; do not put local
Plane URLs in git or PR descriptions.

## Checkouts

Keep the primary `main` checkout for synchronization and GitNexus indexing.
Reuse one working checkout for sequential deliveries:

```sh
node scripts/start-work.mjs fix/files-polish
```

The command fetches `origin`, creates the branch from `origin/main`, and uses
the sibling `<repository>-work` checkout. Reuse requires a clean checkout and
proof that its previous head is already in `origin/main` or has a merged PR
with the same head SHA. The latter handles squash merges and requires `gh`.
Unpushed commits, untracked files and unmerged work stop reuse. Existing branches
must be resumed explicitly; the command does not reset, delete or force-push them.

For concurrent work, request a separate checkout:

```sh
node scripts/start-work.mjs fix/terminal-resize --parallel
```

This creates `<repository>-fix-terminal-resize`. Each checkout has one editor
owner. Review a frozen commit: pause editing during review, or review an isolated
snapshot if implementation continues. Preserve the checkout through fix rounds.
After merge, reuse the sequential checkout; remove parallel checkouts only after
checking they are clean and all work is pushed. Clean up only the delivery's own
branches and resources.

The bootstrap links ignored `docs/ORCHESTRATION.md` and `docs/test-server.md` to
their primary-checkout originals when available, so they do not become stale
copies. It creates the local OpenCode model pin only when absent. Agent dispatch,
model verification and operator/test-server details remain in those local notes;
this tracked document is the common workflow policy available in fresh checkouts.

## Review and CI

1. Implement the delivery and run relevant local checks across its entire scope.
2. Review and fix locally, keeping the review tied to the exact commit.
3. Push and open a ready PR. Open a draft early only when collaboration needs it.
4. Require `CI gate` green on the final ready PR, then merge and synchronize `main`.

Drafts run scope/policy tests and defer expensive checks. Their gate deliberately
fails with a draft message until marked ready; they cannot supply a green merge
gate. `ready_for_review` reruns CI even without a new commit. Further pushes to a
ready PR rerun applicable checks and cancel the superseded PR run.

CI always starts one scope job, compares the PR head with the merge base using
the full git history, and includes both sides of renames/deletions. It chooses:

| Changed scope | Checks before merge |
| --- | --- |
| `site/` | VitePress documentation build |
| `docs/`, root `README.md` and `AGENTS.md` | Scope/policy tests |
| `web/`, `internal/server/webdist/` | Go build/test/lint, frontend checks and browser smoke |
| Backend packages and templates | Go build/test/lint, DB tests, API E2E and browser smoke |
| `internal/store/` | Backend checks plus sqlc generation drift |
| `proto/` | Backend checks plus proto lint/generation drift |
| `deploy/` | Go build/test/lint and installer contracts |
| Shared config, CI/bootstrap scripts or unrecognized paths | Full suite |

Mixed changes take the union. Shared Go/config changes conservatively run all
checks. Embedded SPA assets do not select backend API E2E merely because they
live under `internal/server/`. All pushes to `main` run the full suite, including
documentation and both E2E suites. Release signing keeps its separate protected
workflow and environment.

`CI gate` depends on scope discovery and every selectable job, including the
reusable E2E workflows. Selected jobs must succeed; failures, cancellations,
missing results and unexpectedly skipped jobs fail the gate. Only jobs that
scope discovery explicitly did not select may be skipped. Discovery failure
also fails the gate. Standalone manual E2E runs are diagnostic and do not replace
the PR gate.

The main-branch ruleset requires `CI gate` from GitHub Actions. When migrating
from the individual required checks, first obtain a successful full PR run with
the new gate, then replace only the required-status-check list. Preserve the
other main-branch rules, bypass policy and release-tag protections.

## Verification

Run `node --test .github/scripts/*.test.mjs` for CI selection, gate and checkout
safety. Run `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` for workflow
syntax and dependency checks. Application changes still require the applicable
Go, frontend, DB and real-environment checks from `AGENTS.md` and the local
operator notes. SQL migrations remain forward-only; SPA/sqlc/proto generated
outputs stay committed and checked for drift.
