package storages

import "errors"

// The storage destination domain owns its own sentinels. Backups aliases the
// shared ones so a provider error travelling through either API keeps the same
// identity.
var (
	ErrNotFound    = errors.New("storages: not found")
	ErrValidation  = errors.New("storages: validation")
	ErrConflict    = errors.New("storages: a storage with that name already exists")
	ErrNotReady    = errors.New("storages: storage repository is not configured")
	ErrBuiltin     = errors.New("storages: the built-in storage cannot be changed or removed")
	ErrInUse       = errors.New("storages: storage is still used by backup schedules")
	ErrHasBackups  = errors.New("storages: storage still holds backups")
	ErrStranded    = errors.New("storages: storage destination is locked by a running backup")
	ErrUnsupported = errors.New("storages: unsupported storage kind")
)
