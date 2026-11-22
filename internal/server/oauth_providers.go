package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/auth"
)

type oauthProviderList struct {
	Providers []auth.OAuthProviderInfo `json:"providers"`
}

// mountOAuthProviderRoutes adds the platform-operator API that manages the
// OAuth providers, next to the instance settings they belong to.
func (s *Server) mountOAuthProviderRoutes(api chi.Router, admin func(http.Handler) http.Handler) {
	if s.oauth == nil {
		return
	}
	api.Group(func(g chi.Router) {
		g.Use(admin)
		g.Get("/v1/instance/oauth-providers", s.handleOAuthProviderList)
		g.Put("/v1/instance/oauth-providers/{provider}", s.handleOAuthProviderUpdate)
	})
}

func (s *Server) handleOAuthProviderList(w http.ResponseWriter, r *http.Request) {
	infos, err := s.oauth.Providers(r.Context())
	if err != nil {
		s.logger.Error("oauth: list providers", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, oauthProviderList{Providers: infos})
}

func (s *Server) handleOAuthProviderUpdate(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")

	var in auth.OAuthProviderUpdate
	if !s.decodeJSON(w, r, &in) {
		return
	}

	info, err := s.oauth.UpdateProvider(r.Context(), provider, in)
	switch {
	case errors.Is(err, auth.ErrProviderUnknown):
		writeJSON(w, http.StatusNotFound, apiError{Message: err.Error()})
	case errors.Is(err, auth.ErrProviderIncomplete):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case err != nil:
		s.logger.Error("oauth: update provider", "provider", provider, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	default:
		s.logger.Info("oauth: provider updated", "provider", provider, "enabled", info.Enabled)
		writeJSON(w, http.StatusOK, info)
	}
}

// handleOAuthProviderNames lists the enabled providers for the login page.
func (s *Server) handleOAuthProviderNames(w http.ResponseWriter, r *http.Request) {
	infos, err := s.oauth.Providers(r.Context())
	if err != nil {
		s.logger.Error("oauth: list providers", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		if info.Enabled {
			names = append(names, info.Name)
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Providers []string `json:"providers"`
	}{Providers: names})
}
