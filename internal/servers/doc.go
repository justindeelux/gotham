// Package servers implements the node-management domain: the server registry,
// SSH validation of candidate nodes, the private key store, the control-plane
// certificate authority, and the gRPC gateway that node agents dial.
//
// It deliberately sits next to internal/server (the HTTP server package): the
// plan's "internal/server (domain)" refers to this package, renamed to avoid a
// collision with the existing HTTP package.
package servers
