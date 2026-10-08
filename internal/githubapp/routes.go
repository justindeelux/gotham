package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxRequestBodyBytes bounds a GitHub App request body.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own RequireAuth accessor, so this package never imports
// the HTTP server.
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// appResponse is the wire representation of a GitHub App. Secrets never
// appear here: the private key and webhook secret stay sealed server-side.
type appResponse struct {
	ID            string                 `json:"id"`
	AppID         int64                  `json:"app_id"`
	Slug          string                 `json:"slug"`
	Name          string                 `json:"name"`
	BaseURL       string                 `json:"base_url"`
	Connected     bool                   `json:"connected"`
	Installations []installationResponse `json:"installations"`
}

// installationResponse is the wire representation of one installation.
type installationResponse struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	Account        string `json:"account"`
}

// appListEnvelope wraps the app list.
type appListEnvelope struct {
	Apps []appResponse `json:"apps"`
}

// manifestRequest starts the manifest flow.
type manifestRequest struct {
	BaseURL string `json:"base_url"`
	Name    string `json:"name"`
}

// manifestResponse carries the manifest form the browser posts to the Git
// host, together with the state binding the callback to this browser.
type manifestResponse struct {
	ActionURL string         `json:"action_url"`
	Manifest  map[string]any `json:"manifest"`
	State     string         `json:"state"`
}

// callbackRequest finishes the manifest flow.
type callbackRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// installResponse starts the installation step.
type installResponse struct {
	InstallURL string `json:"install_url"`
	State      string `json:"state"`
}

// recordInstallationRequest records the setup callback.
type recordInstallationRequest struct {
	InstallationID int64  `json:"installation_id"`
	State          string `json:"state"`
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

// branchResponse is the wire representation of a branch.
type branchResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	Protected bool   `json:"protected"`
}

// branchListEnvelope wraps the branch list.
type branchListEnvelope struct {
	Branches []branchResponse `json:"branches"`
}

// disconnectResponse reports a disconnect with the in-use warning count.
type disconnectResponse struct {
	Deleted           bool  `json:"deleted"`
	ApplicationsUsing int64 `json:"applications_using"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the GitHub App routes for one Service.
type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated GitHub App endpoints on r:
//
//	POST   /v1/providers/github-app/manifest
//	POST   /v1/providers/github-app/callback   (GET also serves the browser redirect)
//	GET    /v1/providers/github-app
//	GET    /v1/providers/github-app/{id}/install
//	POST   /v1/providers/github-app/{id}/installations
//	GET    /v1/providers/github-app/{id}/repos
//	GET    /v1/providers/github-app/{id}/repos/{owner}/{repo}/branches
//	DELETE /v1/providers/github-app/{id}
//
// auth wraps the group with the server's scope boundary. A nil svc mounts
// nothing, so the control plane can call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/providers/github-app/manifest", h.manifest)
		protected.Post("/v1/providers/github-app/callback", h.callbackPost)
		protected.Get("/v1/providers/github-app/callback", h.callbackGet)
		protected.Get("/v1/providers/github-app", h.list)
		protected.Get("/v1/providers/github-app/{id}/install", h.install)
		protected.Post("/v1/providers/github-app/{id}/installations", h.recordInstallation)
		protected.Get("/v1/providers/github-app/{id}/repos", h.listRepos)
		protected.Get("/v1/providers/github-app/{id}/repos/{owner}/{repo}/branches", h.listBranches)
		protected.Delete("/v1/providers/github-app/{id}", h.disconnect)
	})
}

// manifest serves POST .../manifest: it returns the manifest form the browser
// posts to the Git host's app-creation page.
func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req manifestRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	manifest, err := h.svc.StartManifest(r.Context(), userID, req.BaseURL, req.Name, originOf(r))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, manifestResponse(manifest))
}

// callbackPost serves POST .../callback: it exchanges the manifest code for
// stored, sealed app credentials.
func (h *handler) callbackPost(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req callbackRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	app, err := h.svc.Callback(r.Context(), userID, req.Code, req.State, "")
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newAppResponse(app))
}

// callbackGet serves GET .../callback: the browser landing of the manifest
// redirect (GitHub appends ?code=...&state=...). On success it redirects to
// the applications page; failures render a JSON error.
func (h *handler) callbackGet(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if _, err := h.svc.Callback(r.Context(), userID, code, state, ""); err != nil {
		h.writeServiceError(w, err)
		return
	}
	http.Redirect(w, r, "/applications?github_app_connected=1", http.StatusFound)
}

// list serves GET .../github-app.
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

// install serves GET .../{id}/install: the URL that sends the user to the app
// installation page, plus the state binding the setup callback.
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

// recordInstallation serves POST .../{id}/installations: it stores the
// installation_id callback.
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

// listRepos serves GET .../{id}/repos.
func (h *handler) listRepos(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := h.appID(w, r)
	if !ok {
		return
	}
	repos, err := h.svc.ListRepos(r.Context(), userID, appID)
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
	writeJSON(w, http.StatusOK, repoListEnvelope{Repos: response})
}

// listBranches serves GET .../{id}/repos/{owner}/{repo}/branches.
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

// disconnect serves DELETE .../{id}: it deletes the stored credentials and
// reports how many applications still use the github_app source.
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
	writeJSON(w, http.StatusOK, disconnectResponse{Deleted: true, ApplicationsUsing: using})
}

// appID parses the {id} path parameter, answering 400 when it is not a UUID.
func (h *handler) appID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid github app id"})
		return uuid.Nil, false
	}
	return appID, true
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

// writeServiceError maps service sentinels to HTTP responses. Secrets never
// appear here: only the sentinel text and validation messages are returned.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "github app not found"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrTooManyRequests):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: "too many pending requests"})
	default:
		h.logger.Error("githubapp: request failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "github unavailable"})
	}
}

// newAppResponse maps a domain app to its wire representation.
func newAppResponse(app GitHubApp) appResponse {
	response := appResponse{
		ID:            app.ID.String(),
		AppID:         app.AppID,
		Slug:          app.Slug,
		Name:          app.Name,
		BaseURL:       app.BaseURL,
		Connected:     len(app.Installations) > 0,
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

// originOf derives the control-plane public origin from the request, so the
// manifest hook and redirect URLs point back at this server.
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

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
