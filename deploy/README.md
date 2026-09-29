# Gotham deployment

| File | Purpose |
|---|---|
| `gotham.service` | Control-plane systemd unit |
| `gotham-update.sh` | Privileged restart/healthcheck/rollback wrapper |
| `gotham-updater.conf` | Root-owned wrapper configuration (install to `/etc/gotham/updater.conf`) |
| `install-sudoers.sh` | Grants the service user the wrapper (and nothing else) |
| `gotham-agent.service`, `install-agent.sh` | Node agent |
| `compose.dev.yml` | Local PostgreSQL + Redis |

## Control-plane self-update layout

The self-update chain splits privileges so that compromising the control plane
cannot escalate to root:

| Path | Owner | Writable by `gotham` | Why |
|---|---|---|---|
| `/var/lib/gotham/bin/gotham` | `gotham` | yes | The verified binary is swapped here (`StateDirectory`). |
| `/var/lib/gotham/bin/gotham.old` | `gotham` | yes | Last known good binary, kept until the new one is healthy. |
| `/usr/libexec/gotham/gotham-update` | `root` | **no** | The privileged restart wrapper. |
| `/etc/gotham/updater.conf` | `root` | **no** | Fixed binary/service/health/status the wrapper uses. |
| `/run/gotham/update.status` | root (final), `gotham` (staged/read) | runtime dir | Durable outcome of the last update. |

The unit runs `/var/lib/gotham/bin/gotham serve` and does **not** set
`NoNewPrivileges`, because the control plane invokes the fixed root-owned
wrapper with `sudo -n`. sudoers grants exactly:

```
gotham ALL=(root) NOPASSWD: /usr/libexec/gotham/gotham-update
```

The command takes **no arguments** (a command with no arguments in sudoers
matches only an argument-free invocation), so a compromised control plane can
only ask root to restart the fixed service. It cannot replace the wrapper
(not writable), cannot edit its configuration (root-owned, `/etc` read-only
under `ProtectSystem=full`), and cannot point it at another unit, path or URL.

## Install

```sh
# 1. Install the binary and the privileged wrapper.
install -d -o gotham -g gotham -m 0755 /var/lib/gotham/bin
install -m 0755 gotham /var/lib/gotham/bin/gotham
install -d -m 0755 /usr/libexec/gotham
install -m 0755 gotham-update.sh /usr/libexec/gotham/gotham-update
install -m 0644 gotham-updater.conf /etc/gotham/updater.conf

# 2. Install the unit and the sudoers rule.
install -m 0644 gotham.service /etc/systemd/system/gotham.service
sudo deploy/install-sudoers.sh gotham
systemctl daemon-reload && systemctl enable --now gotham
```

## Outcome and recovery

- The control plane stages the new binary, records a `staged` status, then runs
  the wrapper and (typically) is restarted by it.
- The wrapper writes the final `ok` / `rolled_back` / `rollback_failed` /
  `no_backup` status; the control plane logs it at startup and returns it from
  `GET /api/v1/updates/check` as `last_update`.
- A crash mid-swap leaves the target missing with `<binary>.old` present; the
  control plane restores it on startup (`Applier.Recover`).
