package teams

import "fmt"

type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleReadOnly Role = "read_only"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleReadOnly:
		return true
	default:
		return false
	}
}

func (r Role) CanWrite() bool {
	return r == RoleOwner || r == RoleAdmin
}

func (r Role) CanManage() bool {
	return r.CanWrite()
}

func (r Role) IsOwner() bool {
	return r == RoleOwner
}

func ParseRole(raw string) (Role, error) {
	role := Role(raw)
	if !role.Valid() {
		return "", fmt.Errorf("%w: role must be one of owner, admin, read_only", ErrValidation)
	}
	return role, nil
}
