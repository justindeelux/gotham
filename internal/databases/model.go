package databases

import (
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle of a managed database:
//
//	creating → running ⇄ stopped
//	    ↘ error      deleting (soft: the row and the volume are kept)
//
// creating covers the whole provision window (row written, container run,
// healthcheck pending); error is sticky until the operator deletes the row.
type Status string

const (
	// StatusCreating — row written, container not confirmed running yet.
	StatusCreating Status = "creating"
	// StatusRunning — the container is up and healthy.
	StatusRunning Status = "running"
	// StatusStopped — the container exists but is stopped (data intact).
	StatusStopped Status = "stopped"
	// StatusError — provisioning or an agent call failed; retryable by
	// deleting the row.
	StatusError Status = "error"
	// StatusDeleting — soft-deleted; the row is invisible to every read and
	// the volume is retained for the grace window.
	StatusDeleting Status = "deleting"
)

// Database is one managed database instance: a container on a node plus the
// named volume in storage_path. Credentials never live on this struct — they
// are sealed rows in database_secrets referenced by ID (database_id).
type Database struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	ServerID    uuid.UUID
	Name        string
	Engine      string
	Version     string
	Status      Status
	ContainerID string
	// PublicPort is the host port published for external clients; 0 means the
	// engine is only reachable from containers on the node (set at creation:
	// Docker port bindings cannot change on a live container).
	PublicPort int32
	// StoragePath is the named volume backing the database:
	// "gotham-db-{id}". It is kept when the container is removed.
	StoragePath string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// DeletedAt marks the soft delete; zero while the database is live.
	DeletedAt time.Time
}

// Secret is one sealed credential of a database. Ciphertext is
// base64(nonce||ciphertext) sealed with AES-256-GCM (providers.SealSecret);
// it is opened only when the runtime payload is built or when the owner asks
// for the credentials.
type Secret struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	Key        string
	Ciphertext string
	CreatedAt  time.Time
}
