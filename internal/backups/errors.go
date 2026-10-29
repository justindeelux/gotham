package backups

import "errors"

var (
	ErrNotFound   = errors.New("backups: not found")
	ErrValidation = errors.New("backups: validation")
	ErrConflict   = errors.New("backups: conflict")
	ErrDisabled   = errors.New("backups: feature disabled")

	ErrBackupInFlight      = errors.New("backups: a backup or restore is already running")
	ErrBackupNotCompleted  = errors.New("backups: backup cannot be restored")
	ErrTargetStranded      = errors.New("backups: storage provider destination is locked")
	ErrBuiltinProvider     = errors.New("backups: the built-in provider cannot be changed or removed")
	ErrUnsupportedResource = errors.New("backups: this resource type cannot be backed up yet")
	ErrResourceBusy        = errors.New("backups: a deploy, restart or restore is in progress")
)
