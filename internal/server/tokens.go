package server

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

type TokenService interface {
	Create(ctx context.Context, userID uuid.UUID, name string, scopes []string) (*auth.CreatedToken, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*auth.APIToken, error)
	List(ctx context.Context, userID uuid.UUID) ([]auth.APIToken, error)
	Revoke(ctx context.Context, userID, id uuid.UUID) error
	Rotate(ctx context.Context, userID, id uuid.UUID) (*auth.CreatedToken, error)
	Authenticate(ctx context.Context, token string) (*auth.TokenIdentity, error)
}

type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type tokenResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenMetadata struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type tokenListResponse struct {
	Tokens []tokenMetadata `json:"tokens"`
}

func (s *Server) mountTokenRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth)
		protected.Get("/v1/tokens", s.handleListTokens)
		protected.Post("/v1/tokens", s.handleCreateToken)
		protected.Post("/v1/tokens/{id}/rotate", s.handleRotateToken)
		protected.Delete("/v1/tokens/{id}", s.handleRevokeToken)
	})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	tokens, err := s.tokens.List(r.Context(), userID)
	if err != nil {
		s.logger.Error("tokens: list", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	response := tokenListResponse{Tokens: make([]tokenMetadata, 0, len(tokens))}
	for _, token := range tokens {
		response.Tokens = append(response.Tokens, newTokenMetadata(token))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req createTokenRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	scopes, err := auth.NormalizeScopes(req.Scopes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		return
	}

	if callerScopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		if !auth.CanGrantScopes(callerScopes, scopes) {
			writeJSON(w, http.StatusForbidden, apiError{Message: tokenScopeGrantDenied})
			return
		}
	}

	if slices.Contains(scopes, auth.ScopeAdmin) && !s.isOwner(r) {
		writeJSON(w, http.StatusForbidden, apiError{Message: ownerScopeDenied})
		return
	}

	created, err := s.tokens.Create(r.Context(), userID, req.Name, scopes)
	if err != nil {
		if errors.Is(err, auth.ErrValidation) {
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
			return
		}
		s.logger.Error("tokens: create", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	s.logger.Debug("tokens: created", "token_id", created.ID.String(), "user_id", userID.String())
	writeJSON(w, http.StatusCreated, newTokenResponse(created))
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, ok := tokenIDParam(w, r)
	if !ok {
		return
	}

	if !s.authorizeTokenOperation(w, r, "rotate", userID, id, true) {
		return
	}

	rotated, err := s.tokens.Rotate(r.Context(), userID, id)
	if err != nil {
		s.writeTokenError(w, "rotate", err)
		return
	}

	s.logger.Debug("tokens: rotated", "token_id", rotated.ID.String(), "user_id", userID.String())
	writeJSON(w, http.StatusOK, newTokenResponse(rotated))
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, ok := tokenIDParam(w, r)
	if !ok {
		return
	}

	if !s.authorizeTokenOperation(w, r, "revoke", userID, id, false) {
		return
	}

	if err := s.tokens.Revoke(r.Context(), userID, id); err != nil {
		s.writeTokenError(w, "revoke", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizeTokenOperation(w http.ResponseWriter, r *http.Request, op string, userID, id uuid.UUID, requireOperatorForAdmin bool) bool {
	existing, err := s.tokens.Get(r.Context(), userID, id)
	if err != nil {
		s.writeTokenError(w, op, err)
		return false
	}
	if requireOperatorForAdmin && slices.Contains(existing.Scopes, auth.ScopeAdmin) && !s.isOwner(r) {
		writeJSON(w, http.StatusForbidden, apiError{Message: ownerScopeDenied})
		return false
	}
	if callerScopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		if !auth.CanGrantScopes(callerScopes, existing.Scopes) {
			writeJSON(w, http.StatusForbidden, apiError{Message: tokenScopeGrantDenied})
			return false
		}
	}
	return true
}

func (s *Server) writeTokenError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, auth.ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	default:
		s.logger.Error("tokens: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

func tokenIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid token id"})
		return uuid.Nil, false
	}
	return id, true
}

func newTokenResponse(created *auth.CreatedToken) tokenResponse {
	return tokenResponse{
		ID:        created.ID.String(),
		Name:      created.Name,
		Scopes:    created.Scopes,
		Token:     created.Token,
		CreatedAt: created.CreatedAt,
	}
}

func newTokenMetadata(token auth.APIToken) tokenMetadata {
	return tokenMetadata{
		ID:         token.ID.String(),
		Name:       token.Name,
		Scopes:     token.Scopes,
		LastUsedAt: token.LastUsedAt,
		RevokedAt:  token.RevokedAt,
		CreatedAt:  token.CreatedAt,
	}
}

func RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scopes, isAPIToken := ScopesFromContext(r.Context())
			if !isAPIToken || auth.ScopesAuthorize(scopes, required...) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, apiError{Message: "insufficient scope"})
		})
	}
}

var sensitiveReadSuffixes = []string{"/credentials", "/files/content"}

func requireResourceScopes(next http.Handler) http.Handler {
	read := RequireScopes(auth.ScopeRead)(next)
	deploy := RequireScopes(auth.ScopeDeploy)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !sensitiveRead(r.URL.Path) {
			read.ServeHTTP(w, r)
			return
		}
		deploy.ServeHTTP(w, r)
	})
}

func sensitiveRead(rawPath string) bool {
	cleaned := path.Clean(rawPath)
	for _, suffix := range sensitiveReadSuffixes {
		if strings.HasSuffix(cleaned, suffix) {
			return true
		}
	}
	return false
}

const tokenScopeGrantDenied = "a token may only grant scopes it holds"
