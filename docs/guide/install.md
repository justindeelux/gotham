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
- At least 1 GB of RAM and 1 CPU; 2 GB is comfortable.
- Outbound HTTPS to `github.com`, where the signed releases are downloaded.
- Optional: a domain name for the panel. If you use one, ports 80 and 443 must
  be free on this server.

The installer adds PostgreSQL, Redis and Docker for you. It does not need a
repository checkout.

## Automatic install (recommended)

Run this one command on the server. It downloads the installer and starts it
with root rights:

<<< @/snippets/install-command.sh{sh}

The installer asks three questions. Press Enter to keep the default:

| Prompt | What to answer |
|---|---|
| `Admin email [admin@example.com]` | The address of the first account. Press Enter to use `admin@example.com`, or type your own. |
| `Admin password (empty to generate)` | The password for that account, typed twice (8 to 128 characters). Press Enter and the installer generates a strong password and prints it once at the end. |
| `Panel domain (optional, Enter skips)` | A domain such as `panel.example.com`, if it already points at this server. Press Enter to skip and use the server address for now. |

While it runs, the installer:

1. picks the newest release for your CPU and verifies its Ed25519 signature
   before using any file from it;
2. installs PostgreSQL and Redis from the distribution packages (unless you
   point it at managed services);
3. installs the control plane (binary, updater and systemd unit), then Docker
   and the node agent on the same host;
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
that last step fails, the panel stays installed and the installer prints the
exact command to retry it.

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

### Unattended install

On a server without a terminal the installer never prompts. Start it with
`GOTHAM_ADMIN_EMAIL` set (and, optionally, `GOTHAM_ADMIN_PASSWORD_FILE` pointing
at a root-readable file that holds the password) and it creates the account
without asking. Without an email it skips account creation and prints the
manual command.

To see every step without changing the server, pass `--dry-run` the same way
as `--domain`.

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
| `GOTHAM_ADMIN_EMAIL` | Email of the first admin account for an unattended install. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | File holding the first admin password (single line, root-readable). |
| `GOTHAM_INSTALL_ROOT` | Install under a prefix instead of `/` (testing only; enables test mode). |
| `GOTHAM_INSTALL_TEST_PUBLIC_KEY` | Test-only: replace the pinned release key; only honoured with `GOTHAM_INSTALL_ROOT`. |
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
