package databases

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

type Database struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	ServerID        uuid.UUID
	EnvironmentID   uuid.UUID
	Name            string
	Engine          string
	Version         string
	Status          Status
	ContainerID     string
	PublicPort      int32
	StoragePath     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       time.Time
	EnvironmentName string
	ProjectID       uuid.UUID
	ProjectName     string
	ServerName      string
}

type EnvironmentRef struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
}

type Secret struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	Key        string
	Ciphertext string
	CreatedAt  time.Time
}
