---
name: go-review
description: Review Go changes in the gotham repo (control plane in internal/, node agent in agent/, updatecore/, cmd/, proto/) for correctness, security, concurrency, error handling and project invariants. Use whenever the user asks to review, check, audit or "look over" Go code, a .go diff, a PR or branch touching Go, sqlc/goose/gRPC/Chi code — and proactively before merging any change that modifies .go files, even if they don't say "review".
---

# Go review (gotham)

Report findings only; do not rewrite code. Stack: Go 1.27+, Chi, sqlc + pgx, PostgreSQL 16, goose (forward-only), gRPC + protobuf with mTLS, Redis pub/sub, single binary for linux/amd64 + arm64. Project rules live in `AGENTS.md`.

## 1. Scope the diff

- PR: base from `gh pr view --json baseRefName,mergeStateStatus,statusCheckRollup`. If checks are red or there are conflicts, stop and report. Never hard-code `main`.
- Local: `git diff --staged -- '*.go'`, then `git diff -- '*.go'`; shallow history → `git show --patch HEAD -- '*.go'`.
- Read the surrounding function and callers before commenting; most false positives come from reviewing a hunk in isolation. Use GitNexus (`impact`, `context`) for callers of changed symbols rather than grep, and treat `risk: UNKNOWN` as unresolved.

## 2. Run the gates (what CI runs)

```bash
go build ./... && go vet ./... && golangci-lint run
go test -race ./...            # at least the changed packages
govulncheck ./...              # if dependencies changed
make sqlc-check proto-check    # if queries/*.sql or proto/ changed
```
Report any failure verbatim; a red gate is a BLOCK on its own.

## 3. Project invariants (check first — these are the ones that actually bite)

- **Import boundaries**: `agent/` must not import `internal/`; domain packages must not import the HTTP server (`internal/server`). `updatecore/` stays transport-agnostic.
- **Migrations are forward-only**: a diff that edits an already-merged goose migration is CRITICAL. Schema change = new migration. Check it is safe on a populated table (locks, NOT NULL without default, backfill).
- **Generated code**: sqlc/protobuf output must match its source (`.sql`, `.proto`); hand-edits to generated files are CRITICAL. A changed query without regenerated code, or a breaking proto change (renumbered/removed field), is HIGH.
- **Embedded SPA**: `internal/server/webdist` is committed; if `web/` changed, the dist must be rebuilt (CI drift check).
- **Lean single binary**: a new dependency needs justification; prefer stdlib or what `go.mod` already has.
- **Language policy**: one language for code, comments, errors, commit text.

## 4. Review priorities

**CRITICAL — security**
- SQL built by string concatenation (pgx/database/sql); only sqlc or parameterized queries.
- Command injection in `os/exec` (deploy/build/container paths are the hot spot) — user input reaching args or a shell.
- Path traversal in files/builds/templates: user paths need `filepath.Clean` + prefix check against the root; watch symlinks and `..` in archive extraction.
- mTLS/agent trust: `InsecureSkipVerify`, skipped peer-identity checks, gRPC handlers lacking authn/authz.
- Auth: JWT validation gaps (alg, expiry, audience), argon2id parameters weakened, secrets/tokens in logs or error messages, missing ownership/team checks on resource lookups (IDOR across teams/projects).
- Self-update: any weakening of Ed25519 manifest verification or the atomic swap.
- Hardcoded secrets; `unsafe` without justification.

**CRITICAL — errors**
- Discarded errors (`_ =`), `return err` without `fmt.Errorf("…: %w", err)` context, `==` on errors instead of `errors.Is/As`, `panic` on recoverable paths, partially applied state after an error (deploy/update steps need rollback or idempotence).

**HIGH — concurrency & resources**
- Goroutines without cancellation (`context.Context`) or a `WaitGroup`/`errgroup`; unbuffered sends with no receiver; shared maps/slices without a lock; lock held across I/O; missing `defer mu.Unlock()`.
- Long-running work (WebSocket, Redis subscribers, gRPC streams, build/log tailing) that ignores `ctx.Done()` or leaks on client disconnect.
- Missing timeouts on outbound calls (HTTP, gRPC, DB, docker); `defer` inside loops; unclosed `rows`, bodies, files.
- HTTP: handler writes after error without return, no request-body size limit, missing input validation at the boundary.

**HIGH — design**
- Mutable package-level state; interfaces defined at the implementer instead of the consumer, or with no second implementation; functions >50 lines / nesting >4; business logic in Chi handlers instead of the domain package.

**MEDIUM**
- N+1 queries, missing pre-allocation in hot paths, `strings.Builder` in loops, `ctx` not first param, uppercase/punctuated error strings, new behavior without table-driven tests (a bug fix without a regression test is MEDIUM at minimum), flaky time/sleep-based tests.

## 5. Output

Group by severity. Each finding:

```
[SEVERITY] short title
File: path/to/file.go:42
Issue: one sentence.
Why: concrete impact (which input/state breaks what).
Fix: specific change.
```
Only report what you can tie to a line and a failure scenario; skip style nits that `gofmt`/golangci-lint already enforce. End with:

```
## Review Summary
| Severity | Count | Status |
| CRITICAL | n | pass/block |
| HIGH     | n | pass/block |
| MEDIUM   | n | info |
Verdict: APPROVE | WARNING (MEDIUM only) | BLOCK (any CRITICAL/HIGH or red gate)
```
