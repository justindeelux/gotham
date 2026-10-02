package databases

import (
	"time"

	"github.com/google/uuid"
)

// BackupType is what triggered a backup run.
type BackupType string

const (
	// BackupManual — an operator pressed "backup now" (or the API was called).
	BackupManual BackupType = "manual"
	// BackupScheduled — the internal cron scheduler fired the job.
	BackupScheduled BackupType = "scheduled"
)

// BackupStatus is the lifecycle of one backup run:
//
//	running → completed | failed
//
// A failed run keeps its row (with the error text) so the operator can see
// what went wrong and retry; only the bytes of a completed run exist in the
// storage target.
type BackupStatus string

const (
	// BackupRunning — the temporary container is dumping (or the bytes are on
	// their way to the target).
	BackupRunning BackupStatus = "running"
	// BackupCompleted — the artifact is stored and the row carries its
	// location and size.
	BackupCompleted BackupStatus = "completed"
	// BackupFailed — the job failed; Error says why.
	BackupFailed BackupStatus = "failed"
)

// Backup is one run of the per-engine dump job. Credentials never appear on
// this struct: they are sealed rows in database_secrets and are opened only
// while a temporary container is built.
type Backup struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	// ScheduleID is set when the cron scheduler triggered the run, zero for
	// manual backups.
	ScheduleID uuid.UUID
	Type       BackupType
	Status     BackupStatus
	// Size is the compressed artifact length in bytes.
	Size int64
	// Location is where the bytes live: "s3://bucket/key" or
	// "file:///abs/path". Empty while the run is still going.
	Location string
	// TargetID is the storage target used, zero for the default local store.
	TargetID uuid.UUID
	// ContainerID is the temporary container that produced the dump, kept for
	// diagnostics after the container itself is removed.
	ContainerID string
	// WasRunning records whether the database was running when the job paused
	// it. The boot-time sweep restarts a database only when this is true, so it
	// never starts one the user had already stopped.
	WasRunning bool
	// Error holds a bounded failure summary when Status is failed.
	Error     string
	CreatedAt time.Time
	// FinishedAt is when the run reached a terminal state.
	FinishedAt time.Time
}

// RestoreStatus is the lifecycle of one restore run:
//
//	running → completed | failed
//
// The row exists so an interrupted restore is recoverable: the boot-time sweep
// finds the running ones, marks them failed and cleans up the resources they
// may still hold.
type RestoreStatus string

const (
	// RestoreRunning — the artifact is being staged or applied.
	RestoreRunning RestoreStatus = "running"
	// RestoreCompleted — the artifact was applied; the row is durable.
	RestoreCompleted RestoreStatus = "completed"
	// RestoreFailed — the job failed; Error says why.
	RestoreFailed RestoreStatus = "failed"
)

// Restore is one run of a backup's restore job. It is separate from Backup so
// a restore never mutates the dump it came from and survives the deletion of
// that dump's row (backup_id carries no foreign key).
type Restore struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	BackupID   uuid.UUID
	Status     RestoreStatus
	// Error holds a bounded failure summary when Status is failed.
	Error      string
	CreatedAt  time.Time
	FinishedAt time.Time
}

// BackupSchedule is one cron entry of the automatic backups. next_run_at is
// denormalised from Cron on every write, so the scheduler is a single
// due-time query instead of evaluating expressions at tick time.
type BackupSchedule struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	Cron       string
	// TargetID is where scheduled runs store their bytes; zero means the
	// local default.
	TargetID  uuid.UUID
	Enabled   bool
	LastRunAt time.Time
	NextRunAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TargetKind selects the storage backend of a target.
type TargetKind string

const (
	// TargetS3 — an S3-compatible endpoint (AWS S3, Cloudflare R2, MinIO).
	TargetS3 TargetKind = "s3"
	// TargetLocal — the control plane's own disk (the default when a backup
	// carries no target at all).
	TargetLocal TargetKind = "local"
)

// Sealed credential keys of a target, stored in backup_target_secrets. Both
// halves of the S3 login are sealed: an access key is still a credential and
// must never appear in a log line or an API response.
const (
	targetSecretAccessKey = "access_key"
	targetSecretSecretKey = "secret_key"
)

// BackupTarget is a storage destination. Endpoint, bucket and prefix are
// plain configuration; the credentials live sealed in backup_target_secrets
// and are opened only while a storage client is built.
type BackupTarget struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Kind      TargetKind
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TargetSecret is one sealed credential of a target, mirroring Secret.
type TargetSecret struct {
	ID         uuid.UUID
	TargetID   uuid.UUID
	Key        string
	Ciphertext string
	CreatedAt  time.Time
}
