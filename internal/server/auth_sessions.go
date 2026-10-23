package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/clientip"
)

type sessionResponse struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

type sessionsResponse struct {
	Sessions []sessionResponse `json:"sessions"`
}

func parseSessionClaim(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	sessionID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return sessionID
}

func (s *Server) sessionMeta(r *http.Request) auth.SessionMeta {
	return auth.SessionMeta{
		UserAgent: r.UserAgent(),
		IP:        clientip.ClientIP(r, s.trustedProxies),
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	sessions, err := s.auth.ListSessions(r.Context(), userID, SessionIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}
		s.logger.Error("auth: list sessions", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	out := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, sessionResponse{
			ID:         session.ID,
			UserAgent:  session.UserAgent,
			IP:         session.IP,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
			Current:    session.Current,
		})
	}
	writeJSON(w, http.StatusOK, sessionsResponse{Sessions: out})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "session not found"})
		return
	}

	if err := s.auth.RevokeSession(r.Context(), userID, id); err != nil {
		switch {
		case errors.Is(err, auth.ErrNotFound):
			writeJSON(w, http.StatusNotFound, apiError{Message: "session not found"})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: revoke session", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	if err := s.auth.RevokeOtherSessions(r.Context(), userID, SessionIDFromContext(r.Context())); err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionUnknown):
			writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: revoke other sessions", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
