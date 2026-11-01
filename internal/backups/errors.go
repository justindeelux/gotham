package backups

import (
	"errors"

	"github.com/justindeelux/gotham/internal/storages"
)

// Storage destination errors now live in the storages domain. They are aliased
// so an error raised by either service keeps the same identity for callers and
// tests; backups-owned errors stay below.
var (
	ErrNotFound   = storages.ErrNotFound
	ErrValidation = storages.ErrValidation
	ErrConflict   = storages.ErrConflict

	ErrBuiltinProvider    = storages.ErrBuiltin
	ErrProviderInUse      = storages.ErrInUse
	ErrProviderHasBackups = storages.ErrHasBackups
	ErrTargetStranded     = storages.ErrStranded

	ErrDisabled = errors.New("backups: feature disabled")

	ErrBackupInFlight      = errors.New("backups: a backup or restore is already running")
	ErrBackupNotCompleted  = errors.New("backups: backup cannot be restored")
	ErrUnsupportedResource = errors.New("backups: this resource type cannot be backed up yet")
	ErrResourceBusy        = errors.New("backups: a deploy, restart or restore is in progress")
	ErrInsufficientSpace   = errors.New("backups: not enough free space on the backup disk")
	ErrCleanupFailed       = errors.New("backups: could not delete the backup artifact")
	ErrBackupDamaged       = errors.New("backups: this backup is damaged")
)
