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
	// ErrProviderDisabled is returned when an OAuth provider is unknown or not
	// configured.
	ErrProviderDisabled = errors.New("auth: oauth provider disabled")
	// ErrStateMismatch is returned when an OAuth state is missing, expired,
	// already consumed, or does not match the initiating request.
	ErrStateMismatch = errors.New("auth: oauth state mismatch")
	// ErrMissingEmail is returned when an OAuth identity exposes no usable
	// email address.
	ErrMissingEmail = errors.New("auth: oauth identity missing email")
	// ErrRegistrationClosed is returned by Register when the instance already
	// has an account and no valid invite token is supplied (P-A2: exactly one
	// admin account; members join through admin-created invites).
	ErrRegistrationClosed = errors.New("auth: registration is closed")
	// ErrCurrentPasswordIncorrect is returned by ChangePassword when the
	// account has a password and the supplied current password does not match
	// (or is missing), or when a concurrent change moved the credential after
	// the check. The message is the API contract body, so handlers render it
	// verbatim with a 400 (never 401/403: the web layer treats 401 as session
	// expiry).
	ErrCurrentPasswordIncorrect = errors.New("current password is incorrect")
	// ErrDisplayNameInvalid is returned by UpdateProfile when the trimmed name
	// is not 1-64 characters. The message is the API contract body, rendered
	// verbatim with a 400.
	ErrDisplayNameInvalid = errors.New("display name must be 1-64 characters")
)
