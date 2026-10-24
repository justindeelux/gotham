---
title: First login
---

# First login

The installer creates one account for you and prints its address and the panel
URL. This page covers signing in, inviting other people, and what to do if the
account was not created.

## What you need

- The panel URL printed by the installer, usually `http://<server-ip>:8000`.
- The admin email and password from that summary. If you let the installer
  generate the password, it was printed once at the end.
- A terminal on the control-plane server for the `admin` commands below.

## Sign in

Open the panel URL and sign in with the admin email and password. Registration
is closed: exactly one account bootstraps the instance, and everyone else joins
through an invite from an admin.

The installer collects the credentials before anything is installed and creates
the account right after the database migrations, before the service starts.
Nobody can register through the open web form while the installer waits at a
prompt.

- **Interactive install** (a terminal is present, `curl | sh` included): the
  installer asks for the admin email, then for the password with hidden input
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
- `--force` adds a second account deliberately. Without it the CLI refuses once
  an account exists.
- `gotham admin exists` prints `admin-exists: true|false` for scripting (that
  is how the installer detects a re-run).

## Invite other people

1. Open **Teams** and create an invite, then send the shown link.
2. The member opens `/register?invite=<token>` and chooses their credentials.
   The link is shown once and expires.
3. Add servers from **Servers**; see [Add nodes](/guide/nodes). Each node
   reports heartbeats and appears in the panel once it is ready.

## Lost the admin password

Replace it on the control-plane server:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password --email ops@example.com
```

This revokes the account's refresh sessions. Bearer access tokens are stateless
JWTs, so a token already minted stays valid until it expires (15 minutes);
resetting is not an instant, fleet-wide logout.

## The first account is the platform administrator

On a fresh instance the account created while the users table is empty (first
registration, first OAuth sign-in, or `gotham admin create` without `--force`)
gets the platform-admin flag, and its sessions carry the `admin` role claim.
Platform-global operations (node-wide proxy sync, DNS providers) then work
without further configuration.

Upgraded instances are unchanged: the migration leaves every existing row
non-admin, so keep listing the operator account email in `PLATFORM_ADMINS` in
`/etc/gotham/gotham.env` (or mint an admin-scoped API token).

**Takeover risk.** On a fresh instance registration is open until the first
account exists, so whoever registers first becomes the admin. On a public,
unconfigured instance, create that account (or set `PLATFORM_ADMINS`) before
exposing the control plane. Emptying the users table by hand re-opens first-run
registration, and the next registrant becomes platform admin again.

An admin-scoped API token minted by an admin session outlives a later demotion
of that account: no session or token revoke is tied to role changes, so rotate
the token if the grant must end.

## Next

- [Domain and HTTPS](/guide/domain-and-https) for a real domain and a
  certificate.
- [Add nodes](/guide/nodes) to grow beyond the local server.
- [Updates and rollback](/guide/updates) to keep the installation current.
