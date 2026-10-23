package databases

import (
	"time"

	"github.com/google/uuid"
)

type BackupType string

const (
	BackupManual    BackupType = "manual"
	BackupScheduled BackupType = "scheduled"
)

type BackupStatus string

const (
	BackupRunning   BackupStatus = "running"
	BackupCompleted BackupStatus = "completed"
	BackupFailed    BackupStatus = "failed"
)

type Backup struct {
	ID          uuid.UUID
	DatabaseID  uuid.UUID
	ScheduleID  uuid.UUID
	Type        BackupType
	Status      BackupStatus
	Size        int64
	Location    string
	TargetID    uuid.UUID
	ContainerID string
	WasRunning  bool
	Error       string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

type RestoreStatus string

const (
	RestoreRunning   RestoreStatus = "running"
	RestoreCompleted RestoreStatus = "completed"
	RestoreFailed    RestoreStatus = "failed"
)

type Restore struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	BackupID   uuid.UUID
	Status     RestoreStatus
	Error      string
	CreatedAt  time.Time
	FinishedAt time.Time
}

type BackupSchedule struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	Cron       string
	TargetID   uuid.UUID
	Enabled    bool
	LastRunAt  time.Time
	NextRunAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type TargetKind string

const (
	TargetS3    TargetKind = "s3"
	TargetLocal TargetKind = "local"
)

const (
	targetSecretAccessKey = "access_key"
	targetSecretSecretKey = "secret_key"
)

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

type TargetSecret struct {
	ID         uuid.UUID
	TargetID   uuid.UUID
	Key        string
	Ciphertext string
	CreatedAt  time.Time
}
