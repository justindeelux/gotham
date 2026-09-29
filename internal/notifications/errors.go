package notifications

import "errors"

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrValidation — invalid input, unknown kind or malformed config (400).
	ErrValidation = errors.New("notifications: validation")
	// ErrNotFound — channel does not exist or belongs to another team (404).
	ErrNotFound = errors.New("notifications: not found")
	// ErrForbidden — the caller's role does not permit the mutation (403).
	ErrForbidden = errors.New("notifications: forbidden")
	// errNotReady — the service was built without a repository.
	errNotReady = errors.New("notifications: repository is not configured")
)
