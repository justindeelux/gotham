package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/servers"
)

// resourceHistoryService is implemented by the node server service. The route
// stays unmounted when the service is unavailable, like the other optional
// monitoring surfaces.
type resourceHistoryService interface {
	ResourceHistory(ctx context.Context, resourceType string, resourceID uuid.UUID, rangeName, service string) (servers.ResourceHistory, error)
}

func (s *Server) mountResourceHistoryRoutes(api chi.Router) {
	service, ok := s.servers.(resourceHistoryService)
	if !ok {
		return
	}
	api.Group(func(protected chi.Router) {
		protected.Use(s.resourceAuth())
		protected.Get("/v1/applications/{id}/metrics", s.handleResourceHistory(service, servers.ResourceTypeApplication))
		protected.Get("/v1/databases/{id}/metrics", s.handleResourceHistory(service, servers.ResourceTypeDatabase))
	})
}

func (s *Server) handleResourceHistory(service resourceHistoryService, resourceType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid id"})
			return
		}
		if !s.resourceExists(r, resourceType, id) {
			writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
			return
		}
		rangeName := r.URL.Query().Get("range")
		if rangeName == "" {
			rangeName = servers.ResourceRange1h
		}
		history, err := service.ResourceHistory(r.Context(), resourceType, id, rangeName, r.URL.Query().Get("service"))
		if err != nil {
			s.writeServerError(w, "resource history", err)
			return
		}
		writeJSON(w, http.StatusOK, history)
	}
}

func (s *Server) resourceExists(r *http.Request, resourceType string, id uuid.UUID) bool {
	if s.persistence == nil {
		return false
	}
	ctx := r.Context()
	var err error
	switch resourceType {
	case servers.ResourceTypeApplication:
		_, err = s.persistence.GetApplication(ctx, pgtype.UUID{Bytes: id, Valid: true})
	case servers.ResourceTypeDatabase:
		_, err = s.persistence.GetDatabase(ctx, pgtype.UUID{Bytes: id, Valid: true})
	default:
		return false
	}
	return err == nil || !errors.Is(err, pgx.ErrNoRows)
}
