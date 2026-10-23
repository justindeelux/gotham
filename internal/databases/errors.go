package databases

import "errors"

var (
	ErrNotFound         = errors.New("databases: not found")
	ErrValidation       = errors.New("databases: validation")
	ErrConflict         = errors.New("databases: conflict")
	ErrPortConflict     = errors.New("databases: public port already in use")
	ErrServerNotFound   = errors.New("databases: server not found")
	ErrAgentUnavailable = errors.New("databases: agent unavailable")
	ErrHealthcheck      = errors.New("databases: healthcheck failed")
	ErrDisabled         = errors.New("databases: feature disabled")
	ErrDatabaseBusy     = errors.New("databases: a backup or restore is running")
	ErrDeployInFlight   = errors.New("databases: a deploy is in progress")
	ErrServerPinned     = errors.New("databases: a database cannot change server once created")
	ErrNameConflict     = errors.New("databases: name already exists in the target environment")
)

var (
	ErrBackupInFlight     = errors.New("databases: a backup or restore is already running")
	ErrBackupNotCompleted = errors.New("databases: backup cannot be restored")
	ErrTargetStranded     = errors.New("databases: storage target destination is locked")
)
