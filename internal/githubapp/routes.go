package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxRequestBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type appResponse struct {
	ID            string                 `json:"id"`
	AppID         int64                  `json:"app_id"`
	Slug          string                 `json:"slug"`
	Name          string                 `json:"name"`
	BaseURL       string                 `json:"base_url"`
	Connected     bool                   `json:"connected"`
	CreatedAt     time.Time              `json:"created_at"`
	Installations []installationResponse `json:"installations"`
}

type installationResponse struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	Account        string `json:"account"`
}

type appListEnvelope struct {
	Apps []appResponse `json:"apps"`
}

type manifestRequest struct {
	BaseURL string `json:"base_url"`
	Name    string `json:"name"`
}

type manifestResponse struct {
	ActionURL string         `json:"action_url"`
	Manifest  map[string]any `json:"manifest"`
	State     string         `json:"state"`
}

type callbackRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type installResponse struct {
	InstallURL string `json:"install_url"`
	State      string `json:"state"`
}

type recordInstallationRequest struct {
	InstallationID int64  `json:"installation_id"`
	State          string `json:"state"`
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
	Repos     []repoResponse `json:"repos"`
	Truncated bool           `json:"truncated"`
}

type branchResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	Protected bool   `json:"protected"`
}

type branchListEnvelope struct {
	Branches []branchResponse `json:"branches"`
}

type disconnectResponse struct {
	Deleted           bool     `json:"deleted"`
	ApplicationsUsing int64    `json:"applications_using"`
	Applications      []string `json:"applications"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc             *Service
	userID          UserIDFunc
	logger          *slog.Logger
	controlPlaneURL func(ctx context.Context) string
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	MountWithControlPlaneURL(r, auth, userID, svc, nil)
}

func MountWithControlPlaneURL(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service, controlPlaneURL func(ctx context.Context) string) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default(), controlPlaneURL: controlPlaneURL}
	r.Get("/v1/providers/github-app/callback", h.callbackGet)
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/providers/github-app/manifest", h.manifest)
		protected.Post("/v1/providers/github-app/callback", h.callbackPost)
		protected.Get("/v1/providers/github-app/install-state", h.installState)
		protected.Get("/v1/providers/github-app", h.list)
		protected.Get("/v1/providers/github-app/{id}/install", h.install)
		protected.Post("/v1/providers/github-app/{id}/installations", h.recordInstallation)
		protected.Get("/v1/providers/github-app/{id}/repos", h.listRepos)
		protected.Get("/v1/providers/github-app/{id}/repos/{owner}/{repo}/branches", h.listBranches)
		protected.Delete("/v1/providers/github-app/{id}", h.disconnect)
	})
}

func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req manifestRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	manifest, err := h.svc.StartManifest(r.Context(), userID, req.BaseURL, req.Name, h.origin(r))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, manifestResponse(manifest))
}

func (h *handler) callbackPost(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req callbackRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	app, err := h.svc.Callback(r.Context(), userID, req.Code, req.State)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newAppResponse(app))
}

type callbackResult string

const (
	callbackConnected callbackResult = "connected"
	callbackFailed    callbackResult = "failed"
	callbackExpired   callbackResult = "expired"
)

func (h *handler) callbackGet(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	userID, ok := h.stateUser(r.Context(), state)
	if !ok {
		h.redirectResult(w, r, "", callbackExpired)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	app, err := h.svc.Callback(r.Context(), userID, code, state)
	if err != nil {
		h.logger.Warn("githubapp: browser callback failed", "reason", err.Error())
		h.redirectResult(w, r, "", callbackResultFor(err))
		return
	}
	h.redirectResult(w, r, app.ID.String(), callbackConnected)
}

func (h *handler) redirectResult(w http.ResponseWriter, r *http.Request, appID string, result callbackResult) {
	target := "/applications/github-app/callback?github_app=" + string(result)
	if appID != "" {
		target += "&id=" + appID
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func callbackResultFor(err error) callbackResult {
	if errors.Is(err, ErrExpiredState) {
		return callbackExpired
	}
	return callbackFailed
}

func (h *handler) installState(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	owner, appID, ok := h.svc.InstallStateOwner(state)
	if !ok || owner != userID {
		writeJSON(w, http.StatusNotFound, errorBody{Message: "install state not found"})
		return
	}
	writeJSON(w, http.StatusOK, installStateResponse{AppID: appID.String()})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	apps, err := h.svc.ListApps(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]appResponse, 0, len(apps))
	for _, app := range apps {
		response = append(response, newAppResponse(app))
	}
	writeJSON(w, http.StatusOK, appListEnvelope{Apps: response})
}

type installStateResponse struct {
	AppID string `json:"app_id"`
}

func (h *handler) install(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	url, state, err := h.svc.InstallURL(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, installResponse{InstallURL: url, State: state})
}

func (h *handler) recordInstallation(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	var req recordInstallationRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	inst, err := h.svc.RecordInstallation(r.Context(), userID, appID, req.InstallationID, req.State)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, installationResponse{
		ID:             inst.ID.String(),
		InstallationID: inst.InstallationID,
		Account:        inst.Account,
	})
}

func (h *handler) listRepos(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	repos, truncated, err := h.svc.ListRepos(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]repoResponse, 0, len(repos))
	for _, repo := range repos {
		response = append(response, repoResponse{
			ID:            repo.ExternalID,
			Name:          repo.Name,
			FullName:      repo.FullName,
			Private:       repo.Private,
			DefaultBranch: repo.DefaultBranch,
			CloneURL:      repo.CloneURL,
			SSHURL:        repo.SSHURL,
			HTMLURL:       repo.HTMLURL,
		})
	}
	writeJSON(w, http.StatusOK, repoListEnvelope{Repos: response, Truncated: truncated})
}

func (h *handler) listBranches(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	repo := strings.TrimSpace(chi.URLParam(r, "owner")) + "/" + strings.TrimSpace(chi.URLParam(r, "repo"))
	branches, err := h.svc.ListBranches(r.Context(), userID, appID, repo)
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

func (h *handler) disconnect(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	using, err := h.svc.Disconnect(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, disconnectResponse{Deleted: true, ApplicationsUsing: using.ApplicationsUsing, Applications: using.Applications})
}

func (h *handler) stateUser(ctx context.Context, state string) (uuid.UUID, bool) {
	if h.svc == nil || strings.TrimSpace(state) == "" {
		return uuid.Nil, false
	}
	_ = ctx
	return h.svc.StateOwner(state)
}

func (h *handler) appID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid github app id"})
		return uuid.Nil, false
	}
	return appID, true
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
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "github app not found"})
	case errors.Is(err, ErrValidation), errors.Is(err, ErrExpiredState):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrTooManyRequests):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "too many pending requests"})
	default:
		h.logger.Error("githubapp: request failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "github unavailable"})
	}
}

func newAppResponse(app GitHubApp) appResponse {
	response := appResponse{
		ID:            app.ID.String(),
		AppID:         app.AppID,
		Slug:          app.Slug,
		Name:          app.Name,
		BaseURL:       app.BaseURL,
		Connected:     len(app.Installations) > 0,
		CreatedAt:     app.CreatedAt,
		Installations: make([]installationResponse, 0, len(app.Installations)),
	}
	for _, inst := range app.Installations {
		response.Installations = append(response.Installations, installationResponse{
			ID:             inst.ID.String(),
			InstallationID: inst.InstallationID,
			Account:        inst.Account,
		})
	}
	return response
}

func (h *handler) origin(r *http.Request) string {
	if h.controlPlaneURL != nil {
		if base := strings.TrimSpace(h.controlPlaneURL(r.Context())); base != "" {
			if control, err := url.Parse(base); err == nil && control.Scheme != "" && control.Host != "" {
				return control.Scheme + "://" + control.Host
			}
		}
	}
	return originOf(r)
}

func originOf(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
