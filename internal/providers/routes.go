package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxRequestBodyBytes bounds a provider request body.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

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

// createProviderRequest is the body of POST /v1/providers.
type createProviderRequest struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
	Scopes       string `json:"scopes"`
}

// authorizeResponse is the body returned when an OAuth connection starts.
type authorizeResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

// connectProviderRequest is the body of POST /v1/providers/{id}/connect.
type connectProviderRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// autoProvisionGitLabRequest is the body of
// POST /v1/providers/gitlab/auto-provision: a one-time admin token creates
// the GitLab OAuth application, and only the resulting client id/secret are
// stored (sealed). The admin token is never stored, logged or returned.
type autoProvisionGitLabRequest struct {
	BaseURL     string `json:"base_url"`
	AdminToken  string `json:"admin_token"`
	Name        string `json:"name"`
	RedirectURL string `json:"redirect_url"`
	Scopes      string `json:"scopes"`
}

// gitLabSetupInfoResponse answers GET /v1/providers/gitlab/setup-info: the
// exact redirect URI and scopes for a manually created OAuth application.
type gitLabSetupInfoResponse struct {
	BaseURL     string `json:"base_url"`
	RedirectURI string `json:"redirect_uri"`
	Scopes      string `json:"scopes"`
}

// branchResponse is the wire representation of a repository branch.
type branchResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	Protected bool   `json:"protected"`
}

// branchListEnvelope wraps the branch list.
type branchListEnvelope struct {
	Branches []branchResponse `json:"branches"`
}

// handler serves the provider routes for one ProviderService.
type handler struct {
	svc    ProviderService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated provider endpoints on r:
//
//	GET    /v1/providers
//	POST   /v1/providers
//	DELETE /v1/providers/{id}
//	GET    /v1/providers/{id}/authorize
//	POST   /v1/providers/{id}/connect
//	GET    /v1/providers/{id}/repos
//	GET    /v1/providers/{id}/branches?repo=<full-name>
//	POST   /v1/providers/gitlab/auto-provision
//	GET    /v1/providers/gitlab/setup-info
//
// auth wraps the group; the server passes its method-based resource scope
// boundary (read for GET/HEAD, deploy for the create/connect mutations). A nil
// svc is a no-op so the control plane can call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ProviderService) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/providers", h.list)
		protected.Post("/v1/providers", h.create)
		protected.Delete("/v1/providers/{id}", h.delete)
		protected.Get("/v1/providers/{id}/authorize", h.authorize)
		protected.Post("/v1/providers/{id}/connect", h.connect)
		protected.Get("/v1/providers/{id}/repos", h.listRepos)
		protected.Get("/v1/providers/{id}/branches", h.listBranches)
		protected.Post("/v1/providers/gitlab/auto-provision", h.gitLabAutoProvision)
		protected.Get("/v1/providers/gitlab/setup-info", h.gitLabSetupInfo)
	})
}

// create serves POST /v1/providers: it stores a new, unconnected provider
// connection from the caller's OAuth application.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	var req createProviderRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.svc.Create(r.Context(), userID, CreateProviderInput{
		Name:         req.Provider,
		BaseURL:      req.BaseURL,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		RedirectURL:  req.RedirectURL,
		Scopes:       req.Scopes,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newProviderResponse(provider))
}

// authorize serves GET /v1/providers/{id}/authorize: it returns the provider
// authorization URL and the state binding the browser to this connection.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providerID, ok := h.providerID(w, r)
	if !ok {
		return
	}

	url, state, err := h.svc.Authorize(r.Context(), userID, providerID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authorizeResponse{URL: url, State: state})
}

// connect serves POST /v1/providers/{id}/connect: it completes an OAuth
// connection by redeeming the code and state for stored tokens.
func (h *handler) connect(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providerID, ok := h.providerID(w, r)
	if !ok {
		return
	}

	var req connectProviderRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.svc.Connect(r.Context(), userID, providerID, req.Code, req.State)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newProviderResponse(provider))
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

	providerID, ok := h.providerID(w, r)
	if !ok {
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

// delete serves DELETE /v1/providers/{id}: it forgets the stored connection,
// its credentials and its cached repositories. Deleting twice is a success.
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	providerID, ok := h.providerID(w, r)
	if !ok {
		return
	}

	if err := h.svc.Delete(r.Context(), userID, providerID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteEnvelope{Deleted: true})
}

// deleteEnvelope reports an idempotent delete.
type deleteEnvelope struct {
	Deleted bool `json:"deleted"`
}

// listBranches serves GET /v1/providers/{id}/branches?repo=<full-name>.
func (h *handler) listBranches(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	providerID, ok := h.providerID(w, r)
	if !ok {
		return
	}

	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "repo is required"})
		return
	}

	branches, err := h.svc.ListBranches(r.Context(), userID, providerID, repo)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	response := make([]branchResponse, 0, len(branches))
	for _, branch := range branches {
		response = append(response, branchResponse(branch))
	}
	writeJSON(w, http.StatusOK, branchListEnvelope{Branches: response})
}

// gitLabAutoProvision serves POST /v1/providers/gitlab/auto-provision: it
// creates the GitLab OAuth application from the one-time admin token and
// stores the connection. The response never carries credentials.
func (h *handler) gitLabAutoProvision(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	var req autoProvisionGitLabRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.svc.AutoProvisionGitLab(r.Context(), userID, AutoProvisionGitLabInput(req))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newProviderResponse(provider))
}

// gitLabSetupInfo serves GET /v1/providers/gitlab/setup-info: the exact
// redirect URI and scopes for a manually created GitLab OAuth application.
func (h *handler) gitLabSetupInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.currentUser(w, r); !ok {
		return
	}

	info, err := h.svc.GitLabSetupInfoFor(
		strings.TrimSpace(r.URL.Query().Get("base_url")),
		strings.TrimSpace(r.URL.Query().Get("redirect_url")),
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitLabSetupInfoResponse(info))
}

// providerID parses the {id} path parameter, answering 400 when it is not a
// UUID.
func (h *handler) providerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	providerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid provider id"})
		return uuid.Nil, false
	}
	return providerID, true
}

// decodeJSON decodes a size-limited JSON body, rejecting unknown fields and
// trailing input.
func (h *handler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
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
	case errors.Is(err, ErrTooManyRequests):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "too many pending requests"})
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
