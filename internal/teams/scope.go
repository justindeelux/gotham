package teams

import (
	"context"

	"github.com/google/uuid"
)

// scopeKey is the unexported context key for the active-team scope.
type scopeKey struct{}

// Scope is the team a request operates in: the active team, the caller and the
// caller's role in that team. The server's RequireTeam middleware resolves it
// from the optional X-Team-Id header (the caller's personal team when the
// header is absent) and stores it in the request context; resource packages
// read it with ScopeFor.
//
// A zero TeamID means no team context was resolved (the teams surface is not
// wired, or a non-HTTP caller). That is the pre-teams compatibility path: list
// queries fall back to creator scoping and mutations authorize by creator, so
// behavior is exactly what it was before teams existed.
type Scope struct {
	UserID uuid.UUID
	TeamID uuid.UUID
	Role   Role
}

// Active reports whether a team context is present.
func (s Scope) Active() bool {
	return s.TeamID != uuid.Nil
}

// CanWrite reports whether the caller's role in the active team permits
// mutations. A scope without a team context keeps pre-teams behavior.
func (s Scope) CanWrite() bool {
	if !s.Active() {
		return true
	}
	return s.Role.CanWrite()
}

// AuthorizeResource authorizes an action on one resource: read access needs
// the resource to be in the active team (or, without a team context, the caller
// to be the creator); write access additionally needs an owner/admin role. A
// resource the caller may not touch answers ErrNotFound, so resource IDs
// cannot be probed across teams.
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

// AuthorizeOptionalTeam authorizes an action on a resource whose team may be
// unset: a legacy row or a request without a team context keeps pre-teams
// behavior (nodes were shared by every authenticated user), while a team row
// follows AuthorizeResource's rules.
func (s Scope) AuthorizeOptionalTeam(resourceTeam uuid.UUID, write bool) error {
	if !s.Active() || resourceTeam == uuid.Nil {
		return nil
	}
	return s.AuthorizeResource(resourceTeam, uuid.Nil, write)
}

// WithScope stores the request's team scope in ctx.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// FromContext returns the scope stored by the server's RequireTeam middleware.
func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}

// ScopeFor returns the request's active-team scope for userID. Without a team
// context (teams not wired, or a non-HTTP caller) it returns the pre-teams
// creator scope, which is what keeps unit tests and the flag-off path
// unchanged. The stored user ID wins; userID fills it in when the caller stored
// only a team and role.
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
