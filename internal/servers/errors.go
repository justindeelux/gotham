package servers

import "errors"

var (
	ErrNotFound      = errors.New("servers: not found")
	ErrValidation    = errors.New("servers: validation failed")
	ErrConflict      = errors.New("servers: conflict")
	ErrNoCredentials = errors.New("servers: no ssh credentials")
)
