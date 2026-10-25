---
title: Add nodes
---

# Add nodes

An installation starts with one node: the control-plane server itself, named
`localhost`. This page adds more servers and installs the host tools the panel
uses to run builds and containers on them.

The installer adds the `localhost` node for you. You only follow these steps
for **additional** nodes.

## What you need

- A Linux server (`amd64` or `arm64`) with `systemd`, `curl`, `openssl` 3,
  `sudo` (with `visudo`) and `coreutils`.
- Docker Engine and the compose plugin, or pass `--full` on Ubuntu/Debian to
  let the installer add Docker from the official repository.
- Outbound HTTPS to `github.com` (or your mirror via `GOTHAM_RELEASES_URL`).
- The control plane's public CA certificate, copied from the control-plane
  server. Never copy `ca.key`.
- An enrollment token for the node's first registration.

The panel also has a **Servers → Add server** wizard that generates the same
install block for you, including the token; this page is for doing it by hand.

## Before you add a node: the control plane address

Remote agents connect to the control plane over TLS and verify its certificate.
The listener certificate always includes the loopback names (`localhost`,
`127.0.0.1`, `::1`) and the machine hostname. The name or IP an agent dials
(`GOTHAM_AGENT_CP_ADDR`) must also be in that list, or the handshake fails with
`certificate is valid for …, not <name>`.

Add the control plane's address when you install it (or reinstall):

```sh
sudo deploy/install.sh --cp-host cp.example.com --cp-host 203.0.113.10
```

`--cp-host` is repeatable, and `GOTHAM_GRPC_HOSTS=<a,b>` is the environment
form. The list is persisted next to the CA; re-running the installer without
the flag keeps it. `GOTHAM_GRPC_HOSTS` in the service environment overrides the
persisted list at runtime, and `gotham ca init --host <name-or-ip>` can set it
directly. A node that dials an address which is not in the list fails closed
(never plaintext).

An operator who runs `gotham serve` directly without a CA gets the development
fallback (plaintext gRPC, logged as a warning); the installer never leaves a
production host in that state.

## Install the agent on the node

On the new node, copy the public CA certificate and run the agent installer:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
git clone https://github.com/justindeelux/gotham.git
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

The first registration also needs the enrollment token. The control plane
creates one at `<GOTHAM_CA_DIR>/enrollment-token` (by default
`/var/lib/gotham/ca/enrollment-token`), unless you set
`GOTHAM_GRPC_ENROLLMENT_TOKEN`. Copy it to a root-owned file, mode 0600, not a
symlink, and pass that file:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/enrollment-token /root/token
sudo chown root:root /root/token && sudo chmod 0600 /root/token
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt --enrollment-token-file /root/token
```

Or pass it without echo as the `GOTHAM_AGENT_ENROLLMENT_TOKEN` environment
variable with `sudo -E`. Never write it as `sudo VAR=<token> …`: that exposes
the secret in `ps`/`/proc` and shell history. Only the first registration needs
it. Without a token (and without the explicit
`GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true` opt-in) first enrollment is refused.
Nodes enrolled before this change keep working, and a self-update never touches
the node key or certificate.

The installer **fails closed** without a CA: it refuses to run unless
`--ca <path>` (or `GOTHAM_AGENT_CA_FILE=<path>`) is given. It installs the
certificate as `/etc/gotham/ca.crt` (root-owned, `0644`) and writes
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt` to `/etc/gotham/agent.env`, so the agent
verifies the control plane over TLS.

`--insecure` is the documented **local-development-only** override: it skips
the CA check, writes `GOTHAM_AGENT_INSECURE=true`, and leaves
`GOTHAM_AGENT_CA` unset. The agent itself also fails closed: with no CA it
refuses to start unless `GOTHAM_AGENT_INSECURE=true`, and a plaintext listener
is confined to loopback (the installer defaults the address to
`127.0.0.1:9443`). Never use it on a real node.

## How the agent and control plane trust each other

Over TLS the channel is mutual: the agent generates a key, sends a CSR on
`Register`, receives a node certificate, and presents it on every later
connection. The control plane binds the certificate identity and key to the
node, and each agent accepts only the control plane (a certificate carrying the
`spiffe://gotham/control-plane` identity), so a node cannot act as the control
plane toward another node.

A node's first registration has no certificate yet and needs the enrollment
token described above. After that it authenticates with its own certificate and
key.

## What the agent installer installs

It uses the same signed-manifest verification as the control plane
(`deploy/release-verify.sh`) for `gotham-agent-linux-<arch>` and
`gotham-agent-manifest-<arch>.txt`, with the pinned release public key and no
runtime override. It installs:

- the agent binary into `/var/lib/gotham-agent/bin/gotham-agent`;
- the shared update wrapper as `gotham-agent-update`;
- the config `/etc/gotham/agent-updater.conf`;
- the sudoers rule and the systemd unit.

Set `GOTHAM_AGENT_CP_ADDR` (control-plane gRPC address) and
`GOTHAM_AGENT_NODE_ID` in the environment before running it; they are written
to `/etc/gotham/agent.env`.

Useful agent installer flags and variables:

| Flag / variable | Purpose |
|---|---|
| `--ca <path>` | Install this control-plane CA certificate (required). |
| `GOTHAM_AGENT_CA_FILE` | Same as `--ca`, via the environment. |
| `--insecure` | Development only: connect without TLS. |
| `--full` / `GOTHAM_AGENT_FULL=1` | Also install Docker Engine and the compose plugin from the official Docker apt repository (Ubuntu/Debian only; no-op when already installed). On by default for the localhost agent `install.sh` sets up; off by default for direct runs. |
| `--dry-run` | Print what would be done; makes no change. |
| `--enrollment-token-file <path>` | Root-owned file holding the first-registration token. |
| `GOTHAM_AGENT_CA` | The agent-side path the installer writes (`/etc/gotham/ca.crt`). It is **not** read from the ambient environment; on a reinstall the installer keeps the value already in `agent.env`. Use `--ca`/`GOTHAM_AGENT_CA_FILE` to change it. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | Release channel this node accepts, `stable` (default) or `beta`. A `beta` node accepts stable and beta offers; a `stable` node refuses a beta offer; an empty or unknown channel is refused. |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | Parent of every application bind mount the node accepts (default `/var/lib/gotham/volumes`); falls back to the shared `GOTHAM_MANAGED_VOLUME_ROOT`. Must match the control plane. |
| `--file-manager` / `--no-file-manager` / `GOTHAM_AGENT_FILE_MANAGER=yes\|no` | Answer the installer's file-manager question without a prompt. The installer asks (default **no**; no terminal also means no). Yes writes `GOTHAM_AGENT_FILES=true` and a `gotham-agent.service.d/file-manager.conf` drop-in that runs the agent **as root** without systemd sandboxing; no writes `GOTHAM_AGENT_FILES=false` and removes the drop-in. A re-run keeps the previous answer (a hand-set `GOTHAM_AGENT_FILES=true` is kept as is, without the root drop-in); an ambient `GOTHAM_AGENT_FILES` is ignored in favour of that answer. |
| `GOTHAM_AGENT_FILES` | The remote file service on the node. On unless set to `false` (the installer writes the answer above). |
| `GOTHAM_AGENT_FILE_FULL_ACCESS` | On unless set to `false`: the service exposes the **whole filesystem** as one writable root `system` (`/`), with no protected paths — the operator accepted that risk. `false` restores the confined roots. See [Remote file service](/guide/file-service). |
| `GOTHAM_AGENT_FILE_ROOTS` | Extra browsable roots, colon-separated absolute paths (default none). Operator roots are **read-only** in this phase; a root that is missing, not absolute, in a dangerous tree or overlapping another root refuses startup. |
| `GOTHAM_AGENT_FILE_MAX_UPLOAD` | Ceiling for one upload body in bytes (default `1073741824`, 1 GiB); a larger body is refused with `ResourceExhausted` (413). Should match the control plane's `FILES_MAX_UPLOAD_BYTES`. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` | Days an item may stay in one root's recycle bin before the agent purges it (default `30`). `0` keeps entries forever and disables age retention. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES` | Ceiling in bytes for one root's recycle bin (default `0`, unbounded). Over the ceiling the oldest entries are purged first until the bin fits. |
| `GOTHAM_AGENT_ARCHIVE_MAX_BYTES` | Total uncompressed bytes one **compress or extract** may read/write before it is refused with `ResourceExhausted` (413) — the size cap and half of the zip-bomb guard. Default `4294967296` (4 GiB). |
| `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` | Maximum entries one archive operation may walk or unpack (default `100000`); beyond it the operation is refused with `ResourceExhausted` (413). |
| `GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES` | Maximum on-disk size of an archive file that **extract** will read (default `1073741824`, 1 GiB); an archive above it is refused with `ResourceExhausted` (413) before any parser runs. Applies to every format. |
| `GOTHAM_AGENT_COMPOSE_ROOT` | Directory holding one subdirectory per compose project (default `/var/lib/gotham-agent/compose`); browsable read-only when the file service is enabled. |

## Check the agent address and settings

`GOTHAM_AGENT_CP_ADDR` must use a name or IP that is one of the control plane's
listener SANs (see above); otherwise the TLS handshake fails and the node never
registers. It must be `host:port` without spaces, quotes or `=`. Every other
`agent.env` value is validated up front, before the installer changes anything:

- control characters and trailing backslashes are rejected everywhere;
- the node id is held to what the agent accepts (at most 253 bytes; no spaces,
  tabs, `*`, `/` or backslashes) and additionally refuses quotes, because
  systemd's EnvironmentFile parser would strip or regroup them;
- paths must be absolute;
- the docker endpoint must be `unix:///abs/path`, `/abs/path` or
  `tcp://host:port`;
- auto-update must be exactly `true`/`false`;
- the interval must be a Go duration (day/week units and overflowing values are
  refused).

`install.sh` runs these checks before mutation and always installs its local
agent as `localhost`, connected to `127.0.0.1:9442`.

## The localhost node

The control plane creates or repairs the shared `localhost` node on startup;
its status becomes ready only after an agent heartbeat. Deleting it returns
HTTP 409; PATCH is rejected too.

Upgrade migration 49 renames the saved local agent identity in place, preserving
the server UUID, apps, deployments and routers. The installer passes the
previous `agent.env` ID to the migration, so changing the hostname or running
migrations in a container is safe. For a manual migration, set
`GOTHAM_LEGACY_LOCAL_NODE_ID` to that saved node ID.

Forward migration 51 also detects a single loopback-addressed node with a
custom ID (for example `test-node-1` at `127.0.0.1`), including local agent
addresses such as `localhost:9443` and `[::1]:9443`. Startup repeats this check
before creating `localhost`, preserving the UUID, apps, databases, services and
routers. Migration 51 removes a prior startup's empty, pending,
never-registered `localhost` placeholder only after checking that no foreign-key
references exist, then renames the legacy row. A registered or referenced
`localhost`, or multiple local candidates, stops migration with an actionable
error. Inspect the saved `agent.env` and server records, reconcile their
workloads and remove duplicate local-agent entries before retrying; no
workloads are merged or deleted.

Startup scans local candidates only when `localhost` is absent; later
registrations cannot block a restart. Detection uses the recorded IP/agent
address, never a bare node ID; IPs copied directly from an agent-chosen IP node
ID are excluded. Candidates must have agent registration data and no SSH user,
key, or password; SSH-managed loopback/tunnel nodes retain their identity and
team. For a legacy node with a public or missing IP, supply the saved identity
to migration 49 as described above; migration 51 does not reinterpret that
environment hint.

After a manual upgrade, change `GOTHAM_AGENT_NODE_ID` in the local agent's saved
environment (normally `/etc/gotham/agent.env`) to `localhost` and restart the
agent. The installer does this automatically; keeping the old ID can register
the legacy node again and leave two records targeting the same agent.

The Git URL host policy's denial of the name `localhost` applies to Git remotes
only; it does not validate node IDs or the agent's loopback dial path. The exact
`localhost` node ID is accepted only from a loopback peer. Its certificate uses
the separate SAN `localhost.agent.gotham.invalid`, and the control plane
verifies that name when dialing `127.0.0.1:9443`. It cannot be used as a
control-plane certificate for `localhost` or a loopback IP. Do not include the
agent SAN in the control-plane listener hosts; enrollment refuses that
collision. Other control-plane listener identities and aliases remain reserved.

Remote agents must set their own distinct node ID and control-plane address:

```sh
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=worker-1 \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

## Node host tools (install / uninstall)

The agent exposes the `ToolService` (ListTools/InstallTool/UninstallTool) over
mTLS; the control plane drives it from the node's tool manager. Every privileged
step runs through one fixed, root-owned helper — the same model as
`gotham-hostctl` — never a blanket `sudo`:

```sh
sudo install -D -m 0755 -o root -g root deploy/gotham-toolctl.sh /usr/libexec/gotham/gotham-toolctl
sudo deploy/install-toolctl-sudoers.sh gotham-agent
```

`install-agent.sh` installs both, so a normal agent install needs no extra step.
The sudoers drop-in (`/etc/sudoers.d/gotham-toolctl`) grants exactly one rule
per verb **and** tool id:

- `install-tool <id>` — `railpack`, `buildkit`, `pack`, `docker`, `git`;
- `uninstall-tool <id>` — `railpack`, `buildkit`, `pack`, `git` (`docker` is
  never removed: it would destroy every workload on the node).

The agent passes only a catalog id; the helper owns every executable argv and
re-validates the id against the same allow-list, so a compromised agent cannot
name a package, a URL or an extra flag. Detect-only host units
(`systemd-networkd`, `systemd-resolved`, `networkctl`, `systemctl`, …) are
refused with `FAILED_PRECONDITION` and have no installer at all.

Behaviour:

- **Dependencies are resolved** by the agent, dependency-first:
  `railpack → buildkit → docker`. Each tool is installed by one helper call.
- **Pinned and verified.** Railpack and pack are installed from a versioned
  release tarball whose sha256 is hard-coded in the helper (never the unpinned
  `install.sh` piped to a shell); BuildKit is the digest-pinned
  `moby/buildkit:v0.34.0` image run as the managed privileged `buildkit`
  container. A checksum mismatch aborts.
- **Idempotent.** A tool already detected on the node is skipped. The BuildKit
  install clears a stale container before recreating it and tags the detection
  reference only after the container runs, so a failed run does not leave the
  tool looking installed.
- **One at a time.** A node-wide lock allows one install or uninstall at a time;
  a second call is refused with `RESOURCE_EXHAUSTED`. The request is cancelled
  when the control plane drops the stream, and bounded by a 15-minute timeout.
  The helper holds its own `flock` for the whole job, so even a job whose agent
  request was cancelled (the transient unit keeps running until it finishes or
  its `RuntimeMaxSec=840` bound fires, whichever is first) blocks a retry with a
  clear "already running" refusal instead of overlapping it; a graceful cancel
  (`SIGTERM`) may defer the helper's stop trap until the foreground
  `systemd-run` returns, so it does not guarantee an immediate stop.
- **Upgrading a node.** The helper and its sudoers rule are installed by
  `install-agent.sh`, not by the self-update binary swap: a node that updated
  only through `gotham-agent` self-update keeps the old install and its tool
  install/uninstall calls fail with sudo's "not allowed". Re-run
  `deploy/install-agent.sh` on the node (idempotent) to install them.
- **Uninstall refuses while a tool is in use** (`FAILED_PRECONDITION`): while a
  build runs, and while another installed tool depends on it (for example
  BuildKit cannot go while Railpack is installed). Only in-flight *builds* are
  tracked as "in use"; a compose service or a plain container using Docker is
  not (Docker is not uninstallable, so only `buildkit`/`git` removals need the
  guard).
- **Sandbox.** The agent unit runs with `ProtectSystem=full`, so `/usr`, `/etc`
  and `/boot` are read-only in its mount namespace and a `sudo` child inherits
  that. The helper therefore re-executes itself through
  `systemd-run --pipe --wait --collect`, which starts a fresh transient unit
  outside the agent's namespace; the node needs systemd (already required by
  the agent unit). Streamed output is the helper's own stdout/stderr, then a
  final `ToolResult{status, exit_code}`.

The helper also runs standalone for debugging (as root, without `systemd-run`):

```sh
sudo GOTHAM_TOOLCTL_DRYRUN=1 GOTHAM_TOOLCTL_ARCH=amd64 GOTHAM_TOOLCTL_DISTRO=apt \
    /usr/libexec/gotham/gotham-toolctl install-tool railpack
```
