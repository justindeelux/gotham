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

**System → Updates** is owner-only. It shows the current version, the
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

The control plane treats its own release as the node-agent target. When it runs
a release, every agent that polls it is offered that same signed release as a
rollout, so a control-plane update reaches the nodes without a separate step. A
node that was offline during the update applies it when it reconnects, because
the condition is the running control-plane version, not a timer.

An agent does not move to a release the control plane has not taken: a plain
offer (a newer release the control plane has not installed) is informational
unless the node sets `GOTHAM_AGENT_AUTO_UPDATE=true` in its agent environment.

The self-update replaces the whole node install, not only the binary. The signed
release also carries the host helper, its sudoers rules and the systemd unit,
and the privileged wrapper applies them before it restarts the agent. A
stable-channel node still refuses a beta offer, and an unsigned, mismatched or
unverifiable release is rejected before anything is applied.

A node installed before this release still runs the old privileged wrapper. Its
agent binary updates, but that wrapper never installs the new one, so the
helper, unit and sudoers refresh starts only after one `deploy/install-agent.sh`
run on that node. The installer is idempotent and keeps
`/etc/gotham/agent.env`; after that run, self-update keeps the whole install
current.

The Servers page shows each node's agent version next to the control-plane
version and flags a mismatch. **Update agent** on an outdated node queues the
release for that node and clears a previous failed-update state, so the same
version is retried without a shell command. The node updates on its next poll;
the card then shows pending, updating, up to date or the failure reason.

An owner can still trigger a fleet-wide rollout through the API:

```sh
curl -X POST -H "Authorization: Bearer <admin-token>" \
  https://<panel>/api/v1/servers/agents/update-all
```

The call records a rollout target and returns without waiting: each agent picks
the target up on its next poll. `GET /api/v1/servers/agents` returns the
per-node reported versions and states, the active rollout target and the latest
release the control plane can see.

A failed activation rolls back and backs off like the control plane. The
**Update agent** button clears that backoff for the node it targets; the
node-side reset remains available when the agent cannot reach the control plane:

```sh
sudo gotham-agent update reset
```

The self-update also fixes the docker CLI environment under the systemd sandbox:
when the unit hides `~/.docker` (`ProtectHome=true`), the agent points
`DOCKER_CONFIG` at `/var/lib/gotham-agent/.docker` at startup, so `docker
compose` finds its plugin without a reinstall. A reinstall is needed once on a
node installed before this release so its wrapper can refresh the helper and
unit; after that it is only needed to change install-time choices (the file
manager or CA) or to repair a broken install.

Reinstalling a specific version is a normal install with
`GOTHAM_VERSION=vX.Y.Z`; see [Install](/guide/install).

## Reinstalling on an existing host

Run the installer again on a host that is already installed to upgrade it in
place. Before it changes any file it refuses an unsupported platform and a host
that already has a remote `agent.env` (a node installed from another control
plane): move that remote agent, or repoint it explicitly, before retrying. It
keeps `/etc/gotham/gotham.env`: the managed keys
(`GOTHAM_DATABASE_DSN`, `GOTHAM_REDIS_ADDR`, `GOTHAM_SERVER_PORT`,
`GOTHAM_CA_DIR`, `GOTHAM_SECRET_KEY`, the JWT key paths) are refreshed — a DSN given on the
command line wins, otherwise the existing value is kept — and any operator-added
keys (`AUTO_UPDATE`, `AUTO_UPDATE_INTERVAL`, ...) are left untouched. A managed DSN
is never replaced by the local default, and local PostgreSQL/Redis are only
provisioned when the resolved DSN is the built-in local default. The installer
restarts `gotham.service` after a reinstall, so the just-installed binary is the
one that runs (without the restart an upgraded host would keep serving the
previous version).

## Release note: the panel port default

A fresh install serves the panel on port `9074` instead of `8000`. An existing
installation keeps the port it already uses: the installer and the update
wrapper write it to `GOTHAM_SERVER_PORT` in `/etc/gotham/gotham.env` before the
new binary starts, so bookmarks, webhooks and OAuth callback URLs registered
against the old port keep working. An install configured through `gotham.yaml`
keeps its `server.port`: nothing is pinned over it. The updater health check
follows whichever port is configured.
To move the panel, change `GOTHAM_SERVER_PORT` in `gotham.env` and restart; the
updater probes the new port.

A host installed before the mTLS change now gets a CA, so the gRPC gateway
switches from plaintext to TLS. Previously installed agents connect in
plaintext and have no CA, so they **go offline** (Register/Heartbeat fail
closed at the handshake) until each is reinstalled with the CA:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .           # on each node
sudo gotham/deploy/install-agent.sh --ca ./ca.crt
```

The agent binary does not need updating; only `/etc/gotham/agent.env` gains
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt`. If you cannot reach the control plane by
a name in the listener SANs yet, add it first (`--cp-host`; see
[Add nodes](/guide/nodes)).

Releases without `admin exists` (`v0.2.0`) skip automatic account creation and
print the manual `admin create` command instead of failing with a database
warning; releases without any admin commands (`v0.1.0` and earlier) print
upgrade guidance instead, since that binary cannot create an account either.

When upgrading from an older installer, the quote rejection also applies to
values preserved from an existing `agent.env`, so a node id containing a quote
written by an older installer now blocks the upgrade with a clear error naming
the key — remove the quote from `agent.env` and re-run. The same holds for
`gotham.env`: a preserved DSN or Redis value with quotes or a trailing
backslash now blocks the upgrade naming the key — fix the value in
`gotham.env` (keyword DSNs with quoted values must use the URL form) and
re-run.

## Release note: owner and member accounts

This release replaces the team model with one owner and any number of members.

- The earliest existing account becomes the **owner**; every other account
  becomes a **member**. There is exactly one owner.
- Teams and their roles are gone. Projects are now instance-level; a project
  name must be unique across the instance.
- Members start with **no project access**. After upgrading, sign in as the
  owner, open **Users → Project access**, and grant each member the projects they
  need. Until then they see an empty console.
- Members can only reach granted projects and their resources. Servers, SSH
  keys, Git connections, instance settings, global Domain & SSL, updates, the
  host terminal/file manager and the App store are owner-only.
- The previous email-allowlist setting is no longer read. Remove that line from
  `/etc/gotham/gotham.env`; owner access now comes from the account role, and an
  owner API token needs the `admin` scope.

See [Members and access](/using/members-access) for the new model.

## Release note: OAuth sign-in moves into Settings

GitHub sign-in is configured in the Gotham UI's instance settings instead of
`gotham.yaml` or the environment. `GOTHAM_OAUTH_GITHUB_CLIENT_ID`,
`GOTHAM_OAUTH_GITHUB_CLIENT_SECRET`, `GOTHAM_OAUTH_GITHUB_REDIRECT_URL` and an
`oauth:` block are ignored; nothing is imported from them.

An instance that used them keeps signing in with email and password until the
GitHub OAuth app's client ID and secret are entered in the instance settings.
The app's authorization callback URL is
`/api/v1/auth/oauth/github/callback` on the instance's control-plane origin.
Saving the credentials enables the provider without a restart.

## What verification covers

For both binaries, the manifest is signed with the release key embedded in the
running binary, and it binds the version, architecture, file name and channel.
The downloaded artifact's SHA-256 is compared against the signed manifest; any
mismatch aborts the update. Draft releases are skipped, and a release that is
missing its assets or manifests is never offered. No signing key material is
stored on, or needed by, the host.

If an update fails, see [Troubleshooting](/guide/troubleshooting).
