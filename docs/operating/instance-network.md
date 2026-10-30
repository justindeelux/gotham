---
title: Settings and node host settings
---

# Settings and node host settings

**Settings** (`/settings/instance`) configures the Gotham instance itself: its
public control-plane address and name. Everything that describes one machine —
timezone, network and Linux system options — lives on that machine's node page
under **Servers → [node] → Node settings** instead. Both surfaces are visible
to the owner only.

## Settings

| Setting | Purpose |
|---|---|
| Control-plane URL | Public base URL used for agent enrollment, OAuth redirects and webhooks. It also decides the panel route and its certificate. |
| Instance name | Display name of the instance. Default: `gotham`. |

The URL must be `http` or `https` with a host, without credentials, query or
fragment. Each field shows where its value comes from — the environment, the
database or the built-in default. A field set by an environment variable is
locked and cannot be changed here; the page names the variable.

The control-plane URL is the address agents enroll against and external
services call back to, so changing it is guarded:

1. Saving the new URL records a pending change and pushes the new panel route.
2. The new configuration counts as applied only when you confirm it **through
   the new address**, within two minutes. Open Gotham at the new URL and keep
   the change there.
3. If the new route has not synced, confirmation is refused. If you cannot
   reach Gotham at the new address, do nothing: the previous route is restored
   when the countdown ends.

Only one control-plane URL change can be pending at a time.

### Domain and certificate status

The page reports the control-plane hostname's live state:

- **Router** — `synced` when the node serves the current generated
  configuration, `pending` when a sync is needed, `unknown` when there is no
  sync history. The control-plane host must be a registered node for its
  router to sync.
- **Certificate** — `issued` with an expiry date, `pending`, `failed`, or
  `not applicable` when the URL is plain HTTP. The status is read from the
  node's ACME storage, never stored as a claim.
- **DNS** — not reported. The API does not check that the hostname resolves to
  this instance.

The panel certificate itself is managed by the proxy; see
[Proxy and DNS](/operating/proxy-dns). Changing the URL reconciles it
automatically.

## Node settings

Each node's **Node settings** tab edits the host that node runs on. The read
is live from that node on every visit; nothing is cached in the control plane.
Changing one node never changes another.

| Section | Settings |
|---|---|
| Network | DNS servers, IPv4 (DHCP or static address and gateway), IPv6 (off, automatic or static). |
| System | Hostname, NTP on/off with up to four servers, and the node's timezone. |
| Host helper | The privileged helper that applies the two sections. |

The timezone of the **localhost** node is also the control plane's timezone:
schedules, log display and backups run in it. A remote node's timezone only
changes that machine's clock.

### Network

- **DNS servers** — primary required, alternate optional, up to three distinct
  IPv4 or IPv6 addresses.
- **IPv4** — automatic (DHCP), or static with an address in CIDR notation and a
  gateway.
- **IPv6** — enabled with automatic or static addressing and a gateway, or
  turned off.

With no change pending, the form shows the node's live configuration rather
than the last saved values.

### Apply, keep, revert

Applying a network change puts it live immediately but leaves it
**unconfirmed**. A countdown banner offers **Keep changes** and **Revert now**:

- **Keep changes** confirms the configuration and clears the pending state.
- **Revert now** restores the previous configuration immediately.
- **Do nothing** — the node restores the previous configuration when the
  two-minute countdown ends. This is the safe path when the change cut your
  connection: you do not need to reach Gotham to undo it. On a remote node the
  countdown timer runs on that node, so it still reverts if the agent link
  drops.

Only one network change per node can be pending. Settle it before making
another; a second apply on the same node is refused while it waits.

A **DNS-only change** — when the interface configuration is unchanged — still
gets the countdown, but only the resolver settings are written: the interface
file is not rewritten and no risky-change confirmation is needed.

A change that would disturb the active interface — a different address or
gateway, switching a static interface to DHCP, or disabling an address family
— shows a warning and requires you to tick an explicit confirmation before it
can be applied. The node may become unreachable until the change is confirmed
or reverted.

The section needs a host that runs systemd-networkd, systemd-resolved and
`networkctl`.

### System

- **Hostname** — letters, digits and hyphens; dots separate labels.
- **NTP** — enable time synchronisation and list up to four servers. Empty
  leaves the system defaults.
- **Timezone** — searchable IANA zone with its current UTC offset.

System settings are applied immediately; they have no confirmation countdown.
They need `systemctl` and `hostnamectl` on the node.

## Capability states

Every node reports one capability. Only **Ready** allows edits; anything else
shows both sections read-only with a plain-language reason, so a save never
fails:

| State | Meaning |
|---|---|
| `ready` | The host helper runs and the node has the network and time stack. |
| `unsupported_stack` | The node lacks systemd-networkd/resolved or timedatectl. Installing the helper cannot fix a missing stack. |
| `helper_outdated` | The helper is older than this control plane and cannot report the timezone. On the control-plane machine **Install host helper** updates it; on a remote node, update the agent or re-run the agent installer. |
| `helper_missing` | The helper file is absent. On the control-plane machine **Install host helper** can restore it when the privileged repair path is present. On a remote node, re-run the agent installer. |
| `helper_denied` | The helper is present but the privilege rule refuses it. Re-run the matching installer. |
| `agent_outdated` | The agent predates host settings. Update the agent, or re-run the agent installer on that node, then reload the page. |
| `offline` | The agent is not reachable. Settings are read-only until it reconnects. |

A failed helper install shows the installer's output so you can read the
cause. Re-running the agent installer on every remote node is a manual step:
existing agents are not upgraded to host support automatically.

See [Nodes and containers](/operating/nodes-containers) for registering a node
and the agent installer.
