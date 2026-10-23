package services

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusCreating Status = "creating"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
	StatusDeleting Status = "deleting"
)

type Service struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	TeamID          uuid.UUID
	ServerID        uuid.UUID
	EnvironmentID   uuid.UUID
	Name            string
	Status          Status
	ComposeYAML     string
	Env             map[string]string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       time.Time
	EnvironmentName string
	ProjectID       uuid.UUID
	ProjectName     string
	ServerName      string
}

type DeployState string

const (
	DeployDeploying DeployState = "deploying"
	DeployRunning   DeployState = "running"
	DeployFailed    DeployState = "failed"
	DeployStopped   DeployState = "stopped"
)

type Deploy struct {
	ID          uuid.UUID
	ServiceID   uuid.UUID
	State       DeployState
	ComposeYAML string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FinishedAt  time.Time
}

type EnvironmentRef struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
}

type DomainRoute struct {
	Service string `json:"service"`
	Domain  string `json:"domain"`
	Port    int32  `json:"port"`
}

type StorageMount struct {
	Service  string `json:"service"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Named    bool   `json:"named,omitempty"`
}

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
