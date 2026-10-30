---
title: Operating Gotham
---

# Operating Gotham

This track covers the infrastructure side of the platform: the hosts Gotham
manages, the containers running on them, and the host-level tools the owner uses
to look inside a node. It is not about deploying applications or databases;
those live under [Using Gotham](/using/).

Every workload runs on a node the owner registered, so start here.

## Access

The owner account runs the instance. It registers servers, connects Git
providers, changes instance and network settings, manages the global Domain &
SSL surface, applies updates, and opens the host terminal and file manager. An
owner API token also needs the `admin` scope for these routes.

Members work inside the projects the owner granted. They deploy and run their
applications, services and databases there; they do not see servers, server
keys, Git connections, instance settings, global proxy/DNS, updates or the host
tools. Application container terminals are owner-only as well. A member who
loses access to a project loses its resources on the next request.

See [Security](/operating/security) for the trust model behind these routes.

## Pages

- [Nodes and containers](/operating/nodes-containers) — register and edit a node,
  revalidate SSH, reset a pinned host key, read the node overview and metrics,
  and manage the node's containers and their logs.
- [Terminal and files](/operating/terminal-files) — host shells on a node and the
  remote file manager, with the feature flags that gate both.
- [App store](/operating/app-store) — install and remove host tools on a node.
- [Settings and node host settings](/operating/instance-network) — the
  control-plane address and name, plus each node's timezone, network and
  system settings.
- [Proxy and DNS](/operating/proxy-dns) — Traefik routers, certificates and DNS
  providers.
- [Security](/operating/security) — the owner and member accounts, project access,
  API tokens and the trust model behind host-level access.
