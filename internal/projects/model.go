package projects

import (
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/services"
)

type ResourceCounts struct {
	Applications int
	Services     int
	Databases    int
}

type Project struct {
	ID               uuid.UUID
	Name             string
	Description      string
	EnvironmentCount int
	Resources        ResourceCounts
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Environment struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Resources ResourceCounts
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EnvironmentResources struct {
	Environment  Environment
	Project      Project
	Applications []deploy.Application
	Services     []services.Service
	Databases    []databases.Database
}
