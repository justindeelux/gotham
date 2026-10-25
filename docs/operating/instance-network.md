---
title: Instance and network
---

# Instance and network

**Settings → Instance** configures the control-plane host itself: its public
address and identity, its network stack, and a few Linux system options. It is
visible to the owner only and applies to the control-plane host, not
to the nodes.

The page has three tabs: **General**, **Network** and **System**.

## General

| Setting | Purpose |
|---|---|
| Control-plane URL | Public base URL used for agent enrollment, OAuth redirects and webhooks. It also decides the panel route and its certificate. |
| Instance name | Display name of the instance. Default: `gotham`. |
| Timezone | IANA zone applied to schedules, log display and backups. Default: `UTC`. |

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
   when the countdown ends, or you can revert immediately from the page.

Only one control-plane URL change can be pending at a time.

### Domain and certificate status

The General tab reports the control-plane hostname's live state:

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

## Network

The Network tab edits the control-plane host's addressing:

- **DNS servers** — primary required, alternate optional, up to three distinct
  IPv4 or IPv6 addresses.
- **IPv4** — automatic (DHCP), or static with an address in CIDR notation and a
  gateway.
- **IPv6** — enabled with automatic or static addressing and a gateway, or
  turned off.

With no change pending, the form shows the host's live configuration rather
than the last saved values.

### Apply, keep, revert

Applying a network change puts it live immediately but leaves it
**unconfirmed**. A countdown banner offers **Keep changes** and **Revert now**:

- **Keep changes** confirms the configuration and clears the pending state.
- **Revert now** restores the previous configuration immediately.
- **Do nothing** — the host restores the previous configuration when the
  two-minute countdown ends. This is the safe path when the change cut your
  connection: you do not need to reach Gotham to undo it.

Only one network change can be pending. Settle it before making another; a
second apply is refused while it waits.

A **DNS-only change** — when the interface configuration is unchanged — still
gets the countdown, but only the resolver settings are written: the interface
file is not rewritten and no risky-change confirmation is needed.

A change that would disturb the active interface — a different address or
gateway, switching a static interface to DHCP, or disabling an address family
— shows a warning and requires you to tick an explicit confirmation before it
can be applied. The host may become unreachable until the change is confirmed
or reverted.

The section needs a host that runs systemd-networkd, systemd-resolved and
`networkctl`. On any other host the values are read-only and the page says the
stack is unsupported; installing the host helper cannot change that.

## System

The System tab sets:

- **Hostname** — letters, digits and hyphens; dots separate labels.
- **NTP** — enable time synchronisation and list up to four servers. Empty
  leaves the system defaults.

System settings are applied immediately; they have no confirmation countdown.
They need `systemctl` and `hostnamectl` on the host, and are read-only on a
host that lacks them.

## The host helper

Network and system changes are applied by a fixed, root-owned host helper with
a verb-pinned `sudo` rule, not by a general-purpose privileged shell. The page
reports one of three helper states:

| State | Meaning |
|---|---|
| Ready | The helper is installed and runnable. Both sections can be edited if their stack is supported. |
| Missing | The helper file is absent. The in-product **Install host helper** action can restore it when the privileged repair path is present. |
| Denied | The helper is present but `sudo` refuses it because its rule is missing. Only re-running the control-plane installer (or upgrading) can repair this. |

**Install host helper** appears only when the helper is missing and the
in-product repair can actually run. A failed install shows the installer's
output so you can read the cause. A host that is denied, or whose root-owned
payload is gone, has no in-product repair; the page says so, and you must
re-run the control-plane installer.

Without a ready helper, the **Network** and **System** panels are read-only and
explain why. **General** is unaffected: the URL, name and timezone are stored
by the control plane and do not need the host helper.
