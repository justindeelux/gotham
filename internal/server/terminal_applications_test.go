package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/terminal"
)

// fakeTerminalResolver implements the narrow deploy seam the application
// authorizer consumes.
type fakeTerminalResolver struct {
	access    deploy.TerminalApplicationAccess
	accessErr error
}

func (f fakeTerminalResolver) ListTerminalContainers(context.Context, uuid.UUID, uuid.UUID) (deploy.TerminalTargetSet, error) {
	return deploy.TerminalTargetSet{}, nil
}

func (f fakeTerminalResolver) ResolveTerminalContainer(context.Context, uuid.UUID, uuid.UUID, string, string) (deploy.TerminalTargetSet, deploy.TerminalContainer, error) {
	return deploy.TerminalTargetSet{}, deploy.TerminalContainer{}, nil
}

func (f fakeTerminalResolver) TerminalApplicationAccess(context.Context, uuid.UUID) (deploy.TerminalApplicationAccess, error) {
	if f.accessErr != nil {
		return deploy.TerminalApplicationAccess{}, f.accessErr
	}
	return f.access, nil
}

// newApplicationAuthorizer builds the authorizer over a rotating auth store and
// a team service that grants role to userID.
func newApplicationAuthorizer(t *testing.T, userID uuid.UUID, role teams.Role) (applicationTerminalAuthorizer, *rotatingAuth, uuid.UUID, deploy.TerminalApplicationAccess) {
	t.Helper()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	sessionID := uuid.New()
	store := &rotatingAuth{
		user:     &auth.User{Email: "dev@example.com", Role: "member"},
		sessions: []auth.SessionInfo{{ID: sessionID, CreatedAt: signIn}},
	}
	teamID := uuid.New()
	teamService := newFakeTeamService()
	teamService.allow(teamID, userID, role)
	access := deploy.TerminalApplicationAccess{
		ID:       uuid.New(),
		TeamID:   teamID,
		OwnerID:  userID,
		ServerID: uuid.New(),
	}
	authorizer := applicationTerminalAuthorizer{resolver: fakeTerminalResolver{access: access}, auth: store, teams: teamService}
	return authorizer, store, sessionID, access
}

func TestApplicationAuthorizerRequiresWriteAccess(t *testing.T) {
	targetFor := func(access deploy.TerminalApplicationAccess) terminal.Target {
		return terminal.Target{Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID}
	}
	t.Run("owner", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
		target := targetFor(access)
		marker, err := authorizer.Authorize(context.Background(), authorizerClaims(userID, sessionID), target)
		if err != nil || marker.SignInAt.IsZero() {
			t.Fatalf("owner authorize = (%+v, %v)", marker, err)
		}
		if ok, err := authorizer.Revalidate(context.Background(), authorizerClaims(userID, sessionID), marker, target); err != nil || !ok {
			t.Fatalf("owner revalidate = (%v, %v)", ok, err)
		}
	})
	t.Run("read-only role", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleReadOnly)
		target := targetFor(access)
		if _, err := authorizer.Authorize(context.Background(), authorizerClaims(userID, sessionID), target); err == nil {
			t.Fatal("a read-only member was authorized")
		}
		// Revalidation is definitive, not a transient failure.
		marker := terminal.Marker{SignInAt: time.Now().UTC()}
		if ok, err := authorizer.Revalidate(context.Background(), authorizerClaims(userID, sessionID), marker, target); err != nil || ok {
			t.Fatalf("read-only revalidate = (%v, %v), want (false, nil)", ok, err)
		}
	})
	t.Run("cross-team member", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleAdmin)
		// Replace the membership with a different user: the caller is not a
		// member of the application's team.
		teamService := newFakeTeamService()
		teamService.allow(access.TeamID, uuid.New(), teams.RoleOwner)
		authorizer.teams = teamService
		target := targetFor(access)
		if _, err := authorizer.Authorize(context.Background(), authorizerClaims(userID, sessionID), target); err == nil {
			t.Fatal("a cross-team caller was authorized")
		}
	})
	t.Run("target kind isolation", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
		if _, err := authorizer.Authorize(context.Background(), authorizerClaims(userID, sessionID),
			terminal.Target{Kind: terminal.TargetHost, ServerID: access.ServerID}); err == nil {
			t.Fatal("the application authorizer accepted a host target")
		}
		host := terminalAuthorizer{auth: &rotatingAuth{user: &auth.User{Role: "admin"}}}
		if _, err := host.Authorize(context.Background(), authorizerClaims(userID, sessionID), targetFor(access)); err == nil {
			t.Fatal("the host authorizer accepted a container target")
		}
	})
	t.Run("application moved to another node", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
		target := terminal.Target{Kind: terminal.TargetContainer, ServerID: uuid.New(), ApplicationID: access.ID}
		if _, err := authorizer.Authorize(context.Background(), authorizerClaims(userID, sessionID), target); err == nil {
			t.Fatal("a session on the previous node was authorized")
		}
	})
	t.Run("token without a sid", func(t *testing.T) {
		userID := uuid.New()
		authorizer, _, _, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
		claims := authorizerClaims(userID, uuid.New())
		claims.SessionID = ""
		if _, err := authorizer.Authorize(context.Background(), claims, terminal.Target{
			Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID,
		}); err == nil {
			t.Fatal("a token without a sid was authorized")
		}
	})
}

func TestApplicationAuthorizerSurvivesRefreshRotation(t *testing.T) {
	userID := uuid.New()
	authorizer, store, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
	target := terminal.Target{Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID}
	claims := authorizerClaims(userID, sessionID)
	marker, err := authorizer.Authorize(context.Background(), claims, target)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	// Rotate the refresh token: a new sid with the same sign-in created_at.
	replacementID := uuid.New()
	store.rotate(sessionID, replacementID)
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err != nil || !ok {
		t.Fatalf("after rotation = (%v, %v), want (true, nil)", ok, err)
	}
	store.logout(replacementID)
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err != nil || ok {
		t.Fatalf("after logout = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestApplicationAuthorizerAcceptsPreRotationToken pins the same JUS-258 rule
// on the container route: a token minted before a refresh rotation attaches
// while its sign-in lineage is alive.
func TestApplicationAuthorizerAcceptsPreRotationToken(t *testing.T) {
	userID := uuid.New()
	authorizer, store, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
	target := terminal.Target{Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID}
	claims := authorizerClaims(userID, sessionID)
	signIn := store.sessions[0].CreatedAt
	store.rotate(sessionID, uuid.New())

	marker, err := authorizer.Authorize(context.Background(), claims, target)
	if err != nil {
		t.Fatalf("Authorize with a pre-rotation token: %v", err)
	}
	if !marker.SignInAt.Equal(signIn) {
		t.Fatalf("marker = %s, want %s", marker.SignInAt, signIn)
	}
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err != nil || !ok {
		t.Fatalf("revalidate after attach = (%v, %v), want (true, nil)", ok, err)
	}
}

// TestApplicationAuthorizerRefusesPreRotationTokenOfRevokedFamily proves a
// revoked family never attaches even though its revoked row is on record.
func TestApplicationAuthorizerRefusesPreRotationTokenOfRevokedFamily(t *testing.T) {
	userID := uuid.New()
	authorizer, store, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
	target := terminal.Target{Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID}
	claims := authorizerClaims(userID, sessionID)
	store.rotate(sessionID, uuid.New())
	store.revokeAll()

	if _, err := authorizer.Authorize(context.Background(), claims, target); err == nil {
		t.Fatal("a revoked family authorized a pre-rotation token")
	}
}

func TestApplicationAuthorizerTransientErrorsKeepShell(t *testing.T) {
	userID := uuid.New()
	authorizer, store, sessionID, access := newApplicationAuthorizer(t, userID, teams.RoleOwner)
	target := terminal.Target{Kind: terminal.TargetContainer, ServerID: access.ServerID, ApplicationID: access.ID}
	claims := authorizerClaims(userID, sessionID)
	marker, err := authorizer.Authorize(context.Background(), claims, target)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	store.setListErr(errors.New("database is down"))
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err == nil || ok {
		t.Fatalf("transient list error = (%v, %v), want (false, err)", ok, err)
	}
	store.setListErr(nil)
	authorizer.resolver = fakeTerminalResolver{accessErr: errors.New("database is down")}
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err == nil || ok {
		t.Fatalf("transient app lookup = (%v, %v), want (false, err)", ok, err)
	}
	authorizer.resolver = fakeTerminalResolver{accessErr: deploy.ErrNotFound}
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, target); err != nil || ok {
		t.Fatalf("deleted application = (%v, %v), want (false, nil)", ok, err)
	}
}
