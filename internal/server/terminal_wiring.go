package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

type terminalDialer interface {
	DialTerminalClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.TerminalClient, error)
}

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

type terminalAuthorizer struct {
	auth                 AuthService
	isPlatformAdminEmail func(string) bool
}

func (s *Server) terminalAuthorizer() terminal.Authorizer {
	return terminalAuthorizer{auth: s.auth, isPlatformAdminEmail: s.isPlatformAdminEmail}
}

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
	signInAt, ok, err := resolveSignIn(ctx, a.auth, userID, sessionID, sessions)
	if err != nil {
		return terminal.Marker{}, fmt.Errorf("terminal: resolve sign-in lineage: %w", err)
	}
	if !ok {
		return terminal.Marker{}, errors.New("terminal: the session is no longer live")
	}
	return terminal.Marker{SignInAt: signInAt}, nil
}

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
	return liveSignIn(sessions, marker.SignInAt), nil
}

func resolveSignIn(ctx context.Context, authSvc AuthService, userID, sessionID uuid.UUID, sessions []auth.SessionInfo) (time.Time, bool, error) {
	for _, info := range sessions {
		if info.ID == sessionID {
			return info.CreatedAt, true, nil
		}
	}
	signInAt, found, err := authSvc.SessionSignInAt(ctx, userID, sessionID)
	if err != nil || !found {
		return time.Time{}, false, err
	}
	if !liveSignIn(sessions, signInAt) {
		return time.Time{}, false, nil
	}
	return signInAt, true, nil
}

func liveSignIn(sessions []auth.SessionInfo, signInAt time.Time) bool {
	for _, info := range sessions {
		if info.CreatedAt.Equal(signInAt) {
			return true
		}
	}
	return false
}

func (a terminalAuthorizer) operator(user *auth.User) bool {
	if user == nil {
		return false
	}
	if strings.EqualFold(user.Role, "admin") {
		return true
	}
	return a.isPlatformAdminEmail != nil && a.isPlatformAdminEmail(user.Email)
}
