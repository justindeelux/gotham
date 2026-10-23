package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxRequestBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type ControlPlaneURLSource func(ctx context.Context) string

type providerResponse struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	BaseURL   string    `json:"base_url"`
	Connected bool      `json:"connected"`
	Scopes    string    `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type providerListEnvelope struct {
	Providers []providerResponse `json:"providers"`
}

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

type repoListEnvelope struct {
	Repos []repoResponse `json:"repos"`
}

type errorBody struct {
	Message string `json:"message"`
}

type createProviderRequest struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
	Scopes       string `json:"scopes"`
}

type authorizeResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type connectProviderRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type autoProvisionGitLabRequest struct {
	BaseURL     string `json:"base_url"`
	AdminToken  string `json:"admin_token"`
	Name        string `json:"name"`
	RedirectURL string `json:"redirect_url"`
	Scopes      string `json:"scopes"`
}

type gitLabSetupInfoResponse struct {
	BaseURL     string `json:"base_url"`
	RedirectURI string `json:"redirect_uri"`
	Scopes      string `json:"scopes"`
}

type branchResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	Protected bool   `json:"protected"`
}

type branchListEnvelope struct {
	Branches []branchResponse `json:"branches"`
}

type handler struct {
	svc             ProviderService
	userID          UserIDFunc
	logger          *slog.Logger
	controlPlaneURL ControlPlaneURLSource
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ProviderService) {
	MountWithControlPlaneURL(r, auth, userID, svc, nil)
}

func MountWithControlPlaneURL(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ProviderService, src ControlPlaneURLSource) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default(), controlPlaneURL: src}

	r.Get("/v1/providers/gitlab/callback", h.gitLabCallback)

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

type deleteEnvelope struct {
	Deleted bool `json:"deleted"`
}

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

func (h *handler) gitLabAutoProvision(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	var req autoProvisionGitLabRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if !h.redirectMatches(r, req.RedirectURL) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "redirect_url must name this control plane's host"})
		return
	}

	provider, err := h.svc.AutoProvisionGitLab(r.Context(), userID, AutoProvisionGitLabInput(req))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newProviderResponse(provider))
}

func (h *handler) gitLabSetupInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.currentUser(w, r); !ok {
		return
	}

	redirectURL := strings.TrimSpace(r.URL.Query().Get("redirect_url"))
	if !h.redirectMatches(r, redirectURL) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "redirect_url must name this control plane's host"})
		return
	}
	info, err := h.svc.GitLabSetupInfoFor(
		strings.TrimSpace(r.URL.Query().Get("base_url")),
		redirectURL,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitLabSetupInfoResponse(info))
}

func (h *handler) gitLabCallback(w http.ResponseWriter, r *http.Request) {
	name, err := h.svc.ConnectCallback(r.Context(),
		strings.TrimSpace(r.URL.Query().Get("code")),
		strings.TrimSpace(r.URL.Query().Get("state")),
	)
	if err != nil {
		http.Redirect(w, r, "/providers/callback?status=error&reason="+callbackReason(err), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/providers/callback?status=ok&provider="+url.QueryEscape(name), http.StatusFound)
}

func callbackReason(err error) string {
	if errors.Is(err, ErrValidation) {
		return "invalid"
	}
	return "failed"
}

func (h *handler) redirectMatches(r *http.Request, redirectURL string) bool {
	if h.controlPlaneURL != nil {
		if base := strings.TrimSpace(h.controlPlaneURL(r.Context())); base != "" {
			if control, err := url.Parse(base); err == nil && control.Hostname() != "" {
				parsed, err := url.Parse(strings.TrimSpace(redirectURL))
				if err != nil || parsed.Hostname() == "" {
					return false
				}
				return strings.EqualFold(parsed.Hostname(), control.Hostname())
			}
		}
	}
	return redirectMatchesOrigin(r, redirectURL)
}

func redirectMatchesOrigin(r *http.Request, redirectURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(redirectURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := r.Host
	if name, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = name
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	return strings.EqualFold(parsed.Hostname(), host)
}

func inUseNames(err error) string {
	if names, ok := strings.CutPrefix(err.Error(), ErrInUse.Error()+": "); ok {
		return names
	}
	return err.Error()
}

func (h *handler) providerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	providerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid provider id"})
		return uuid.Nil, false
	}
	return providerID, true
}

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

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	var httpErr *httpError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "provider not found"})
	case errors.Is(err, ErrNotConnected):
		writeJSON(w, http.StatusConflict, errorBody{Message: "provider is not connected"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "this provider is already connected"})
	case errors.Is(err, ErrInUse):
		writeJSON(w, http.StatusConflict, errorBody{Message: "applications still use this connection: " + inUseNames(err)})
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
