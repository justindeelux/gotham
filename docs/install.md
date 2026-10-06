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
   same release tag; node id `<hostname>-agent`), unless `--no-local-agent`
   is given or the existing `/etc/gotham/agent.env` already points at a
   remote control plane (that agent is left untouched: its `ca.crt` and
   config are never overwritten with the local ones);
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
| `--no-local-agent` / `GOTHAM_NO_LOCAL_AGENT=1` | Skip the localhost agent install (remote-only control plane). |
| `GOTHAM_AGENT_NODE_ID` | Node id for the localhost agent (default `<hostname>-agent`); also honoured by direct `install-agent.sh` runs. |
| `GOTHAM_AGENT_CP_ADDR` | Dial address for the localhost agent (default `127.0.0.1:9442`); also honoured by direct `install-agent.sh` runs. |
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
first node, `<hostname>-agent`); the steps below add *further* nodes. Copy
the control plane's public CA certificate to the node first (see
[Control plane](#control-plane)), then run the installer:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
git clone https://github.com/justindeelux/gotham.git
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

The installer **fails closed** without a CA: it refuses to run unless `--ca
<path>` (or `GOTHAM_AGENT_CA_FILE=<path>`) is given, so a node never dials the
control plane in plaintext. It installs the certificate as `/etc/gotham/ca.crt`
(root-owned, `0644`) and writes `GOTHAM_AGENT_CA=/etc/gotham/ca.crt` to
`/etc/gotham/agent.env`, which makes the agent verify the control plane over TLS.
`--insecure` is the documented **local-development-only** override: it skips the
CA check, writes `GOTHAM_AGENT_INSECURE=true`, and leaves `GOTHAM_AGENT_CA`
unset, so the agent connects without TLS. The agent itself now fails closed:
with no CA it refuses to start unless `GOTHAM_AGENT_INSECURE=true`, and a
plaintext listener is confined to loopback (the installer defaults the address
to `127.0.0.1:9443`). Never use it on a real node.

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
created. `install.sh` runs the same agent checks, but only when the
localhost agent step will actually run: a skipped step (unsupported
platform, remote `agent.env`, `--no-local-agent`) is never blocked by agent
values, while the DSN/Redis gate (control characters, a trailing backslash,
quotes) always runs.

`GOTHAM_AGENT_NODE_ID` must not name the control plane itself. The CP refuses to
register its own listener identities (its bind host, the loopback names, its
machine hostname and every `GOTHAM_GRPC_HOSTS` entry, compared after
canonicalization), because accepting one would issue a certificate that
impersonates the control plane. On a **co-located** install — the agent on the
same host as the CP — the agent's default node id is that hostname, so set an
explicit distinct value:

```sh
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=worker-1 \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

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