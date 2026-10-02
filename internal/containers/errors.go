package containers

import "errors"

// Sentinel errors returned by the container service and mapped to HTTP
// responses by the route handlers.
var (
	// ErrServerNotFound reports an unknown server ID or a container the
	// agent does not know.
	ErrServerNotFound = errors.New("containers: server not found")
	// ErrContainerNotFound reports a container the agent does not know.
	ErrContainerNotFound = errors.New("containers: container not found")
	// ErrAgentUnavailable reports that the node agent cannot be reached:
	// no dialer is configured yet, the connection failed, or the RPC timed
	// out.
	ErrAgentUnavailable = errors.New("containers: agent unavailable")
	// ErrValidation reports a malformed request (empty image, empty IDs).
	ErrValidation = errors.New("containers: invalid request")
	// ErrPortConflict reports a Docker host-port bind that another container
	// already holds, so the caller can answer 409 instead of 500.
	ErrPortConflict = errors.New("containers: host port already in use")
	// ErrForbidden reports a caller whose team role does not permit the
	// mutation (read_only member).
	ErrForbidden = errors.New("containers: insufficient team role")
)
