---
name: go-review
description: Review Gotham Go/backend changes, including SQL migrations, sqlc, protobuf, Redis and deployment code, against the repository's implementation skills. Use for Go/backend code reviews and before merging changes in those areas.
---

# Go review (gotham)

Review read-only; do not fix code, push, merge or post comments unless explicitly asked.
Use the versions in `go.mod` and the existing Chi, sqlc/pgx, goose, gRPC/mTLS and Redis
patterns. Read `AGENTS.md`. Project rules and the issue's acceptance criteria take
precedence over generic examples in source skills.

## 1. Load the implementation requirements

Read the core sources on every Go review, then the conditional sources for the touched
paths. Links resolve from this file; paths used by commands are repository-relative.
Review applicable requirements, not just the summaries below. If a required source is
missing, report the coverage gap rather than silently substituting generic advice.

| When | Source | Review focus |
| --- | --- | --- |
| Always | [golang-patterns](../../../.opencode/skills/golang-patterns/SKILL.md) | Errors, cancellation, resource ownership, interfaces, simplicity |
| Always | [golang-testing](../../../.opencode/skills/golang-testing/SKILL.md) | Behavioral regression tests, isolation, race detection, coverage |
| Always | [security-patterns](../../../.opencode/skills/security-patterns/SKILL.md) | Trust boundaries, auth, secrets, validation, dependency security |
| HTTP/API contracts | [api-design](../../../.opencode/skills/api-design/SKILL.md) | Methods/statuses, errors, pagination, authorization, compatibility |
| SQL, migrations, persistence | [postgres-patterns](../../../.opencode/skills/postgres-patterns/SKILL.md) | Query/index fit, types, pooling, populated-schema safety |
| Redis, caches, jobs, realtime | [redis-patterns](../../../.opencode/skills/redis-patterns/SKILL.md) | Atomicity, TTLs, leases, delivery guarantees, cleanup |
| Docker, Compose, installer/release paths | [docker-patterns](../../../.opencode/skills/docker-patterns/SKILL.md) | Image/build order, persistence, privileges, isolated installer proofs |
| PR/history review | [git-workflow](../../../.opencode/skills/git-workflow/SKILL.md) | Stated scope, review evidence, CI and conflicts; no history mutations |

Do not turn example schemas, TTLs, deployment topologies or suggested abstractions into
new product requirements. Gotham serves a small self-hosted installation; new shell/env
setup requirements violate the installer/UI-first product principle.

## 2. Scope the exact revision

- Record the requested range, head SHA and acceptance criteria. For a PR, get
  `baseRefName,headRefOid,mergeStateStatus,statusCheckRollup` with `gh pr view --json`;
  compare its base to that exact head. Never assume the PR targets `main`.
- For local work, inspect staged and unstaged diffs and relevant untracked files.
  Include SQL, proto, generated output, config, dependencies, tests and deployment
  files, not only `*.go`. For branch review, use the requested base/head range.
  Shallow/missing history is a limitation; `git show HEAD` is not a whole-branch review.
- Read changed functions, callers, middleware, queries and tests. Use GitNexus `query`
  for flows, `context`/upstream `impact` for named symbols, and `detect_changes` with
  `scope: compare` and the actual base for branch review (`scope: all` for local work).
  Bind `repo: gotham` and the reviewed worktree. Report HIGH/CRITICAL graph risk;
  resolve empty/UNKNOWN results with text search. Re-run partial/truncated analyses;
  never present them as a clean check.
- Red CI/conflicts prevent merge approval; continue the review where possible so the
  report includes actionable findings. If no backend paths apply, report N/A.

## 3. Verify without touching shared data

Inspect current CI/Makefile before choosing commands. Run applicable gates from the
module root; record each command, revision, result and important failure output:

```bash
go build ./...
go vet ./...
golangci-lint run
# Only after configuring isolated PostgreSQL/Redis and GOTHAM_TEST_DSN:
go test ./...
```

- **Before the first `go test`**, create throwaway PostgreSQL 16/Redis 7 on separate
  ports and explicitly set `GOTHAM_TEST_DSN` and the relevant Redis test configuration.
  Never use shared `gotham-dev-*` services or let helpers fall back to development data.
  Remove only resources created for this review when done. If Docker is down, run
  build/vet/lint and report DB-backed tests as unrun; do not restart shared Docker.
- Run `go test -race` for changed packages and affected concurrent paths. Use focused
  repros for failure/cancellation cases; benchmarks/fuzzing when relevant to the change.
- SQL/schema/sqlc config changes: `make sqlc-check`; proto/config changes:
  `make proto-check`. These regenerate files: run in a disposable copy of the exact
  revision, not the worker's checkout. Do not run formatters or `go mod tidy` as review.
- Dependency changes: `govulncheck ./...` when available; distinguish reachable issues
  from advisory-only results. Cross-platform/release changes need the relevant build
  matrix or environment evidence; a local build alone does not prove Linux behavior.
- Mixed backend/frontend work also needs `vue-review`. Generated SPA assets are
  **not committed** except the placeholder `internal/server/webdist/index.html`;
  release packaging builds the SPA before Go. Do not demand a committed dist.

## 4. Apply the sources to the changed paths

- **Project boundaries:** `agent/` cannot import `internal/`; domain packages cannot
  import `internal/server`; `updatecore/` remains transport-agnostic. Reuse existing
  helpers and dependencies. Check language policy and supported architectures.
- **Errors/state:** preserve error chains (`%w`, `errors.Is/As`) where callers need
  them, useful context, and safe public messages. Check ignored errors, recoverable
  panics, transaction rollback, retries/idempotence and partially applied deploy/update
  operations. Documented best-effort cleanup or an already contextual error returned
  directly is not automatically a defect.
- **Concurrency/resources:** cancellation and shutdown ownership for goroutines,
  streams, Redis subscriptions and Docker/log work; synchronized shared state;
  channel closure/backpressure; no deadlock or lock across avoidable blocking I/O.
  Check outbound timeouts and closure of rows, response bodies, files and timers.
- **HTTP/contracts:** bounded body/upload streams, Go-side validation independent of
  Zod, trusted authentication, resource ownership/roles, appropriate CSRF for cookie
  paths, trusted-proxy rate-limit keys and safe errors. Preserve existing envelopes,
  naming and token transport. Check methods/statuses, bounded pagination and allowed
  filters/sorts; update affected consumers/docs rather than redesigning the API.
- **Security:** trace attacker-controlled values to SQL, commands, paths and network
  requests. Parameterize SQL; distinguish safe argv from shell/option injection.
  Path containment must handle sibling prefixes, symlinks and archive entries;
  `filepath.Clean` or a raw string prefix alone is not proof. Check SSRF protections,
  JWT verification/refresh/revocation, password hashing, mTLS peer identity, secret
  leakage and Ed25519 verification/atomic update recovery against reachable paths.
- **PostgreSQL/generated contracts:** merged migrations are immutable; require a new
  forward migration and check populated-table locks, defaults, backfills, constraints
  and transaction safety. Check sqlc/proto sources against generated output, protobuf
  field-number compatibility, query/index fit, N+1s, deterministic pagination, data
  types and connection pooling. Do not prescribe ID-type/schema rewrites from examples.
- **Redis:** distinguish misses from errors; check invalidation after DB writes,
  bounded key growth/TTLs and shared-client ownership. Multi-command limits need a
  transaction/Lua, not an ordinary pipeline. Leases need unique tokens, atomic
  owner-checked release and expiry/renewal safety. Pub/Sub loses disconnected events;
  durable Streams consumers need success-before-ack, replay idempotence and pending
  recovery when delivery is required. Do not add queues/caches without a requirement.
- **Docker/installers:** persistent volumes, limited exposure, secrets outside image
  layers and explicit privilege boundaries for host/agent work. Test installer changes
  on disposable targets; never wipe the shared box or use release signing keys for
  tests. Linux containers do not prove macOS/Windows behavior.
- **Tests/design:** require a meaningful regression check for non-trivial behavior,
  including invalid input, errors, cancellation and ownership as applicable. Prefer
  table-driven cases, `httptest`, interface fakes, `t.Helper`, `t.Cleanup`, `t.TempDir`
  and independent parallel tests; avoid sleeps/shared state and mocking everything.
  Use source coverage targets (80% general, 90% public APIs, 100% critical logic;
  exclude generated code) to identify relevant gaps, not to demand unrelated tests.
  Do not infer TDD order from a final diff. Small consumer-owned interfaces, useful
  zero values and measured optimization are guidance; line counts or a single
  implementation alone do not establish a blocking defect.

## 5. Output

Report only evidence-backed findings tied to a changed line or affected contract.
Group by severity based on actual impact, not the category heading. For each finding:

```text
[SEVERITY] [in-spec | extra] short title
File: path/to/file.go:42
Requirement: source skill/section or project rule.
Issue: concrete input/state and the reachable failure or regression.
Evidence: caller/flow, repro command and observed result, or precise code reasoning.
Fix: smallest specific correction.
```

End with the exact head/range, loaded sources, severity counts, gate results and
unverified paths. Use one verdict: `MERGE-GO` (no blocking findings, required evidence
green), `FIX-FIRST` (in-spec HIGH/MEDIUM findings), or `BLOCK` (CRITICAL, failed required
gate, conflicts or missing mandatory evidence). Separate unavailable infrastructure
from code failures. Extra-scope suggestions and style preferences do not block merge;
skip nits already covered by tooling. Write a report file only at the requested path.
