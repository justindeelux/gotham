---
title: Operating Gotham
---

# Operating Gotham

This track covers the infrastructure side of the platform: the hosts Gotham
manages, the containers running on them, and the host-level tools an operator
uses to look inside a node. It is not about deploying applications or databases;
those live under [Using Gotham](/using/).

Every workload runs on a node an operator registered, so start here.

## Access

Two permission levels apply on this track:

- **Node and container management** is team-scoped. A member of the node's team
  can read the node and its containers; starting, stopping, editing or deleting
  requires the `owner` or `admin` team role. See
  [Security](/operating/security).
- **Platform operators only**: the host shell and the file manager. A platform
  operator is a session with the `admin` role claim, an account whose email is
  listed in `PLATFORM_ADMINS`, or an API token holding the `admin` scope. A team
  owner without that access gets `403` from these routes.

Application container terminals are a separate, team-level surface: a shell
inside a running application container needs write access to that application's
team, not platform-operator access. See
[Applications](/using/applications#detail-tabs).

## Pages

- [Nodes and containers](/operating/nodes-containers) — register and edit a node,
  revalidate SSH, reset a pinned host key, read the node overview and metrics,
  and manage the node's containers and their logs.
- [Terminal and files](/operating/terminal-files) — host shells on a node and the
  remote file manager, with the feature flags that gate both.
- [App store](/operating/app-store) — install and remove host tools on a node.
- [Instance network](/operating/instance-network) — control-plane address,
  hostname, DNS and time settings.
- [Proxy and DNS](/operating/proxy-dns) — Traefik routers, certificates and DNS
  providers.
- [Security](/operating/security) — operators, teams and the trust model behind
  host-level access.
