---
title: Nodes and containers
---

# Nodes and containers

A node is a Linux host running `gotham-agent`. The control plane never calls
Docker directly: every container command, metric read and log stream goes
through the agent on that node. Register nodes on **Servers**
(`/servers`); open one to see its overview, metrics and settings, and use its
**Containers** tab for the Docker inventory.

Node management is owner-only. Only the owner account can register a node, read
or change it, or open its containers and host tools. See
[Security](/operating/security) for the owner boundary used by the file manager
and host shell.

## Add a node

**Add server** opens a four-step wizard: **Connect**, **Validate**, **Install**,
**Finish**.

1. **Connect** asks for a node name, an address (IPv4 or hostname), the SSH
   port (usually 22), the SSH user, and one credential: a pasted private key in
   PEM form, or a node password. A passphrase for a protected key is used for
   that run only. Stored secrets are encrypted and never returned by the API.
   Continuing registers the server row.
2. **Validate** runs the SSH probes automatically: Docker Engine present and
   running, CPU architecture and cores, available RAM, free disk. Probes stop
   after 15 seconds. A failing Docker probe offers **Install Docker**, which
   installs Docker Engine over the same SSH session using the node's package
   manager and streams the output.
3. **Install** shows a one-time install command to run on the node over SSH. It
   copies the control-plane CA, clones the installer's sibling files and
   registers the agent. The page polls every five seconds; there is no simulated
   progress.
4. **Finish** confirms that the agent checked in. The node is **Ready** only
   after its first heartbeat — a successful SSH validation alone does not make
   it ready.

The node's SSH host key is pinned on the first successful validation
(trust-on-first-use). Later validations fail closed if the key changed. A node
that authenticates with a password needs **Trust this host key on first
validation** checked for that first run, because a first-use man-in-the-middle
would otherwise also capture the password.

### Node statuses

| Status | Meaning |
|---|---|
| `Pending` | Registered, but the agent has not heartbeated yet. Also the state after the address, user or credential changes. |
| `Validating` | SSH probes are running. |
| `Ready` | A heartbeat arrived within the last 30 seconds. |
| `Offline` | No heartbeat for 30 seconds. The switch happens when the node list is read. |
| `Error` | The last SSH validation failed. |

## Edit, revalidate, reset, delete

**Edit** changes the name, address, SSH port, SSH user or credential. Key and
password are alternatives, never both. Changing the address, user or credential
clears the pinned host key and returns the node to `Pending`, so the new
endpoint has to be validated again.

**Revalidate SSH** on a node card, or **Check SSH** on the page, re-runs the
probes. **Check SSH** validates every registered node and reports how many
passed. A successful run updates the recorded OS, architecture, Docker version
and resources; it also re-pins a changed host key only if the pin was reset
first. The status after validation is derived from the last heartbeat, so a
node whose agent is gone ends up `Offline`, not `Ready`.

**Reset a pinned host key.** After a legitimate key rotation on the host, the
next validation fails until the old pin is forgotten. There is no console
control for this; an operator calls:

```http
DELETE /api/v1/servers/{id}/host-key
```

The action only clears the stored fingerprint. The next validation pins the new
key. It does not change the node's address or credentials.

**Delete** removes the node from the control plane only. The request is refused
while applications, services or databases are still pinned to the node; move or
delete those first. The `localhost` node (the built-in local agent) cannot be
edited or deleted. Containers, volumes and certificates on the machine are left
untouched.

## Node overview

Opening a node shows tabs:

| Tab | What it shows |
|---|---|
| **Overview** | Name, address, SSH user, node ID, OS, architecture, Docker version, credential type, CPU/memory/disk usage, container count, last seen, registration time, and the danger zone with **Delete node**. |
| **Containers** | A shortcut to the node's container page. |
| **Files** | Owner only: a shortcut to the file manager for this node. |
| **Terminal** | Owner only: a host shell tab for this node. |
| **Metrics** | The node's time-series charts. |
| **Proxy & Traefik** | The Traefik routers on the node. |
| **Node settings** | A read-only summary of the node's identity. |

## Metrics

The **Metrics** tab charts four series: **CPU**, **RAM**, **Disk I/O** and
**Network**. Values come from the agent's heartbeats; the control plane rolls
them into buckets and never invents a point for a bucket the node did not
report, so a gap in a series is a gap on the chart, not a zero.

| Range | Window |
|---|---|
| Step `1m` | Last hour |
| Step `1h` | Last 24 hours |
| Step `1d` | Last 7 days |

**Auto-refresh** is off by default and can be set to 15 or 60 seconds. A manual
**Refresh** reloads the selected window.

Samples are kept for **30 days**, removed by an hourly retention sweep. The
heartbeat path writes at most one stored sample every five seconds; the live
snapshot on the node row updates on every heartbeat.

The surface is controlled by `FEATURE_METRICS`, on by default. Setting it to
`false` unmounts the metrics range route — the console's request gets `404` and
hides the charts — and stops heartbeat samples from being persisted. The rest of
the node registry keeps working.

## Containers

`/servers/{id}/containers` lists every container on the node, including stopped
ones. Filter by **All**, **Running** or **Exited**, or search by container name
or image. The table shows name, image, state, ports, CPU, RAM, uptime and
actions; the agent's container contract does not report per-container CPU or
RAM, so those two columns show a dash rather than a guess.

- A running container offers **Restart** and **Stop**; a stopped one offers
  **Start**.
- The list is served from a short cache (10 seconds). **Refresh** fetches the
  current list from the node.
- Reading the list, starting, stopping and restarting a container all require the
  owner account.

### Pull an image and run a container

These two actions exist on the API but have no console control. The console is
the place to operate workloads Gotham manages; a raw container created this way
is not a deployed application, has no route, no variables and no rollback.

```http
POST /api/v1/servers/{id}/images/pull
POST /api/v1/servers/{id}/containers/run
```

**Pull** takes `{"image": "registry/repo:tag"}` and answers `202` after the node
has pulled the image. **Run** takes an `image` plus optional `name`, `env`,
`command`, `entrypoint`, `labels`, `ports`, `volumes`, `networks`,
`restart_policy` and `healthcheck`, and answers `201` with the new container id.

A raw run cannot spoof platform-owned resources: a label starting with
`gotham.` is refused, as is a mount of a `gotham-` named volume. Use the
application, database or service flows to create managed workloads.

### Live logs

**Logs** on a container row, or a click on the row, opens the log drawer. It
streams the container's standard output and standard error from the agent.

| Control | Effect |
|---|---|
| **Follow** / **Following** | Keeps the view at the newest line. Scrolling away from the tail turns it off; the button snaps back to the tail when re-enabled. |
| **Pause** / **Resume** | Freezes the view. Lines that arrive while paused are queued and appended in order on resume. |
| **Clear** | Empties the rendered buffer. It does not affect the container or the node. |
| **Download** | Saves the lines currently in the view as a `.log` file named after the container id, with the displayed timestamps. |

The drawer keeps the newest **2,000** lines; older lines are dropped. Its status
badge reports `Streaming`, `Paused`, `Connecting`, `Reconnecting`, `Offline`,
`Closed` or `Idle`, and a refused subscription or a reconnecting stream is shown
as a notice line in the output. The stream ends when the container stops.
