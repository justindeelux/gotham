package teams

import (
	"time"

	"github.com/google/uuid"
)

// Team is one team. Role carries the requesting user's role when the team was
// read through a membership lookup (Get, List); it is empty otherwise.
type Team struct {
	ID         uuid.UUID
	Name       string
	IsPersonal bool
	Role       Role
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Member is one membership, joined with the account's email for display.
type Member struct {
	UserID    uuid.UUID
	Email     string
	Role      Role
	CreatedAt time.Time
}

// Invite is a pending (or consumed) invitation to join a team. The token is
// never stored in plaintext; only its hash lives in the database and the raw
// token is returned exactly once, when the invite is created.
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
