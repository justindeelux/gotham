package toolchain

import "errors"

var (
	ErrNotFound         = errors.New("toolchain: not found")
	ErrForbidden        = errors.New("toolchain: forbidden")
	ErrValidation       = errors.New("toolchain: invalid request")
	ErrConflict         = errors.New("toolchain: conflict")
	ErrAgentUnavailable = errors.New("toolchain: node agent unavailable")
)
