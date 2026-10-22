---
title: Install
---

# Installing Gotham

This guide covers installing the control plane and a node agent, first login,
updates and rollback, and the release/signing flow.

## Requirements

- **Control plane host:** Ubuntu 22.04 (or any systemd Linux) with `curl`,
  `openssl` 3, `sudo` (with `visudo`) and `coreutils`. The installer provisions
  PostgreSQL and Redis from the distribution packages unless you point it at
  managed services with `GOTHAM_DATABASE_DSN` / `GOTHAM_REDIS_ADDR` (or set
  `GOTHAM_SKIP_DEPS=1`).
- **Node host:** Linux (`amd64` or `arm64`) with `systemd`, `curl`, `openssl` 3,
  `sudo` (with `visudo`) and Docker Engine (or pass `--full` on Ubuntu/Debian
  to have the installer set Docker up from the official repository).
- Outbound HTTPS to `github.com` (or your mirror via `GOTHAM_RELEASES_URL` /
  `GOTHAM_BASE_URL`).

## Control plane

Run the installer from a checkout, so it can use its sibling files (the
root-owned update wrapper, its config, the sudoers rule and the systemd unit):

```sh
git clone https://github.com/justindeelux/gotham.git
sudo gotham/deploy/install.sh
```

The installer:

1. detects the architecture (`amd64`/`arm64`);
2. resolves the version — `GOTHAM_VERSION` if given, otherwise the newest tag
   from the first redirect of `<releases>/latest` — and downloads
   `gotham-linux-<arch>`, `gotham-manifest-<arch>.txt` and
   `gotham-manifest-<arch>.txt.sig` from that single pinned tag;
3. verifies the manifest's Ed25519 signature against the **embedded** release
   public key, binds the manifest to the version/arch/file name, and verifies
   the artifact SHA-256 against the signed manifest — any failure aborts;
4. installs the binary into `/var/lib/gotham/bin/gotham` (owned by the `gotham`
   service user), the wrapper into `/usr/libexec/gotham/gotham-update`, the
   config into `/etc/gotham/updater.conf`, the status directory and sudoers
   rule, and the unit into `/etc/systemd/system/gotham.service`;
5. provisions the gRPC mTLS certificate authority at `/var/lib/gotham/ca`
   (`gotham ca init`, idempotent — an existing CA is kept);
6. applies the database migrations and starts `gotham.service`;
7. installs and starts the node agent on the same host as the first node
   (`install-agent.sh --full --ca` with the provisioned CA, pinned to the
   same release tag; fixed node id `localhost`, control-plane dial address
   `127.0.0.1:9442`). There is no opt-out. Unsupported platforms and an
   existing remote `agent.env` are refused before the installer changes files;
   move that remote agent or repoint it explicitly before retrying;
8. prints the Web UI URL and the first-login steps.

If step 7 fails on a supported platform, the control plane is left installed
and running (nothing is rolled back) and the installer exits nonzero with the
exact agent-only retry command.

The gRPC gateway the node agents connect to runs **TLS** as soon as that CA
exists: `gotham serve` loads `/var/lib/gotham/ca/ca.crt` + `ca.key` and presents
a server certificate signed by it. To add a node, copy the public certificate to
it (never `ca.key`):

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
```

The listener certificate's SANs always include the loopback names
(`localhost`, `127.0.0.1`, `::1`) and the machine hostname. A remote agent dials
`GOTHAM_AGENT_CP_ADDR`, so the name or IP it uses must also be a SAN — otherwise
the agent handshake fails with `certificate is valid for …, not <name>`. Add the
control plane's address when installing:

```sh
sudo deploy/install.sh --cp-host cp.example.com --cp-host 203.0.113.10
```

`--cp-host` is repeatable (and `GOTHAM_GRPC_HOSTS=<a,b>` is the environment
form); the list is persisted next to the CA. Re-running the installer without it
keeps the persisted list. `GOTHAM_GRPC_HOSTS` in the service environment
overrides the persisted list at runtime, and `gotham ca init --host <name-or-ip>`
can set it directly. A node that dials an address which is not a SAN fails
closed (never plaintext).

An operator who runs `gotham serve` directly without a CA gets the development
fallback (plaintext gRPC, logged as a warning); the installer never leaves a
production host in that state.

### HTTPS for the panel

Set the instance `control_plane_url` to `https://panel.example.com` in General
settings. The registered `localhost` node must run Traefik, with inbound ports
80 and 443 open. Saving starts a confirmation trial and creates a panel-owned
certificate intent; no application is required. Confirm through the proposed
host before the trial expires. Both the proposed and previous hosts stay routed
during the trial, and revert restores the previous host if issuance fails.

HTTP-01 is the default: the domain's A/AAAA records must point directly to this
box, and Cloudflare records must be **DNS only** (proxy disabled). Remove stale
AAAA records when the box cannot serve IPv6. To keep Cloudflare's proxy enabled,
configure a DNS provider and select DNS-01 for the panel intent through the
platform-admin `PATCH /api/v1/proxy/panel-certificates/{id}` API using the existing
`challenge` and `dns_provider_id` fields. DNS-01 needs a token allowed to update
the configured zone; use Full (strict) once the origin certificate is issued.

The HTTPS router starts ACME issuance while HTTP continues forwarding to the
panel. HTTP redirects to HTTPS only after the local agent reports an unexpired
certificate; readiness is refreshed every 30 seconds. Pending or failed issuance
keeps HTTP available. The routers API reports the proposed host during a trial
and adds `certificate.status` (`issued`, `pending`, or `failed`) and
`certificate.not_after` for panel rows. The platform-only
`GET /api/v1/proxy/panel-certificates` API lists panel intents with the existing
storage `status` plus additive `panel_state` and `owner_kind` fields. `pending`
means no usable certificate has been observed yet; `failed` includes unusable
configuration or unavailable storage, since the agent does not report ACME
order errors. Application certificate APIs retain their team scope.

### Git sources and the SSRF host policy

Application clone URLs are operator-supplied, and the control plane dials
them with `git` at deploy time (default-branch resolution) and on the
Test-connection button. Every such dial passes the SSRF host policy
(`internal/deploy/githostpolicy.go`): loopback, link-local (including the
`169.254.169.254` cloud metadata address), unspecified, multicast,
carrier-grade NAT, benchmarking, NAT64 and 6to4 remotes are always refused,
as are numeric-IP tricks (`2130706433`, `0x7f.0.0.1`), userinfo and IPv6 zone
ids. RFC1918, ULA and loopback remotes are refused too, unless the operator
allows private hosts for a self-hosted git server:

```yaml
# gotham.yaml
deploy:
  git_allow_private_hosts: true
```

or `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS=true` in the service environment
(`1` and `true` both count, matching the config file reader).
Link-local and the other always-denied ranges stay denied. Note the name/IP
split: the `localhost` NAME stays denied even with the setting (names
rebind), while an explicit `127.0.0.1` is allowed by it. The host is
resolved twice and a changed answer (DNS rebinding) refuses the dial; `https`
remotes are pinned to the resolved address (`http.curloptResolve`), and every
`http(s)` git invocation refuses redirects (`http.followRedirects=false`), so
a public URL cannot bounce to an internal one (renamed repositories fail
closed — that is the trade-off).
Deploy-log errors carry the classified hint only — never resolved internal
addresses. Local paths and `file://` URLs stay behind `GOTHAM_DEV_CLONE_LOCAL`
(development fixtures only).

Residuals the operator should know:

- `ssh` and `git://` remotes are not pinned (git offers no equivalent
  option): their two compared resolutions are the whole mitigation, so a
  fast-flux answer racing that window still reaches the dial.
- An `http(s)` proxy (`HTTP(S)_PROXY` in the service environment) bypasses
  the pin: curl resolves and connects through the proxy, so proxy
  environments must pair this policy with proxy rules that refuse internal
  targets.
- The pin needs git >= 2.37 (older git ignores `http.curloptResolve`
  silently). The control plane logs a startup warning naming the installed
  version when it predates 2.37; upgrade git to restore the pin.

### Upgrading an existing control plane

Re-running `deploy/install.sh` on a host that was installed before this change
now provisions a CA, so the gRPC gateway switches from plaintext to TLS.
Previously installed agents connect in plaintext and have no CA, so they **go
offline** (Register/Heartbeat fail closed at the handshake) until each is
reinstalled with the CA:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .           # on each node
sudo gotham/deploy/install-agent.sh --ca ./ca.crt
```

The agent binary does not need updating; only `/etc/gotham/agent.env` gains
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt`. If you cannot reach the CP by a name in
the listener SANs yet, add it first (`--cp-host`).

Re-running the installer keeps `/etc/gotham/gotham.env`: the managed keys
(`GOTHAM_DATABASE_DSN`, `GOTHAM_REDIS_ADDR`, `GOTHAM_CA_DIR`,
`GOTHAM_SECRET_KEY`, the JWT key paths) are refreshed — a DSN given on the
command line wins, otherwise the existing value is kept — and any operator-added
keys (`AUTO_UPDATE`, `PLATFORM_ADMINS`, ...) are left untouched. A managed DSN
is never replaced by the local default, and local PostgreSQL/Redis are only
provisioned when the resolved DSN is the built-in local default. The installer
restarts `gotham.service` after a reinstall, so the just-installed binary is the
one that runs (without the restart an upgraded host would keep serving the
previous version). Releases without
`admin exists` (`v0.2.0`) skip automatic creation and print the manual
`admin create` command instead of failing with a database warning; releases
without any admin commands (`v0.1.0` and earlier) print upgrade guidance
instead, since that binary cannot create an account either. Upgrading from
an older installer: the quote rejection also applies to values preserved from
an existing `agent.env`, so a node id containing a quote written by an older
installer now blocks the upgrade with a clear error naming the key — remove
the quote from `agent.env` and re-run. The same holds for `gotham.env`: a
preserved DSN or Redis value with quotes or a trailing backslash now blocks
the upgrade naming the key — fix the value in `gotham.env` (keyword DSNs
with quoted values must use the URL form) and re-run.

Useful overrides:

| Variable | Purpose |
|---|---|
| `GOTHAM_VERSION` | Install a specific tag (default: resolve the latest tag). |
| `GOTHAM_RELEASES_URL` | GitHub-style releases root for a mirror (default `https://github.com/<repo>/releases`). |
| `GOTHAM_BASE_URL` | Full static asset base URL; requires `GOTHAM_VERSION`. |
| `GOTHAM_REPO` | `owner/name` (default `justindeelux/gotham`). |
| `GOTHAM_DATABASE_DSN` | Managed PostgreSQL DSN; skips local provisioning. |
| `GOTHAM_REDIS_ADDR` | Redis `host:port` (default `localhost:6379`). |
| `GOTHAM_SKIP_DEPS=1` | Do not install or configure PostgreSQL/Redis. |
| `GOTHAM_ADMIN_EMAIL` | Email of the first admin account (non-interactive installs; without it creation is skipped). On a terminal it is the email prompt default. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | Single-line file holding the first admin password (with `GOTHAM_ADMIN_EMAIL`, or as the tty default without prompting); without it a random password is generated and printed once. A group/world-readable file prints a warning. |
| `GOTHAM_AUTH_ALLOW_REGISTRATION` | Test/dev only: reopens self-registration after the first account (the default is closed — members join through invites). |
| `--cp-host <name-or-ip>` | Add a DNS name or IP to the gRPC listener certificate SANs (repeatable). |
| `GOTHAM_AGENT_NODE_ID` | Defaults to `localhost`; fixed for `install.sh`. Set a distinct id for each remote `install-agent.sh` run. |
| `GOTHAM_AGENT_CP_ADDR` | Fixed at `127.0.0.1:9442` for `install.sh`; remote agents must set their control-plane address. |
| `GOTHAM_GRPC_HOSTS` | Comma-separated SAN hosts (same as `--cp-host`); also overrides the persisted list at runtime. |
| `GOTHAM_MANAGED_VOLUME_ROOT` | Parent of every application bind mount (default `/var/lib/gotham/volumes`). An application bind must live in `<root>/<app id>`; a named volume is namespaced to the application. Must match the node's `GOTHAM_AGENT_MANAGED_VOLUME_ROOT`. |
| `GOTHAM_INSTALL_ROOT` | Install under a prefix instead of `/` (testing only; non-root; enables test mode). |
| `GOTHAM_INSTALL_TEST_PUBLIC_KEY` | Test-only: replace the pinned trust anchor (PEM or base64); honoured **only** with `GOTHAM_INSTALL_ROOT`, otherwise warned and ignored. |

`deploy/install.sh --dry-run` prints what it would do without changing the host.

## First login

Registration is closed: exactly one account bootstraps the instance and members
join through admin-created invites.

The installer creates that first account for you. Credentials are collected
before anything is installed, and the account is created right after the
database migrations and before the service starts — so nobody can register
first through the open web form while you ponder the prompt:

- **Interactive install** (a terminal is present, `curl | sh` included): the
  installer prompts for the admin email, then for the password with hidden
  input and confirmation (mismatches and policy violations re-prompt, up to
  3 creation attempts). Leaving the password empty generates a strong random
  one, printed once in the final summary together with the email and the
  login URL — store it now, it is not shown again.
- **Non-interactive install** (no terminal): the installer never prompts.
  Set `GOTHAM_ADMIN_EMAIL` — and optionally `GOTHAM_ADMIN_PASSWORD_FILE`
  (a path to a root-readable file holding the password on a single line) —
  to create the account; without a password file a random password is
  generated and printed once in the final summary. Without an email the
  installer skips creation and prints the manual command below.
- **Set both ways**: on a terminal, `GOTHAM_ADMIN_EMAIL` is the email prompt
  default (empty input keeps it, `-` skips) and `GOTHAM_ADMIN_PASSWORD_FILE`
  is used without prompting. A group/world-readable password file prints a
  warning but is still used.
- **Re-run / upgrade**: if the instance already has an account the installer
  says `admin already exists, skipping` and changes nothing (it checks before
  prompting). If creation loses a race with another registration, it says
  `an account already exists (created by someone else?)` instead of the
  generic failure.
- **`--dry-run`** prompts nothing and changes nothing.

The password is never passed as a command-line argument, never put in the
environment, and never written to disk, the env file, logs or shell history:
it travels to the binary on stdin only (`sh -x` tracing is disabled around
the secret handling).

Prefer to do it by hand (or skipped the prompt with an empty email)?

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

The password prompt is hidden. Useful flags: `--password-stdin` reads the
password from stdin (a single line, for scripts — pipe it, never use
`--password`, which is visible in `ps`), and `--generate-password` creates a
random one and prints it once as `generated-password: <value>`. Re-run with
`--force` only to add a second account deliberately; the CLI refuses by
default once one exists. `gotham admin exists` prints
`admin-exists: true|false` for scripting (that is how the installer detects
a re-run).

Then:

1. Open `http://<host>:8000` and sign in.
2. To add a member, create an invite in **Teams** and send the shown link:
   the member opens `/register?invite=<token>` and chooses their credentials.
   The link is shown once and expires.
3. From **Servers**, install an agent on a node (below); the node then reports
   heartbeats and is visible in the UI.

Lost the admin password? `sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password
--email ops@example.com` replaces it and revokes the account's refresh sessions.
Bearer access tokens are stateless JWTs, so a token already minted stays valid
until it expires (15 minutes); resetting is not an instant, fleet-wide logout.

The first account is automatically the platform administrator: on a fresh
instance the account created while the users table is empty (first
registration, first OAuth sign-in, or `gotham admin create` without `--force`)
gets the platform-admin flag and its sessions carry the `admin` role claim, so
the platform-global operations (node-wide proxy sync, DNS providers) work
without further configuration. Upgraded instances are unchanged — the
migration leaves every existing row non-admin, so keep listing the operator
account email in `PLATFORM_ADMINS` in `/etc/gotham/gotham.env` (or mint an
admin-scoped API token).

Takeover risk: on a fresh instance registration is open until the first
account exists, so whoever registers first becomes the admin. On a public,
unconfigured instance create that account (or set `PLATFORM_ADMINS`) before
exposing the control plane. Emptying the users table by hand re-opens
first-run registration, and the next registrant becomes platform admin again.

An admin-scoped API token minted by an admin session outlives a later
demotion of that account: no session or token revoke is tied to role changes
(out of scope to fix here), so rotate the token if the grant must end.

## Node agent

`install.sh` already installs the agent on the control-plane host itself (the
first node, `localhost`); the steps below add *further* nodes. Copy
the control plane's public CA certificate to the node first (see
[Control plane](#control-plane)), then run the installer:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
git clone https://github.com/justindeelux/gotham.git
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

The installer **fails closed** without a CA: it refuses to run unless
`--ca <path>` (or `GOTHAM_AGENT_CA_FILE=<path>`) is given, so a node never
dials the control plane in plaintext. It installs the certificate as
`/etc/gotham/ca.crt` (root-owned, `0644`) and writes
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt` to `/etc/gotham/agent.env`, which makes the
agent verify the control plane over TLS.
`--insecure` is the documented **local-development-only** override: it skips the
CA check, writes `GOTHAM_AGENT_INSECURE=true`, and leaves `GOTHAM_AGENT_CA`
unset, so the agent connects without TLS. The agent itself now fails closed:
with no CA it refuses to start unless `GOTHAM_AGENT_INSECURE=true`, and a
plaintext listener is confined to loopback (the installer defaults the address
to `127.0.0.1:9443`). Never use it on a real node.

Over TLS the channel is mutual: the agent generates a key, sends a CSR
on `Register`, receives a node certificate, and presents it on every later
connection; the control plane binds the certificate identity and key to the
node, and each agent accepts only the control plane (a certificate carrying the
`spiffe://gotham/control-plane` identity), so a node cannot act as the control
plane toward another node. A node's first registration has no certificate yet and
needs the enrollment token: the control plane generates one
`<GOTHAM_CA_DIR>/enrollment-token` (or you set `GOTHAM_GRPC_ENROLLMENT_TOKEN`),
and you hand it to the installer with `--enrollment-token-file /root/token` (a
root-owned file, mode 0600, not a symlink) or, without echo, the
`GOTHAM_AGENT_ENROLLMENT_TOKEN` environment variable for `sudo -E`. Never write
it as `sudo VAR=<token> …`: that exposes the secret in `ps`/`/proc` and shell
history. Without a token (and without the explicit
`GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true` opt-in) first enrollment is refused. Only the first registration needs it. Nodes
enrolled before this change keep working (the control plane accepts their
existing certificate and binds its key), and a self-update never touches the node
key or certificate.

The agent installer uses the same signed-manifest verification
(`deploy/release-verify.sh`) for `gotham-agent-linux-<arch>` and
`gotham-agent-manifest-<arch>.txt`, with the pinned release public key and no
runtime override. It installs the agent into
`/var/lib/gotham-agent/bin/gotham-agent`, the shared wrapper as
`gotham-agent-update`, the config `/etc/gotham/agent-updater.conf`, the
sudoers rule and the unit. Set `GOTHAM_AGENT_CP_ADDR` (control-plane gRPC
address) and `GOTHAM_AGENT_NODE_ID` in the environment before running it; they
are written to `/etc/gotham/agent.env`.

Useful agent installer flags and variables:

| Flag / variable | Purpose |
|---|---|
| `--ca <path>` | Install this control-plane CA certificate (required). |
| `GOTHAM_AGENT_CA_FILE` | Same as `--ca`, via the environment. |
| `--insecure` | Development only: connect without TLS. |
| `--full` / `GOTHAM_AGENT_FULL=1` | Also install Docker Engine and the compose plugin from the official Docker apt repository (Ubuntu/Debian only; no-op when already installed). On by default for the localhost agent `install.sh` sets up; off by default for direct runs. |
| `--dry-run` | Print what would be done; makes no change. |
| `GOTHAM_AGENT_CA` | The agent-side path the installer writes (`/etc/gotham/ca.crt`). It is **not** read from the ambient environment; on a reinstall the installer keeps the value already in `agent.env`. Use `--ca`/`GOTHAM_AGENT_CA_FILE` to change it. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | Release channel this node accepts, `stable` (default) or `beta`; an offer with a different or empty channel is refused. |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | Parent of every application bind mount the node accepts (default `/var/lib/gotham/volumes`); falls back to the shared `GOTHAM_MANAGED_VOLUME_ROOT`. Must match the control plane. |
| `--file-manager` / `--no-file-manager` / `GOTHAM_AGENT_FILE_MANAGER=yes\|no` | Answer the installer's file-manager question without a prompt. The installer asks (default **no**; no terminal also means no). Yes writes `GOTHAM_AGENT_FILES=true` and a `gotham-agent.service.d/file-manager.conf` drop-in that runs the agent **as root** without systemd sandboxing; no writes `GOTHAM_AGENT_FILES=false` and removes the drop-in. A re-run keeps the previous answer (a hand-set `GOTHAM_AGENT_FILES=true` is kept as is, without the root drop-in); an ambient `GOTHAM_AGENT_FILES` is ignored in favour of that answer. |
| `GOTHAM_AGENT_FILES` | The remote file service on the node. On unless set to `false` (the installer writes the answer above). |
| `GOTHAM_AGENT_FILE_FULL_ACCESS` | On unless set to `false`: the service exposes the **whole filesystem** as one writable root `system` (`/`), with no protected paths — the operator accepted that risk. `false` restores the confined roots described below. |
| `GOTHAM_AGENT_FILE_ROOTS` | Extra browsable roots, colon-separated absolute paths (default none). Operator roots are **read-only** in this phase; a root that is missing, not absolute, in a dangerous tree or overlapping another root refuses startup. |
| `GOTHAM_AGENT_ARCHIVE_MAX_BYTES` | Total uncompressed bytes one **compress or extract** may read/write before it is refused with `ResourceExhausted` (413) — the size cap and half of the zip-bomb guard. Default `4294967296` (4 GiB). |
| `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` | Maximum entries one archive operation may walk or unpack (default `100000`); beyond it the operation is refused with `ResourceExhausted` (413). |
| `GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES` | Maximum on-disk size of an archive file that **extract** will read (default `1073741824`, 1 GiB); an archive above it is refused with `ResourceExhausted` (413) before any parser runs. Applies to every format. |
| `GOTHAM_AGENT_COMPOSE_ROOT` | Directory holding one subdirectory per compose project (default `/var/lib/gotham-agent/compose`); browsable read-only when the file service is enabled. |

`GOTHAM_AGENT_CP_ADDR` must use a name or IP that is one of the control plane's
listener SANs (see [Control plane](#control-plane)); otherwise the TLS handshake
fails and the node never registers. It must be `host:port` without spaces,
quotes or `=` (the listen address follows the same rule); every other
`agent.env` value is likewise validated up front — control characters and
trailing backslashes rejected everywhere, the node id held to what the agent
itself accepts (at most 253 bytes; no spaces, tabs, `*`, `/` or backslashes)
and additionally refusing quotes (systemd's EnvironmentFile parser would
strip or regroup them, so the unit would see a different value than the
installer wrote), paths absolute, the docker endpoint one of
`unix:///abs/path`, `/abs/path` or `tcp://host:port`, auto-update exactly
`true`/`false`, the interval a Go duration (whitespace-padded values trimmed
as the agent trims them; day/week units and overflowing values refused) —
before the installer changes anything, so a bad value fails with nothing
created. `install.sh` runs these checks before mutation and always installs
its local agent as `localhost`, connected to `127.0.0.1:9442`.

The control plane creates or repairs the shared `localhost` node on startup;
its status becomes ready only after an agent heartbeat. Deleting it returns
HTTP 409; PATCH is rejected too. Upgrade migration 49 renames the saved local
agent identity in place, preserving the server UUID, apps, deployments and
routers. The installer passes the previous `agent.env` ID to the migration,
so changing the hostname or running migrations in a container is safe. For a
manual migration, set `GOTHAM_LEGACY_LOCAL_NODE_ID` to that saved node ID.
Forward migration 51 also detects a single loopback-addressed node with a custom
ID (for example, `test-node-1` at `127.0.0.1`), including local agent addresses
such as `localhost:9443` and `[::1]:9443`. Startup repeats this check before
creating `localhost`, preserving the UUID, apps, databases, services and routers.
Migration 51 removes a prior startup's empty, pending, never-registered
`localhost` placeholder only after checking that no foreign-key references exist,
then renames the legacy row. A registered or referenced `localhost`, or multiple
local candidates, stops migration with an actionable error. Inspect the saved
`agent.env` and server records, reconcile their workloads and remove duplicate
local-agent entries before retrying; no workloads are merged or deleted.
Startup scans local candidates only when `localhost` is absent; later registrations
cannot block a restart. Detection uses the recorded IP/agent address, never a bare
node ID; IPs copied directly from an agent-chosen IP node ID are excluded.
Candidates must have agent registration data and no SSH user, key, or password;
SSH-managed loopback/tunnel nodes retain their identity and team. For a legacy
node with a public or missing IP, supply the saved identity to migration 49
as described above; migration 51 does not reinterpret that environment hint.

After a manual upgrade, change `GOTHAM_AGENT_NODE_ID` in the local agent's saved
environment (normally `/etc/gotham/agent.env`) to `localhost` and restart the
agent. The installer does this automatically; keeping the old ID can register
the legacy node again and leave two records targeting the same agent.

The Git URL host policy's denial of the name `localhost` applies to Git
remotes only; it does not validate node IDs or the agent's loopback dial path.
The exact `localhost` node ID is accepted only from a loopback peer. Its
certificate uses the separate SAN `localhost.agent.gotham.invalid`, and the
control plane verifies that name when dialing `127.0.0.1:9443`. It cannot be
used as a control-plane certificate for `localhost` or a loopback IP. Do not
include the agent SAN in the control-plane listener hosts; enrollment refuses
that collision. Other
control-plane listener identities and aliases remain reserved. Remote agents
must set their own distinct node ID and control-plane address:

```sh
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=worker-1 \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

### Node host tools (install / uninstall)

The agent exposes the `ToolService` (ListTools/InstallTool/UninstallTool) over
mTLS; the control plane drives it from the node's tool manager. Every
privileged step runs through one fixed, root-owned helper — the same model as
`gotham-hostctl` — never a blanket `sudo`:

```sh
sudo install -D -m 0755 -o root -g root deploy/gotham-toolctl.sh /usr/libexec/gotham/gotham-toolctl
sudo deploy/install-toolctl-sudoers.sh gotham-agent
```

`install-agent.sh` installs both, so a normal agent install needs no extra
step. The sudoers drop-in (`/etc/sudoers.d/gotham-toolctl`) grants exactly one
rule per verb **and** tool id:

- `install-tool <id>` — `railpack`, `buildkit`, `pack`, `docker`, `git`;
- `uninstall-tool <id>` — `railpack`, `buildkit`, `pack`, `git`
  (`docker` is never removed: it would destroy every workload on the node).

The agent passes only a catalog id; the helper owns every executable argv and
re-validates the id against the same allow-list, so a compromised agent cannot
name a package, a URL or an extra flag. Detect-only host units
(`systemd-networkd`, `systemd-resolved`, `networkctl`, `systemctl`, …) are
refused with `FAILED_PRECONDITION` and have no installer at all.

Behaviour:

- **Dependencies are resolved** by the agent, dependency-first:
  `railpack → buildkit → docker`. Each tool is installed by one helper call.
- **Pinned and verified.** Railpack and pack are installed from a versioned
  release tarball whose sha256 is hard-coded in the helper (never the unpinned
  `install.sh` piped to a shell); BuildKit is the digest-pinned
  `moby/buildkit:v0.34.0` image run as the managed privileged `buildkit`
  container. A checksum mismatch aborts.
- **Idempotent.** A tool already detected on the node is skipped. The BuildKit
  install clears a stale container before recreating it and tags the detection
  reference only after the container runs, so a failed run does not leave the
  tool looking installed.
- **One at a time.** A node-wide lock allows one install or uninstall at a
  time; a second call is refused with `RESOURCE_EXHAUSTED`. The request is
  cancelled when the control plane drops the stream, and bounded by a 15-minute
  timeout. The helper holds its own `flock` for the whole job, so even a job
  whose agent request was cancelled (the transient unit keeps running until it
  finishes or its `RuntimeMaxSec=840` bound fires, whichever is first) blocks a
  retry with a clear "already running" refusal instead of overlapping it; a
  graceful cancel (`SIGTERM`) may defer the helper's stop trap until the
  foreground `systemd-run` returns, so it does not guarantee an immediate stop.
- **Upgrading a node.** The helper and its sudoers rule are installed by
  `install-agent.sh`, not by the self-update binary swap: a node that updated
  only through `gotham-agent` self-update keeps the old install and its tool
  install/uninstall calls fail with sudo's "not allowed". Re-run
  `deploy/install-agent.sh` on the node (idempotent) to install them.
- **Uninstall refuses while a tool is in use** (`FAILED_PRECONDITION`): while a
  build runs, and while another installed tool depends on it (for example
  BuildKit cannot go while Railpack is installed). Only in-flight *builds* are
  tracked as "in use"; a compose service or a plain container using Docker is
  not (Docker is not uninstallable, so only `buildkit`/`git` removals need the
  guard).
- **Sandbox.** The agent unit runs with `ProtectSystem=full`, so `/usr`, `/etc`
  and `/boot` are read-only in its mount namespace and a `sudo` child inherits
  that. The helper therefore re-executes itself through
  `systemd-run --pipe --wait --collect`, which starts a fresh transient unit
  outside the agent's namespace; the node needs systemd (already required by
  the agent unit). Streamed output is the helper's own stdout/stderr, then a
  final `ToolResult{status, exit_code}`.

The helper also runs standalone for debugging (as root, without `systemd-run`):

```sh
sudo GOTHAM_TOOLCTL_DRYRUN=1 GOTHAM_TOOLCTL_ARCH=amd64 GOTHAM_TOOLCTL_DISTRO=apt \
    /usr/libexec/gotham/gotham-toolctl install-tool railpack
```

## Remote file service

**Whole-filesystem mode (default).** The file service is on unless
`GOTHAM_AGENT_FILES=false`, and by default it exposes one writable root named
`system` that is `/`: every file the agent process can reach, with no dangerous
or protected path excluded (including the agent's own key and certificates). The
owner accepted this risk; only platform operators can use it. What the agent can
really read or change is bounded by its Unix user, so the installer asks whether
you want the file manager (default no) and, on yes, runs the agent as root via
the `file-manager.conf` drop-in. Run as the unprivileged `gotham-agent` user the
same root is mostly read-only. Existing nodes that auto-update to this version
expose `/` to operators with the permissions of their current user until
`GOTHAM_AGENT_FILES=false` is set.

In this mode the application-directory guard still protects top-level app directories of the managed volumes root (no rename/delete while a container mounts them). `os.Root` refuses to follow an *absolute* symlink even inside `/`, so a path through one (for example `/var/run`) cannot be entered through the link; use the real path. Answering no after a yes runs `chown -R gotham-agent` on `/var/lib/gotham-agent` so the certificate and key the root agent wrote belong to the service user again. `GOTHAM_AGENT_FILES` and `GOTHAM_AGENT_FILE_FULL_ACCESS` are switched off by `false`, `0`, `no` or `off`; any other value leaves them on.

**Confined mode** (`GOTHAM_AGENT_FILE_FULL_ACCESS=false`). Roots are an
agent-local allow-list:

- `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` — the writable root, holding the
  application bind mounts.
- `GOTHAM_AGENT_COMPOSE_ROOT` — the compose tree, browsable read-only (the next
  `ComposeUp` overwrites edits).
- `GOTHAM_AGENT_FILE_ROOTS` — colon-separated operator roots, read-only in this
  phase.

Every path is resolved and validated at startup. A root that is not absolute,
resolves into `/etc /proc /sys /dev /run /boot /usr /home /root`, Docker's data
root (the `data-root` from `/etc/docker/daemon.json`, else `/var/lib/docker`) or
the agent's own state refuses startup; so does a root that overlaps another
root. A built-in root that does not exist yet (Docker and the compose service
create those lazily) is skipped with a warning instead of aborting the agent.
Confinement itself is `os.OpenRoot`: every access is resolved relative to a held
directory descriptor, so a `..` component, an absolute path or a symlink that
leaves the tree is refused by the descriptor walk. The service must not be
exposed without the agent's CA configured, since confinement is not
authentication.

`List` returns at most 5,000 entries per page, sorted by name. A page with more
entries follows reports `truncated=true` and an opaque base64 `cursor`; pass the
cursor back to resume, and keep paging until `truncated=false` — a truncated
page always carries a non-empty cursor that makes progress, so a client can
never loop. The cursor is base64 because a boundary name may hold bytes that are
not valid UTF-8 (legal on Linux), which a raw proto3 string could not carry; the
displayed `name` of such an entry is scrubbed to U+FFFD and therefore cannot be
fed back to `Stat`/`Read`, but the cursor still addresses the real bytes. A
malformed cursor is rejected with `InvalidArgument`. A single `List` scans up to
100,000 directory entries and keeps only the page in memory; a directory larger
than that fails with `ResourceExhausted` ("directory too large") rather than
returning a silently partial listing.

### Archives (compress / extract)

`Compress` and `Extract` build and unpack `zip`, `tar` and `tar.gz` **without a
shell** (`archive/zip`, `archive/tar`, `compress/gzip` only; no `os/exec`
anywhere). Compress skips symbolic links and special files (never following a
link) and counts them in the final progress message (which the control plane
does not surface to the browser — see below); the archive is staged in a
temporary file and renamed into place, so a failed or cancelled compress never
leaves a partial archive. Extract refuses symbolic-link, hard-link, device and
FIFO entries, strips setuid/setgid/sticky bits, and writes every entry through
`os.Root`, so an absolute name, a `..` segment or a symlinked parent cannot
escape the destination root. A file that already exists is refused unless the
caller sets `overwrite`; an existing directory is reused, never removed. Each
file is written to a temporary sibling and renamed over the target, so a failed
overwrite never truncates the existing file. Both guard the running-app
directory (an archive cannot replace a top-level application directory of the
managed volumes root), refuse a read-only root, and honour the free-space
reserve and transfer budget of the write path.

**Extract is not transactional.** A failure part-way leaves the entries already
unpacked in place (only the file being written is staged and removed), so the
caller must treat a failed extract as a partial result and clean up if needed.

Four bounds guard an archive: the **on-disk size of the archive file** extract will read
(`GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES`, default 1 GiB, checked before any parser runs and for
every format); the total uncompressed bytes (`GOTHAM_AGENT_ARCHIVE_MAX_BYTES`, default 4 GiB); the
entry count (`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES`, default 100000); and a **fixed** compression ratio
of **200:1** against the archive's own size (not env-configurable). Directory entries count toward
the entry cap. For a zip, the end-of-central-directory record (and the zip64 locator/record when
present) is read from the file's tail before `zip.NewReader` runs, and the archive is refused when
its declared entry count exceeds `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` or its declared
central-directory size exceeds 64 MiB — so a crafted archive declaring millions of entries is
rejected before any header is allocated. An entry name deeper than 64 segments is refused. An
over-limit archive is refused before it fills the disk. All limits are read as positive integers;
an unparseable or non-positive value silently falls back to its default (like the other
`GOTHAM_AGENT_FILE_*` limits), so a typo does not disable the cap.

The skipped-entry count a compress emits is **not surfaced to the browser**: the agent reports it
in the stream's terminal `Progress.skipped`, but the control plane drains the stream and returns
only the created archive, so the UI cannot show it.

An archive limit is answered **413** with a distinct `archive_limit` body code,
a refused archive member (a link, device, FIFO or unsupported type) **409**
with an `archive_entry` code, and a truncated or corrupt archive body **400**
with an `invalid_archive` code, so the UI shows an archive-specific message
instead of "file is too large to edit/upload", "conflict" or an internal error.

An archive request **stays open for the whole transfer**: the control plane
holds the connection up to its transfer budget, and the browser disables its own
client timeout, but a reverse proxy in front of the control plane must allow a
long-lived response, or the request will be cut while the agent keeps working.
The node's Traefik edge does not front the file API, so its timeouts do not
apply to it.

### Control-plane side

The `/api/v1/servers/{id}/files` routes are mounted by default; set
`FEATURE_FILES=false` to switch them off (they then answer **503**). The whole
surface is limited to **platform operators** (an admin-scoped API token, a session
with the `admin` role claim, or an email in `PLATFORM_ADMINS`): everyone else gets
**403**, because the API exposes the node's filesystem. Operators also need to belong to the node's team, and read/deploy-scoped API tokens no longer reach the file API (an admin-scoped token is required). The routes are also team-scoped: a
browsable server must belong to a team (a legacy team-less node answers 404),
`roots`/`list`/`stat` need any member, and `content`, `download`, uploads and
every mutation need an owner/admin role. File bodies can hold secrets (a `0600`
compose `.env`), so `GET /content` and `POST /download-ticket` stay owner/admin.
For API tokens: `roots`/`list`/`stat` are readable with a **read**-scoped token;
every write, `GET /content` and `POST /download-ticket` need the **deploy** scope
(the non-GET method gate plus the secret-bearing read suffix); and the
ticket-gated `GET /files/download` sits outside the auth group, so the
single-use ticket is its credential.

| Variable | Default | Purpose |
|---|---|---|
| `FEATURE_FILES` | on | Set `false` to disable the file API (503 while off). |
| `FILES_MAX_UPLOAD_BYTES` | `1073741824` (1 GiB) | Ceiling for one write/upload body (413 above it); should match the agent's `GOTHAM_AGENT_FILE_MAX_UPLOAD`. |
| `FILES_SPOOL_DIR` | `<tmpdir>/gotham-spool` (0700) | Directory staging a streamed upload body before it reaches the agent. |
| `FILES_SPOOL_CONCURRENCY` | `4` | Max upload bodies staged at once; beyond it an upload answers 429. |

A streamed body is staged on the control plane (the agent's write header must
declare an exact size, which a multipart part does not carry). The spool is
swept at startup and hourly, and its free space is checked against the ceiling
before any write. The file API is served by the control plane directly, so it
does **not** traverse the node's Traefik; see
`deploy/README.md` → "File-manager uploads (staging)"
for the staging knobs.

### Permissions (chmod)

`POST /files/chmod` (owner/admin, deploy scope for an API token) changes one
entry's permission bits. The body is `{root, path, mode}` and **`mode` is
required**: an omitted field is refused **400** rather than silently applied as
`000`. Only the low `0o777` bits are accepted: a request carrying setuid
(`0o4000`), setgid (`0o2000`), sticky (`0o1000`) or any higher bit is refused
**400** before the agent is dialed. A symbolic link is refused **400** (it never
follows the link to change its target), a special file (FIFO/socket/device)
**409**, and a read-only root **403**. The agent also refuses a directory that
would lose its owner execute bit (it must stay traversable) and a hard-linked
regular file in a confined root (its inode may be reachable outside the root);
the whole-filesystem root has no "outside", so the hard-link check is moot there.
Existing setuid/setgid/sticky bits are **preserved, not editable**: only the
plain permission bits change and a special bit is never created. An old agent
that predates the RPC answers **501**. chmod never changes an entry's type, so
the application-directory guard (rename/delete only) does not apply; recursion
is not supported.

The confinement opens the root and every parent directory with `O_PATH` (needs
search permission, not read), `openat`s the entry with `O_NOFOLLOW`, `fstat`s
the descriptor to classify the type, and chmods that same inode — via
`fchmodat2(AT_EMPTY_PATH)` where the kernel supports it (Linux 6.6+) and the
`/proc/self/fd` magic link otherwise. Nothing outside the root can change, a
swapped-in link cannot be followed, and an entry the agent just made unreadable
(`000` or `0o111`) can still be recovered through the UI, including by a
non-root agent.

### Permissions model

The agent runs as the unprivileged `gotham-agent` user with no
`CAP_DAC_OVERRIDE`, so **who owns a file decides what the file manager can do**:

- The `volumes` root (default `/var/lib/gotham/volumes`) is the writable root, but Docker
  creates each application bind directory as `root:root 0755`. The agent can list it and read
  world-readable files, but **cannot create, rename, delete or edit** anything a container
  wrote; a `0600` file is unreadable. This is the deliberate read-mostly UX with per-entry
  "permission denied" states.
- The `compose` root and `GOTHAM_AGENT_FILE_ROOTS` are **read-only** in the service even though
  the filesystem may allow writes (a compose edit would be overwritten by the next `ComposeUp`).

For containers to be editable, provisioning must make `<root>/<appID>` agent-writable, by
**either** the control plane chowning it to `gotham-agent`, **or** a shared group plus a setgid
(`2775`) application directory (the agent must also be in that group). This is an **owner
decision** pending before either flag is flipped on for such nodes; setgid alone is not enough
until the agent joins the group.

## Updates and rollback

Releases are verified with the public key embedded in the running binary, so no
manual verification is needed at update time. The `gotham` binary lives in the
service-owned directory `/var/lib/gotham/bin`, so run the operator CLI as the
service user:

```sh
sudo -u gotham /var/lib/gotham/bin/gotham update check
sudo -u gotham /var/lib/gotham/bin/gotham update apply
sudo -u gotham /var/lib/gotham/bin/gotham update rollback
```

Run as root it would install a `root:root` binary over the service-owned one and
the next service-run apply would fail at the hardlink backup
(`fs.protected_hardlinks`); `gotham update apply`/`rollback` refuses a
mismatched owner with a clear message.

- **Control plane:** the commands above, or set `AUTO_UPDATE=true` in
  `/etc/gotham/gotham.env` for unattended updates. The wrapper health-checks the
  new binary and rolls back to `gotham.old` on failure.
- **Node agent:** agents update from the control plane over the CA-verified TLS
  channel; enable unattended applies with `GOTHAM_AGENT_AUTO_UPDATE=true`, or
  trigger a fleet rollout with the operator-only
  `POST /api/v1/servers/agents/update-all` API — there is no UI control for it
  yet. A release that fails to activate rolls back and its version is backed off
  (see `deploy/README.md`).
- **Rollback:** `gotham update rollback` restores the previous binary on disk.
  The running process keeps the current binary until it is restarted, so run
  `systemctl restart gotham` afterwards to actually execute the restored
  binary.

Reinstalling a specific version is a normal install with `GOTHAM_VERSION=vX.Y.Z`.

## Release and signing flow

Releases are built and published by `.github/workflows/release.yml` on a `v*`
tag, on a GitHub-hosted runner. The release job targets the GitHub **`release`
environment** (with a required reviewer), so it reads the environment secrets
and waits for the owner's approval before it can use them:

1. The job fails closed unless **both** environment secrets are set and
   non-empty: `GOTHAM_UPDATE_PUBLIC_KEY` (base64 raw Ed25519 public key,
   embedded into both binaries with `-ldflags`) and `GOTHAM_UPDATE_SIGNING_KEY`
   (PKCS#8 PEM Ed25519 private key, used only on the runner).
2. It also checks that the private key corresponds to
   `GOTHAM_UPDATE_PUBLIC_KEY` and to the committed `deploy/gotham-signing-key.pub`
   (and therefore to the key embedded in `deploy/install.sh`).
3. GoReleaser builds `gotham` and `gotham-agent` for `linux/amd64` and
   `linux/arm64` as raw binaries named `gotham-linux-<arch>` /
   `gotham-agent-linux-<arch>` (the names the update checkers resolve), embeds
   the version and public key, and creates a **draft** GitHub Release with the
   binaries, `checksums.txt` and the published public key.
4. The workflow signs the per-arch manifests (`gotham-manifest-<arch>.txt` and
   `gotham-agent-manifest-<arch>.txt`) over the exact built bytes. It locates
   the **draft** through the releases list (drafts are not returned by
   `/releases/tags/<tag>`), checks the 6 base assets GoReleaser uploaded,
   uploads the 8 manifest/signature files, then asserts the full 14-asset set
   (4 binaries, `checksums.txt`, the public key and all 8 manifest/signature
   files) before publishing the draft and verifying it is no longer a draft.
   Draft releases are skipped by the checker, so an incomplete release is never
   offered.

The asset naming is a contract with the update code — see
`internal/updates/checker.go` (control plane), `internal/updates/agents.go`
(agent) and `updatecore.ManifestName` / `ManifestNameWithPrefix`.

### Release runner

The release job and every PR workflow (`ci.yml`, `e2e.yml`, `ui-e2e.yml`) run on
GitHub-hosted runners, so untrusted `pull_request` code never executes on the
machine that builds and signs a release. The signing key stays in the `release`
environment, and a `v*` tag build only reads it after the required reviewer
approves the run. The reviewer gate therefore governs *who can produce a signed
release*: keep the `v*` tag ruleset and the required reviewers limited to project
owners.

### Keypair

- **Public key** (`deploy/gotham-signing-key.pub`): safe to publish. It is
  embedded in both binaries, in `deploy/install.sh`, and uploaded as a release
  asset so users can compare it.
- **Private key** (`GOTHAM_UPDATE_SIGNING_KEY` environment secret): the only
  secret. It is never committed or printed. **Back the private key up offline** — if it
  is lost no existing installation can verify a new release and a new keypair
  must be rotated in (which requires re-installing or embedding the new key in
  a manual release). Generate it with:

  ```sh
  go run ./cmd/signer keygen -out signing.key
  # put signing.key (PKCS#8 PEM) in the GOTHAM_UPDATE_SIGNING_KEY release environment secret
  # put signing.key.pub at deploy/gotham-signing-key.pub
  # put the base64 value printed above in GOTHAM_UPDATE_PUBLIC_KEY and in
  # deploy/install.sh (GOTHAM_RELEASE_PUBLIC_KEY_B64) and install-agent.sh
  ```

## Testing the install chain without GitHub

`deploy/test-release-install.sh` builds a signer and snapshot binaries, signs
the manifests, serves them from a loopback fake releases server (including a
`/releases/latest` redirect), and runs `deploy/install.sh` against it under a
scratch `GOTHAM_INSTALL_ROOT`. It covers the pinned-tag path, the default
latest-tag resolution, the agent family, re-install with no operator additions
(the B1 regression), re-install preservation (operator keys and the managed
DSN), and the fail-closed cases (tampered artifact, tampered manifest,
pinned-key mismatch):

```sh
sh deploy/test-release-install.sh
```
