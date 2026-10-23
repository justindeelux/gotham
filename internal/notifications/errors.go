package notifications

import "errors"

var (
	ErrValidation      = errors.New("notifications: validation")
	ErrNotFound        = errors.New("notifications: not found")
	ErrForbidden       = errors.New("notifications: forbidden")
	errNotReady        = errors.New("notifications: repository is not configured")
	errRedirectRefused = errors.New("notifications: redirect refused")
)
