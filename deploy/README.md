# Gotham deployment

| File | Purpose |
|---|---|
| `install.sh` | Signed one-line control-plane installer (INFRA-9.1) |
| `release-verify.sh` | Shared signature + digest verification for the installers |
| `gotham-signing-key.pub` | Published release signing public key (embedded in the binaries and installers) |
| `test-release-install.sh` | Local (no GitHub) dry run of the sign → serve → verify → install chain |
| `gotham.service` | Control-plane systemd unit |
| `gotham-update.sh` | Privileged restart/healthcheck/rollback wrapper (shared; installed as `gotham-update` for the CP and `gotham-agent-update` for the agent) |
| `gotham-updater.conf` | Root-owned wrapper configuration (install to `/etc/gotham/updater.conf`) |
| `install-sudoers.sh` | Grants the service user the wrapper (and nothing else) |
| `verify-systemd.sh` | Root-only integration check for a Linux host with systemd |
| `verify-agent-update.sh` | Root-only end-to-end check of the agent remote-update flow (BE-9.2) on a Linux/systemd/Docker host |
| `gotham-agent.service`, `install-agent.sh` | Node agent (installer verifies the signed manifest) |
| `gotham-agent-updater.conf` | Root-owned agent wrapper configuration (install to `/etc/gotham/agent-updater.conf`) |
| `install-agent-sudoers.sh` | Grants the agent user the agent wrapper (and nothing else) |
| `compose.dev.yml` | Local PostgreSQL + Redis |

## Control-plane self-update layout

The self-update chain splits privileges so that compromising the control plane
cannot escalate to root or redirect a root write:

| Path | Owner | Writable by `gotham` | Why |
|---|---|---|---|
| `/var/lib/gotham/bin/gotham` | `gotham` | yes | Fixed `ExecStart` and swap target (`StateDirectory`). |
| `/var/lib/gotham/bin/gotham.old` | `gotham` | yes | Hardlink to the last known good binary. |
| `/var/lib/gotham/update.pending` | `gotham` | yes | Staged-update marker that gates a second apply. |
| `/var/lib/gotham/update.lock` | `gotham` | yes | Lock shared with the wrapper. |
| `/usr/libexec/gotham/gotham-update` | root | **no** | Privileged wrapper (no arguments). |
| `/etc/gotham/updater.conf` | root | **no** | Fixed binary/service/health/status/pending/lock. |
| `/var/lib/gotham-updater/update.status` | root | **no** (read-only) | Authoritative outcome; the root wrapper writes it in a root-owned directory, so a symlink planted by the service user cannot redirect the write. |

The unit runs `/var/lib/gotham/bin/gotham serve`, keeps `KillMode=process` so
the wrapper survives the restart cgroup, and does **not** set
`NoNewPrivileges` because the control plane invokes the fixed root-owned wrapper
with `sudo -n`. sudoers grants exactly:

```
gotham ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-update ""
```

The trailing `""` pins the wrapper to **zero arguments** (`sudoers(5)`: a
command with no argument list permits any arguments; `""` permits none). The
wrapper also refuses to run with arguments and, when `SUDO_USER`/`SUDO_UID` is
set, ignores every `GOTHAM_*` override, so `env_reset` and `!SETENV` remain a
hard requirement.

## Install

`deploy/install.sh` automates the layout below for a clean systemd host:
download → verify the signed manifest and artifact digest → install the binary,
wrapper, config, status directory, sudoers rule and unit → migrate → start. It
uses the shared `release-verify.sh`; see `docs/install.md`.

The manual layout it produces is:

```sh
# 1. Install the binary, the wrapper, the config and the root-owned status dir.
install -d -o gotham -g gotham -m 0755 /var/lib/gotham/bin
# The binary MUST be owned by the service user: the updater hardlinks it to
# <binary>.old, and fs.protected_hardlinks=1 (the default on Debian/Ubuntu/
# Fedora) makes os.Link of a root-owned file fail with EPERM.
install -m 0755 -o gotham -g gotham gotham /var/lib/gotham/bin/gotham
install -d -m 0755 -o root -g root /usr/libexec/gotham
install -m 0755 gotham-update.sh /usr/libexec/gotham/gotham-update
install -m 0644 gotham-updater.conf /etc/gotham/updater.conf
install -d -m 0755 -o root -g root /var/lib/gotham-updater

# 2. Install the unit and the sudoers rule.
install -m 0644 gotham.service /etc/systemd/system/gotham.service
sudo deploy/install-sudoers.sh gotham
systemctl daemon-reload && systemctl enable --now gotham
```

`util-linux` (`flock`) is required by the wrapper; without it the wrapper fails
closed and refuses to update rather than running unserialized.

## Certificate authority (mTLS)

`install.sh` provisions the gRPC certificate authority with `gotham ca init`,
which writes `ca.crt` + `ca.key` (`0600`) into `GOTHAM_CA_DIR`
(`/var/lib/gotham/ca`, owned by the `gotham` service user) and is idempotent — a
reinstall keeps the existing CA. `gotham serve` loads it and the gRPC gateway
runs TLS, presenting a server certificate signed by the CA and accepting a client
certificate when one is presented. Without a CA (only when an operator runs
`gotham serve` directly) the gateway falls back to plaintext and logs a warning.

The listener certificate SANs are the operator-declared hosts
(`install.sh --cp-host <name-or-ip>` repeated, or `GOTHAM_GRPC_HOSTS=<a,b>`)
persisted as `<GOTHAM_CA_DIR>/hosts`, plus the bind-address host, the loopback
names and the machine hostname. A remote agent must dial one of these SANs or the
handshake fails closed; add the control plane's public name/IP at install time.
`gotham ca init --host <name-or-ip>` sets the list directly, and
`GOTHAM_GRPC_HOSTS` in the service environment overrides it at runtime.

To add a node, copy `ca.crt` (never `ca.key`) to it and install the agent with
`--ca`:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
sudo deploy/install-agent.sh --ca ./ca.crt
```

`install-agent.sh` fails closed without a CA; `--insecure` is the
development-only override that leaves the agent channel in plaintext.

Re-running `install.sh` on a pre-existing plaintext control plane flips it to
TLS; agents installed before that go offline until they are reinstalled with
`--ca` (see `docs/install.md`, "Upgrading an existing control plane").


## Outcome, gate and recovery

- The control plane writes `staged` to `/var/lib/gotham/update.pending`, swaps
  the binary (hardlinking the previous one to `<binary>.old`, so the fixed
  `ExecStart` is never absent), then runs the wrapper.
- While the pending marker is `staged`, a second **Apply is refused**
  (`409`/`ErrUpdatePending`); the wrapper removes the marker once it records the
  final `ok` / `rolled_back` / `rollback_failed` / `no_backup` status in the
  root-owned status file.
- If the wrapper cannot be launched, the control plane rolls back and records
  `rolled_back` (or `rollback_failed`); a wrapper that exits without recording
  its own outcome (`monitorRestart`) is treated the same way, so the unproven
  binary is never left armed and the next apply cannot lose the known-good
  backup.
- If the wrapper cannot acquire the update lock it fails closed, records
  `wrapper_failed` in the root-owned status, and exits nonzero; the pending
  marker is left in place so the gate stays closed and the control plane's
  monitor rolls back.
- If the host crashes or reboots during the health window, the next startup sees
  a `staged` marker and relaunches the wrapper once (marker rewritten to
  `resuming`, so it never loops); the new binary is then health-checked or
  rolled back.
- A crash *before* the commit rename leaves target and `<binary>.old` as the
  same inode; startup recovery clears the marker so the gate reopens.
- **systemd start rate limit.** A new binary that starts but crash-loops (for
  example a broken build) makes systemd trip its start rate limit and mark the
  unit failed, after which `systemctl restart` is refused with "Start request
  repeated too quickly". Before every restart the wrapper clears that state with
  `systemctl reset-failed "<service>"` (best-effort), so the rollback restart
  still runs the restored binary and the outcome is `rolled_back`, not
  `rollback_failed` with the service left down. The shared wrapper therefore
  covers the control plane and the agent identically.
- `GET /api/v1/updates/check` returns the outcome; release notes and the
  wrapper's `detail` (which can contain paths) are only shown to a platform
  operator.
- Operator reset for a stale `staged`/`resuming` marker (for example after a
  hard kill): `gotham update reset` or `rm -f /var/lib/gotham/update.pending`.
- Run `gotham update apply`/`rollback` as the service user
  (`sudo -u gotham /var/lib/gotham/bin/gotham update apply`). Run as root, the
  swap installs a `root:root 0755` binary over the service-owned one and the
  next service-run apply fails at the hardlink backup (`fs.protected_hardlinks`
  EPERM); the CLI refuses a mismatched binary owner with a clear message.
  `gotham update rollback` only restores the file — the running process keeps
  the current binary until `systemctl restart gotham`.
- **Failed-release backoff (`AUTO_UPDATE`).** The scheduled check/apply loop
  mirrors the agent's durable backoff: a `rolled_back`/`rollback_failed` status
  for a newer version seeds a persisted failed-attempt count
  (`/var/lib/gotham/update.backoff`), so the loop does not re-download and
  re-apply the same release on every interval and restart the control plane in a
  loop. The delay escalates 5m, 10m, 20m, 40m, then caps at 1h; a **newer**
  release is offered immediately, and `gotham update reset` clears the backoff
  for an operator-forced retry.
- Crash recovery: the target is never absent, but if an older version left it
  missing, both `Applier.Recover` (startup) and the wrapper restore
  `<binary>.old` before starting the service.

## Known residuals

- **`KillMode=process` applies to every stop.** Control-plane children such as
  `git`/`ssh` are no longer killed by `systemctl stop`/`restart` and systemd may
  report left-over processes. This is required so the wrapper survives the
  update restart. Upgrade path: have the wrapper re-exec itself into a transient
  scope (`systemd-run --unit=gotham-update-<id> --collect`) and restore the
  default `control-group` mode.
- **Root `mv` inside the gotham-owned binary directory.** `restore` performs a
  root rename of `<binary>.old` over `<binary>` in `/var/lib/gotham/bin`, which
  the service user owns; a very tight race could swap the `bin` component for a
  symlink between the rename lookups. The hardlink+rename Go swap is not
  affected. Upgrade path: perform the restore as the service user via
  `runuser`/`setpriv`.
- **Root reads the gotham-owned pending marker** (for the version label only).
  It is guarded to a regular, non-symlink file and the value is sanitized; the
  marker is also read by the control plane. Upgrade path: the same
  `runuser`/`setpriv` handoff.
- **If only the wrapper dies during the health window (LOW).** A wrapper that is
  killed or OOM-killed (but not a host crash) after it restarted the unit leaves
  the update `staged`: the old process's monitor died with the restart and the
  new process already skipped `Resume` while the wrapper held the lock. The
  unproven binary keeps serving until the next restart (when startup `Resume`
  fires) or `gotham update reset`. It fails closed and self-heals on restart.
- **Check-then-open TOCTOU in the wrapper (LOW).** The wrapper checks
  `[ -L ]`/`[ -f ]` before opening the lock and reading the pending marker, so a
  service user that swaps in a symlink between the check and the open could make
  root open a FIFO (the wrapper hangs, the gate stays `staged`) or a device node.
  The opens are read-only, so nothing is truncated; the leak is at most a
  sanitized 64-character `version=` line. Worst case is a self-DoS by an
  already-compromised service user. Upgrade path: the same `runuser`/`setpriv`
  handoff.
- **systemd start-limit tuning (LOW).** `Restart=always` + `RestartSec=5` means a
  crash-looping new binary can trip the default start rate limit during the
  wrapper's health window; the wrapper now clears it with `reset-failed`, so the
  rollback is correct. Operators who prefer the unit to give up sooner (or later)
  can tune `StartLimitIntervalSec`/`StartLimitBurst`/`RestartSec` in the unit;
  the wrapper does not depend on the exact values.
- **The release runner is shared with PR CI (HIGH, carried).** `release.yml` runs
  on the same self-hosted runner as `ci.yml`/`e2e.yml`/`ui-e2e.yml`, which
  execute `pull_request` code. The `release` environment's required reviewer
  gates *who can trigger the release job*, not host compromise: a persistent
  runner reached by untrusted code can tamper with the signed bytes or exfiltrate
  `GOTHAM_UPDATE_SIGNING_KEY`. If any untrusted run ever executes on that runner,
  rotate the signing key and re-release; move the release job to an
  isolated/ephemeral runner before the next public release. Actions are
  SHA-pinned and the sqlc download is checksum-verified to narrow this surface.

## Node-agent self-update layout

The agent reuses the same hardened wrapper, installed under the agent name
(`gotham-agent-update`) so it selects `/etc/gotham/agent-updater.conf` by
default and fails closed if that root-owned file is missing (it must never fall
back to the control-plane defaults). The privilege split is the same:

| Path | Owner | Writable by `gotham-agent` | Why |
|---|---|---|---|
| `/var/lib/gotham-agent/bin/gotham-agent` | `gotham-agent` | yes | Fixed `ExecStart` and swap target (`StateDirectory`). |
| `/var/lib/gotham-agent/bin/gotham-agent.old` | `gotham-agent` | yes | Hardlink to the last known good binary. |
| `/var/lib/gotham-agent/update.pending` | `gotham-agent` | yes | Staged-update marker that gates a second apply. |
| `/var/lib/gotham-agent/update.lock` | `gotham-agent` | yes | Lock shared with the wrapper. |
| `/var/lib/gotham-agent/update.retry` | `gotham-agent` | yes | Operator retry marker: a running agent clears its failed-update backoff when it appears. |
| `/var/lib/gotham-agent/update.backoff` | `gotham-agent` | yes | Persisted failed-attempt count so the seeded backoff escalates across restarts. |
| `/usr/libexec/gotham/gotham-agent-update` | root | **no** | Privileged wrapper (no arguments). |
| `/etc/gotham/agent-updater.conf` | root | **no** | Fixed binary/service/health/status/pending/lock. |
| `/var/lib/gotham-agent-updater/update.status` | root | **no** (read-only) | Authoritative outcome. |

The agent unit keeps `KillMode=process` (the wrapper must survive the restart
cgroup) and does **not** set `NoNewPrivileges` (the agent invokes the fixed
wrapper with `sudo -n`). sudoers grants exactly:

```
gotham-agent ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-agent-update ""
```

The agent serves a loopback liveness endpoint (`GOTHAM_AGENT_HEALTH_ADDR`,
default `127.0.0.1:8001/healthz`) that the wrapper probes after a restart. It
reports process liveness only, never the control-plane connection, so a node
whose CP is temporarily unreachable is not rolled back.

Install (see `install-agent.sh`):

```sh
install -d -o gotham-agent -g gotham-agent -m 0755 /var/lib/gotham-agent/bin
install -m 0755 -o gotham-agent -g gotham-agent gotham-agent /var/lib/gotham-agent/bin/gotham-agent
install -m 0755 deploy/gotham-update.sh /usr/libexec/gotham/gotham-agent-update
install -m 0644 deploy/gotham-agent-updater.conf /etc/gotham/agent-updater.conf
install -d -m 0755 -o root -g root /var/lib/gotham-agent-updater
sudo deploy/install-agent-sudoers.sh gotham-agent
```

The same residuals as the control-plane wrapper apply (root `mv` inside the
agent-owned `bin`, root read of the agent-owned pending marker, check-then-open
TOCTOU, wrapper-death window); the upgrade path is the same `runuser`/`setpriv`
handoff.

### Failed-release backoff and operator retry

A release that fails to activate is rolled back and the root-owned status
records `rolled_back`/`rollback_failed` with its version. The agent seeds a
durable-status backoff at startup and persists the failed-attempt count
(agent-owned `update.backoff`), so the seeded delay escalates across the wrapper
restarts that follow each rollback — 5m, 10m, 20m, 40m, then capped at 1h —
instead of a flat retry, and a wrapper restart does not immediately re-apply the
same broken release and crash-loop while the rollout target is unchanged. A
**newer** release is applied automatically; only retrying the *same* version
needs the operator path:

```sh
sudo gotham-agent update reset   # clears the pending marker, the status (as root),
                                 # the agent-owned retry marker and the attempt count
```

A running agent consumes `update.retry` on its next poll and clears its
in-memory backoff; a non-root `gotham-agent update reset` still works through
that marker (it just cannot remove the root-owned status file). The command
resolves paths from the process environment, then `/etc/gotham/agent.env` (the
systemd `EnvironmentFile`; `GOTHAM_AGENT_ENV_FILE` overrides the path), then the
built-in defaults, so a service with custom paths is reset correctly. The retry
marker is written with a temp-file + rename and the mode is set on the file
descriptor, so a symlink or FIFO planted by the service user in its own directory
cannot redirect or block a root run.

Keep `GOTHAM_AGENT_UPDATE_RETRY`/`_BACKOFF` **directly in the agent's
StateDirectory** (`/var/lib/gotham-agent`). A nested path (for example
`…/gotham-agent/state/update.retry`) would put an intermediate directory the
service user can replace with a symlink on the write path; the reset CLI refuses
a symlinked immediate parent, but a plain nested directory is not otherwise
special-cased. Quoted values in `agent.env` (`KEY="value"`) are unquoted on
read.

## Linux/systemd verification

`sudo deploy/verify-systemd.sh` runs a scratch service and the real wrapper on a
systemd host. See the script header and the BE-9.1 report for exactly what it
proves.

CI covers the installer chain on every PR: the `installer` job in
`.github/workflows/ci.yml` runs `sh -n` over `deploy/*.sh`, `actionlint` over the
workflows, and `deploy/test-release-install.sh` (the checkout-level chain with a
loopback fake releases server and no host changes). The two scripts that need a
real systemd host and root — `deploy/verify-systemd.sh` and
`deploy/verify-agent-update.sh` — stay **manual** merge gates, run on the test
box with their logs attached to the milestone report.

## Agent remote-update verification (BE-9.2)

`sudo GOTHAM_GO=/path/to/go sh deploy/verify-agent-update.sh` is the merge gate
for the agent remote-update flow. It is entirely scratch — scratch systemd units
(`gotham-agent-verify-<pid>-a/b`), a scratch user, a scratch database
(`<base>_verify_<pid>`), a high scratch port range and a scratch directory — and
cleans all of it up on EXIT/INT/TERM. It never touches a real
`gotham`/`gotham-agent` unit, database, certificate or binary.

It builds the control plane and two agent versions from this repository with the
release Ed25519 public key **embedded** via `-ldflags` (the dev env override is
not used), signs a `v2.0.0` agent release with `cmd/signer`, serves it from a
loopback `python3 -m http.server`, runs a scratch control plane and two scratch
agents as systemd units (with the shared wrapper, its config and a scratch
sudoers rule), and drives the operator API. Postgres is reached through the
`gotham-dev-postgres` container (`VERIFY_DATABASE_URL` overrides the DSN).

Checks (the reviewer's three merge confirmations plus the target/negative cases):

- **C1** `update-all` with the control plane already on the newest version starts
  a rollout anyway (the target is the agent release family, not the CP version).
- **C2** both agents restart through the root-owned wrapper and their status
  files record `ok`, keeping the previous binary as `gotham-agent.old`.
- **C3** both heartbeats converge on the new version and the planted install
  directories are otherwise byte-identical (md5+mtime, excluding exactly the
  binary, backup, pending/lock/retry/backoff markers and status files).
- **C4** the control plane resolves the agent release family (`v2.0.0`).
- **NEG1** a tampered asset (manifest digest mismatch) leaves the agents on the
  old version.
- **NEG2** a validly signed but broken release rolls back and records
  `rolled_back`, leaving the agents on the old version.

A tampered **manifest** is rejected at the control plane before any agent sees
it, and a tampered **digest** is rejected at the agent before the swap, so those
two paths are covered by the `internal/updates` and `agent` unit tests
(`TestAgentUpdaterRejectsTamperedManifest`, `TestAgentUpdaterRejectsDigestMismatch`)
and by NEG1 end to end; the wrapper rollback status is covered by NEG2.
