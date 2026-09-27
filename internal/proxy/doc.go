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
//     ACME certificatesResolvers (HTTP-01 on web; DNS-01 arrives with BE-6.2).
//   - dynamic/gotham.yml — dynamic configuration: one HTTP forwarding router
//     and one HTTPS router per application domain, and the load-balancer
//     service targeting the application's live published host port. The
//     HTTP→HTTPS redirect is emitted by BE-6.2 once certificates are
//     configured and verified; BE-6.1 must serve plain HTTP.
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
// Each successfully pushed configuration is retained for one day
// (proxy_config_versions) and can be re-pushed with the same API's revert
// flag, which is the phase plan's fast-revert path for a bad rollout. A global
// sync covers every registered node — including nodes whose last domain was
// just removed — plus every node hosting a proxied application.
//
// FEATURE_PROXY=false disables the whole surface: no routes are mounted, no
// deploy hook is wired and no configuration is pushed.
package proxy
