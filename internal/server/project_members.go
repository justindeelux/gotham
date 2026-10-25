package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) mountProjectMembers(api chi.Router) {
	if s.persistence == nil {
		return
	}
	api.Group(func(r chi.Router) {
		r.Use(s.RequireAuth, s.RequireOwner)
		r.Get("/v1/projects/{id}/members", s.handleProjectMembers)
		r.Put("/v1/projects/{id}/members", s.handleProjectMembers)
		r.Delete("/v1/projects/{id}/members", s.handleProjectMembers)
	})
}

func (s *Server) handleProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		hideResource(w)
		return
	}
	if _, err := s.persistence.ResourceProject(r.Context(), "project", projectID); err != nil {
		hideResource(w)
		return
	}
	before, err := s.persistence.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		writeJSON(w, 500, apiError{Message: "internal error"})
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"members": before})
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	var req struct {
		UserID  uuid.UUID   `json:"user_id"`
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch r.Method {
	case http.MethodPut:
		if req.UserID != uuid.Nil {
			if req.UserIDs != nil {
				writeJSON(w, 400, apiError{Message: "use user_id or user_ids"})
				return
			}
			err = s.persistence.GrantProject(r.Context(), projectID, req.UserID, ownerID)
		} else if req.UserIDs != nil {
			err = s.persistence.ReplaceProjectMembers(r.Context(), projectID, ownerID, req.UserIDs)
		} else {
			writeJSON(w, 400, apiError{Message: "user_id or user_ids is required"})
			return
		}
	case http.MethodDelete:
		if req.UserID == uuid.Nil {
			writeJSON(w, 400, apiError{Message: "user_id is required"})
			return
		}
		err = s.persistence.RevokeProject(r.Context(), projectID, req.UserID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			hideResource(w)
		} else {
			writeJSON(w, 500, apiError{Message: "internal error"})
		}
		return
	}
	after, err := s.persistence.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		writeJSON(w, 500, apiError{Message: "internal error"})
		return
	}
	if s.realtime != nil {
		for _, old := range before {
			retained := false
			for _, current := range after {
				if old.UserID == current.UserID {
					retained = true
					break
				}
			}
			if !retained {
				s.realtime.RevokeUserSubscriptions(old.UserID)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"members": after})
}
