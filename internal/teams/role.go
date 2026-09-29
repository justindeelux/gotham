package teams

import "fmt"

// Role is a member's permission level inside one team. The values are the
// strings stored in team_members.role and invites.role.
type Role string

// Team roles.
const (
	// RoleOwner may do everything, including deleting the team and managing
	// owners.
	RoleOwner Role = "owner"
	// RoleAdmin may manage resources and members, except owners.
	RoleAdmin Role = "admin"
	// RoleReadOnly may read resources and members but never mutate.
	RoleReadOnly Role = "read_only"
)

// Valid reports whether r is one of the three stored roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleReadOnly:
		return true
	default:
		return false
	}
}

// CanWrite reports whether the role may create, update or delete resources.
// This is the resource-level RBAC rule: read_only reads, everyone else writes.
func (r Role) CanWrite() bool {
	return r == RoleOwner || r == RoleAdmin
}

// CanManage reports whether the role may invite or remove members and change
// roles (subject to the owner protections in the service).
func (r Role) CanManage() bool {
	return r.CanWrite()
}

// IsOwner reports whether the role is the team owner.
func (r Role) IsOwner() bool {
	return r == RoleOwner
}

// ParseRole parses a role string, rejecting anything else.
func ParseRole(raw string) (Role, error) {
	role := Role(raw)
	if !role.Valid() {
		return "", fmt.Errorf("%w: role must be one of owner, admin, read_only", ErrValidation)
	}
	return role, nil
}
