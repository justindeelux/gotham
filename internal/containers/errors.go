package containers

import "errors"

var (
	ErrServerNotFound    = errors.New("containers: server not found")
	ErrContainerNotFound = errors.New("containers: container not found")
	ErrAgentUnavailable  = errors.New("containers: agent unavailable")
	ErrValidation        = errors.New("containers: invalid request")
	ErrPortConflict      = errors.New("containers: host port already in use")
	ErrForbidden         = errors.New("containers: insufficient team role")
)
