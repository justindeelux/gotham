package proxy

import "errors"

var (
	ErrValidation       = errors.New("proxy: validation")
	ErrAgentUnavailable = errors.New("proxy: agent unavailable")
	ErrReload           = errors.New("proxy: reload not confirmed")
	ErrServerNotFound   = errors.New("proxy: server not found")
	ErrPartialSync      = errors.New("proxy: partial sync")
	ErrVersionNotFound  = errors.New("proxy: no configuration version to revert to")
	ErrHistory          = errors.New("proxy: configuration history degraded")
	ErrConflict         = errors.New("proxy: conflict")
	ErrNotFound         = errors.New("proxy: not found")
	ErrForbidden        = errors.New("proxy: forbidden")
	ErrSecret           = errors.New("proxy: the deployment secret is not configured")
)
