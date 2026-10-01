# P-A3 — CLI: `gotham admin create` / `gotham admin reset-password`

## Requirement
Operator CLI on the server host for (a) creating the single admin account
(P-A2) and (b) resetting a password without email flow.

## Current code (verified)
- `cmd/gotham/main.go` dispatch: `serve | migrate | ca | update | version | help`
  — add `admin` with subcommands.
- Config: `serve` loads config via viper (`internal/config`); the service env
  file `/etc/gotham/gotham.env` provides `GOTHAM_DATABASE_DSN`. The CLI must
  load the same config source as `serve` (reuse the existing loader; do NOT
  invent a new one).
- Password hashing: `internal/auth/password.go HashPassword` (argon2id,
  PHC-encoded). Domain package — CLI may import it (no HTTP import; invariant
  #6 holds).
- Store: `internal/store/queries/users.sql` — needs new queries:
  `CountUsers :one`, `UpdateUserPasswordHash :exec` (by email), plus the
  `CreateUser` that already exists. Run `make sqlc-generate`.

## Commands
```
gotham admin create --email ops@example.com [--password ...]
  - refuses when CountUsers > 0 unless --force (P-A2 invariant: one account)
  - password: prompt (hidden) when omitted; validate with the same
    validatePassword rules as Register
  - prints the created email; exits nonzero on ErrEmailTaken

gotham admin reset-password --email ops@example.com [--password ...]
  - updates the argon2id hash for the existing user
  - unknown email → nonzero exit with clear message (no user enumeration
    concern: operator is root on the host)
  - ALSO revokes the user's sessions (sessions.sql: delete by user id) so the
    old JWT refresh chain dies with the password change
  - password: prompt when omitted
```

## Implementation sketch
- `cmd/gotham/admin.go`: flag parsing (stdlib flag), prompt via
  `golang.org/x/term` — already an x/ dependency? Check go.mod; if absent use
  `bufio` + `stty` fallback… prefer `x/term` (tiny, standard).
- Wire store: open pgx pool the same way `serve` does (reuse
  `internal/config` + store bootstrap helper; extract a small helper if `serve`
  inlines it — keep diff minimal).
- `main.go`: `case "admin":` → `runAdmin(os.Args[2:])`.
- No HTTP surface, no migration.

## Tests
- Unit: admin.go logic against a test store (fake or pgx test container —
  follow existing store test patterns; check how other store tests run).
- E2E smoke on the test box: create → login via API; reset-password → old
  refresh token rejected, new password works.

## Docs
- `docs/install.md`: first-login = `gotham admin create`; lockout = reset.
- `deploy/README.md` sudoers note: run as the `gotham` service user or root?
  Root can read the DSN env file; simplest documented form:
  `sudo /var/lib/gotham/bin/gotham admin create ...` (reads
  /etc/gotham/gotham.env). Verify the config loader actually reads that env
  file when not running under systemd; adjust if not (may need
  `--env-file /etc/gotham/gotham.env`).

## PR
1 PR `feat/cli-admin` (depends on P-A2's CountUsers/ErrRegistrationClosed or
lands together — recommend ONE PR covering P-A2+P-A3 to keep the invariant
atomic). Codex review + full CI.