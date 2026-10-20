package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

// terminalDialer is the mTLS TerminalService dial implemented by
// *servers.ServerService. The HTTP layer type-asserts its server registry to
// this interface, so tests with a fake registry leave the dialer unwired
// instead of forcing a wider interface change.
type terminalDialer interface {
	DialTerminalClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.TerminalClient, error)
}

// terminalService builds the host terminal registry and plugs the agent dialer.
// It returns nil without a server registry, so the routes mount nothing.
func (s *Server) terminalService() *terminal.Service {
	if s.servers == nil {
		return nil
	}
	cfg := terminal.Config{Registry: s.servers, Logger: s.logger}
	if dialer, ok := s.servers.(terminalDialer); ok {
		cfg.Dial = func(ctx context.Context, server *servers.Server) (terminal.TerminalClient, error) {
			return dialer.DialTerminalClient(ctx, server.ID)
		}
	}
	return terminal.NewService(cfg)
}

// terminalAuthorizer implements terminal.Authorizer from the auth service.
// The access token is verified once at the handshake; every later check reads
// server-side state only. The liveness key is the sign-in lineage
// (SessionInfo.CreatedAt), not the token's sid: refresh-token rotation revokes
// the presented session row and inserts a replacement with a new id but the
// same created_at, so matching the created_at survives ordinary rotation while
// logout/revoke-all/password reset delete the lineage.
type terminalAuthorizer struct {
	auth                 AuthService
	isPlatformAdminEmail func(string) bool
}

// terminalAuthorizer builds the authorizer used by the terminal routes.
func (s *Server) terminalAuthorizer() terminal.Authorizer {
	return terminalAuthorizer{auth: s.auth, isPlatformAdminEmail: s.isPlatformAdminEmail}
}

// Authorize runs at the WebSocket handshake. A token without a sid is refused:
// no server-side lineage exists that a logout could revoke. Only a host target
// is accepted: application container streams use their own authorizer, so host
// operator access never implies container access.
func (a terminalAuthorizer) Authorize(ctx context.Context, claims *auth.Claims, target terminal.Target) (terminal.Marker, error) {
	if target.Kind != terminal.TargetHost {
		return terminal.Marker{}, errors.New("terminal: the host authorizer does not serve container targets")
	}
	if claims == nil || a.auth == nil {
		return terminal.Marker{}, errors.New("terminal: no authorizer")
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
	user, err := a.auth.Me(ctx, userID)
	if err != nil {
		return terminal.Marker{}, fmt.Errorf("terminal: load account: %w", err)
	}
	if !a.operator(user) {
		return terminal.Marker{}, errors.New("terminal: platform operator access is required")
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

// Revalidate re-checks the account, the current operator role and the sign-in
// lineage. Only a definitive refusal returns (false, nil); a lookup failure
// returns an error so the caller can keep the shell and retry.
func (a terminalAuthorizer) Revalidate(ctx context.Context, claims *auth.Claims, marker terminal.Marker, target terminal.Target) (bool, error) {
	if target.Kind != terminal.TargetHost || claims == nil || a.auth == nil {
		return false, nil
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return false, nil
	}
	user, err := a.auth.Me(ctx, userID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return false, nil
		}
		return false, fmt.Errorf("terminal: load account: %w", err)
	}
	if !a.operator(user) {
		return false, nil
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

// operator resolves the operator role from the current account record, never
// from the immutable role claim in the handshake token, so a demotion is
// noticed on revalidation.
func (a terminalAuthorizer) operator(user *auth.User) bool {
	if user == nil {
		return false
	}
	if strings.EqualFold(user.Role, "admin") {
		return true
	}
	return a.isPlatformAdminEmail != nil && a.isPlatformAdminEmail(user.Email)
}
