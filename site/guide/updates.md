---
title: Updates
---

# Updates

Gotham updates itself from signed GitHub releases. The control plane and the
node agents share one release train: each binary is published with a signed
manifest, and every artifact is signature-verified automatically before it is
installed. There is nothing to verify by hand and no key to manage on the host.

## Command line

The binary lives in the service-owned directory, so run the operator CLI as the
service user. `apply` and `rollback` refuse to run as root: the swap would
install a `root:root` binary over the service-owned one.

```sh
sudo -u gotham /var/lib/gotham/bin/gotham update check
sudo -u gotham /var/lib/gotham/bin/gotham update apply
sudo -u gotham /var/lib/gotham/bin/gotham update rollback
sudo -u gotham /var/lib/gotham/bin/gotham update reset
```

| Command | Effect |
|---|---|
| `check` | Reports the newest available version and its channel, or that the running version is up to date. |
| `apply` | Downloads, verifies and stages the newest release. `--channel stable\|beta` selects the channel for this run. |
| `rollback` | Restores the previous binary on disk. The running process keeps the current binary until you run `systemctl restart gotham`. |
| `reset` | Clears a stale pending-update marker and the failed-release backoff, for an operator-forced retry. |

An `apply` that stages a release prints `staged <version>`: the privileged
restart wrapper then health-checks the new binary and records the outcome. If
the check fails, the wrapper restores the previous binary and records
`rolled_back` (or `rollback_failed` if that also fails). The console shows the
last outcome, so a failed update is visible instead of looking successful.

A release that rolled back is backed off by the automatic loop — 5 minutes, 10,
20, 40, then at most 1 hour — so the scheduler does not reinstall the same broken
version in a loop. A newer release is offered immediately; `gotham update reset`
clears the backoff when you want to retry the same one.

Both commands check the binary owner first, so a mismatch fails with an explicit
message instead of breaking the next update.

## The console schedule

**Settings → Updates** is platform-admin only. It shows the current version, the
latest available version, the last check, the changelog and the outcome of the
last update attempt, and offers **Check for updates** and **Update now**.

The **Update schedule** card controls the automatic loop:

- **Check for updates automatically** — look for a new release on the schedule.
- **Apply updates automatically** — install a found release without
  confirmation. Off by default, and it requires automatic checks.
- **Channel** — `stable` or `beta`.
- **Frequency** — every N hours (1 to 720), daily, or weekly, with an HH:MM time
  in the instance timezone and, for weekly, a day of the week.

Saving applies the schedule immediately; no restart is needed. Until a schedule
is saved, the environment defaults apply: `AUTO_UPDATE=true` in the service
environment enables both the check and the apply, and `AUTO_UPDATE_INTERVAL`
sets the interval (6 hours by default).

If the page says self-update is turned off, the installation runs with
`FEATURE_UPDATES=false`.

## Channels

`stable` ignores prereleases. `beta` includes them. The channel is chosen per
installation in the schedule (or with `GOTHAM_UPDATE_CHANNEL`), and per node for
agents with `GOTHAM_AGENT_UPDATE_CHANNEL`:

- A `stable` node refuses a `beta` offer.
- A `beta` node accepts both stable and beta releases.

The offer carries the release's own channel, not the subscriber's preference, so
a beta installation taking a newer stable release still verifies the stable
manifest.

## Fleet-wide agent rollout

Agents do not update themselves on their own unless you opt in. Each agent polls
the control plane over the CA-verified channel every 5 minutes by default. For
each supported architecture (`amd64`, `arm64`) the control plane resolves the
newest agent release, verifies its signed manifest, and offers it only when it is
newer than the version the agent reported on its last heartbeat.

Trigger a rollout with the platform-operator API:

```sh
curl -X POST -H "Authorization: Bearer <admin-token>" \
  https://<panel>/api/v1/servers/agents/update-all
```

The call records a rollout target and returns without waiting: each agent picks
the target up on its next poll. There is no console control for this yet.
`GET /api/v1/servers/agents` returns the per-node reported versions, the active
rollout target and the latest release the control plane can see.

To let a single node update unattended, set `GOTHAM_AGENT_AUTO_UPDATE=true` in
its agent environment. A failed activation rolls back and backs off like the
control plane; retrying the same version needs an operator reset on the node:

```sh
sudo gotham-agent update reset
```

Reinstalling a specific version is a normal install with
`GOTHAM_VERSION=vX.Y.Z`; see [Install](/guide/install).

## What verification covers

For both binaries, the manifest is signed with the release key embedded in the
running binary, and it binds the version, architecture, file name and channel.
The downloaded artifact's SHA-256 is compared against the signed manifest; any
mismatch aborts the update. Draft releases are skipped, and a release that is
missing its assets or manifests is never offered. No signing key material is
stored on, or needed by, the host.

If an update fails, see [Troubleshooting](/guide/troubleshooting).
