// Package proxy integrates with Traefik for routing and TLS certificates.
//
// Phase 6 (BE-6.1) uses Traefik's file provider: the control plane generates
// the complete Traefik configuration from its own state (applications with a
// base_domain) and pushes the rendered files to the node over an agent RPC.
// The agent writes them under the node's proxy config directory and pings the
// local Traefik API to confirm the reload, so no Traefik labels are attached
// to application containers and the Phase 4 container-creation flow stays
// untouched.
//
// Two documents are generated:
//
//   - traefik.yml — static configuration: entrypoints (web/websecure plus a
//     loopback-only traefik entrypoint for /ping), the file provider and the
//     ACME certificatesResolvers (HTTP-01 on web; DNS-01 arrives with BE-6.2).
//   - dynamic/gotham.yml — dynamic configuration: one HTTPS router and one
//     HTTP→HTTPS redirect router per application domain, the load-balancer
//     service targeting the application's published host port, and the shared
//     redirectScheme middleware.
//
// A gotham-traefik container is bootstrapped on the node during server
// validation: the generated files are written first (the static file must
// exist before Traefik starts), then the container is created with the config
// directory and the ACME volume mounted and ports 80/443 (plus a
// loopback-bound 8080 for /ping) published.
//
// Every mutation that affects routing — an application created, updated or
// deleted, and every deployment that reaches running — triggers a resync for
// the affected servers. The resync is best effort on those paths (a failure is
// logged and must never fail the mutation) and returned to this package's own
// domain API (POST /v1/proxy/sync) only. Generation is a pure function of
// database state, so syncs are idempotent: running them twice yields
// byte-identical files, and bootstrap is idempotent too (an existing
// gotham-traefik container is started, never recreated).
//
// Only an application with a pinned host port is routable: without one Docker
// assigns an ephemeral port that no generated configuration can know, so the
// sync reports such a row as a validation error instead of silently dropping
// it.
//
// FEATURE_PROXY=false disables the whole surface: no routes are mounted, no
// deploy hook is wired and no configuration is pushed.
package proxy
