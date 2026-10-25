package server

import (
	"context"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/justindeelux/gotham/internal/auth"
)

type realtimeTokenVerifier struct{ server *Server }

func (v realtimeTokenVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if v.server.auth == nil {
		return nil, auth.ErrUnauthorized
	}
	if !strings.HasPrefix(token, auth.APITokenPrefix) {
		return v.server.auth.VerifyAccessToken(token)
	}
	if v.server.tokens == nil {
		return nil, auth.ErrUnauthorized
	}
	identity, err := v.server.tokens.Authenticate(context.Background(), token)
	if err != nil || !auth.ScopesContain(identity.Scopes, auth.ScopeRead) {
		return nil, auth.ErrUnauthorized
	}
	return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: identity.UserID.String()}}, nil
}
