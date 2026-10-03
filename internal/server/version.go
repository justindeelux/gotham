package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// versionResponse is the body of GET /api/v1/version.
type versionResponse struct {
	Version string `json:"version"`
}

// mountVersionRoutes registers the running-binary version endpoint under /api.
// It takes only RequireAuth (no team scope): the sidebar tag renders inside
// the signed-in layout for every role.
func (s *Server) mountVersionRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth)
		protected.Get("/v1/version", s.handleGetVersion)
	})
}

// handleGetVersion returns the running control-plane binary version. It reads
// through the existing versionReporter seam (the node registry carries the
// -ldflags version) and answers "dev" without one.
func (s *Server) handleGetVersion(w http.ResponseWriter, _ *http.Request) {
	current := "dev"
	if reporter, ok := s.servers.(versionReporter); ok {
		if v := reporter.Version(); v != "" {
			current = v
		}
	}
	writeJSON(w, http.StatusOK, versionResponse{Version: current})
}
