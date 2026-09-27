package deploy

import "errors"

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrNotFound — resource does not exist (404).
	ErrNotFound = errors.New("deploy: not found")
	// ErrValidation — invalid input (400).
	ErrValidation = errors.New("deploy: validation")
	// ErrConflict — an active deployment already exists for the application (409).
	ErrConflict = errors.New("deploy: conflict")
	// ErrServerNotFound — target server missing or not assigned to the user (404).
	ErrServerNotFound = errors.New("deploy: server not found")
	// ErrAgentUnavailable — node agent unreachable; the only retryable failure.
	ErrAgentUnavailable = errors.New("deploy: agent unavailable")
	// ErrHealthcheck — container did not become healthy in time (terminal).
	ErrHealthcheck = errors.New("deploy: healthcheck failed")
	// ErrDisabled — FEATURE_APPLICATIONS=false disables the whole feature (503).
	ErrDisabled = errors.New("deploy: feature disabled")
)
