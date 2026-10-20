package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/terminal"
)

// terminalApplicationResolver adapts the deploy domain to the transport's
// resolution contract and translates its sentinels into the terminal package's
// HTTP-facing errors. Keeping the adapter here means neither domain package
// imports the other.
type terminalApplicationResolver struct {
	deploy deploy.TerminalResolver
}

var _ terminal.ApplicationTargets = terminalApplicationResolver{}

// ListContainers implements terminal.ApplicationTargets.
func (r terminalApplicationResolver) ListContainers(ctx context.Context, userID, appID uuid.UUID) (terminal.ContainerTargetSet, error) {
	set, err := r.deploy.ListTerminalContainers(ctx, userID, appID)
	if err != nil {
		return terminal.ContainerTargetSet{}, mapTerminalTargetError(err)
	}
	return newTerminalTargetSet(set), nil
}

// ResolveContainer implements terminal.ApplicationTargets.
func (r terminalApplicationResolver) ResolveContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (terminal.ContainerTargetSet, terminal.ContainerTarget, error) {
	set, target, err := r.deploy.ResolveTerminalContainer(ctx, userID, appID, service, containerID)
	if err != nil {
		return terminal.ContainerTargetSet{}, terminal.ContainerTarget{}, mapTerminalTargetError(err)
	}
	converted := newTerminalTargetSet(set)
	for _, candidate := range converted.Containers {
		if candidate.ContainerID == target.ContainerID && candidate.Service == target.Service {
			return converted, candidate, nil
		}
	}
	return converted, terminal.ContainerTarget{}, terminal.ErrContainerNotFound
}

// newTerminalTargetSet maps the deploy view onto the transport view.
func newTerminalTargetSet(set deploy.TerminalTargetSet) terminal.ContainerTargetSet {
	converted := terminal.ContainerTargetSet{
		ApplicationID:   set.ApplicationID,
		ApplicationName: set.ApplicationName,
		ServerID:        set.ServerID,
		SourceType:      set.SourceType,
		Containers:      make([]terminal.ContainerTarget, 0, len(set.Containers)),
	}
	for _, container := range set.Containers {
		converted.Containers = append(converted.Containers, terminal.ContainerTarget{
			Service:       container.Service,
			ContainerID:   container.ContainerID,
			Name:          container.Name,
			Image:         container.Image,
			State:         container.State,
			Replica:       container.Replica,
			Label:         container.Label,
			ServerID:      set.ServerID,
			ApplicationID: set.ApplicationID,
		})
	}
	return converted
}

// mapTerminalTargetError maps deploy sentinels onto the terminal package's
// actionable errors. An unknown error is returned unchanged so the handler logs
// it (without terminal I/O) and answers a generic internal error.
func mapTerminalTargetError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, deploy.ErrDisabled):
		return terminal.ErrApplicationsDisabled
	case errors.Is(err, teams.ErrForbidden):
		return terminal.ErrForbidden
	case errors.Is(err, deploy.ErrNoRunningDeployment):
		return terminal.ErrNoRunningDeployment
	case errors.Is(err, deploy.ErrContainerNotFound):
		return terminal.ErrContainerNotFound
	case errors.Is(err, deploy.ErrContainerNotRunning):
		return terminal.ErrContainerNotRunning
	case errors.Is(err, deploy.ErrConflict), errors.Is(err, deploy.ErrDeployInFlight):
		return terminal.ErrDeployInProgress
	case errors.Is(err, deploy.ErrAgentUnavailable):
		return terminal.ErrAgentUnavailable
	case errors.Is(err, deploy.ErrNotFound), errors.Is(err, deploy.ErrServerNotFound):
		return terminal.ErrNotFound
	case errors.Is(err, deploy.ErrValidation):
		return fmt.Errorf("%w: %v", terminal.ErrInvalid, err)
	default:
		return err
	}
}

// applicationTerminalAuth is the control-route chain for container terminal
// discovery, creation and deletion: authentication, an interactive JWT session
// (a scoped API token must never drive a shell), the team scope and the team
// write gate. The WebSocket attach has its own in-band JWT check.
func (s *Server) applicationTerminalAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.withTeam()(s.requireInteractiveSession(next))
	}
}

// applicationTerminalAuthorizer authorizes a container terminal with the
// application write permission, never the platform-operator role. It also
// pins the stream to the session's sign-in lineage exactly like the host
// authorizer: refresh-token rotation keeps the created_at, while logout,
// revoke-all, password reset and account deletion remove it.
type applicationTerminalAuthorizer struct {
	resolver deploy.TerminalResolver
	auth     AuthService
	teams    teams.TeamService
}

var _ terminal.Authorizer = applicationTerminalAuthorizer{}

// Authorize runs at the application WebSocket handshake.
func (a applicationTerminalAuthorizer) Authorize(ctx context.Context, claims *auth.Claims, target terminal.Target) (terminal.Marker, error) {
	if target.Kind != terminal.TargetContainer || target.ApplicationID == uuid.Nil {
		return terminal.Marker{}, errors.New("terminal: the application authorizer only serves container targets")
	}
	if claims == nil || a.auth == nil || a.resolver == nil {
		return terminal.Marker{}, errors.New("terminal: no application authorizer")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return terminal.Marker{}, errors.New("terminal: token has no user")
	}
	if claims.SessionID == "" {
		return terminal.Marker{}, errors.New("terminal: access token has no session id; sign in again")
	}
	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil || sessionID == uuid.Nil {
		return terminal.Marker{}, errors.New("terminal: malformed session id")
	}
	access, err := a.resolver.TerminalApplicationAccess(ctx, target.ApplicationID)
	if err != nil {
		return terminal.Marker{}, fmt.Errorf("terminal: load application: %w", err)
	}
	if target.ServerID != uuid.Nil && access.ServerID != target.ServerID {
		return terminal.Marker{}, errors.New("terminal: the application moved to another node")
	}
	if err := a.authorizeWrite(ctx, userID, access); err != nil {
		return terminal.Marker{}, err
	}
	sessions, err := a.auth.ListSessions(ctx, userID, sessionID)
	if err != nil {
		return terminal.Marker{}, fmt.Errorf("terminal: list sessions: %w", err)
	}
	for _, info := range sessions {
		if info.ID == sessionID {
			return terminal.Marker{SignInAt: info.CreatedAt}, nil
		}
	}
	return terminal.Marker{}, errors.New("terminal: the session is no longer live")
}

// Revalidate re-checks the application's current write permission and the
// sign-in lineage every tick. Only a definitive refusal returns (false, nil); a
// lookup failure returns an error so the caller keeps the shell and retries.
func (a applicationTerminalAuthorizer) Revalidate(ctx context.Context, claims *auth.Claims, marker terminal.Marker, target terminal.Target) (bool, error) {
	if target.Kind != terminal.TargetContainer || target.ApplicationID == uuid.Nil ||
		claims == nil || a.auth == nil || a.resolver == nil {
		return false, nil
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return false, nil
	}
	access, err := a.resolver.TerminalApplicationAccess(ctx, target.ApplicationID)
	if err != nil {
		if errors.Is(err, deploy.ErrNotFound) || errors.Is(err, deploy.ErrValidation) {
			return false, nil
		}
		return false, fmt.Errorf("terminal: load application: %w", err)
	}
	if target.ServerID != uuid.Nil && access.ServerID != target.ServerID {
		return false, nil
	}
	if err := a.authorizeWrite(ctx, userID, access); err != nil {
		if isDefinitiveRefusal(err) {
			return false, nil
		}
		return false, fmt.Errorf("terminal: authorize application: %w", err)
	}
	sessions, err := a.auth.ListSessions(ctx, userID, uuid.Nil)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return false, nil
		}
		return false, fmt.Errorf("terminal: list sessions: %w", err)
	}
	for _, info := range sessions {
		if info.CreatedAt.Equal(marker.SignInAt) {
			return true, nil
		}
	}
	return false, nil
}

// authorizeWrite resolves the caller's current role in the application's team.
// A legacy row without a team keeps the pre-teams creator rule.
func (a applicationTerminalAuthorizer) authorizeWrite(ctx context.Context, userID uuid.UUID, access deploy.TerminalApplicationAccess) error {
	if access.TeamID == uuid.Nil {
		if userID != access.OwnerID {
			return teams.ErrForbidden
		}
		return nil
	}
	if a.teams == nil {
		// Tests and the pre-teams compatibility path: only the creator may act.
		if userID != access.OwnerID {
			return teams.ErrForbidden
		}
		return nil
	}
	role, err := a.teams.Membership(ctx, access.TeamID, userID)
	if err != nil {
		return err
	}
	if !role.CanWrite() {
		return teams.ErrForbidden
	}
	return nil
}

// isDefinitiveRefusal distinguishes "the caller may not" from "the lookup
// failed": only the first ends an open shell.
func isDefinitiveRefusal(err error) bool {
	return errors.Is(err, teams.ErrForbidden) || errors.Is(err, teams.ErrNotFound)
}
