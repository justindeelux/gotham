---
title: First login
---

# First login

The installer creates one account for you and prints its address and the panel
URL. This page covers signing in, inviting other people, and what to do if the
account was not created.

## What you need

- The panel URL printed by the installer, usually `http://<server-ip>:8000`.
- The owner email and password from that summary. If you let the installer
  generate the password, it was printed once at the end.
- A terminal on the control-plane server for the `admin` commands below.

## Sign in

Open the panel URL and sign in with the owner email and password. Registration
is closed: exactly one account bootstraps the instance, and everyone else joins
through an invite from the owner.

The installer collects the credentials before anything is installed and creates
the account right after the database migrations, before the service starts.
Nobody can register through the open web form while the installer waits at a
prompt.

- **Interactive install** (a terminal is present, `curl | sh` included): the
  installer asks for the owner email, then for the password with hidden input
  and confirmation. A mismatch or a password outside 8–128 characters asks
  again, up to 3 creation attempts. Leaving the password empty generates a
  strong random one, printed once in the final summary. Store it immediately;
  it is not shown again.
- **Non-interactive install** (no terminal): the installer never prompts. Set
  `GOTHAM_ADMIN_EMAIL` — and optionally `GOTHAM_ADMIN_PASSWORD_FILE`, a path to
  a root-readable file holding the password on one line — to create the
  account. Without a password file a random password is generated and printed
  once. Without an email the installer skips creation and prints the manual
  command below.
- **Set both ways**: on a terminal, `GOTHAM_ADMIN_EMAIL` is the email prompt
  default (empty input keeps it, `-` skips) and `GOTHAM_ADMIN_PASSWORD_FILE` is
  used without prompting. A group/world-readable password file prints a warning
  but is still used.
- **Re-run or upgrade**: if the instance already has an account, the installer
  prints `admin already exists, skipping` and changes nothing. If creation loses
  a race with another registration it says
  `an account already exists (created by someone else?)`.
- **`--dry-run`** prompts nothing and changes nothing.

Self-registration stays closed once that account exists: only the owner's
invites create members. An invite is tied to one email address and to the
projects the new member may open, is valid for seven days and can be used once.
The owner manages members, their project access, and the reset of a member's
sessions and two-factor authentication from the panel.

The password is never passed as a command-line argument, never put in the
environment, and never written to disk, the env file, logs or shell history: it
travels to the binary on stdin only (`sh -x` tracing is disabled around the
secret handling).

## If no account was created

If you skipped the email prompt, or the install was non-interactive without
`GOTHAM_ADMIN_EMAIL`, create the account by hand on the control-plane server:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

The password prompt is hidden. Useful flags:

- `--password-stdin` reads the password from stdin (a single line, for
  scripts). Pipe it in; never use `--password`, which is visible in `ps`.
- `--generate-password` creates a random password and prints it once as
  `generated-password: <value>`.
- `--force` adds an account even though the instance already has an owner. The
  new account is a member, not a second owner.
- `gotham admin exists` prints `admin-exists: true|false` for scripting (that
  is how the installer detects a re-run).

## Invite other people

1. Open **Users**, choose **Invite user**, pick the projects the person may use
   and create the invite, then send the shown link.
2. The member opens `/register?invite=<token>` and chooses their credentials
   with the invited email. The link is shown once, works once and expires after
   seven days.
3. Change a member's project access, or reset their sessions and two-factor
   authentication, from the same page. Removing a member ends their sessions
   and API tokens immediately.

An invite only creates a new account. To give an existing member more projects,
edit **Project access** for that account instead.

Add servers from **Servers**; see [Add nodes](/guide/nodes). Each node reports
heartbeats and appears in the panel once it is ready.

## Lost the owner password

Replace it on the control-plane server:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password --email ops@example.com
```

This revokes every session of the account. Bearer access tokens are checked
against their live session on every request, so a token minted before the reset
stops working immediately; resetting is an instant logout on every device.

## The first account is the owner

On a fresh instance the account created while the users table is empty (first
registration, first OAuth sign-in, or `gotham admin create`) becomes the owner.
Exactly one account holds that role.

The owner is the only account that can manage people and invites, change project
access, add servers and keys, connect Git providers, change instance and network
settings, manage the global Domain & SSL surface, apply updates, and use the host
terminal and file manager.

Upgraded instances follow the same rule: the earliest existing account becomes
the owner and every other account becomes a member. Members start with no
project grants, so the owner must grant each project they need from **Users →
Project access**.

**Takeover risk.** On a fresh instance registration is open until the first
account exists, so whoever registers first becomes the owner. On a public,
unconfigured instance, create that account before exposing the control plane.
Emptying the users table by hand re-opens first-run registration, and the next
registrant becomes the owner.

## Next

- [Domain and HTTPS](/guide/domain-and-https) for a real domain and a
  certificate.
- [Add nodes](/guide/nodes) to grow beyond the local server.
- [Updates and rollback](/guide/updates) to keep the installation current.
