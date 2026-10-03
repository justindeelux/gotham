# P-A2 — Exactly one admin account; registration only via admin invite

## Requirement (owner-confirmed)
- Exactly ONE admin account on an instance.
- Public self-registration is CLOSED.
- Members get accounts ONLY through an invite link created by the admin
  (owner decision 2026-10-01): admin generates the invite, shares the link,
  the member registers through it.

## Current code (verified)
- `POST /api/v1/auth/register` → `internal/server/auth.go:104` →
  `internal/auth/service.go Register()` → `store.CreateUser` (users.sql has
  GetUserByEmail / GetUserByID / CreateUser only — no Count).
- Teams: `internal/teams` — CreateInvite / GetInviteByTokenHash / AcceptInvite
  exist; AcceptInvite today requires an EXISTING userID (member must already
  have an account) — that is the gap the invite-registration path closes.
- Roles: team owner/admin/read_only; PLATFORM_ADMINS env gates platform ops.
- UI: LoginPage tab "Create account" → RegisterPage; `/register` route is
  `publicOnly`.
- **E2E dependency:** `web/e2e/global-setup.ts` + `internal/e2e` create
  accounts via `POST /register` — they break when registration is closed.

## Backend design
1. `auth.Register(ctx, email, password)` gains an invite-aware variant:
   - `CountUsers == 0` → allowed (fresh-install bootstrap; the CLI admin
     create in P-A3 uses this same path).
   - otherwise → `auth.ErrRegistrationClosed` unless a VALID one-time invite
     token is supplied.
2. Register endpoint accepts an optional `inviteToken` field:
   `POST /api/v1/auth/register {email, password, inviteToken?}`.
   - Validate the token hash via the teams store (GetInviteByTokenHash),
     create the user, then accept the invite for the new userID in the SAME
     transaction path (reuse AcceptInvite; consume the invite).
   - Invalid/expired/unknown token → 403 `registration is closed` (do NOT
     distinguish token errors publicly; no enumeration).
3. Public config endpoint: `GET /api/v1/auth/config` →
   `{ registrationOpen: bool }` (true only when the instance has zero users).
   Rate-limited like the other auth routes.
4. Invite validate endpoint for the UI (public, rate-limited):
   `GET /api/v1/auth/invites/validate?token=…` → `{ team: name, email }` or 404.
   The member sees which team they are joining BEFORE creating credentials.

## UI
- Auth store fetches `/auth/config` once; LoginPage renders the
  "Create account" tab only when `registrationOpen`.
- `/register` stays reachable ONLY as `/register?invite=<token>`:
  route guard calls the validate endpoint; invalid token → redirect `/login`.
  RegisterPage shows the team name from the validation response and posts the
  token with the credentials.
- TeamsPage (admin): invite creation already exists — surface the shareable
  link `/register?invite=<plaintext>` next to the created invite (plaintext
  token is shown once, same as today's API token convention).

## Store
- New sqlc queries: `CountUsers :one` (users.sql), and whatever AcceptInvite
  needs to run for a fresh user (existing queries suffice; verify transaction
  boundary). No schema change → no migration (forward-only rule untouched).

## Tests
- Unit: register on empty store ok; second register without token →
  ErrRegistrationClosed; register with valid invite creates user + membership
  and consumes the invite; with expired/unknown token → 403; reused token →
  403 (one-time).
- HTTP: `/auth/config` shape; invite validate 200/404.
- `web/e2e/global-setup.ts`: create the smoke account via the CP CLI
  (`gotham admin create`, P-A3) instead of POST /register; add a spec for the
  invite-register flow (create invite via API as admin → open
  /register?invite=… → account created → membership visible).
- `internal/e2e`: same CLI bootstrap for accounts.

## Docs
- `docs/install.md`: first login = `sudo …/gotham admin create`; members join
  via admin-created invite links.
- `deploy/README.md`: none (no wrapper change).

## PR
ONE PR together with P-A3 (`feat/single-admin`) so the one-account invariant,
the CLI and the invite path land atomically. Codex review + full CI.