# Installing Gotham

This guide covers installing the control plane and a node agent, first login,
updates and rollback, and the release/signing flow.

## Requirements

- **Control plane host:** Ubuntu 22.04 (or any systemd Linux) with `curl`,
  `openssl` 3, and `coreutils`. The installer provisions PostgreSQL and Redis
  from the distribution packages unless you point it at managed services with
  `GOTHAM_DATABASE_DSN` / `GOTHAM_REDIS_ADDR` (or set `GOTHAM_SKIP_DEPS=1`).
- **Node host:** Linux (`amd64` or `arm64`) with `systemd`, `curl`, `openssl` 3
  and Docker Engine.
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
5. applies the database migrations and starts `gotham.service`;
6. prints the Web UI URL and the first-login steps.

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

```sh
git clone https://github.com/justindeelux/gotham.git
sudo gotham/deploy/install-agent.sh
```

The agent installer uses the same signed-manifest verification
(`deploy/release-verify.sh`) for `gotham-agent-linux-<arch>` and
`gotham-agent-manifest-<arch>.txt`, with the pinned release public key and no
runtime override. It installs the agent into
`/var/lib/gotham-agent/bin/gotham-agent`, the shared wrapper as
`gotham-agent-update`, the config `/etc/gotham/agent-updater.conf`, the
sudoers rule and the unit. Set `GOTHAM_AGENT_CP_ADDR` (control-plane gRPC
address) and `GOTHAM_AGENT_NODE_ID` in the environment before running it; they
are written to `/etc/gotham/agent.env`.

## Updates and rollback

Releases are verified with the public key embedded in the running binary, so no
manual verification is needed at update time.

- **Control plane:** `gotham update check` / `gotham update apply`, or set
  `AUTO_UPDATE=true` in `/etc/gotham/gotham.env` for unattended updates. The
  wrapper health-checks the new binary and rolls back to `gotham.old` on
  failure.
- **Node agent:** agents update from the control plane over the authenticated
  channel; enable unattended applies with `GOTHAM_AGENT_AUTO_UPDATE=true`, or
  trigger a fleet rollout from the UI. A release that fails to activate rolls
  back and its version is backed off (see `deploy/README.md`).
- **Rollback:** `gotham update rollback` restores the previous binary.

Reinstalling a specific version is a normal install with `GOTHAM_VERSION=vX.Y.Z`.

## Release and signing flow

Releases are built and published by `.github/workflows/release.yml` on a `v*`
tag, on the self-hosted runner:

1. The job fails closed unless **both** repository secrets are set and
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
   `/releases/tags/<tag>`), asserts the complete asset set — 4 binaries,
   `checksums.txt`, the public key and all 8 manifest/signature files — uploads
   the manifests and their `.sig` files, then publishes the draft and verifies
   it is no longer a draft. Draft releases are skipped by the checker, so an
   incomplete release is never offered.

The asset naming is a contract with the update code — see
`internal/updates/checker.go` (control plane), `internal/updates/agents.go`
(agent) and `updatecore.ManifestName` / `ManifestNameWithPrefix`.

### Keypair

- **Public key** (`deploy/gotham-signing-key.pub`): safe to publish. It is
  embedded in both binaries, in `deploy/install.sh`, and uploaded as a release
  asset so users can compare it.
- **Private key** (`GOTHAM_UPDATE_SIGNING_KEY` repo secret): the only secret.
  It is never committed or printed. **Back the private key up offline** — if it
  is lost no existing installation can verify a new release and a new keypair
  must be rotated in (which requires re-installing or embedding the new key in
  a manual release). Generate it with:

  ```sh
  go run ./cmd/signer keygen -out signing.key
  # put signing.key (PKCS#8 PEM) in the GOTHAM_UPDATE_SIGNING_KEY repo secret
  # put signing.key.pub at deploy/gotham-signing-key.pub
  # put the base64 value printed above in GOTHAM_UPDATE_PUBLIC_KEY and in
  # deploy/install.sh (GOTHAM_RELEASE_PUBLIC_KEY_B64) and install-agent.sh
  ```

## Testing the install chain without GitHub

`deploy/test-release-install.sh` builds a signer and snapshot binaries, signs
the manifests, serves them from a loopback fake releases server (including a
`/releases/latest` redirect), and runs `deploy/install.sh` against it under a
scratch `GOTHAM_INSTALL_ROOT`. It covers the pinned-tag path, the default
latest-tag resolution, the agent family, re-install preservation, and the
fail-closed cases (tampered artifact, tampered manifest, pinned-key mismatch):

```sh
sh deploy/test-release-install.sh
```