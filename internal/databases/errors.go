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
)
