// Package toolchain owns the control plane's tool install/uninstall queue: the
// persisted jobs, the ordered worker that drives each node's agent ToolService,
// and the realtime events that surface progress to the Web UI.
//
// The package is a domain layer: it depends on the store and on the agent's
// generated client, and never on the HTTP server. The HTTP routes that expose
// it live in internal/server, which maps the sentinel errors below onto status
// codes.
package toolchain
