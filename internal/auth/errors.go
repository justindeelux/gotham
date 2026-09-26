package auth

import "errors"

// Typed errors returned by the auth Service. Callers map them to HTTP status
// codes; anything else is an unexpected internal failure.
var (
	// ErrEmailTaken is returned by Register when the email already exists.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrInvalidCredentials is returned by Login for an unknown email or a
	// wrong password. The message is intentionally identical for both cases so
	// callers cannot enumerate accounts.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrUnauthorized is returned when a refresh token is missing, revoked,
	// expired, or otherwise unusable.
	ErrUnauthorized = errors.New("auth: unauthorized")
	// ErrInvalidToken is returned when an access token fails verification.
	ErrInvalidToken = errors.New("auth: invalid token")
	// ErrValidation is returned when user-supplied input fails validation.
	ErrValidation = errors.New("auth: validation failed")
	// ErrNotFound is returned when a resource does not exist or is not owned by
	// the caller.
	ErrNotFound = errors.New("auth: not found")
)
