package teams

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestScopeAuthorizeResource(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	owner, member := uuid.New(), uuid.New()

	cases := []struct {
		name     string
		scope    Scope
		resource uuid.UUID
		creator  uuid.UUID
		write    bool
		wantErr  error
	}{
		{
			name:     "active team read",
			scope:    Scope{UserID: member, TeamID: teamA, Role: RoleReadOnly},
			resource: teamA, creator: owner,
		},
		{
			name:     "active team write refused for read_only",
			scope:    Scope{UserID: member, TeamID: teamA, Role: RoleReadOnly},
			resource: teamA, creator: owner, write: true,
			wantErr: ErrForbidden,
		},
		{
			name:     "active team write allowed for admin",
			scope:    Scope{UserID: member, TeamID: teamA, Role: RoleAdmin},
			resource: teamA, creator: owner, write: true,
		},
		{
			name:     "other team is not found",
			scope:    Scope{UserID: member, TeamID: teamA, Role: RoleOwner},
			resource: teamB, creator: owner,
			wantErr: ErrNotFound,
		},
		{
			name:     "other team write is not found",
			scope:    Scope{UserID: member, TeamID: teamA, Role: RoleOwner},
			resource: teamB, creator: owner, write: true,
			wantErr: ErrNotFound,
		},
		{
			name:     "legacy scope reads the creator's row",
			scope:    Scope{UserID: owner},
			resource: teamB, creator: owner,
		},
		{
			name:     "legacy scope refuses another creator",
			scope:    Scope{UserID: member},
			resource: teamB, creator: owner,
			wantErr: ErrNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.scope.AuthorizeResource(tc.resource, tc.creator, tc.write)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AuthorizeResource = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestScopeAuthorizeOptionalTeam(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	userID := uuid.New()

	active := Scope{UserID: userID, TeamID: teamA, Role: RoleReadOnly}
	if err := active.AuthorizeOptionalTeam(uuid.Nil, true); err != nil {
		t.Errorf("legacy row under an active scope = %v, want allowed", err)
	}
	if err := active.AuthorizeOptionalTeam(teamA, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("team row write as read_only = %v, want ErrForbidden", err)
	}
	if err := active.AuthorizeOptionalTeam(teamB, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("other team row = %v, want ErrNotFound", err)
	}

	// No team context: pre-teams behavior, nodes are shared.
	legacy := Scope{UserID: userID}
	if err := legacy.AuthorizeOptionalTeam(teamB, true); err != nil {
		t.Errorf("legacy scope on any row = %v, want allowed", err)
	}
}

func TestScopeForFallsBackAndFillsUser(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()

	// No scope stored: the creator scope (pre-teams behavior).
	scope := ScopeFor(context.Background(), userID)
	if scope.Active() || scope.UserID != userID {
		t.Fatalf("ScopeFor(no scope) = %+v, want the creator scope", scope)
	}
	if !scope.CanWrite() {
		t.Fatal("a scope without a team context must keep write access")
	}

	// A stored scope wins, and a missing user ID is filled in from the call.
	ctx := WithScope(context.Background(), Scope{TeamID: teamID, Role: RoleReadOnly})
	scope = ScopeFor(ctx, userID)
	if scope.UserID != userID || scope.TeamID != teamID || scope.Role != RoleReadOnly {
		t.Fatalf("ScopeFor(stored) = %+v", scope)
	}
	if scope.CanWrite() {
		t.Fatal("read_only must not write")
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("FromContext must report a missing scope")
	}
}
