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
	// ErrDeployInFlight — a deployment is running, so a server change is
	// refused (409 with the contract's exact body).
	ErrDeployInFlight = errors.New("deploy: a deploy is in progress")
	// ErrNameConflict — the target environment already holds the name (409).
	ErrNameConflict = errors.New("deploy: name already exists in the target environment")
	// ErrServerNotFound — target server missing or not assigned to the user (404).
	ErrServerNotFound = errors.New("deploy: server not found")
	// ErrAgentUnavailable — node agent unreachable; the only retryable failure.
	ErrAgentUnavailable = errors.New("deploy: agent unavailable")
	// ErrHealthcheck — container did not become healthy in time (terminal).
	ErrHealthcheck = errors.New("deploy: healthcheck failed")
	// ErrDisabled — FEATURE_APPLICATIONS=false disables the whole feature (503).
	ErrDisabled = errors.New("deploy: feature disabled")
	// ErrNotConnected — the caller has no usable connection for the
	// application's provider, so no deploy key can be registered (409).
	ErrNotConnected = errors.New("deploy: provider is not connected")
	// ErrProvider — a Git-host API call (registering or removing a deploy key)
	// failed; handlers answer a gateway status instead of leaking the body.
	ErrProvider = errors.New("deploy: provider call failed")
)
