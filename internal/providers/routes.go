package providers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own RequireAuth accessor, so this package never imports the
// HTTP server.
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// providerResponse is the wire representation of a provider, without
// credentials.
type providerResponse struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	BaseURL   string    `json:"base_url"`
	Connected bool      `json:"connected"`
	Scopes    string    `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// providerListEnvelope wraps the provider list.
type providerListEnvelope struct {
	Providers []providerResponse `json:"providers"`
}

// repoResponse is the wire representation of a repository.
type repoResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	HTMLURL       string `json:"html_url"`
}

// repoListEnvelope wraps the repository list.
type repoListEnvelope struct {
	Repos []repoResponse `json:"repos"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the provider routes for one ProviderService.
type handler struct {
	svc    ProviderService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated provider endpoints on r:
//
//	GET /v1/providers
//	GET /v1/providers/{id}/repos
//
// auth wraps the group (the server passes its RequireAuth + read scope); a nil
// svc is a no-op so the control plane can call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ProviderService) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/providers", h.list)
		protected.Get("/v1/providers/{id}/repos", h.listRepos)
	})
}

// list serves GET /v1/providers.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	providers, err := h.svc.List(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	response := make([]providerResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, newProviderResponse(provider))
	}
	writeJSON(w, http.StatusOK, providerListEnvelope{Providers: response})
}

// listRepos serves GET /v1/providers/{id}/repos.
func (h *handler) listRepos(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	providerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid provider id"})
		return
	}

	repos, err := h.svc.ListRepos(r.Context(), userID, providerID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	response := make([]repoResponse, 0, len(repos))
	for _, repo := range repos {
		response = append(response, newRepoResponse(repo))
	}
	writeJSON(w, http.StatusOK, repoListEnvelope{Repos: response})
}

// currentUser resolves the authenticated user, answering 401 when absent.
func (h *handler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	userID, ok := h.userID(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return userID, true
}

// writeServiceError maps service sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	var httpErr *httpError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "provider not found"})
	case errors.Is(err, ErrNotConnected):
		writeJSON(w, http.StatusConflict, errorBody{Message: "provider is not connected"})
	case errors.Is(err, ErrUnsupported), errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.As(err, &httpErr):
		h.logger.Warn("providers: provider API error", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "provider unavailable"})
	default:
		h.logger.Error("providers: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// newProviderResponse maps a domain provider to its wire representation.
func newProviderResponse(p Provider) providerResponse {
	return providerResponse{
		ID:        p.ID.String(),
		Provider:  p.Name,
		BaseURL:   p.BaseURL,
		Connected: p.Connected(),
		Scopes:    p.Scopes,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

// newRepoResponse maps a domain repository to its wire representation.
func newRepoResponse(repo Repo) repoResponse {
	return repoResponse{
		ID:            repo.ExternalID,
		Name:          repo.Name,
		FullName:      repo.FullName,
		Private:       repo.Private,
		DefaultBranch: repo.DefaultBranch,
		CloneURL:      repo.CloneURL,
		SSHURL:        repo.SSHURL,
		HTMLURL:       repo.HTMLURL,
	}
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
