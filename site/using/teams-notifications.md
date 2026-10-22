---
title: Teams and notifications
---

# Teams and notifications

Every resource belongs to exactly one team, and your role in that team decides
what you can change. Teams also own notification channels. Account settings —
display name, password, two-factor authentication and sessions — live under
**Settings**.

## Roles

There are three roles: `owner`, `admin` and `read_only`. There is no `member`
role.

| Role | Can do |
|---|---|
| `owner` | Everything, including changing owners and deleting the team. |
| `admin` | Manage resources and members, except owners. |
| `read_only` | Read resources and members, and never mutate. |

The role applies inside one team only. The operator surfaces — Domains & SSL,
Updates and Instance settings — need the instance-level platform-admin flag,
which is separate from a team role.

Team roles are enforced by the API, not only by the console. A `read_only`
member can read the team's resources and members, but every write — creating or
editing a resource, changing a member, managing an invite, sending a test,
deleting a channel — is rejected server-side. Some controls stay visible and
report that your role does not allow the action; do not read a visible button
as permission.

Select the active team on **Teams** (click its name); the Notifications page has
the same selector. Lists, notifications and resources are scoped to that team.

## Managing teams

- **Create** a team from **Teams**. You become its owner; a new team starts
  empty.
- **Rename** a team from the team list (owner or admin).
- **Delete** a team from the team list (owner only). A team that still owns
  resources or nodes is refused, so move or delete those first. A personal team
  can never be deleted: it is the account's default scope.

## Members

The **Members** tab lists each member's email, role and join date. An
owner or admin can change a member's role or remove them, with these
protections:

- A team always keeps at least one owner. Demoting or removing the last owner
  is refused.
- Only an owner may grant the owner role, or change or remove another owner.
  An admin cannot touch an owner.
- The personal team's owner membership is immutable: it cannot be demoted or
  removed.

A role change is immediate; resources the member can reach follow the new role
on their next request. The console may still render a control your role cannot
use — the API answers with the refusal.

## Invites

Owners and admins create invites from the **Invites** tab. An invite names an
email address and a role, and can grant only `admin` or `read_only`; ownership
is transferred from the members table, never invited.

Invites are **single-use** and bound to the invited email address. They expire
seven days after creation. The token is stored only as a hash, so the link is
shown exactly once, when the invite is created; email delivery is not wired, so
copy the link and send it to the invitee yourself. A pending invite can be
revoked before it is accepted.

The dialog offers two links:

- **New member link** — `/register?invite=…`. Registration is closed once the
  instance has an account, so this is how a new person gets an account.
- **Accept link** — `/invite/accept?token=…`. For someone who already has an
  account.

Either link accepts the invite. The register link creates the account and
consumes the token in one step; the accept link is for someone who already has
an account. The token is posted with the session they are signed in with, so
they must sign in as the invited address; the account email and the invite
email must match. A used, expired, revoked or wrong-address invite never grants
a membership, and after acceptance the token is removed from the address bar so
a reload cannot replay it.

A `read_only` member does not see the invites list: listing and managing
invites is an owner/admin action.

## Notification channels

Open **Settings → Notifications** (`/settings/notifications`). Channels belong
to the active team, and the page follows the team picker.

| Kind | Configuration |
|---|---|
| Discord | Incoming webhook URL. |
| Slack | Incoming webhook URL. |
| Telegram | Bot token and the chat ID the bot posts to. |
| Email (SMTP) | SMTP host, port, optional username and password, from address and at least one recipient. |

Transport secrets — webhook URLs, bot tokens, SMTP passwords — are sealed
server-side before they are stored, are never logged and are never returned by
the API. Reads show a masked value with at most the last four characters, and
a saved connection shows whether a secret is configured. Editing a channel and
leaving the masked value in place keeps the stored secret; the channel kind
cannot be changed after creation.

### Scope and events

A channel is either **team-wide** or scoped to one resource:

- A team-wide channel receives every selected event for any resource in the
  team.
- An application-scoped channel receives only that application's deploy events.
- A database-scoped channel receives only that database's backup events.

The subscribable events are `deploy_success`, `deploy_failure`,
`backup_success` and `backup_failure`. A new channel subscribes to all of them
unless you select a subset. A disabled channel receives nothing.

### Test send and delivery history

**Send test** delivers a synthetic test message to that one channel and reports
the result immediately. It is recorded in the history like a real attempt.

**Delivery history** shows the channel's newest 20 attempts, newest first: the
event (or **Test message**), delivered or failed, the time, and a bounded error
summary for failures. A channel keeps its last 50 attempts; older rows are
trimmed automatically. Delivery never blocks a deploy or backup — it runs on a
bounded background queue, and a failing channel does not stop the others.

Creating, editing, deleting, enabling and testing a channel require owner or
admin. A `read_only` member can read the channels and their history, but the
API rejects every mutation, including the test send.

## Account settings

Open **Settings → Profile** (`/settings/profile`).

- **Account** shows your email, platform role and member-since date. Your email
  address cannot be changed here.
- **Display name** replaces your email in the sidebar. It is 1–64 characters;
  clear it to fall back to the email. An avatar comes from GitHub when you sign
  in with GitHub, otherwise your initials are shown.
- **Change password** asks for the current password (when the account has one)
  and a new password: at least 10 characters with two character classes.
  Changing it signs out every other device and keeps this session signed in.
- **Two-factor authentication** uses a time-based code from an authenticator
  app. Setup shows a QR code and a manual setup key, then asks for the current
  password and a 6-digit code. Enabling returns ten recovery codes, shown only
  once; each works once if you lose the authenticator. Enabling, turning 2FA
  off or regenerating the recovery codes all sign out your other devices, and
  the last two also require the password and a current code. Signing in with a
  recovery code ends every other session.
- **Active sessions** lists each signed-in device with its IP, creation time and
  last activity, and marks this device. **Sign out** ends one session; **Sign
  out all other devices** ends every session except the current one. Ending
  this device returns you to the sign-in page.

Session management needs a signed-in browser session; a scoped API token cannot
list or revoke sessions.

## API tokens

API tokens (`gt1_` with read, deploy or admin scope) can be created, rotated
and revoked through the API, but there is no console page for them yet. See
[API tokens](/reference/api-tokens).
