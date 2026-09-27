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
)
