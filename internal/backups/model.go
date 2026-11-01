package backups

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/storages"
)

// ResourceType names a kind of resource the backup core can capture. Only
// database has a driver in this issue; the others are accepted by the API and
// rejected with a clear validation error until their drivers land.
type ResourceType string

const (
	ResourceApplication ResourceType = "application"
	ResourceDatabase    ResourceType = "database"
	ResourceService     ResourceType = "service"
)

func ParseResourceType(raw string) (ResourceType, error) {
	switch ResourceType(strings.ToLower(strings.TrimSpace(raw))) {
	case ResourceApplication:
		return ResourceApplication, nil
	case ResourceDatabase:
		return ResourceDatabase, nil
	case ResourceService:
		return ResourceService, nil
	default:
		return "", fmt.Errorf("%w: resource_type must be database, application or service", ErrValidation)
	}
}

// ResourceRef identifies the resource a run, schedule or restore belongs to.
type ResourceRef struct {
	Type ResourceType `json:"resource_type"`
	ID   uuid.UUID    `json:"resource_id"`
}

func (r ResourceRef) Valid() bool {
	return r.Type != "" && r.ID != uuid.Nil
}

func (r ResourceRef) String() string {
	return string(r.Type) + "/" + r.ID.String()
}

// Resource is a resolved resource: identity plus what the core needs to check
// provider ownership and name the resource in notifications.
type Resource struct {
	Type    ResourceType
	ID      uuid.UUID
	OwnerID uuid.UUID
	Name    string
}

func (r Resource) Ref() ResourceRef {
	return ResourceRef{Type: r.Type, ID: r.ID}
}

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
	ID           uuid.UUID
	ResourceRef  ResourceRef
	ScheduleID   uuid.UUID
	Type         BackupType
	Status       BackupStatus
	Size         int64
	Location     string
	TargetID     uuid.UUID
	ProviderName string
	// ProviderRemoved marks a run whose storage provider was deleted. The
	// artifact is unreachable but the run stays listed instead of vanishing.
	ProviderRemoved bool
	ContainerID     string
	WasRunning      bool
	// PauseDuringBackup is the choice the run was started with. False on a
	// running resource means the archive was taken live and may be
	// inconsistent; history keeps the label even if the schedule changed.
	PauseDuringBackup bool
	// ChecksumSHA256 is the hash of the stored artifact bytes. Empty on runs
	// written before integrity checks existed: those still restore, labelled
	// "not verified".
	ChecksumSHA256 string
	ChecksumAlgo   string
	Error          string
	// CleanupPending means the row is kept only until its artifact is gone.
	CleanupPending       bool
	CleanupError         string
	CleanupAttempts      int
	CleanupNextAttemptAt time.Time
	CreatedAt            time.Time
	FinishedAt           time.Time
}

type RestoreStatus string

const (
	RestoreRunning   RestoreStatus = "running"
	RestoreCompleted RestoreStatus = "completed"
	RestoreFailed    RestoreStatus = "failed"
)

type Restore struct {
	ID          uuid.UUID
	ResourceRef ResourceRef
	BackupID    uuid.UUID
	Status      RestoreStatus
	Error       string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

type BackupSchedule struct {
	ID                uuid.UUID
	ResourceRef       ResourceRef
	Cron              string
	TargetID          uuid.UUID
	Enabled           bool
	LastRunAt         time.Time
	NextRunAt         time.Time
	LastSlotAt        time.Time
	TargetChangedAt   time.Time
	MaxRunSeconds     int
	PauseDuringBackup bool
	// KeepLast is the number of completed scheduled runs to keep. Nil means
	// unlimited; schedules created before GOTHAM-131 stay nil so an upgrade
	// never deletes a backup the owner did not agree to lose.
	KeepLast  *int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Storage destinations are owned by internal/storages now. The names below are
// kept so the backup code and its API aliases read naturally.
type (
	BackupTarget  = storages.Storage
	ProviderUsage = storages.Usage
	TargetKind    = storages.Kind
	TargetCheck   = storages.Check
	TargetSecret  = storages.Secret
)

const (
	TargetS3    = storages.KindS3
	TargetLocal = storages.KindLocal

	// BuiltinProviderName is the name of the storage every owner gets on
	// first start. It always points at the control-plane disk.
	BuiltinProviderName = storages.BuiltinName
)

// ArtifactCleanup is an artifact left behind by a hard-deleted resource. It has
// no resource foreign key on purpose: it must outlive the backup row cascade.
type ArtifactCleanup struct {
	ID            uuid.UUID
	TargetID      uuid.UUID
	Location      string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
}

// Content is one item a resource's backup captures. The resource tab lists it
// so a run never looks like it captures more than it does.
type Content struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}
