package servers

import "errors"

// Sentinel errors returned by the servers domain. Callers map them to HTTP
// status codes; anything else is an unexpected internal failure.
var (
	// ErrNotFound is returned when a server or private key does not exist.
	ErrNotFound = errors.New("servers: not found")
	// ErrValidation is returned when input fails validation or an SSH
	// validation run reports a failed check.
	ErrValidation = errors.New("servers: validation failed")
	// ErrConflict is returned when a resource already exists, e.g. a node_id
	// already registered to another server.
	ErrConflict = errors.New("servers: conflict")
	// ErrNoCredentials is returned when a server has neither an SSH key nor a
	// password to authenticate with.
	ErrNoCredentials = errors.New("servers: no ssh credentials")
)
