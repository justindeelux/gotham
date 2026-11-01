package storages

import (
	"time"

	"github.com/google/uuid"
)

// Kind is the storage backend a destination uses.
type Kind string

const (
	KindS3    Kind = "s3"
	KindLocal Kind = "local"
)

// BuiltinName is the name of the destination every owner gets on first start.
// It always points at the control-plane disk.
const BuiltinName = "Local disk"

const (
	secretKeyAccessKey = "access_key"
	secretKeySecretKey = "secret_key"
)

// Storage is one destination backups can write to. The rows live in the
// backup_targets table kept from the backups module; the domain and API are
// storage-owned.
type Storage struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Kind      Kind
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	Builtin   bool
	CreatedAt time.Time
	UpdatedAt time.Time

	// Usage is display data computed from run rows, never persisted here.
	Usage Usage
}

// Usage is per-storage usage from backup run rows. Free and total space are
// only meaningful for the local disk.
type Usage struct {
	BackupCount int64 `json:"backup_count"`
	TotalSize   int64 `json:"total_size"`
	FreeSpace   int64 `json:"free_space,omitempty"`
	TotalSpace  int64 `json:"total_space,omitempty"`
}

// Request is the create/update payload. Credentials are only ever written:
// they are sealed at rest and never returned by the API.
type Request struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Endpoint  string  `json:"endpoint,omitempty"`
	Region    *string `json:"region,omitempty"`
	Bucket    string  `json:"bucket,omitempty"`
	Prefix    *string `json:"prefix,omitempty"`
	AccessKey string  `json:"access_key,omitempty"`
	SecretKey string  `json:"secret_key,omitempty"`
}

// Check is the result of a connection test.
type Check struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Secret is one sealed credential value of a storage.
type Secret struct {
	ID         uuid.UUID
	TargetID   uuid.UUID
	Key        string
	Ciphertext string
	CreatedAt  time.Time
}
