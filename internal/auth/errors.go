package auth

import "errors"

var (
	ErrEmailTaken               = errors.New("auth: email already registered")
	ErrInvalidCredentials       = errors.New("auth: invalid credentials")
	ErrUnauthorized             = errors.New("auth: unauthorized")
	ErrInvalidToken             = errors.New("auth: invalid token")
	ErrValidation               = errors.New("auth: validation failed")
	ErrNotFound                 = errors.New("auth: not found")
	ErrProviderDisabled         = errors.New("auth: oauth provider disabled")
	ErrStateMismatch            = errors.New("auth: oauth state mismatch")
	ErrMissingEmail             = errors.New("auth: oauth identity missing email")
	ErrRegistrationClosed       = errors.New("auth: registration is closed")
	ErrCurrentPasswordIncorrect = errors.New("current password is incorrect")
	ErrDisplayNameInvalid       = errors.New("display name must be 1-64 characters")
	ErrSessionUnknown           = errors.New("sign in again to manage other sessions")
	ErrInvalidCode              = errors.New("invalid code")
	ErrTOTPNotConfigured        = errors.New("auth: two-factor authentication is not configured")
	ErrTOTPAlreadyEnabled       = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotEnrolled          = errors.New("start two-factor setup first")
	ErrTOTPLocked               = errors.New("too many incorrect codes, try again later")
)
