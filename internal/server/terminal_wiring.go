package server

import (
	"context"
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

// terminalOperator re-authorizes a terminal stream from the verified
// access-token identity. It proves the account still exists, resolves the
// operator role from that current account record (never from the immutable
// role claim baked into the handshake token), and, when the token carries a
// "sid", proves the refresh session is still live. The terminal stream calls it
// at attach and every revalidation tick, so a logout/revocation, role demotion
// or removed PLATFORM_ADMINS entry closes an open shell within one interval.
// The token's own expiry is checked once at the handshake; revalidation uses
// server-side state only so an 8h shell is not cut at the 15m access-token
// lifetime. A scoped API token cannot open a browser stream because the
// handshake carries an access token, not an API token.
func (s *Server) terminalOperator(ctx context.Context, claims *auth.Claims) bool {
	if claims == nil || s.auth == nil {
		return false
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return false
	}
	user, err := s.auth.Me(ctx, userID)
	if err != nil || user == nil {
		return false
	}
	// An access token minted before PF-2 has no sid; the account and its
	// current operator role still bound the stream.
	if claims.SessionID != "" {
		sessionID, err := uuid.Parse(claims.SessionID)
		if err != nil {
			return false
		}
		sessions, err := s.auth.ListSessions(ctx, userID, sessionID)
		if err != nil {
			return false
		}
		live := false
		for _, info := range sessions {
			if info.ID == sessionID {
				live = true
				break
			}
		}
		if !live {
			return false
		}
	}
	if strings.EqualFold(user.Role, "admin") {
		return true
	}
	return s.isPlatformAdminEmail(user.Email)
}
