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
install -m 0755 gotham /var/lib/gotham/bin/gotham
install -d -m 0755 -o root -g root /usr/libexec/gotham
install -m 0755 gotham-update.sh /usr/libexec/gotham/gotham-update
install -m 0644 gotham-updater.conf /etc/gotham/updater.conf
install -d -m 0755 -o root -g root /var/lib/gotham-updater

# 2. Install the unit and the sudoers rule.
install -m 0644 gotham.service /etc/systemd/system/gotham.service
sudo deploy/install-sudoers.sh gotham
systemctl daemon-reload && systemctl enable --now gotham
```

## Outcome, gate and recovery

- The control plane writes `staged` to `/var/lib/gotham/update.pending`, swaps
  the binary (hardlinking the previous one to `<binary>.old`, so the fixed
  `ExecStart` is never absent), then runs the wrapper.
- While the pending marker is `staged`, a second **Apply is refused**
  (`409`/`ErrUpdatePending`); the wrapper removes the marker once it records the
  final `ok` / `rolled_back` / `rollback_failed` / `no_backup` status in the
  root-owned status file.
- If the wrapper cannot be launched, the control plane records `wrapper_failed`
  in the pending marker instead of leaving `staged` forever.
- `GET /api/v1/updates/check` returns the outcome; release notes and the
  wrapper's `detail` (which can contain paths) are only shown to a platform
  operator.
- Operator reset for a stale `staged` marker (for example after a hard kill):
  `gotham update reset` or `rm -f /var/lib/gotham/update.pending`.
- Crash recovery: the target is never absent, but if an older version left it
  missing, both `Applier.Recover` (startup) and the wrapper restore
  `<binary>.old` before starting the service.

## Linux/systemd verification

`sudo deploy/verify-systemd.sh` runs a scratch service and the real wrapper on a
systemd host. See the script header and the BE-9.1 report for exactly what it
proves.
