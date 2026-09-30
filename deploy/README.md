# Gotham deployment

| File | Purpose |
|---|---|
| `gotham.service` | Control-plane systemd unit |
| `gotham-update.sh` | Privileged restart/healthcheck/rollback wrapper |
| `gotham-updater.conf` | Root-owned wrapper configuration (install to `/etc/gotham/updater.conf`) |
| `install-sudoers.sh` | Grants the service user the wrapper (and nothing else) |
| `verify-systemd.sh` | Root-only integration check for a Linux host with systemd |
| `gotham-agent.service`, `install-agent.sh` | Node agent |
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
- `GET /api/v1/updates/check` returns the outcome; release notes and the
  wrapper's `detail` (which can contain paths) are only shown to a platform
  operator.
- Operator reset for a stale `staged`/`resuming` marker (for example after a
  hard kill): `gotham update reset` or `rm -f /var/lib/gotham/update.pending`.
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
- **Check-then-open TOCTOU in the wrapper (LOW).** The wrapper checks
  `[ -L ]`/`[ -f ]` before opening the lock and reading the pending marker, so a
  service user that swaps in a symlink between the check and the open could make
  root open a FIFO (the wrapper hangs, the gate stays `staged`) or a device node.
  The opens are read-only, so nothing is truncated; the leak is at most a
  sanitized 64-character `version=` line. Worst case is a self-DoS by an
  already-compromised service user. Upgrade path: the same `runuser`/`setpriv`
  handoff.

## Linux/systemd verification

`sudo deploy/verify-systemd.sh` runs a scratch service and the real wrapper on a
systemd host. See the script header and the BE-9.1 report for exactly what it
proves.
