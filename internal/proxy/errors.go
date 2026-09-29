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
	// answer its ping afterwards, so the write is unconfirmed (502). This is
	// a health signal, never a configuration-acceptance claim.
	ErrReload = errors.New("proxy: reload not confirmed")
	// ErrServerNotFound — the node has no registry row (404).
	ErrServerNotFound = errors.New("proxy: server not found")
	// ErrPartialSync — the node's healthy routes were pushed, but one or more
	// application rows could not be routed; Diagnostics lists them (200 with
	// diagnostics, never a silent success).
	ErrPartialSync = errors.New("proxy: partial sync")
	// ErrVersionNotFound — no stored configuration version to revert to (404).
	ErrVersionNotFound = errors.New("proxy: no configuration version to revert to")
	// ErrHistory — the node may serve the new configuration but the history
	// record could not be prepared or promoted, so the push is a degraded
	// outcome until the next same-content sync reconciles (502).
	ErrHistory = errors.New("proxy: configuration history degraded")
	// ErrConflict — a same-name container exists that this service cannot
	// prove it owns, or another routing conflict needs operator action (409).
	ErrConflict = errors.New("proxy: conflict")
	// ErrNotFound — a DNS provider, certificate config or application the
	// request targets does not exist (404). A per-application resource of
	// another team answers this too, so IDs cannot be probed.
	ErrNotFound = errors.New("proxy: not found")
	// ErrForbidden — the caller's team role does not permit the mutation
	// (read_only).
	ErrForbidden = errors.New("proxy: insufficient team role")
	// ErrSecret — the deployment secret that seals and opens DNS provider
	// credentials is not configured. Credential writes are refused instead of
	// falling back to the public empty-string key (503).
	ErrSecret = errors.New("proxy: the deployment secret is not configured (set GOTHAM_SECRET_KEY)")
)
