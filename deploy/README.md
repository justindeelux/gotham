# Gotham deployment

| File | Purpose |
|---|---|
| `install.sh` | Signed one-line control-plane installer (INFRA-9.1) |
| `release-verify.sh` | Shared signature + digest verification for the installers |
| `gotham-signing-key.pub` | Published release signing public key (embedded in the binaries and installers) |
| `release-key-rotation.md` | Runbook: the embedded current + next key ring, planned rotation, compromise/loss recovery |
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
certificate when one is presented. A directory holding exactly one of
`ca.crt`/`ca.key` is an **incomplete CA** and is refused at startup rather than
silently downgraded to plaintext, and a `ca.key` readable by group or others is
refused too (it signs every agent certificate). With no CA at all, serve refuses
to start unless the operator sets `GOTHAM_GRPC_INSECURE=true` (development only);
only then does the gateway run plaintext and log a warning.

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

The control plane installer already did this for the local host: `install.sh`
installs and starts the agent on the same machine by default (same release
tag, `--full` so Docker Engine and the compose plugin are set up too) and the
agent registers itself as the first node — the Servers page lists it once its
heartbeats land. Pass `--no-local-agent` (or `GOTHAM_NO_LOCAL_AGENT=1`) for a
remote-only control plane.

First registration of the local node is server-authenticated TLS: the agent
verifies the control plane against the provisioned CA, but the control plane
does not yet verify the agent's client credential on first contact (known
limitation; see "The agent channel is not mutual yet" below). `<host>-agent`
is only the default node id, not a reserved one: any id other than the control
plane's own listener identities (its hostname and addresses) is accepted. Set
`GOTHAM_AGENT_NODE_ID` on a fresh install to pick another id; re-running
`install.sh` never repoints an existing `agent.env` (a hostname change is
kept as-is, so no duplicate node appears). When that `agent.env` points at a
remote control plane, the localhost step is skipped entirely, so a re-run
never overwrites that host's `ca.crt` or agent config with the local ones.
If the local agent step fails on a supported platform, the control plane is
left installed and running (nothing is rolled back) and the installer exits
nonzero with the exact agent-only retry command (values shell-quoted, so it
re-runs as shown).

`install-agent.sh` fails closed without a CA; `--insecure` is the
development-only override that leaves the agent channel in plaintext. The agent
itself also fails closed: with no CA it refuses to start unless
`GOTHAM_AGENT_INSECURE=true`, and its plaintext listener is confined to
loopback.

`GOTHAM_AGENT_NODE_ID` must differ from the control plane's own listener
identities (its hostname and addresses): the CP refuses to register those, so a
co-located install must set an explicit distinct node id. See `docs/install.md`,
"Node agent".

Re-running `install-agent.sh` is a safe reinstall: values the invocation does not
set (for example the control plane address and node id) are kept from the
existing `agent.env`, operator-added keys are preserved, and a running agent is
restarted onto the newly installed binary. Pass a value again (flag or
environment) to override it. Upgrading from an older installer: the quote
rejection below also applies to values preserved from an existing `agent.env`,
so a node id containing a quote written by an older installer now blocks the
upgrade with a clear error naming the key — remove the quote from `agent.env`
and re-run.

Every value written to `agent.env` is validated before the installer changes
anything (service user, directories, binary, CA): control characters and
trailing backslashes are rejected everywhere, the node id mirrors the
agent's own gate (at most 253 bytes; no spaces, tabs, `*`, `/` or
backslashes) and additionally refuses quotes (systemd's EnvironmentFile
parser strips a leading quote and unquotes a balanced pair, so the unit
would see a different value than the installer wrote — proven on Ubuntu
22.04), `CERT_DIR`/`KEY`/`CA` must be absolute paths without
whitespace, quotes or backslashes, the docker endpoint must be
`unix:///abs/path`, `/abs/path` or `tcp://host:port`, the dial and listen
addresses must be `host:port` without spaces, quotes or `=`, auto-update
accepts only `true`/`false` (the agent only honours `true`), and the
interval must match Go durations (surrounding whitespace trimmed as the
agent trims it; day/week units and overflowing values Go errors on are
refused) while the channel must match channel names. `install.sh` runs the
same agent checks — but only when the localhost agent step will actually
run (the distro/arch/systemd/remote-`agent.env` skip decisions are computed
before the first mutation, so a value the skipped step would never use
cannot abort the control-plane install) — plus the DSN/Redis gate (control
characters, a trailing backslash, quotes), which always runs before the first
mutation, so a bad value fails the run with nothing created; a failing agent
step itself still keeps the control plane. `GOTHAM_DATABASE_DSN` and
`GOTHAM_REDIS_ADDR` get the same control-character check before they go into
`gotham.env`. Upgrading from an older installer: the DSN/Redis gate also
applies to values preserved from an existing `gotham.env`, so a DSN with
quotes or a trailing backslash written by an older installer now blocks the
upgrade with a clear error naming the key — fix the value in `gotham.env`
(keyword DSNs with quoted values must use the URL form) and re-run.

`install-agent.sh --full` (or `GOTHAM_AGENT_FULL=1`) also installs Docker
Engine and the compose plugin from the official Docker apt repository before
anything else. It is off by default and on for the localhost agent the
control-plane installer sets up. The repository key is fingerprint-pinned
before use and every package is apt-verified; on non-Ubuntu/Debian distros it
fails with the manual step instead of guessing. Re-running it is a no-op when
`docker compose version` already works.

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
- If the status directory is not an existing, non-symlink, root-owned directory
  the wrapper refuses to record there and falls back to the checked lock open
  (the residual above). With a usable directory the lock failure above records
  `wrapper_failed` and leaves the marker `staged`; with an unusable one, if the
  fallback open fails the wrapper exits nonzero with no status recorded and
  again leaves a regular `staged` marker until the monitor or the next startup
  resolves it (fail closed), while if it succeeds the update proceeds, removes
  the pending marker and exits 0 with no root-owned status record. Either way
  the wrapper never performs a root write into a service-writable directory.
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

- **`KillMode=process` applies to every stop (children handled in Go).**
  `KillMode=process` is required so the wrapper survives the update restart, so
  systemd itself does not kill control-plane children (`git`/`ssh`) on
  `systemctl stop`/`restart`. The cloner compensates: it runs `git` in its own
  process group and kills the whole group when the clone context is cancelled
  (step timeout, deploy cancellation, graceful service shutdown), and on Linux
  it sets `PR_SET_PDEATHSIG` (SIGKILL) on the forking thread, so a hard
  `SIGKILL`/OOM of the control plane still kills `git` even though no Go code
  runs. Residual: after a hard kill, a grandchild that escaped git's process
  group (`setsid`) can outlive the service until its own socket timeouts. Only
  the cgroup path closes that completely: re-exec the wrapper into a transient
  scope (`systemd-run --unit=gotham-update-<id> --collect`) and restore the
  default `control-group` mode — a root/dbus workflow that stays the upgrade
  path. Guards: `TestRunGitCancelKillsProcessGroup` and
  `TestRunGitChildDiesWithItsParent` (`internal/deploy/cloner_test.go`).
- **Root `mv` inside the gotham-owned binary directory (INFO).** `restore`
  performs a root rename of `<binary>.old` over `<binary>` in the service-owned
  `bin` directory, so a tightly timed path swap could make root rename a
  different pair of files. `rename(2)` requires write + search permission on
  both parent directories, and every directory on that path is owned by the
  service user, so a successful rename can only move service-user files inside
  service-writable space; a destination in a root-owned directory fails with
  `ENOENT`/`EACCES`, and no root-owned file can become the source. The status
  write is not exposed: `write_status` and `acquire_lock` refuse unless
  `GOTHAM_STATUS`'s directory is an existing, non-symlink, **root-owned**
  directory (runtime `-O` check in `status_dir_usable`), matching
  `/var/lib/gotham-updater` root:root 0755 — created by `deploy/install.sh`
  (which runs as root), outside the unit's `ReadWritePaths` and unreachable
  through the sudoers grant. The theoretical upgrade path for the rename itself
  remains performing it as the service user (`runuser`/`setpriv`).
- **Root reads the gotham-owned pending marker** (for the version label only).
  It is guarded to a regular, non-symlink file and the value is sanitized; the
  marker is also read by the control plane. Upgrade path: the same
  `runuser`/`setpriv` handoff.
- **If only the wrapper dies during the health window (LOW).** A wrapper that is
  killed or OOM-killed (but not a host crash) after it restarted the unit leaves
  the update `staged`: the old process's monitor died with the restart and the
  new process already skipped `Resume` while the wrapper held the lock. The
  unproven binary keeps serving until the next restart (when startup `Resume`
  fires) or `gotham update reset`. This is deliberate fail-closed behavior and
  must not change. Guards: `TestResumeStagedRelaunches` and
  `TestResumeStagedLaunchErrorRollsBack` (`updatecore/applier_test.go`) prove a
  later startup resumes or rolls back, `TestResumeStagedSkipsWhileLockHeld`
  (`updatecore/resume_unix_test.go`) proves a live wrapper is never awaited,
  `TestMonitorRestartRollsBack` / `TestWrapperFailedPreservesKnownGood` prove a
  wrapper that dies without recording cannot arm the unproven binary, and
  `TestServiceAutoUpdateBacksOffRolledBackRelease` / `TestUpdateBackoff*`
  (`internal/updates/backoff_test.go`) prove the AUTO_UPDATE loop does not
  re-apply the same failed release.
- **Wrapper lock open is pinned (LOW, reduced).** `acquire_lock` no longer
  opens the service-swappable lock path directly: `ln` hardlinks whatever inode
  the path resolves to into the root-owned status directory (`link(2)` never
  opens the inode, so a FIFO swapped in cannot block it), the pinned name is
  required to be a regular, non-symlink file that still matches the lock path,
  and only that name is opened read-only. The pinned name cannot be swapped out
  of a root-owned directory, and locking the hardlink locks the same inode the
  control plane locks. A post-`flock` check refuses when the lock path was
  replaced while root waited, and each run first sweeps pins whose embedded PID
  is no longer alive (or is its own PID, i.e. PID reuse), so a pin left by a
  hard-killed wrapper cannot silently degrade a later run to the fallback.
  Residual: when no pin can be made (missing status
  directory, hardlinks unsupported), the wrapper falls back to the previous
  checked read-only open, where a FIFO swapped in between the check and the open
  can still block root; the worst case stays a self-DoS by an
  already-compromised service user, and the durable fix is moving the lock into
  a root-owned directory or the `runuser`/`setpriv` handoff.
- **systemd start-limit tuning (LOW).** `Restart=always` + `RestartSec=5` means a
  crash-looping new binary can trip the default start rate limit during the
  wrapper's health window; the wrapper now clears it with `reset-failed`, so the
  rollback is correct. Operators who prefer the unit to give up sooner (or later)
  can tune `StartLimitIntervalSec`/`StartLimitBurst`/`RestartSec` in the unit;
  the wrapper does not depend on the exact values.
- **Beta channel binding (M4) — resolved.** Offers now carry the release's own
  channel (`stable` unless the GitHub release is a prerelease), and the agent
  accepts stable offers on every non-beta configuration, so a beta node takes a
  newer stable release; stable nodes still never see prereleases and the
  offer→manifest channel binding stays enforced (`internal/updates/checker.go`,
  `agent/updater.go`).
- **The agent channel is not mutual yet (LOW-4).** The gRPC listener uses
  `VerifyClientCertIfGiven`, so any peer that can reach the port can ask for
  (signed) update offers and force cached release lookups. The agent verifies the
  control plane; the control plane does not yet verify a client certificate.
  Making the channel mutual waits on the registration bootstrap acquiring a
  client credential.
- **Key ring shipped; remote revocation of a compromised key still needs
  upgrades (R1, residual).** Every release binary now embeds the current plus an
  optional pre-positioned next key (`updatecore.NextPublicKey`), verifies
  manifests against any key in the ring, and the release workflow validates and
  asserts the next key when configured — see `release-key-rotation.md`. A lost
  key is recoverable by promoting the pre-shipped next key through a release the
  installed fleet already trusts. What remains is inherent: a **compromised**
  current key cannot be revoked on a node that has not yet received a ring
  release (the anchor is compiled in), so those nodes still need an upgrade or
  reinstall; the runbook covers the emergency sequencing.
- **`GOTHAM_UPDATE_CURRENT` pin — resolved for release builds (INFO).** The
  override still exists for `deploy/verify-systemd.sh` and tests, but a build
  that embeds release key material ignores it (with a warning) and reports the
  node-registry version, so a stray environment value cannot pin a production
  build; `/etc/gotham/gotham.env` stays root-owned (`internal/server/server.go`,
  `updatecore.HasEmbeddedKey`).
- **`release-verify.sh` cleans up on a signal (I5) — resolved.** The shared
  verifier installs EXIT/INT/TERM/HUP handlers for the duration of one
  verification, chaining the caller's handlers instead of clobbering them,
  restoring them on success and aborting nonzero on a signal, so a
  SIGINT/SIGTERM mid-download can no longer leave `/tmp/gotham-verify.XXXXXX`
  behind (`deploy/release-verify.sh`).
- **M9 real-release evidence is partial.** A signed `v0.1.0` release (14 assets)
  was installed on a clean Ubuntu 22.04 container and re-installed; the signed
  manifests and artifact digests verify with the pinned key, and the two-agent
  systemd update path is proven on the test box. Still unproven: applying a
  *newer* GitHub-hosted release through `gotham update` (control plane and agent
  rollout) and exercising the unattended `AUTO_UPDATE` loop. See
  `docs/plans/10-self-update-release.md` and `docs/TODO.md` (Phase 9 residuals).
- **Release trigger custody (INFO).** The release job and PR CI now run on
  GitHub-hosted runners and the signing key stays in the approval-gated
  `release` environment, so untrusted `pull_request` code no longer shares a host
  with `GOTHAM_UPDATE_SIGNING_KEY`. The reviewer gate still governs *who can
  build a signed release*: keep the `v*` tag ruleset and the required reviewers
  limited to project owners. Actions stay SHA-pinned and the sqlc download stays
  checksum-verified.

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
agent-owned `bin`, root read of the agent-owned pending marker, wrapper-death
window); the shared wrapper carries the same mitigations (pinned lock open,
root-owned status-directory floor enforced at run time) and the upgrade path is
the same `runuser`/`setpriv` handoff.

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
built-in defaults. It does **not** see `Environment=` values that exist only on
the unit: put custom paths in `/etc/gotham/agent.env` (or export them) so a
service with custom paths is reset correctly. The retry marker is written with a
temp-file + rename and the mode is set on the file descriptor, so a symlink or
FIFO planted by the service user in its own directory cannot redirect or block a
root run.

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

## Instance settings host helper (JUS-92)

`Settings → Instance` applies network (DNS, IPv4/IPv6) and system (hostname, NTP)
settings through `gotham-hostctl.sh`, a fixed root-owned helper with the same
privilege model as the update wrapper. It is optional: without it the General
section still works and the host sections are read-only.

```sh
sudo install -D -m 0755 -o root -g root deploy/gotham-hostctl.sh /usr/libexec/gotham/gotham-hostctl
sudo deploy/install-hostctl-sudoers.sh gotham
```

- sudoers grants exactly five verbs (`status`, `apply-network`, `confirm-network`,
  `revert-network`, `apply-system`); values arrive on stdin and are re-validated.
- Supported stack: systemd-networkd + systemd-resolved + systemd-timesyncd. Gotham
  owns only `05-gotham.network`, `resolved.conf.d/05-gotham.conf` and
  `timesyncd.conf.d/05-gotham.conf`; other hosts report the sections as unsupported.
- A network change is tentative: a systemd timer reverts it after 120 s unless the
  operator confirms in the UI, so a wrong address cannot lock them out.
- `status` also reports `host_network`: the default-route interface as the
  host sees it (live addresses/gateways from `ip`, modes from the winning
  `.network` file, effective DNS from resolvectl). The control plane seeds the
  Network form from it, so the form shows the host truth instead of the stored
  `dhcp` defaults.
- A DNS-only change (`dns_only=true`, sent by the control plane when the
  interface fields match the host) writes `resolved.conf.d/05-gotham.conf`,
  a DNS-only networkd drop-in on the winning `.network` file
  (`<name>.network.d/05-gotham.conf` with `DNS=` only, never
  `Address`/`Gateway`/`[Route]`) and sets the link DNS at runtime
  (`resolvectl dns`), so per-link DNS from a netplan-generated file no
  longer shadows the change; `networkctl reload` (never `reconfigure`) picks
  up the drop-in, so addresses/routes stay untouched. Neither apply nor
  revert reconfigures the interface in that path. Revert restores the link
  drop-in and the runtime link DNS exactly as snapshotted, plus the files
  the apply snapshotted, so a DNS-only revert cannot delete a pre-existing
  interface file.
- The control plane refuses server-side any apply that would disturb the
  active interface (static-to-DHCP, a different address/gateway) unless the
  request carries `confirm_interface_change=true`; the UI shows a warning and
  a confirmation checkbox for exactly those changes.
- Never apply `ipv4_mode=dhcp` (or a different static address/gateway) on the
  interface you are connected through without that confirmation: the host drops
  its current address as soon as networkd reconfigures, and only the revert
  timer brings it back. Verify DNS, hostname and NTP first; exercise address
  changes from the console. `revert_after` accepts 30–600 s (the control plane
  passes 120 s).

### JUS-100 postscript (2026-10-10): a DNS-only change dropped the static IPv4

On the shared test box (static IPv4 via netplan, `dhcp4: no`) changing only
the DNS servers cut the box off the network until the 120 s auto-revert fired.
Root cause, all three at once: the form showed the stored `dhcp` default
instead of the host's static config, so Apply sent `ipv4_mode=dhcp` with the
new DNS; `apply-network` unconditionally rewrote the whole `05-gotham.network`
(`DHCP=ipv4`, no `Address`/`Gateway`); nothing refused a static-to-DHCP switch
on the active interface. Fixed by the four bullets above (host-truth status,
DNS-only apply path, server-side confirmation guard, form warning); covered by
`deploy/hostctl_test.go` (`TestHostctlStatusReportsHostTruth`,
`TestHostctlDNSOnlyLeavesInterfaceAlone`), `internal/instance` service tests
and `web/tests/network-guard.test.ts`.

### JUS-101 postscript (2026-10-11): a DNS-only change was ignored with per-link DNS

On the same box the JUS-100 fix was safe but ineffective: the new servers
landed in the global drop-in while `Link 2 (ens160)` kept `8.8.8.8 8.8.4.4`
from `/run/systemd/network/10-netplan-ens160.network`, and resolved prefers
per-link DNS, so `resolvectl query` kept using the old servers. Fixed by
also writing a DNS-only networkd drop-in on the winning `.network` file and
setting the link DNS at runtime in the DNS-only path (revert restores both
exactly; `status` already reported the link DNS as the effective list);
covered by `TestHostctlDNSOnlyOverridesLinkDNS` and
`TestGetReportsEffectiveLinkDNS`.
