---
title: App store
---

# App store

The App store (`/toolchain`) manages the host tools a node needs. It is a
per-node view of the platform's fixed tool catalog, not an application store:
nothing installed here is a workload. Every node needs Docker; the build tools
are optional.

Installs and removals run on the node through a fixed, root-owned host helper.
The control plane and the agent only pass a catalog id; the helper owns the
command it runs and re-validates the id against its own allow-list. See
[Install](/guide/install) for how the helper is placed on a node.

## Select a node

Pick a node in the page header. The catalog, the installed state and the job
queue are all specific to that node, so switching nodes switches the whole
view. With no nodes registered the page asks you to add a server first; see
[Nodes and containers](/operating/nodes-containers).

Tools are grouped by the catalog's `build`, `runtime` and `network`
categories, and the tabs double as filters. Search matches a tool's name, id
or category. A selection survives tab switches, so one bulk install can cover
tools from several categories.

## The catalog

Fourteen entries:

| Tool | Group | What the platform uses it for | Actions |
|---|---|---|---|
| Railpack | Build | Builds an image from a generated language plan (the "auto" build pack). | Install / Uninstall |
| BuildKit | Build | Container image Railpack builds with; started through Docker, never installed as a host binary. | Install / Uninstall |
| Cloud Native Buildpacks (`pack`) | Build | Builds an image with the Cloud Native Buildpacks lifecycle. | Install / Uninstall |
| Git | Build | Clones application source and commits; 2.37 or newer is required for DNS pinning. | Install / Update |
| Docker | Runtime | Runs application, database and proxy containers; every node needs it. | Install |
| systemd-networkd | Network | Host network configuration; Gotham writes a drop-in and reloads it. | Detect only |
| systemd-resolved | Network | Host DNS resolution; Gotham writes a drop-in and reloads it. | Detect only |
| `networkctl` | Network | Inspects and reloads systemd-networkd links. | Detect only |
| OpenSSH client | Runtime | Clones a private repository over SSH. | Install |
| `sudo` | Runtime | Runs privileged host helpers through `sudo -n`. | Install |
| `systemctl` | Runtime | Manages the agent, proxy and database systemd units. | Detect only |
| `curl` | Runtime | Downloads releases and installer material on the host. | Install |
| `resolvectl` | Network | Reads and sets per-link DNS through systemd-resolved. | Detect only |
| `ip` | Network | Reads the host's live addresses and default route. | Detect only |

Detection resolves each tool on the host's `PATH` (BuildKit and the systemd
units are probed through Docker and `systemctl` instead). A tool that is
present but does not answer its version probe is still shown as installed,
with no version. Only Git declares a minimum version, 2.37, so it is the only
row that can report **Update available** when the detected version is older.

Dependencies are part of the catalog: Railpack needs BuildKit and BuildKit needs
Docker, and the agent installs dependencies first. The systemd network tools
declare the units they drive — for example `networkctl` needs
systemd-networkd and `resolvectl` needs systemd-resolved — which detection
checks. An uninstall is refused while another installed tool still needs the
one being removed.

## Install, update, remove

- **Install** queues the tool. An install that is already queued or running
  for the same tool and action is skipped and named in the confirmation, so a
  double click never creates a second job.
- **Install / Update** appears for Git when its detected version is below the
  catalog minimum of 2.37; no other entry declares a minimum version today.
  The console does not compare versions against upstream releases and has no
  version picker: it installs the release the host helper pins.
- **Uninstall** is offered only for Railpack, BuildKit and Buildpacks. Docker
  is never removed because that would destroy every workload on the node, and
  Git, the SSH client, `sudo`, `curl` and the system units are required by the
  platform, so their removal is refused server-side even if a caller asks.
- **Detect only** rows have no install or remove action at all. The console
  says so instead of offering a button that would fail; these are host
  packages the platform configures but never manages.

Only missing, installable tools can be checked for a bulk install. Installed
rows, detect-only rows and tools with an update available are handled one at a
time from their own row.

## Queue and job output

**Queue** opens the install queue for the selected node. Each tool action is
one job, and the job list shows its state:

| State | Meaning |
|---|---|
| Queued | Waiting for the node's current job to finish. |
| Running | The helper is installing or removing the tool. |
| Succeeded | The tool reached the requested state. |
| Failed | The action failed; the output says why. |
| Canceled | Canceled before it started. |

Statuses update live while the queue is open. The node runs one job at a
time, so several queued tools run in order; different nodes run in parallel.

Select a job to see its install output. Lines stream while the job runs, and
a finished job's output stays available. A job's stored output is capped at
256 KiB; past that the log ends with a truncation marker. Closing the queue
modal does not stop anything — jobs keep running in the background.

- **Cancel** is available while a job is still queued. A running job cannot be
  canceled from the console.
- **Retry** is offered for a failed job. It queues a new job for the same tool
  and action; the old job stays in the list as history.

A node that the control plane cannot reach shows an agent-unreachable error
instead of an empty catalog. Check the node's status and retry.

## Host tools power the build engines

Railpack and Buildpacks are wired build engines whose host tools come from this
catalog. Railpack builds a language project without a Dockerfile, but installing
Railpack alone is not enough — it needs BuildKit and Docker as well.
Buildpacks needs `pack`. A missing tool is reported on the deployment. See
[Applications](/using/applications).

## When an install fails

- **The node agent is unreachable** — nothing was queued. Check that the node
  is online.
- **The host helper is missing or refuses the verb** — a node that was updated
  only through the agent's self-update can keep an old host install. Re-run the
  node installer, which is idempotent, then retry; see
  [Install](/guide/install).
- **A tool is in use** — an uninstall is refused while a build runs, or while
  another installed tool depends on it. Remove the dependent tool first
  (Railpack before BuildKit).
- **The output shows a checksum or package-manager error** — the job failed on
  the host and changed nothing. Read the output, fix the host condition, and
  retry.
