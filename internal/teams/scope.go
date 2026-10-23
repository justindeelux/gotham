package teams

import (
	"context"

	"github.com/google/uuid"
)

type scopeKey struct{}

type Scope struct {
	UserID uuid.UUID
	TeamID uuid.UUID
	Role   Role
}

func (s Scope) Active() bool {
	return s.TeamID != uuid.Nil
}

func (s Scope) CanWrite() bool {
	if !s.Active() {
		return true
	}
	return s.Role.CanWrite()
}

func (s Scope) AuthorizeResource(resourceTeam, creatorID uuid.UUID, write bool) error {
	if !s.Active() {
		if creatorID == s.UserID {
			return nil
		}
		return ErrNotFound
	}
	if resourceTeam != s.TeamID {
		return ErrNotFound
	}
	if write && !s.Role.CanWrite() {
		return ErrForbidden
	}
	return nil
}

func (s Scope) AuthorizeOptionalTeam(resourceTeam uuid.UUID, write bool) error {
	if !s.Active() || resourceTeam == uuid.Nil {
		return nil
	}
	return s.AuthorizeResource(resourceTeam, uuid.Nil, write)
}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}

func ScopeFor(ctx context.Context, userID uuid.UUID) Scope {
	scope, ok := FromContext(ctx)
	if !ok {
		return Scope{UserID: userID}
	}
	if scope.UserID == uuid.Nil {
		scope.UserID = userID
	}
	return scope
}
