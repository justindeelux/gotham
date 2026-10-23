# Phase 12 — User profile management

Status: implemented and deployed to the test box 2026-10-04. Written from a read of the current code.
Decisions are in section 6; the delivery log is in section 8.

## 1. Where we are

- `users` has `id, email, created_at, password_hash (nullable), avatar (nullable), updated_at,
  credential_version, is_platform_admin`. No display name.
- Auth API: `register, login, refresh, logout, GET /auth/me`. Nothing lets a signed-in user
  change anything about their own account. A password can only be changed by the operator CLI
  (`gotham admin reset-password`).
- `sessions` rows hold only a refresh hash and expiry (no device, IP, last use), so a user cannot
  see or end their own sessions. `credential_version` already lets us invalidate every session
  of one account (P-A5).
- Web: the sidebar `MeCard` dropdown has only "Sign out". The `User` type is
  `{ id, email, avatar?, role?, created_at }`. The sidebar "API tokens" entry is a stub.
- GitHub OAuth finds or creates the account **by email** (`oauth.go`, `GetUserByEmail`). Team
  invites are also matched by email. There is no system mailer: SMTP exists only for
  notification channels, and invite links are copied by hand (BE-8.3 is open).

## 2. Scope

### In v1
1. **Profile page** `/settings/profile` ("Account"), reached from a new "Profile" item in the
   `MeCard` dropdown. Read-only card: email, platform role, per-team roles, member since.
2. **Display name** (new, optional, 1-64 chars). Shown in `MeCard` and the topbar avatar
   tooltip, falling back to the email.
3. **Change password.** Current + new password; accounts without a password (OAuth-created)
   set one without a current password. Ends every other session.
4. **Active sessions.** List (device, IP, created, last used, "this device"), end one, end all
   others.
5. **Avatar:** keep today's behaviour (GitHub avatar when present, initials otherwise). The
   page shows it and says where it comes from. No upload.

### Deliberately out of v1 (and why)
- **Change email.** With OAuth matching by email, an unverified new address would let any
  GitHub user who owns that address sign in to the account. Safe change needs email
  verification, which needs a system mailer we do not have. Until then email is read-only; the
  operator path is a new `gotham admin change-email`. Revisit together with BE-8.3.
- **Delete account.** Blocked by team ownership, platform-admin and first-admin invariants.
  Needs its own design (transfer ownership, last-owner rule).
- **Avatar upload, 2FA, preferences stored on the server.** No demand yet. Theme and similar
  stay in `localStorage`.

## 3. Backend design

### Migration `00032_profile_sessions.sql` (forward-only in prod, with a Down)
- `users.display_name text NULL CHECK (char_length(display_name) BETWEEN 1 AND 64)`.
- `sessions.user_agent text NULL`, `sessions.ip inet NULL`, `sessions.last_used_at timestamptz
  NOT NULL DEFAULT now()`. Existing rows backfill to `created_at`; nothing is locked out.
- Index `sessions (user_id, revoked_at)` only if the list query needs it (measure first).

### Routes (all under `/api/v1/auth`, protected group)
| Route | Body | Result |
|---|---|---|
| `PATCH /me` | `{display_name}` (null clears) | updated `user` |
| `POST /me/password` | `{current_password?, new_password}` | new token pair (the caller keeps working) |
| `GET /me/sessions` | | list, with `current: true` on the caller's |
| `DELETE /me/sessions/{id}` | | 204; ending the current one signs out |
| `POST /me/sessions/revoke-others` | | 204 |

`GET /me` also returns `display_name`, `has_password` and `is_platform_admin`.

### Rules that matter
- **JWT sessions only.** A scoped API token (even `admin`) must never change a password or
  list sessions: reject with 403 in the handler, tested.
- **Re-authentication.** Password change verifies the current password with the existing
  constant-time path (`spendPasswordCheck` on failures) and uses the credential rate limiter.
  Same length rules as register (8-128).
- **Revocation.** Password change bumps `credential_version` in one transaction with the hash
  update and revokes the refresh sessions, then issues a fresh pair for the caller. Access
  tokens already issued to other devices stay valid until they expire (15 min), the same
  documented window as `reset-password`; do not add a per-request DB read for this.
- **Current session id.** The list must mark the caller's session. Preferred: put a `sid` claim
  in the access token at `issue()`. Alternative: send the refresh token in the request body.
  Decide in package PF-2 after reading `jwt.go`.
- **Capture** `user_agent` (truncated to 256) and the client IP (reuse `internal/clientip`) when
  a session is created or rotated; update `last_used_at` on rotation only (no write per
  request).
- **OAuth-only accounts:** `password_hash IS NULL`; setting a password does not require a
  current one but does require a live JWT session, never an API token.
- Log events with `slog` (user id, event, no secrets). No new audit table in v1.

### Go tests
Service: wrong current password, weak password, OAuth-only set, other sessions revoked and the
caller's new pair works, concurrent change vs login, display-name bounds and trimming, clearing
the name. Handler: API token rejected on every new route, unauthenticated 401, rate limit,
cannot end another user's session (404, not 403, to avoid probing). Migration up/down on the
scratch DB helper.

## 4. Web design

Feature `web/src/features/profile/{api,components,composables,schemas,pages}`, route
`settings/profile` (lazy), nav alias so `/settings` still lands on notifications.

- `ProfilePage.vue` only composes panels: `ProfileIdentityCard` (read-only facts + avatar),
  `DisplayNameForm`, `ChangePasswordForm`, `SessionsPanel`. Each panel and its composable stay
  well under 300 lines.
- Validation with zod schemas through `ruleFrom` (required marks via the `required` option),
  `parseWith` for the new API envelopes. Messages in one file, tested like the others.
- Forms follow the form rules: short fields share a `.form-row`, no reserved blank feedback row,
  hint hidden while an error shows. Reuse `PasswordStrengthMeter` through the auth feature's
  `components/` path (allowed by the boundary rule).
- **State per instance, never module scope** (lesson from #151/#155): password fields and the
  session list live in per-mount composables; a remount test proves no typed password survives
  navigation.
- After a successful PATCH or password change, update the auth store user/tokens through the
  existing `setSession` so the sidebar changes without a reload; `resetUserStores` on sign-out
  already covers the rest.
- `MeCard` dropdown: "Profile", then "Sign out". Name display falls back to the email.
- Ending the current session or "sign out everywhere" ends in the existing forced-logout path.
- Copy in English. Destructive actions (end session, end all others) use a confirm dialog.

### Web tests
Unit: schemas, the display-name helper. Mount: each form (errors, required marks, submit
payload, busy state, remount clears), sessions panel (current badge, revoke refreshes the
list), `MeCard` item. Playwright smoke: sign in, open Profile, rename, change password, old
password rejected, new works.

## 5. Delivery

Packages, one worktree each, independent review after each (reviewer variant/effort comes from
the owner at session start, per `docs/ORCHESTRATION.md`):

1. **PF-1 backend profile + password** — migration (`display_name`), `PATCH /me`,
   `POST /me/password`, `/me` fields, tests. Impact check on `Service.Me`, `issue`,
   `RequireAuth` first.
2. **PF-2 backend sessions** — migration (`sessions` columns), capture at issue/rotate,
   list/revoke routes, current-session marking, tests. Depends on PF-1's migration number.
3. **PF-3 web profile page** — route, identity card, display name, change password, `MeCard`
   item. Depends on PF-1.
4. **PF-4 web sessions panel** — depends on PF-2 and PF-3.
5. **PF-5 docs + live check** — `docs/test-server.md`, API docs, Playwright MCP check at 900 and
   1280px on the box, Linear closure.

PF-1 and PF-3 can overlap once the PF-1 contract is fixed. Every package ends green on
`go test`, `npm run build/type-check/test`, rebuilt `webdist`, CI, and the deployed check.

## 6. Decisions (owner, 2026-10-04)

1. Email is read-only in v1; change via a later operator CLI `gotham admin change-email`.
2. The active sessions list is in v1 (so both migrations ship).
3. Display name is free text, 1-64 chars, not unique.
4. No delete-account feature.
5. Platform admins do not see other users' profiles or sessions.

## 7. Risks

- Email-matched OAuth makes any future email-change feature a takeover risk; keep it closed
  until verification exists.
- `sid` in the JWT changes the token shape; old tokens lack it, so the list must treat a missing
  `sid` as "unknown current session" without failing.
- Writing `last_used_at` on every refresh adds write load; rotation-only keeps it bounded.

## 8. Delivery log (2026-10-04)

| Package | Linear | PRs | Notes |
|---|---|---|---|
| PF-1 backend profile + password | JUS-25 | #168 | display name, change password, API-token guard as group middleware |
| PF-2 backend sessions | JUS-26 | #171 | migration `00033`, `sid` claim, revoke deletes rows, guarded revoke-others |
| PF-3 web profile page | JUS-27 | #167, #169, #172 | page, forms, layout and a11y fixes, account refresh on open |
| PF-4 web sessions panel | JUS-28 | #170 | per-mount state, sequence-guarded list writes |
| PF-5 docs and live check | JUS-29 | this PR | |

Findings that independent review and the live box caught before they shipped: the committed
webdist was incomplete (PF-3); every form submitted twice (PF-3); revoking a session left a row
whose replay revoked every session of the user, a denial-of-service on the owner (PF-2);
revoke-others trusting a stale session id could end the caller's own live session (PF-2); a
regression that dropped the boot-time account fetch (PF-6); the identity table collapsing to one
character per line in narrow containers (PF-3b); a stale stored account showing "Member" for an
operator.

### Follow-ups (not scheduled)
- Email change, once a system mailer and verification exist (see section 2).
- A per-device chain id on `sessions`, so revoking a device can also purge its rotated
  ancestors and theft detection stays scoped to that device. Today rotated rows keep the
  account-wide family revoke alive for 30 days.
- A session `id` changes on every refresh rotation, so a DELETE with an id from a list fetched
  before the device rotated can answer 404; the web already re-lists on 404.
- A same-account cross-tab display-name save during an in-flight `/me` read can be overwritten
  by the older read until the next mount (accepted, narrow).
- Setting a first password on an OAuth-only account needs only a live JWT session, so a stolen
  short-lived access token can convert into a persistent credential; consider requiring a
  fresh GitHub re-authentication.
