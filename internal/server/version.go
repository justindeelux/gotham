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

// currentVersion returns the running control-plane binary version: the node
// registry's -ldflags version, or "dev" without one. It is the single source
// shared by the version endpoint and the self-update service so the sidebar
// tag and the updater cannot disagree.
func (s *Server) currentVersion() string {
	if reporter, ok := s.servers.(versionReporter); ok {
		if v := reporter.Version(); v != "" {
			return v
		}
	}
	return "dev"
}

// handleGetVersion returns the running control-plane binary version. It reads
// through the existing versionReporter seam (the node registry carries the
// -ldflags version) and answers "dev" without one.
func (s *Server) handleGetVersion(w http.ResponseWriter, _ *http.Request) {
	// Never cache: a proxy or browser serving a stale version after a
	// self-update would disagree with the running binary.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, versionResponse{Version: s.currentVersion()})
}
