// Package proxy integrates with Traefik for routing and TLS certificates.
//
// Phase 6 (BE-6.1) uses Traefik's file provider: the control plane generates
// the complete Traefik configuration from its own state (applications with a
// base_domain and a running deployment) and pushes the rendered files to the
// node over an agent RPC. The agent writes them under the node's proxy config
// directory and pings the local Traefik API after the write. The ping proves
// the proxy process answered; it is not a claim that Traefik accepted the
// document (the file provider reloads and validates asynchronously), so the
// API never presents it as configuration acceptance. No Traefik labels are
// attached to application containers and the Phase 4 container-creation flow
// stays untouched.
//
// Two documents are generated:
//
//   - traefik.yml — static configuration: entrypoints (web/websecure plus a
//     loopback-only traefik entrypoint for /ping), the file provider and the
//     ACME certificatesResolvers (HTTP-01 on web plus one DNS-01 resolver per
//     enabled DNS provider).
//   - dynamic/gotham.yml — dynamic configuration: one HTTP forwarding router
//     per application domain, and, for routes whose certificate
//     configuration is active, an HTTPS router (with the resolved resolver,
//     and a wildcard tls.domains section where requested) plus the shared
//     gotham-https-redirect middleware on the HTTP router.
//
// Credentials never appear in either document: DNS-01 tokens reach the
// gotham-traefik container as environment variables only (see the SSL
// paragraph below).
//
// A gotham-traefik container is bootstrapped on the node during server
// validation and repaired whenever its published bindings do not match the
// production specifications (including a pre-fix container that lacks the
// loopback ping binding). The generated files are written first (the static
// file must exist before Traefik starts), then the container is created with
// the config directory mounted read-only, the ACME volume writable and ports
// 80/443 (plus a loopback-bound 8080 for /ping) published, under Docker's
// native restart policy.
//
// Routing input comes from each application's newest running deployment: the
// container is resolved through the node's container list, so pinned host
// ports and Docker-assigned ephemeral ports both work. Rows that cannot be
// routed (no running deployment, invalid or migration-disabled domain,
// duplicate binding, missing published port) become per-application
// diagnostics; the healthy routes are still pushed, so one bad row never
// freezes a node's configuration or blocks a deletion.
//
// Every mutation that affects routing — an application created, updated or
// deleted, and every deployment that reaches running — triggers a resync for
// the affected servers. Syncs are serialized (snapshot read through
// bootstrap/write), so a delayed older snapshot can never overwrite a newer
// one. The resync is best effort on the deploy path (a failure is logged and
// must never fail the mutation) and returned to this package's own domain API
// (POST /v1/proxy/sync) only. Generation is a pure function of database state,
// so syncs are idempotent: running them twice yields byte-identical files and
// the bootstrap starts an existing container instead of recreating it.
//
// Configuration history is sequenced (proxy_config_versions): the intent to
// replace the active snapshot is recorded durably before the node is touched,
// an unchanged sync records nothing, the active snapshot is never pruned, and
// a replaced predecessor stays revertable for one day measured from the
// replacement. Every failed push keeps the pending record — a failure can
// land after a partial write, a timeout, a ping or even a dial error — and
// reports a degraded outcome instead of a silent success; only a successful
// promotion clears it. Revert targets the active snapshot while a pending
// push exists so it can never fall back to an older predecessor. A global sync covers every registered node — including nodes
// whose last domain was just removed — plus every node hosting a proxied
// application.
//
// The managed gotham-traefik container is converged, not blindly reused: its
// ownership labels, image, published ports, engine-reported mounts and the
// real Docker restart policy are verified before reuse. Owned drift is
// repaired by recreation; a same-name container without ownership labels
// fails the sync with an actionable conflict and is never deleted.
//
// BE-6.2 adds the SSL surface. dns_providers stores one sealed API token per
// configured DNS provider (Cloudflare, DigitalOcean) with the zones it serves;
// domain_certificates stores one certificate intent per application. The
// generator renders one DNS-01 resolver per enabled provider next to the
// default HTTP-01 resolver, and activates the HTTPS router plus the shared
// gotham-https-redirect middleware only for a route whose certificate intent
// is explicit, enabled, still recorded for the application's current
// base_domain and backed by a usable provider. Every other route keeps the
// BE-6.1 plain-HTTP behavior. A configured-but-inactive certificate is
// reported as a per-application diagnostic instead of failing the node.
//
// DNS-01 credentials travel to the gotham-traefik container as environment
// variables only, and only for providers referenced by an active certificate
// on that node. They never enter the generated documents, the configuration
// history, the API responses or the logs. Because the engine does not report a
// running container's environment back through the agent contract, environment
// drift is detected against a recorded fingerprint label (a keyed, non-
// reversible HMAC of the desired KEY=VALUE pairs): a credential rotation or a
// provider change alters the fingerprint and recreates the container, while an
// environment changed out-of-band under Gotham is invisible until the next
// recorded change.
//
// FEATURE_PROXY=false disables the whole surface: no routes are mounted, no
// deploy hook is wired and no configuration is pushed.
package proxy
