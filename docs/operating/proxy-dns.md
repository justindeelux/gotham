---
title: Proxy and DNS
---

# Proxy and DNS

Every node runs a managed Traefik container. The control plane generates its
complete configuration from its own state and pushes it to the node, where the
file provider loads it. Two documents are generated: the static configuration
(entrypoints and ACME resolvers) and the dynamic configuration (routers,
services, middlewares). Generation is a pure function of database state, so the
same state always produces the same files.

The console surface is **Domains & SSL**. The router list, DNS providers and
the manual sync are platform-operator operations; certificate configurations
and redirects belong to the application's team.

## Generated routers

The **Routers** tab is a read-only view of what is served. One row covers both
the HTTP and HTTPS router of a host, and lists:

- the host and the match rule,
- the source: control plane, application, compose service or redirect,
- the node,
- the entrypoints (`web`, plus `websecure` when TLS is active),
- the TLS resolver, empty for plain HTTP,
- the sync state: `synced`, `pending` or `unknown`.

Routers are generated from each application's newest **running** deployment,
from compose services with a declared host, from redirect rules, and from the
control-plane URL. A stopped or never-deployed application has no router yet. A
row that cannot be routed (no running deployment, duplicate host, missing
published port) becomes a diagnostic for that application instead of failing
the node's whole configuration.

There is nothing to edit here. Change the application's domain, the compose
service, the redirect or the control-plane URL, and the next sync regenerates
the configuration. `pending` means the freshly generated state differs from
the last version pushed to that node (or was never pushed).

## When to sync manually

Most changes sync themselves:

- creating, updating or deleting an application, and every deployment that
  reaches running, triggers a resync of its node;
- saving a certificate configuration, a redirect or a DNS provider triggers a
  best-effort resync;
- saving a new control-plane URL pushes the panel route.

A manual sync is the repair path, not the normal one. Use it when:

- the Routers tab shows `pending` after a failed or interrupted resync;
- a node was offline or unreachable while a change was saved, and has since
  come back;
- a node was added or re-registered and its configuration was never pushed.

The console has no sync button. Sync is a platform-API operation:
`POST /api/v1/proxy/sync`, with an optional `server_id` to sync one node.
Without a body it syncs every registered node and every node hosting a proxied
application. The response lists each node's outcome and any diagnostics. A
`revert` request re-pushes the node's previous stored configuration version
instead of generating from current state; a replaced version stays revertable
for one day from the replacement.

## Certificates

Certificates are recorded as intents, one per domain; the node performs the
ACME order and keeps the certificate in its own storage. A route with an active
certificate answers HTTPS and gets the shared HTTP-to-HTTPS redirect. A route
without one stays plain HTTP.

| Challenge | Use it when | Requirement |
|---|---|---|
| `http-01` | The default. | The host resolves to the node and port 80 is reachable. A proxied Cloudflare record must be DNS only. |
| `dns-01` | Port 80 is unavailable, or you need a wildcard. | A DNS provider whose configured zone covers the domain. Wildcards are only available this way. |

A `dns-01` configuration must name a provider, and the provider must hold a
zone covering the domain (and the wildcard base). A configured provider that
is disabled, or an intent whose application domain no longer matches, is
reported as a per-application diagnostic and the route falls back to HTTP.

Status is observed live from the node and never stored by the control plane:
`present` means the node's ACME storage holds a certificate for the domain and
its expiry is shown, `absent` means the storage was read and holds none, and
`unknown` means the node could not be read. `unknown` is not a failed order.
The recorded domain is the application's primary domain at save time, so after
changing the primary domain, save the certificate configuration again.

### Panel certificate

When the control-plane URL is HTTPS, its hostname gets its own certificate
intent automatically. It belongs to the instance, not to an application: it is
not created or edited from the per-application certificate form, and it is
reconciled from the URL rather than deleted from the application certificate
list. Changing or clearing the URL adds or retires it. Its state appears in
[Instance and network](/operating/instance-network) under the control-plane
domain status. With a plain-HTTP URL there is no panel certificate and the
status reads "not applicable".

## DNS providers

DNS-01 needs an API credential for the provider that hosts the zone. Two
provider types are supported: **Cloudflare** and **DigitalOcean**. Each entry
has a name, one or more zones, a credential and an enabled switch. One enabled
provider per type is allowed.

The credential is write-only from the moment it is saved:

- it is sent once over the API and sealed server-side;
- the API never returns it, and the console can set or rotate it but cannot
  display or copy it;
- leaving the credential blank while editing keeps the stored one; entering a
  new value rotates it;
- deleting a provider is refused while a certificate configuration references
  it.

Zones are recorded, not verified. The zone's nameservers must actually be
delegated to the provider, or the ACME order fails — the control plane does
not check delegation for you.

DNS credentials reach the node only as environment variables for the proxy
container, and only for providers referenced by an active certificate on that
node. They never enter the generated documents or the configuration history,
and values are scrubbed from errors and logs on the push path.

## Redirects

A redirect rule sends one exact source host to one exact target host, with a
permanent (301) or temporary (302) intent and optional path preservation.
`GET` answers the stored code; `HEAD` and other methods answer 308/307 so the
method is kept. Rules are owned by an application and cannot be moved after
creation.

The source host is globally unique and never shadows an application domain. A
target cannot be another enabled rule's source and a source cannot be another
enabled rule's target. Conflicting writes are rejected, and the generator
holds back a rule that would create a chain, so a node's served configuration
stays chain-free. The target must serve its own certificate; a redirect
terminates the request and never proxies to a backend.

Saving a rule applies it on the next dynamic configuration sync — no redeploy
is needed. If the sync cannot reach the node, the rule stays stored and is
applied the next time the node converges; see
[When to sync manually](/operating/proxy-dns#when-to-sync-manually).

For the application-side view of hosts, domains and previews, see
[Domains and previews](/using/domains-previews).
