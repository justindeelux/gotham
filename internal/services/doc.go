// Package services manages compose services (Phase 7, BE-7.1): one service is
// a docker-compose project running on one managed node.
//
// The control plane owns the document: a service stores the user-supplied
// compose YAML plus the environment it is rendered with, validates and
// interpolates it here, and hands the rendered document to the node agent,
// which runs it with the node's `docker compose` CLI (the Phase 7 decision:
// real compose behavior comes from the CLI, never from a re-implementation).
// Every deploy snapshots the rendered document into service_deploys, so a
// rollback is a redeploy of the previous version and the node's project
// directory always holds the file the last deploy ran.
//
// Rendering is two pure steps: Interpolate resolves `${VAR}`, `${VAR:-default}`,
// `${VAR:?message}` and their unset-only variants against the service's stored
// environment (the result escapes every literal dollar as `$$`, so the node's
// compose run never interpolates a second time), then Parse validates the
// schema subset this package must understand. The node agent validates the rest
// with `docker compose config`, so the two layers never disagree about what
// "valid" means.
//
// # Domain map
//
// A compose service declares a public host with the Gotham label convention,
// reusing the Phase 6 proxy vocabulary instead of inventing a second one:
//
//	services:
//	  web:
//	    image: nginx:1.27
//	    labels:
//	      gotham.domain: app.example.com   # validated with proxy.ValidateDomain
//	      gotham.domain.port: "80"         # container port, default 80
//
// The map is computed from the rendered document (a label value may itself be
// a `${VAR}` reference) and returned with every service read and deploy. The
// generated Traefik route reuses the Phase 6 file provider: the proxy
// generator consumes service domains through the same generated router/service
// documents. NOTE: the generator that consumes this map is not wired yet in
// BE-7.1 — the domains are stored and reported, but no router is pushed for
// them until the proxy source learns about services (tracked as the BE-7.1
// residual gap in the phase notes).
//
// # Storage mounts
//
// Named volumes are the project's storage: compose namespaces them under the
// project name ("gotham-<service id>_<name>"), and the agent's ComposeDown
// never passes --volumes, so a stop or a delete keeps every byte. A document
// may not declare or reference a `gotham-db-` volume: those are the managed
// database volumes Phase 5 guarantees, and a compose project must not be able
// to mount (or a down ever delete) them. An anonymous volume (a bare container
// path) is removed with its container by compose down: data that must survive
// a stop belongs in a named volume.
//
// # Trust boundary
//
// A service runs user-supplied compose by design. The inputs are still bounded
// and confidential: the document is capped at MaxComposeYAML, the project name
// is derived from the service id ("gotham-<uuid>") and re-validated by the
// agent, the agent writes it mode 0600 under its own root, and environment
// values are redacted from every error this package stores or returns. Inline
// secrets the user typed directly into the YAML are the user's own content and
// are never treated as environment values.
//
// FEATURE_SERVICES=false disables the whole surface: the routes do not mount
// and no service can be created or deployed.
package services
