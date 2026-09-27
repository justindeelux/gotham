package proxy

import "errors"

// Sentinel errors mapped to HTTP statuses by the routes layer. The
// applications CRUD path logs these instead of returning them: a failed
// proxy push must never fail a mutation (see doc.go).
var (
	// ErrValidation — invalid input (400).
	ErrValidation = errors.New("proxy: validation")
	// ErrAgentUnavailable — the node agent could not be reached or the
	// service has no dialer wired (502).
	ErrAgentUnavailable = errors.New("proxy: agent unavailable")
	// ErrReload — the configuration files were written but Traefik did not
	// answer its ping, so the reload is unconfirmed (502).
	ErrReload = errors.New("proxy: reload not confirmed")
	// ErrServerNotFound — the node has no registry row (404).
	ErrServerNotFound = errors.New("proxy: server not found")
	// ErrPartialSync — the node's healthy routes were pushed, but one or more
	// application rows could not be routed; Diagnostics lists them (200 with
	// diagnostics, never a silent success).
	ErrPartialSync = errors.New("proxy: partial sync")
	// ErrVersionNotFound — no stored configuration version to revert to (404).
	ErrVersionNotFound = errors.New("proxy: no configuration version to revert to")
)
