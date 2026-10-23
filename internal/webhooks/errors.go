package webhooks

import "errors"

var (
	ErrNotFound       = errors.New("webhooks: not found")
	ErrForbidden      = errors.New("webhooks: insufficient team role")
	ErrValidation     = errors.New("webhooks: validation failed")
	ErrBadRequest     = errors.New("webhooks: malformed delivery")
	ErrUnauthorized   = errors.New("webhooks: signature verification failed")
	ErrRateLimited    = errors.New("webhooks: too many deliveries")
	ErrDuplicate      = errors.New("webhooks: delivery already handled")
	ErrConflict       = errors.New("webhooks: already exists")
	ErrNotConnected   = errors.New("webhooks: provider is not connected")
	ErrProvider       = errors.New("webhooks: provider call failed")
	ErrRetryable      = errors.New("webhooks: delivery can be retried")
	errCloseFenceHeld = errors.New("webhooks: preview close fence is held")
)
