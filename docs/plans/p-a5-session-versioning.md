# P-A5 — Credential versioning: make a password reset end every live chain

## Why (origin)
Raised by the Codex review of PR #88 (P1, carried): `admin reset-password`
deletes the account's refresh sessions and replaces the hash in one
transaction, but **session issuance is not serialized with it**. Two windows
remain:

1. A refresh that revokes its old session just before the reset's delete can
   issue a replacement session after it (the delete then matches no live row),
   so the old credential chain survives the reset.
2. A login that verified the old password just before the hash update commits
   can still issue a session afterwards.

`RevokeSessionIfLive` (PR #88) closed the *other* ordering (reset first, then
rotation); it cannot close these.

PR #88 already narrows the honest claim: the CLI reports that **refresh
sessions were revoked**, and documents that stateless access tokens stay valid
until they expire (15 minutes). This plan closes the remaining refresh/login
windows.

## Design
Add a per-account **credential version** and bind every issued token to it.

- Migration (forward-only, new file): `ALTER TABLE users ADD COLUMN
  credential_version integer NOT NULL DEFAULT 1;`
- `admin reset-password` (and any future password change) bumps
  `credential_version` inside the same transaction as the hash update and the
  session delete.
- The issued refresh session stores the version it was minted with
  (`sessions.credential_version`, same migration). A refresh is refused when
  the session's version is older than the account's current version.
- Access tokens: either embed the version as a JWT claim and reject on
  mismatch in `RequireAuth`, or keep the documented 15-minute window. Decide in
  this plan's PR — embedding it removes the last window at the cost of one
  claim read per request (the account row is not otherwise fetched per request).

### Alternative considered
A transaction-scoped advisory lock shared by `issue()` and the reset would
serialize the writers, but `issue()` has no transaction of its own and the
lock would have to be held across the session insert. The version column is
simpler and also covers "log in with the old password while the reset commits".

## Changes
- `internal/store/migrations/000NN_credential_version.sql` (new).
- `internal/store/queries/users.sql`: return `credential_version`; add
  `BumpCredentialVersion`.
- `internal/store/queries/sessions.sql`: store `credential_version` on create;
  return it on read.
- `internal/store/store.go`: `ResetUserPassword` bumps the version in the same
  transaction; `CreateFirstUser`/`CreateUser` set version 1.
- `internal/auth/service.go`: `issue()` records the account's version; `Refresh`
  refuses a stale version; `Login` re-reads the version after verifying the
  password and refuses if it changed mid-flight.
- `internal/server`: if the JWT-claim option is chosen, `RequireAuth` compares
  the claim with the account version (adds one read per request — measure; the
  in-memory cache already exists if needed).
- Tests: refresh with a pre-reset session → `ErrUnauthorized`; concurrent
  reset+refresh (store-level, both orderings); login racer refused; migration
  upgrade keeps existing sessions valid (default version 1 matches).

## Rollout / compatibility
- Existing sessions have no version: backfill them to the account's current
  version in the migration, so nobody is logged out by the upgrade.
- `gotham admin reset-password` output stays accurate as written in PR #88
  (sessions revoked; access tokens expire in ≤15m) unless the JWT option lands,
  in which case drop the caveat.

## Acceptance
- A password reset ends every refresh chain, in every interleaving.
- No account is locked out by the migration.
- The CLI's stated guarantee matches the implemented one.
