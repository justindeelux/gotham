package services

import "errors"

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrNotFound — resource does not exist or belongs to another user (404).
	ErrNotFound = errors.New("services: not found")
	// ErrValidation — invalid input: a bad name, an unparseable or
	// unsupported compose document, an unresolvable environment reference or
	// a bad label value (400).
	ErrValidation = errors.New("services: validation")
	// ErrConflict — a live service with the same name exists (409).
	ErrConflict = errors.New("services: conflict")
	// ErrServerNotFound — target server missing (404).
	ErrServerNotFound = errors.New("services: server not found")
	// ErrAgentUnavailable — node agent unreachable; the only retryable
	// failure (502).
	ErrAgentUnavailable = errors.New("services: agent unavailable")
	// ErrDeployFailed — the node ran compose and it failed (502). The message
	// carries the redacted CLI output.
	ErrDeployFailed = errors.New("services: deploy failed")
	// ErrDisabled — FEATURE_SERVICES=false disables the whole feature (503).
	ErrDisabled = errors.New("services: feature disabled")
)
