package teams

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID         uuid.UUID
	Name       string
	IsPersonal bool
	Role       Role
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Member struct {
	UserID    uuid.UUID
	Email     string
	Role      Role
	CreatedAt time.Time
}

type Invite struct {
	ID         uuid.UUID
	TeamID     uuid.UUID
	Email      string
	Role       Role
	InvitedBy  uuid.UUID
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
}
