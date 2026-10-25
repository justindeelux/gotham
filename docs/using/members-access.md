---
title: Members and access
---

# Members and access

Gotham has two account roles: **owner** and **member**. The account created
during the install is the owner. Everyone else is a member and sees only the
projects the owner grants them.

## What the owner can do

The owner is the only account that can:

- invite people, remove a member, reset a member's sessions and two-factor
  authentication, and change which projects a member can reach;
- add and edit servers and SSH keys;
- connect Git providers and GitHub Apps;
- change instance settings and network settings, and manage the global Domain &
  SSL surface (proxy routes, redirects, DNS providers and panel certificates);
- check for and apply updates, and roll out agent updates;
- open the host terminal and file manager, and install host tools from the App
  store.

These routes require the owner account. An owner API token also needs the
`admin` scope.

## What a member can do

A member sees the projects the owner granted and works inside them: create and
edit environments, applications, services and databases; deploy, roll back,
start and stop them; manage variables, application domains, previews and
notification channels for those applications and databases; and move or delete
what is in the project.

Members do not choose a node: a resource they create is placed on a ready node
by the control plane. A project that is not granted is invisible — lists leave
it out and a direct request answers `404`, the same as a missing resource.
Access is checked on every request, so a grant change takes effect immediately.

Projects are created and deleted by the owner. A member with a grant can rename
that project and manage its contents.

## Manage members

Open **Users**. The list shows every account with its role, project access and
join date. For a member, the owner can:

- **Project access** — tick the projects the member may reach.
- **Reset** — end every session and clear two-factor authentication, so the
  person must sign in and enrol again.
- **Remove** — delete the account. Sessions and API tokens end immediately;
  resources the member created stay and move to the owner.

The owner account cannot be removed or reset from this page. Use the `gotham
admin reset-password` command on the control-plane host instead.

Exactly one account is the owner. The role is not transferred from the console.

## Invite a member

1. On **Users**, choose **Invite user**.
2. Enter the person's email and pick the projects they should reach. Projects
   are optional, but a member without any opens an empty console.
3. Copy the link shown and send it to that address. It is displayed once;
   email delivery is not wired.

The person opens `/register?invite=…` and sets a password using the invited
email. The account is created as a member with the chosen project access.

An invite is single-use, tied to the invited address and expires after seven
days. The owner can revoke a pending invite from the same page. An invite only
creates a new account: an address that already has an account cannot be
invited, so grant projects from **Project access** instead.

## Account settings

Open **Settings → Profile** (`/settings/profile`).

- **Account** shows your email, role and member-since date. Your email address
  cannot be changed here.
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
and revoked from **Settings → API tokens** or through the API. See
[API tokens](/reference/api-tokens). A member's token can never hold the `admin`
scope and stays inside the member's project grants.
