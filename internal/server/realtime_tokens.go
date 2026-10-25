package server

import (
	"context"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/server/ws"
)

type realtimeTokenVerifier struct{ server *Server }

func (v realtimeTokenVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ws.AuthorizationTimeout)
	defer cancel()
	claims, _, err := v.VerifyConnection(ctx, token)
	return claims, err
}

func (v realtimeTokenVerifier) VerifyConnection(ctx context.Context, token string) (*auth.Claims, string, error) {
	claims, err := v.verifyToken(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(token, auth.APITokenPrefix) {
		return v.RevalidateConnection(ctx, token, claims)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, "", auth.ErrUnauthorized
	}
	user, err := v.server.auth.Me(ctx, userID)
	if err != nil || (user.Role != auth.RoleOwner && user.Role != auth.RoleMember) {
		return nil, "", auth.ErrUnauthorized
	}
	return claims, user.Role, nil
}

func (v realtimeTokenVerifier) verifyToken(ctx context.Context, token string) (*auth.Claims, error) {
	if v.server.auth == nil {
		return nil, auth.ErrUnauthorized
	}
	if !strings.HasPrefix(token, auth.APITokenPrefix) {
		return v.server.auth.VerifyAccessToken(token)
	}
	if v.server.tokens == nil {
		return nil, auth.ErrUnauthorized
	}
	identity, err := v.server.tokens.Authenticate(ctx, token)
	if err != nil || !auth.ScopesContain(identity.Scopes, auth.ScopeRead) {
		return nil, auth.ErrUnauthorized
	}
	return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: identity.UserID.String()}}, nil
}

// The JWT was verified at upgrade; its session, rather than access-token expiry,
// determines the lifetime of an established connection.
func (v realtimeTokenVerifier) RevalidateConnection(ctx context.Context, token string, claims *auth.Claims) (*auth.Claims, string, error) {
	if strings.HasPrefix(token, auth.APITokenPrefix) {
		return v.VerifyConnection(ctx, token)
	}
	if claims == nil || v.server.persistence == nil || v.server.persistence.DB == nil {
		return nil, "", auth.ErrUnauthorized
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, "", auth.ErrUnauthorized
	}
	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return nil, "", auth.ErrUnauthorized
	}
	role, err := v.server.liveSessionRole(ctx, userID, sessionID)
	if err != nil {
		return nil, "", err
	}
	return claims, role, nil
}
