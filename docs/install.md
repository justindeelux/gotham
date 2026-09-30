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
  `sudo` (with `visudo`) and Docker Engine.
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
7. prints the Web UI URL and the first-login steps.

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
provisioned when the resolved DSN is the built-in local default.

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
| `--cp-host <name-or-ip>` | Add a DNS name or IP to the gRPC listener certificate SANs (repeatable). |
| `GOTHAM_GRPC_HOSTS` | Comma-separated SAN hosts (same as `--cp-host`); also overrides the persisted list at runtime. |
| `GOTHAM_INSTALL_ROOT` | Install under a prefix instead of `/` (testing only; non-root; enables test mode). |
| `GOTHAM_INSTALL_TEST_PUBLIC_KEY` | Test-only: replace the pinned trust anchor (PEM or base64); honoured **only** with `GOTHAM_INSTALL_ROOT`, otherwise warned and ignored. |

`deploy/install.sh --dry-run` prints what it would do without changing the host.

## First login

1. Open `http://<host>:8000`.
2. Create an account through the sign-up form and sign in.
3. From **Servers**, install an agent on a node (below); the node then reports
   heartbeats and is visible in the UI.

The first account is a normal account, not a platform administrator. The
platform-global operations (node-wide proxy sync, DNS providers) require the
account email in `PLATFORM_ADMINS` in `/etc/gotham/gotham.env`.

## Node agent

Copy the control plane's public CA certificate to the node first (see
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
CA check and leaves `GOTHAM_AGENT_CA` unset, so the agent connects without TLS.
Never use it on a real node.

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
| `--dry-run` | Print what would be done; makes no change. |
| `GOTHAM_AGENT_CA` | The agent-side path the installer writes (`/etc/gotham/ca.crt`). It is **not** read from the ambient environment; on a reinstall the installer keeps the value already in `agent.env`. Use `--ca`/`GOTHAM_AGENT_CA_FILE` to change it. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | Release channel this node accepts, `stable` (default) or `beta`; an offer with a different or empty channel is refused. |

`GOTHAM_AGENT_CP_ADDR` must use a name or IP that is one of the control plane's
listener SANs (see [Control plane](#control-plane)); otherwise the TLS handshake
fails and the node never registers.

## Updates and rollback

Releases are verified with the public key embedded in the running binary, so no
manual verification is needed at update time.

- **Control plane:** `gotham update check` / `gotham update apply`, or set
  `AUTO_UPDATE=true` in `/etc/gotham/gotham.env` for unattended updates. The
  wrapper health-checks the new binary and rolls back to `gotham.old` on
  failure. Run the CLI as the service user that owns the binary
  (`sudo -u gotham /var/lib/gotham/bin/gotham update apply`): run as root it
  would install a root-owned binary over the service-owned one and the next
  service-run apply would fail on the hardlink backup. `gotham update` refuses
  a mismatched owner.
- **Node agent:** agents update from the control plane over the CA-verified TLS
  channel; enable unattended applies with `GOTHAM_AGENT_AUTO_UPDATE=true`, or
  trigger a fleet rollout from the UI. A release that fails to
  activate rolls back and its version is backed off (see `deploy/README.md`).
- **Rollback:** `gotham update rollback` restores the previous binary on disk.
  The running process keeps the current binary until it is restarted, so run
  `systemctl restart gotham` afterwards to actually execute the restored
  binary.

Reinstalling a specific version is a normal install with `GOTHAM_VERSION=vX.Y.Z`.

## Release and signing flow

Releases are built and published by `.github/workflows/release.yml` on a `v*`
tag, on the self-hosted runner. The release job targets the GitHub **`release`
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

### Residual: the release runner is shared with PR CI

The `release` environment's required reviewer gates **who can trigger the
release job**, not what a compromised host can do: the self-hosted runner is the
same host that runs `pull_request` CI (`ci.yml`, `e2e.yml`, `ui-e2e.yml`), which
executes untrusted code. Environment secret isolation and SHA-pinned actions
raise the bar, but if any untrusted workflow run ever executes on that runner,
treat `GOTHAM_UPDATE_SIGNING_KEY` as compromised: **rotate the key and
re-release**, and plan to move the release job to an isolated/ephemeral runner
before the next public release. See `deploy/README.md` (Known residuals).

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