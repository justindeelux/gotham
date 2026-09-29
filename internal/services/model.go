package services

import (
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle of a service:
//
//	creating → running ⇄ stopped
//	    ↘ error      deleting (soft: the row and the volumes are kept)
//
// creating covers the window between the row being written and the first
// successful deploy; error is sticky until the operator deploys again or
// deletes the row.
type Status string

const (
	// StatusCreating — row written, never deployed successfully yet.
	StatusCreating Status = "creating"
	// StatusRunning — the compose project is up.
	StatusRunning Status = "running"
	// StatusStopped — the project was stopped with `compose down`; named
	// volumes are intact.
	StatusStopped Status = "stopped"
	// StatusError — the last deploy reached the node and failed.
	StatusError Status = "error"
	// StatusDeleting — soft-deleted; the row is invisible to every read and
	// the project's named volumes are retained.
	StatusDeleting Status = "deleting"
)

// Service is one compose project: the document plus the environment it is
// rendered with, on one node. ComposeYAML is the user-supplied document (never
// the rendered one) so the environment stays substitutable; the rendered
// snapshot of each deploy lives in Deploy.ComposeYAML.
type Service struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	TeamID   uuid.UUID
	ServerID uuid.UUID
	Name     string
	Status   Status
	// ComposeYAML is the stored compose document exactly as supplied.
	ComposeYAML string
	// Env is the environment variable substitution input. It is stored with
	// the row (the rendered document embeds the values anyway) and redacted
	// from every stored or returned error message.
	Env       map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt marks the soft delete; zero while the service is live.
	DeletedAt time.Time
}

// DeployState is one deploy attempt's state.
type DeployState string

const (
	// DeployDeploying — the node is being driven; the attempt is in flight.
	DeployDeploying DeployState = "deploying"
	// DeployRunning — compose reported the project up.
	DeployRunning DeployState = "running"
	// DeployFailed — the agent rejected the document or compose failed. The
	// previous project (if any) may still be running.
	DeployFailed DeployState = "failed"
	// DeployStopped — reserved for a future stop-as-history write; a stop does
	// not create a deploy row today.
	DeployStopped DeployState = "stopped"
)

// Deploy is one deploy attempt of a service. ComposeYAML is the rendered
// document exactly as it was written to the node, which is what makes the
// deploy history the rollback source: redeploying an older snapshot restores
// that version.
type Deploy struct {
	ID          uuid.UUID
	ServiceID   uuid.UUID
	State       DeployState
	ComposeYAML string
	// Error is the redacted failure message; empty while the attempt is
	// running or after it succeeded.
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt time.Time
}

// DomainRoute is one compose service mapped to a public host through the
// Gotham label convention (see the package documentation). Domain is
// normalized and validated with the Phase 6 proxy rules; Port is the container
// port a Traefik backend must target.
type DomainRoute struct {
	Service string `json:"service"`
	Domain  string `json:"domain"`
	Port    int32  `json:"port"`
}

// StorageMount is one resolved mount of a compose service: a bind mount (an
// absolute or relative host path) or a named volume reference.
type StorageMount struct {
	// Service is the compose service the mount belongs to.
	Service string `json:"service"`
	// Source is the raw source: a host path, a named volume, or empty for an
	// anonymous volume.
	Source string `json:"source,omitempty"`
	// Target is the container mount path.
	Target string `json:"target"`
	// ReadOnly is true when the mount is read-only.
	ReadOnly bool `json:"read_only,omitempty"`
	// Named is true when Source names a compose named volume (it has no "/"
	// and is not a relative path).
	Named bool `json:"named,omitempty"`
}

// ComposeContainer is one container of a project as the node agent reports it.
type ComposeContainer struct {
	Service     string
	ContainerID string
	Name        string
	Image       string
	State       string
	Status      string
	Health      string
	Ports       []string
}
