package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type versionResponse struct {
	Version string `json:"version"`
}

func (s *Server) mountVersionRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth)
		protected.Get("/v1/version", s.handleGetVersion)
	})
}

func (s *Server) currentVersion() string {
	if reporter, ok := s.servers.(versionReporter); ok {
		if v := reporter.Version(); v != "" {
			return v
		}
	}
	return "dev"
}

func (s *Server) handleGetVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, versionResponse{Version: s.currentVersion()})
}
