// Package runtimewatch turns pushed container evidence into application and
// database runtime health: per-replica state, incidents and the read-only
// summary shown by the UI. It consumes the node observation push (GOTHAM-82)
// and the persistent incident lifecycle (GOTHAM-83); it never polls Docker.
package runtimewatch

import (
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

// Resource types accepted by monitoring settings and incidents.
const (
	ResourceApplication = "application"
	ResourceDatabase    = "database"
)

// Detector keys. They stay stable because incident identity derives from them.
const (
	DetectorHealth  = "runtime_health"
	DetectorRestart = "runtime_restart"
	DetectorOOM     = "runtime_oom"
)

// Summary states exposed to the API and UI.
const (
	StateHealthy     = "healthy"
	StateStarting    = "starting"
	StateDegraded    = "degraded"
	StateUnavailable = "unavailable"
	StateStopped     = "stopped"
	StateUnknown     = "unknown"
	StateDisabled    = "disabled"
)

// Coverage values.
const (
	CoverageComplete = "complete"
	CoverageUnknown  = "unknown"
)

// Constraints from the issue and the product contract.
const (
	DefaultGraceSeconds = 120
	MinGraceSeconds     = 30
	MaxGraceSeconds     = 600

	healthConfirmFor = 60 * time.Second
	healthRecoverFor = 60 * time.Second
	// Restart-loop recovery is the five-minute window itself: once the third
	// increase ages out there is no sustained condition left.
	restartWindow = 5 * time.Minute
	// A stuck deployment must not suppress failures forever.
	maxOperationSuppression = 10 * time.Minute
	// Three missed 15-second pushes make evidence stale.
	staleAfter       = 45 * time.Second
	maxRestartEvents = 100
)

// Settings are the owner-controlled knobs. A resource without a stored row
// uses DefaultGraceSeconds and monitoring on.
type Settings struct {
	Enabled      bool
	Grace        time.Duration
	DesiredState string
	GraceUntil   time.Time
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:      true,
		Grace:        DefaultGraceSeconds * time.Second,
		DesiredState: "running",
	}
}

// Target is one application or database assigned to the reporting node.
type Target struct {
	ResourceType string
	ResourceID   uuid.UUID
	Name         string
	NodeID       uuid.UUID
	ProjectID    uuid.UUID
	Compose      bool
	Settings     Settings
	// DatabaseStatus is the existing desired state for a managed database.
	DatabaseStatus string
	// Deployment evidence: an in-flight or just-finished release is an
	// expected transition, not a failure.
	DeploymentState    string
	DeploymentStarted  *time.Time
	DeploymentFinished *time.Time
}

// ContainerEvidence is one pushed container. It aliases the observation type
// so ownership tests can build evidence without a database.
type ContainerEvidence = servers.ContainerObservation

// ReplicaState is the persisted detector state for one replica.
type ReplicaState struct {
	Service       string
	Replica       string
	ContainerID   string
	DeploymentID  string
	State         string
	Health        string
	Reason        string
	Coverage      string
	RestartCount  int64
	RestartEvents []time.Time
	OOMContainer  string
	OOMAt         time.Time
	ObservedAt    time.Time
}
