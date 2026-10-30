# Gotham deployment files

Reference for the scripts, systemd units and configs in `deploy/`: what each
one is for, who runs it and the flags that matter. Operator instructions live in
the docs; this page does not repeat them.

| Task | Read |
|---|---|
| Install the control plane or a node | [Install](../docs/guide/install.md), [Add nodes](../docs/guide/nodes.md) |
| Update, roll back, reinstall | [Updates](../docs/guide/updates.md) |
| Panel domain and HTTPS | [Domain and HTTPS](../docs/guide/domain-and-https.md) |
| Node file manager | [Remote file service](../docs/guide/file-service.md) |
| Build and sign a release | [Release and signing](../docs/guide/release-signing.md) |

## Installers

### `install.sh` — control-plane installer

Run by the operator as root, on a fresh Ubuntu 22.04 / Debian host or to
reinstall an existing one. It picks the newest release, verifies the signed
manifest and artifact digest, installs PostgreSQL/Redis (unless pointed at
managed services), the binary, updater and unit, applies migrations, creates the
first owner account, and installs the localhost agent. A re-run preserves
`/etc/gotham/gotham.env` and restarts the service.

Two ways to run the same file:

- **Piped or downloaded alone** — bootstraps: it downloads the signed
  `gotham-deploy-<version>.tar.gz` deploy bundle, verifies it against the
  signed manifest, then runs the bundled installer. Needs `curl`, `openssl` 3,
  `base64`, `mktemp`, `tar`.
- **From a checkout** — runs in place and needs its sibling files in `deploy/`
  (`release-verify.sh`, `install-agent*.sh`, the units and configs).

Flags: `--domain <host>` (or the prompt), `--cp-host <name-or-ip>` (repeatable,
adds gRPC listener SANs), `--file-manager` / `--no-file-manager`, `--dry-run`,
`--help` (prints the file header).

Environment: `GOTHAM_VERSION`, `GOTHAM_RELEASES_URL`, `GOTHAM_BASE_URL`,
`GOTHAM_REPO`, `GOTHAM_DATABASE_DSN`, `GOTHAM_REDIS_ADDR`, `GOTHAM_SKIP_DEPS`,
`GOTHAM_ADMIN_EMAIL`, `GOTHAM_ADMIN_PASSWORD_FILE`.

Test seams (never set on a real host): `GOTHAM_INSTALL_ROOT` (non-`/` sandbox,
enables test mode), `GOTHAM_INSTALL_TEST_PUBLIC_KEY` (only with the root
sandbox), `GOTHAM_INSTALL_TEST=1` (honour OS/agent-env seams, only with
`--dry-run`), `GOTHAM_INSTALL_TEST_RUN_AGENT` + `GOTHAM_INSTALL_TEST_AGENT_SCRIPT`,
`GOTHAM_INSTALL_TEST_ADMIN_BIN` + `GOTHAM_INSTALL_TEST_ADMIN_TTY`.

### `install-agent.sh` — node-agent installer

Run by the operator as root on each node. Installs the agent binary into
`/var/lib/gotham-agent/bin`, writes `/etc/gotham/agent.env`, installs the shared
update wrapper as `gotham-agent-update`, its sudoers rule, the host helper
(`gotham-hostctl` with a five-verb sudoers rule for the agent user) and the
unit, then starts it. Re-running is a safe reinstall: keys the invocation does
not set keep their previous value. It verifies the same signed manifest as the
control plane and **fails closed without a CA**.

Flags: `--ca <path>` (or `GOTHAM_AGENT_CA_FILE`), `--enrollment-token-file
<path>` (first registration), `--insecure` (local development only),
`--full` (install Docker Engine + compose plugin on Ubuntu/Debian),
`--file-manager` / `--no-file-manager`, `--dry-run`.

Key values the installer writes to `agent.env` (a key it does not set keeps the
value a previous run wrote): `GOTHAM_AGENT_CP_ADDR`,
`GOTHAM_AGENT_NODE_ID`, `GOTHAM_AGENT_LISTEN_ADDR`, `GOTHAM_AGENT_CA`,
`GOTHAM_AGENT_CERT_DIR`, `GOTHAM_AGENT_KEY`, `GOTHAM_AGENT_DOCKER_SOCK`,
`GOTHAM_AGENT_LOG_LEVEL`, `GOTHAM_AGENT_AUTO_UPDATE`,
`GOTHAM_AGENT_UPDATE_INTERVAL`, `GOTHAM_AGENT_UPDATE_CHANNEL`,
`GOTHAM_AGENT_ENROLLMENT_TOKEN`, `GOTHAM_AGENT_FILES`.

### `install-agent-lib.sh` — agent installer internals

Sourced by `install-agent.sh`, never executed. Holds the reinstall logic
(`agent_env_write`, `agent_env_validate`, `agent_service_restart`) and the
`--full` Docker setup (`ensure_docker_full`). Fixed production paths are
assigned on source and cannot be redirected from the environment. Exercised by
`test-agent-install.sh`.

### `release-verify.sh` — release verification

Sourced by both installers, never executed. Implements `verify_release`:
manifest signature against the pinned Ed25519 key, manifest binding to the
requested version/architecture/file name, then the artifact SHA-256 against the
signed manifest. Any failure aborts. Installs signal handlers for one
verification and cleans its scratch directory.

### `gotham-signing-key.pub` — release public key

PEM Ed25519 public key, safe to publish. It is embedded in both binaries and in
the installers, and uploaded as a release asset. The private key never lives in
this repository; see `release-key-rotation.md`.

### `release-key-rotation.md` — signing-key runbook

Maintainer runbook for the embedded current + next key ring, planned rotations,
and compromise/loss recovery. Related: `.goreleaser.yaml`,
`.github/workflows/release.yml`, `updatecore/sign.go`.

## Self-update chain

The control plane and the agent use the same layout: the service user owns the
binary and the pending/lock markers; a root-owned wrapper next to a root-owned
config performs the restart and records the durable outcome. sudoers grants the
bare wrapper with **zero arguments**.

| Path | Owner | Writable by service |
|---|---|---|
| `/var/lib/gotham/bin/gotham`, `/var/lib/gotham-agent/bin/gotham-agent` | service user | yes (StateDirectory) |
| `/usr/libexec/gotham/gotham-update`, `gotham-agent-update` | root | no |
| `/etc/gotham/updater.conf`, `/etc/gotham/agent-updater.conf` | root | no |
| `/var/lib/gotham-updater/update.status`, `/var/lib/gotham-agent-updater/update.status` | root | read-only |

### `gotham-update.sh` — privileged restart/healthcheck/rollback wrapper

Installed under two names: `gotham-update` (control plane) and
`gotham-agent-update` (agent). Takes **no arguments**; the name selects the
default config. Root-only steps: restart the fixed unit, probe the loopback
`/healthz`, roll back to the previous binary on failure, write the status.
Exit codes: `0` healthy, `1` rolled back or rollback failed, `2` invalid config.
Under sudo it ignores every `GOTHAM_*` override; the root-owned config is the
only source of paths.

### `gotham-updater.conf`, `gotham-agent-updater.conf` — wrapper config

Installed as `/etc/gotham/updater.conf` and `/etc/gotham/agent-updater.conf`,
`root:root 0644`. Keys: `GOTHAM_BINARY`, `GOTHAM_SERVICE`, `GOTHAM_HEALTH`,
`GOTHAM_TIMEOUT` (30 s), `GOTHAM_STATUS`, `GOTHAM_PENDING`, `GOTHAM_LOCK`. The
control-plane config also carries the release base and staging root used by the
host-helper refresh.

### `gotham.service`, `gotham-agent.service` — systemd units

Installed by the respective installer (or by hand to
`/etc/systemd/system/`). Fixed `ExecStart` on the service-owned binary;
`KillMode=process` keeps the wrapper alive across the restart; the units do not
set `NoNewPrivileges` because they invoke the fixed wrapper with `sudo -n`.

### `install-sudoers.sh`, `install-agent-sudoers.sh` — update-wrapper grants

Root-only, run by the installer or by hand after a manual install:

```sh
sudo deploy/install-sudoers.sh [service-user]        # default: gotham
sudo deploy/install-agent-sudoers.sh [service-user]  # default: gotham-agent
```

Each writes one drop-in (`/etc/sudoers.d/gotham-update` or
`/etc/sudoers.d/gotham-agent-update`) granting the bare wrapper command with an
empty argument list, validates it with `visudo`, and requires the `sudo`
package.

## Host helpers

### `gotham-hostbootstrap.sh` — installs the host helpers

The single installer of `gotham-hostctl`, `gotham-toolctl`, the canonical
wrapper, the root-owned release public key and the fetch helper. Run by
`install.sh` as root, or manually as the recovery/upgrade bridge:

```sh
sudo sh deploy/gotham-hostbootstrap.sh create <control-plane-user> <version> [--allow-downgrade]
sudo sh deploy/gotham-hostbootstrap.sh create-agent <agent-user> <version>
```

Writes fixed root-owned paths under `/usr/libexec/gotham/` and a sudoers drop-in
for the recorded users: the control-plane user gets the five host-control verbs
plus the pinned repair verb, the node agent user gets exactly the five
host-control verbs. `create` and `create-agent` keep each other's grantee, so
the control-plane machine can carry both. Validates sudoers with `visudo`, is
idempotent, refuses symlinked destinations, and records a root-owned high-water
mark so a replayed older signed payload is refused. `apply` reapplies the local
payload; `apply-release <dir> <version>` applies a verified payload; `version-cmp
<a> <b>` prints `-1/0/1`. Test seam: `GOTHAM_HOST_BOOTSTRAP_ROOT` (only without
sudo).

### `gotham-hostbootstrap-fetch.sh` — verified helper-payload fetch

Run by the root update wrapper; usage:

```sh
gotham-hostbootstrap-fetch <release-base> <version> <arch> <key> <dest-dir> <staged-binary>
```

Downloads the manifest, signature and payload into a root-owned `0700`
directory (10 MiB cap), verifies the manifest with the root-owned key, binds the
payload and the manifest to the binary being restarted, and extracts only then.
The signed version, not the caller's `version`, decides what is applied.

### `gotham-hostctl.sh` — host settings helper

Installed root-owned and reached only through its sudoers drop-in. Verbs:
`status`, `apply-network`, `confirm-network`, `revert-network`, `apply-system`.
Values arrive on stdin as `key=value` and are re-validated. Only
systemd-networkd, systemd-resolved and systemd-timesyncd are supported, and it
owns only its `05-gotham.*` drop-ins. A network change is tentative: a systemd
timer reverts it (30–600 s window) unless the operator confirms. `status` also
reports the live default-route interface, hostname, NTP state and timezone;
`apply-system` accepts a `timezone` key (an IANA zone name, checked against
tzdata) in addition to `hostname`, `ntp_enabled` and `ntp_servers`. Test seams:
`GOTHAM_HOSTCTL_ROOT`, `GOTHAM_HOSTCTL_DRYRUN` (only without sudo).

### `gotham-toolctl.sh` — host build-tool installer

Installed root-owned by the agent installer and by `gotham-hostbootstrap.sh`;
the agent calls it through a `verb + tool id` sudoers rule. Verbs:
`install-tool <id>`, `uninstall-tool <id>`. Ids: `railpack`, `buildkit`, `pack`,
`docker`, `git` (Docker is never uninstalled). Downloads are pinned (sha256
constants; BuildKit is the digest-pinned `moby/buildkit:v0.34.0` image); a
mismatch aborts. Runs one job at a time (`flock`, `RuntimeMaxSec=840`) and
re-executes itself through a transient `systemd-run` unit to escape the agent's
read-only sandbox. Exit codes: `0` ok, `1` command failed, `2` usage, `3` tool
not allowed, `4` checksum mismatch, `5` unsupported platform, `6` job already
running. Dry run:
`GOTHAM_TOOLCTL_DRYRUN=1 GOTHAM_TOOLCTL_ARCH=amd64 GOTHAM_TOOLCTL_DISTRO=apt
gotham-toolctl install-tool <id>` (only without sudo).

### `install-toolctl-sudoers.sh` — tool-helper grant

Root-only; run by `install-agent.sh` or by hand:

```sh
sudo deploy/install-toolctl-sudoers.sh [service-user]   # default: gotham-agent
```

Writes `/etc/sudoers.d/gotham-toolctl` with one rule per verb and id, and
validates it with `visudo`. Keep its id lists in sync with
`agent/tools/server.go` and `gotham-toolctl.sh`.

## Development and verification

### `compose.dev.yml`

PostgreSQL 16 + Redis 7 for local development, bound to `127.0.0.1` only with
dev-only credentials:

```sh
docker compose -f deploy/compose.dev.yml up -d
docker compose -f deploy/compose.dev.yml down
```

### `pty-drive.py`

Test utility that drives an interactive child through a pty, waiting for each
prompt before typing. Standard library only. Used by `test-release-install.sh`
for the installer's prompt tests; `--no-ctty` runs the child without a
controlling terminal.

### `verify-chain/main.go`

Small fixture built by `verify-systemd.sh` (or reused via
`GOTHAM_CHAIN_BINARY`): serves the loopback health endpoint and exercises the
update service's `Resume` ordering without a real release.

### `*_test.go` — Go unit tests

`go test ./deploy` covers the dev compose loopback ports (`compose_test.go`),
`gotham-hostctl.sh` parsing/refusals (`hostctl_test.go`) and
`gotham-toolctl.sh` allow-lists, pinned downloads and ordering
(`toolctl_test.go`).

### Shell test suites (no root, no host changes)

| Script | Covers | Needs |
|---|---|---|
| `test-agent-install.sh` | `agent_env_*` reinstall semantics, restart, `--full` Docker setup | `sh`, `grep`, `sed`, `awk`, `stat`, `readlink`, `mktemp` |
| `test-host-bootstrap.sh` | `gotham-hostbootstrap.sh` on a fake root, sudoers validation, refusals | `sh`, `mktemp` (fake `visudo` built in) |
| `test-host-helper-refresh.sh` | signed helper fetch/apply, binary binding, anti-rollback, archive guards | `go`, `openssl`, `curl`, `tar`, `base64` |
| `test-release-install.sh` | sign → serve → verify → install, reinstall, dry-run and prompt paths | `go`, `openssl` 3, `curl`, `python3` |

Run each from the repository root, e.g. `sh deploy/test-release-install.sh`.

### Root-only verification gates

| Script | Run | Covers |
|---|---|---|
| `verify-systemd.sh` | `sudo [GOTHAM_GO=/path/to/go] deploy/verify-systemd.sh` | scratch service + real wrapper: status, rollback, cgroup survival, staged/resume wiring, launch chain |
| `verify-agent-update.sh` | `sudo GOTHAM_GO=/path/to/go sh deploy/verify-agent-update.sh` | fleet agent rollout end to end, tampered asset, signed-but-broken release rollback |

Both use scratch units, users, databases, ports and directories, and clean up on
exit. They need a Linux host with systemd (and Docker for the agent flow); they
are manual merge gates, not CI jobs. `verify-agent-update.sh` also uses the
`gotham-dev-postgres` container (`VERIFY_*` variables in its header override
the defaults).

CI runs `sh -n` over `deploy/*.sh`, `actionlint` over the workflows, and the
four shell suites (`test-release-install.sh`, `test-agent-install.sh`,
`test-host-bootstrap.sh`, `test-host-helper-refresh.sh`) on every PR; the Go
job runs `go test ./deploy` with the rest of the module.

## Known residuals

- **Wrapper lock fallback (LOW).** `acquire_lock` pins the service-owned lock
  inode into the root-owned status directory and opens only that; when no pin
  can be made (missing status directory, no hardlink support) it falls back to a
  checked read-only open, where a FIFO swapped in between check and open can
  block root. Worst case is a self-DoS by an already-compromised service user.
  Durable fix: move the lock into a root-owned directory or the
  `runuser`/`setpriv` handoff.
- **Root rename in the service-owned binary directory (INFO).** `restore`
  renames `<binary>.old` over `<binary>`; every directory on that path is
  service-owned, so a tight path swap can only move service-owned files inside
  service-writable space, never a root-owned source or destination.
- **Root reads the pending marker (INFO).** Used for the version label only; it
  is guarded to a regular, non-symlink file and the value is sanitized.
- **Wrapper dies after restart (LOW).** The update stays `staged` until the next
  service start (which resumes or rolls back) or `gotham update reset`. This is
  deliberate fail-closed behaviour.
