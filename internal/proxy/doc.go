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
// Every mutation of an application with a base_domain triggers a resync for
// the affected servers. Generation is a pure function of database state, so
// syncs are idempotent: running them twice yields byte-identical files and a
// second push is a no-op for Traefik. Errors are logged on the applications
// CRUD path (a failed proxy push must never fail a mutation) and returned to
// this package's own domain API (POST /v1/proxy/sync) only.
package proxy
