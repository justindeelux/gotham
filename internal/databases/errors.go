package databases

import "errors"

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrNotFound — resource does not exist or belongs to another user (404).
	ErrNotFound = errors.New("databases: not found")
	// ErrValidation — invalid input (400).
	ErrValidation = errors.New("databases: validation")
	// ErrConflict — a live database with the same name exists (409).
	ErrConflict = errors.New("databases: conflict")
	// ErrPortConflict — the requested public port is already published on the
	// node (409).
	ErrPortConflict = errors.New("databases: public port already in use")
	// ErrServerNotFound — target server missing (404).
	ErrServerNotFound = errors.New("databases: server not found")
	// ErrAgentUnavailable — node agent unreachable; the only retryable
	// failure (502).
	ErrAgentUnavailable = errors.New("databases: agent unavailable")
	// ErrHealthcheck — the container never reached the running state within
	// the engine's window (502).
	ErrHealthcheck = errors.New("databases: healthcheck failed")
	// ErrDisabled — FEATURE_DATABASES=false disables the whole feature (503).
	ErrDisabled = errors.New("databases: feature disabled")
	// ErrDatabaseBusy — a backup or restore job owns the database's volume, so
	// a lifecycle action (Start/Restart) is refused (409).
	ErrDatabaseBusy = errors.New("databases: a backup or restore is running")
	// ErrDeployInFlight — a backup, restore or lifecycle operation holds the
	// database, so a server change is refused (409 with the contract's exact
	// body).
	ErrDeployInFlight = errors.New("databases: a deploy is in progress")
	// ErrServerPinned — the database was already created, so it cannot
	// change node (409): its container and volume live there.
	ErrServerPinned = errors.New("databases: a database cannot change server once created")
	// ErrNameConflict — the target environment already holds the name (409).
	ErrNameConflict = errors.New("databases: name already exists in the target environment")
)

// Backup-surface sentinels, mapped to HTTP statuses by the backup routes.
var (
	// ErrBackupInFlight — a backup or restore is already running for this
	// database; the dump stops the container, so only one job may hold it
	// (409).
	ErrBackupInFlight = errors.New("databases: a backup or restore is already running")
	// ErrBackupNotCompleted — only a completed backup can be restored, and a
	// completed one always has its bytes (409).
	ErrBackupNotCompleted = errors.New("databases: backup cannot be restored")
	// ErrTargetStranded — a storage target's destination cannot change while
	// completed backups still read from it (409).
	ErrTargetStranded = errors.New("databases: storage target destination is locked")
)
