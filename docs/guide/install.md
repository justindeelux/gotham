---
title: Install
---

# Install Gotham

Gotham runs on your own Linux server. The installer sets up everything on one
host: the web panel, the database, and the first node agent. You can add more
servers later.

There are two ways to install. Both run the same installer and finish with a
working panel; pick the one that fits you.

## What you need

- An Ubuntu 22.04 or Debian server (64-bit Intel/AMD or ARM) where you can run
  commands as root, for example over SSH with `sudo`.
- `curl` and `openssl` 3. Both are already installed on a standard image.
- At least 1 GB of RAM and 1 CPU.
- Outbound HTTPS to `github.com`, where the signed releases are downloaded.
- Optional: a domain name for the panel. If you use one, ports 80 and 443 must
  be free on this server.

The installer adds PostgreSQL, Redis and Docker for you. It does not need a
repository checkout.

## Automatic install (recommended)

Run this one command on the server. It downloads the installer and starts it
with root rights:

<<< @/snippets/install-command.sh#plain{sh}

The installer asks these questions, in order. Press Enter to accept the
recommendation:

| Prompt | What to answer |
|---|---|
| `Admin email [admin@example.com] (empty keeps it, - to skip)` | The address of the first account. Press Enter to use `admin@example.com`, type your own, or type `-` to create no account now. |
| `Admin password (empty to generate)` | The password for that account, 8 to 128 characters. Type nothing and press Enter to have a strong password generated and printed once at the end. |
| `Confirm password` | The same password again. A mismatch or a password outside the length limits asks again. |
| `Panel domain (optional, Enter skips)` | A domain such as `panel.example.com`, if it already points at this server. Press Enter to skip and use the server address for now. |
| `Enable the file manager on this node? [y/N]` | **No**, unless you want platform operators to browse and edit every file on this host. Yes runs the agent as root without systemd sandboxing (see [Remote file service](/guide/file-service)). You can change the answer later by re-running `deploy/install-agent.sh`. |

The file-manager question appears while the host agent is installed, after the
first four. `--no-file-manager` answers it without a prompt, and a run without
a terminal skips it (the answer is No).

While it runs, the installer:

1. picks the newest release for your CPU, verifies its Ed25519 signature, and
   checks the artifact's SHA-256 against the signed manifest (which is bound to
   the version, architecture and file name);
2. installs PostgreSQL 16 and Redis 6.2 or newer (7.2 from the official Redis
   repository when your distribution is older) — the local database password
   is generated at install time — unless you point it at managed services;
   [Database versions and upgrades](/guide/upgrade-databases) explains what is
   installed and how to upgrade it by hand;
3. installs the control plane (binary in `/var/lib/gotham/bin`, updater
   `/usr/libexec/gotham/gotham-update`, unit
   `/etc/systemd/system/gotham.service`), then Docker and the node agent on the
   same host (`install-agent.sh --full --ca`, pinned to the same release tag);
4. applies the database migrations and creates the admin account before the
   panel accepts connections, so nobody can register first;
5. prints the panel URL, the login, and a generated password if you asked for
   one.

The last lines look like this:

```text
Gotham v1.2.3 is installed.

  Web UI:   http://203.0.113.10:8000
  Status:   systemctl status gotham
  Logs:     journalctl -u gotham -f

First login:
  Account:  ops@example.com
  Password: <printed once if you left the password empty>
  Login:    http://203.0.113.10:8000
```

Open that URL and sign in. The local server is already listed under
**Servers** as `localhost`. Continue with [First login](/guide/first-login).

The agent for this host is part of the same run and is not optional. If only
that last step fails, nothing is rolled back: the panel stays installed and
running, and the installer exits nonzero with the exact command to retry it.

If the install stops, [Troubleshooting](/guide/troubleshooting) covers the
common causes.

### Set the domain in the same run

Add `-s -- --domain panel.example.com` after `sh` in the command above. The
domain must already point at this server. The installer waits up to 60
seconds for the local agent, sets the panel URL to `https://panel.example.com`,
and prints the HTTP address to open for confirmation. HTTPS is issued after you
confirm it in **General settings** within 2 minutes. If the domain does not
resolve yet, the install continues on the server address and you can set it
later in the panel. See [Domain and HTTPS](/guide/domain-and-https).

### Scripted install (cloud-init, CI)

This recipe is fully unattended only when there is **no terminal**: cloud-init, a
CI job, or an `ssh` command run without a pty. In an ordinary interactive SSH
session the installer still asks three questions — the admin email (prefilled
from `GOTHAM_ADMIN_EMAIL`), the panel domain, and the file-manager question.
Answer them up front by appending `-s -- --domain panel.example.com
--no-file-manager` after `sh` in the command below; `--domain` sets the domain,
Enter (or omitting it) skips it, and `-` at the email prompt skips account
creation.

Inside a `curl | sudo sh` run, the variables must go **after** `sudo` and before
`sh`; setting them on `curl` only configures `curl`, and `sudo` starts the
installer with a fresh environment. Create the password file first (one line,
root-only):

```sh
sudo install -m 0600 -o root -g root /dev/null /root/gotham-admin-password
sudo nano /root/gotham-admin-password   # type the password on one line and save
```

Then run:

<<< @/snippets/install-command.sh#unattended{sh}

If your sudoers policy refuses command-line variables, put them in `env`
instead (replace the right side of the pipe with this):

```sh
sudo env GOTHAM_ADMIN_EMAIL=ops@example.com \
  GOTHAM_ADMIN_PASSWORD_FILE=/root/gotham-admin-password sh
```

The installer creates the admin account without prompting and prints the login
at the end. The password travels from the file on stdin, so it never appears in
the process list or shell history. Leave out `GOTHAM_ADMIN_PASSWORD_FILE` and a
random password is generated and printed once; leave out `GOTHAM_ADMIN_EMAIL`
as well and no account is created now, so the installer prints the manual
command.

For a manual checkout, pass the same variables before the script:

```sh
sudo GOTHAM_ADMIN_EMAIL=ops@example.com \
  GOTHAM_ADMIN_PASSWORD_FILE=/root/gotham-admin-password \
  deploy/install.sh
```

To see every step without changing the server, pass `--dry-run`:

<<< @/snippets/install-command.sh#dry-run{sh}

## Manual install (from a checkout)

Choose this path if you want to read the scripts before running them, install
behind an internal mirror, or work on Gotham itself.

```sh
git clone https://github.com/justindeelux/gotham.git
cd gotham
sudo deploy/install.sh
```

It is the same installer, with the same questions and the same result. The
difference is where the files come from: it runs from your checkout, so keep
the rest of `deploy/` next to the script instead of copying the script on its
own. `--domain panel.example.com` and `--dry-run` work as above.

### Build from source (optional)

Building is for development and inspection; the installer always installs a
signed release, not a local build. You need Go 1.27+ and Node 20+:

```sh
git clone https://github.com/justindeelux/gotham.git
cd gotham
make build     # writes bin/gotham and bin/gotham-agent
```

`make build` also refreshes the web panel when the `web/` dependencies are
installed. The signed release ships the same two binaries.

## Advanced options

The installer accepts a few environment variables and flags for non-standard
setups (a mirror, a pinned version, managed PostgreSQL/Redis, or a test
sandbox). The table below lists them; [Configuration](/reference/configuration)
explains the matching runtime settings.

| Variable or flag | Purpose |
|---|---|
| `GOTHAM_VERSION` | Install a specific tag instead of the newest release. |
| `GOTHAM_RELEASES_URL` | GitHub-style releases root for a mirror (default `https://github.com/<repo>/releases`). |
| `GOTHAM_BASE_URL` | Full static asset base URL; requires `GOTHAM_VERSION`. |
| `GOTHAM_REPO` | `owner/name` of the release repository (default `justindeelux/gotham`). |
| `GOTHAM_DATABASE_DSN` | Managed PostgreSQL DSN; skips local provisioning. |
| `GOTHAM_REDIS_ADDR` | Redis `host:port` (default `localhost:6379`). |
| `GOTHAM_SKIP_DEPS=1` | Do not install or configure PostgreSQL/Redis. |
| `GOTHAM_ADMIN_EMAIL` | Email of the first admin account for a scripted install. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | File holding the first admin password (single line, root-readable). |
| `--dry-run` | Print every step without changing the host. |
| `--cp-host <name-or-ip>` | Add a name or IP to the agent gateway certificate; see [Add nodes](/guide/nodes). |

## After the install

- [First login](/guide/first-login) — sign in, create invites, reset a password.
- [Domain and HTTPS](/guide/domain-and-https) — use your own domain and a
  certificate.
- [Add nodes](/guide/nodes) — add servers and install their host tools.
- [Remote file service](/guide/file-service) — browse and edit node files from
  the panel.
- [Updates and rollback](/guide/updates) — keep the control plane and nodes
  current.
- [Git sources and the SSRF policy](/guide/git-sources) — allow a private git
  server.
- [Release and signing](/guide/release-signing) — maintainers only.
- [Troubleshooting](/guide/troubleshooting) — when something does not start.

## Looking for a section from the old install page?

The install guide used to hold everything. These pages moved:

- <span id="https-for-the-panel"></span>[HTTPS for the panel](/guide/domain-and-https)
- <span id="remote-file-service"></span>[Remote file service](/guide/file-service)
- [First login](/guide/first-login)
- [Node agent](/guide/nodes)
- [Node host tools](/guide/nodes)
- [Upgrading an existing control plane](/guide/updates#reinstalling-on-an-existing-host)
- [Git sources and the SSRF host policy](/guide/git-sources)
- [Release and signing flow](/guide/release-signing)
