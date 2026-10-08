package githubapp

import "errors"

// Sentinel errors. HTTP handlers map these to status codes; anything else is
// an unexpected internal failure.
var (
	// ErrNotFound is returned when a GitHub App is unknown or not owned by
	// the caller.
	ErrNotFound = errors.New("githubapp: not found")
	// ErrValidation is returned when user-supplied input fails validation,
	// including a missing, reused or expired state.
	ErrValidation = errors.New("githubapp: validation failed")
	// ErrUnauthorized is returned when a webhook delivery fails signature
	// verification.
	ErrUnauthorized = errors.New("githubapp: unauthorized")
	// ErrTooManyRequests bounds outstanding manifest/install states.
	ErrTooManyRequests = errors.New("githubapp: too many pending requests")
)
