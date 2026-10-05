package projects

import (
	"time"

	"github.com/google/uuid"
)

// ResourceCounts tallies the workloads attached to a project or an
// environment. PE-2 fills these in; until then they read zero.
type ResourceCounts struct {
	Applications int
	Services     int
	Databases    int
}

// Project is one team's named group of environments.
type Project struct {
	ID               uuid.UUID
	TeamID           uuid.UUID
	Name             string
	Description      string
	EnvironmentCount int
	Resources        ResourceCounts
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Environment is one named slot of a project (production, staging, ...).
// PE-2 attaches the workloads to it.
type Environment struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Resources ResourceCounts
	CreatedAt time.Time
	UpdatedAt time.Time
}
