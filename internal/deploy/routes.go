package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// maxBodyBytes bounds deploy request bodies (the rollback body is tiny).
const maxBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors providers.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// deploymentResponse is the wire representation of a deployment. Sealed
// secrets never appear here — only image references, state and error text.
type deploymentResponse struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	Kind          Kind       `json:"kind"`
	State         State      `json:"state"`
	ImageTag      string     `json:"image_tag,omitempty"`
	RegistryImage string     `json:"registry_image,omitempty"`
	Digest        string     `json:"digest,omitempty"`
	Error         string     `json:"error,omitempty"`
	Attempt       int32      `json:"attempt"`
	ContainerID   string     `json:"container_id,omitempty"`
	RollbackFrom  string     `json:"rollback_from,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// deploymentEnvelope wraps a single deployment.
type deploymentEnvelope struct {
	Deployment deploymentResponse `json:"deployment"`
}

// deploymentListEnvelope wraps a deployment list.
type deploymentListEnvelope struct {
	Deployments []deploymentResponse `json:"deployments"`
}

// rollbackRequest is the optional body of POST .../rollback. With no body the
// server picks the previous successful release.
type rollbackRequest struct {
	DeploymentID string `json:"deployment_id"`
}

// applicationResponse is the wire representation of an application. It mirrors
// `Application` in web/src/features/applications/api/applications.ts field for field: server_id is
// null while no node is assigned.
type applicationResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	EnvironmentID   string `json:"environment_id"`
	EnvironmentName string `json:"environment_name"`
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	Provider        string `json:"provider"`
	Repo            string `json:"repo"`
	CloneURL        string `json:"clone_url"`
	SourceType      string `json:"source_type"`
	// GitHubAppID is the linked GitHub App connection, empty when unlinked.
	GitHubAppID string `json:"github_app_id"`
	Branch      string `json:"branch"`
	BuildPack   string `json:"build_pack"`
	// ImageRef is the prebuilt reference of an image source (GS-9).
	ImageRef string `json:"image_ref"`
	// HasRegistryCredential reports whether a private-registry credential is
	// stored. The username and password are never returned by the API.
	HasRegistryCredential bool   `json:"has_registry_credential"`
	BaseDomain            string `json:"base_domain"`
	// DockerfileContent holds pasted Dockerfile text for dockerfile
	// applications (empty otherwise); BuildArgs holds its --build-arg pairs
	// (never nil on the wire).
	DockerfileContent string            `json:"dockerfile_content"`
	BuildArgs         map[string]string `json:"build_args"`
	// BaseDomainDisabled marks a binding disabled by the domain-uniqueness
	// migration (legacy duplicate); the value is preserved and an explicit
	// domain update re-enables it.
	BaseDomainDisabled bool      `json:"base_domain_disabled"`
	Port               int32     `json:"port"`
	HostPort           int32     `json:"host_port"`
	ServerID           *string   `json:"server_id"`
	ServerName         string    `json:"server_name"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// applicationEnvelope wraps a single application. Webhook reports the
// automatic hook install on create (BE-4.4); it is absent when no install was
// attempted (a pasted public URL, or no hook lifecycle wired) and on the
// read/update responses, which never attempt one.
type applicationEnvelope struct {
	Application applicationResponse `json:"application"`
	Webhook     *webhookOutcome     `json:"webhook,omitempty"`
}

// webhookOutcome is the wire report of the automatic provider-hook install on
// create. installed=false means the application was created but its
// auto-deploy hook was not (the provider call failed or stalled); the caller
// can retry the idempotent install through the webhook route named in error.
// Provider detail stays in the server log, never in this field.
type webhookOutcome struct {
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}

// applicationListEnvelope wraps an application list.
type applicationListEnvelope struct {
	Applications []applicationListItem `json:"applications"`
}

// applicationListItem is the wire representation of an application in list
// responses. It mirrors applicationResponse except the Dockerfile source
// fields: pasted text and --build-arg values travel only on the detail
// routes (get/create/update), so a list read never exposes them. ARG values
// persist in image history on the node, so they must never be treated as
// secrets — the UI warns next to the field instead.
type applicationListItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	EnvironmentID   string `json:"environment_id"`
	EnvironmentName string `json:"environment_name"`
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	Provider        string `json:"provider"`
	Repo            string `json:"repo"`
	CloneURL        string `json:"clone_url"`
	SourceType      string `json:"source_type"`
	// GitHubAppID is the linked GitHub App connection, empty when unlinked.
	GitHubAppID string `json:"github_app_id"`
	Branch      string `json:"branch"`
	BuildPack   string `json:"build_pack"`
	// ImageRef is the prebuilt reference of an image source (GS-9).
	ImageRef string `json:"image_ref"`
	// HasRegistryCredential reports whether a private-registry credential
	// is stored (never the credential itself).
	HasRegistryCredential bool   `json:"has_registry_credential"`
	BaseDomain            string `json:"base_domain"`
	// BaseDomainDisabled marks a binding disabled by the domain-uniqueness
	// migration (legacy duplicate); the value is preserved and an explicit
	// domain update re-enables it.
	BaseDomainDisabled bool      `json:"base_domain_disabled"`
	Port               int32     `json:"port"`
	HostPort           int32     `json:"host_port"`
	ServerID           *string   `json:"server_id"`
	ServerName         string    `json:"server_name"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// envEntryRequest is one environment row on the wire (`EnvVar` in the FE). A
// value carrying the `secret:` prefix names a sealed secret; the API answers
// with `secret:<id>` references and never with plaintext.
type envEntryRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// envListEnvelope wraps the environment collection (`env` in the FE payload).
type envListEnvelope struct {
	Env []envEntryRequest `json:"env"`
}

// storageRequest is one storage mapping on the wire (`StorageMapping` in the
// FE). The collection is named `storage` everywhere — the create payload, the
// replace body and the read response — so the FE reuses one field name.
type storageRequest struct {
	Name          string `json:"name"`
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
}

// storageListEnvelope wraps the storage collection.
type storageListEnvelope struct {
	Storage []storageRequest `json:"storage"`
}

// createApplicationRequest is the POST /applications body; it matches
// CreateApplicationInput in web/src/features/applications/api/applications.ts.
type createApplicationRequest struct {
	Name          string `json:"name"`
	EnvironmentID string `json:"environment_id"`
	Provider      string `json:"provider"`
	Repo          string `json:"repo"`
	CloneURL      string `json:"clone_url"`
	SourceType    string `json:"source_type"`
	// GitHubAppID links the application to its GitHub App connection (GS-5);
	// empty leaves it unlinked.
	GitHubAppID string `json:"github_app_id"`
	Branch      string `json:"branch"`
	BuildPack   string `json:"build_pack"`
	// ImageRef and the registry credential are the image-source (GS-9)
	// fields: the reference to pull and one optional private-registry
	// credential, sealed at rest and never returned by the API.
	ImageRef         string            `json:"image_ref"`
	RegistryUsername string            `json:"registry_username"`
	RegistryPassword string            `json:"registry_password"`
	BaseDomain       string            `json:"base_domain"`
	Port             int32             `json:"port"`
	HostPort         int32             `json:"host_port"`
	ServerID         string            `json:"server_id"`
	Env              []envEntryRequest `json:"env"`
	Storage          []storageRequest  `json:"storage"`
	// DockerfileContent holds pasted Dockerfile text for the dockerfile
	// source type (GS-7); BuildArgs holds its optional --build-arg pairs.
	DockerfileContent string            `json:"dockerfile_content"`
	BuildArgs         map[string]string `json:"build_args"`
}

// updateApplicationRequest is the PUT /applications/{id} body. Fields are
// optional pointers: absent fields stay unchanged. server_id must name a
// server (clearing it is a 400 since the assignment is required).
type updateApplicationRequest struct {
	Name             *string `json:"name"`
	EnvironmentID    *string `json:"environment_id"`
	Branch           *string `json:"branch"`
	BuildPack        *string `json:"build_pack"`
	ImageRef         *string `json:"image_ref"`
	RegistryUsername *string `json:"registry_username"`
	RegistryPassword *string `json:"registry_password"`
	BaseDomain       *string `json:"base_domain"`
	Port             *int32  `json:"port"`
	HostPort         *int32  `json:"host_port"`
	ServerID         *string `json:"server_id"`
	// GitHubAppID relinks the application: a UUID sets the connection, an
	// empty string clears it, absent leaves it unchanged.
	GitHubAppID *string `json:"github_app_id"`
	// DockerfileContent replaces the stored Dockerfile text (dockerfile
	// applications only); BuildArgs replaces the whole --build-arg
	// collection (absent leaves it unchanged).
	DockerfileContent *string            `json:"dockerfile_content"`
	BuildArgs         *map[string]string `json:"build_args"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// deployKeyResponse is the wire representation of an application deploy key.
// Only the public half, its fingerprint and the provider's own key ID appear
// here; the private key stays sealed on the server.
type deployKeyResponse struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Provider      string    `json:"provider"`
	Repo          string    `json:"repo"`
	ProviderKeyID string    `json:"provider_key_id,omitempty"`
	Fingerprint   string    `json:"fingerprint"`
	PublicKey     string    `json:"public_key"`
	CreatedAt     time.Time `json:"created_at"`
}

// deployKeyEnvelope wraps a single deploy key.
type deployKeyEnvelope struct {
	DeployKey deployKeyResponse `json:"deploy_key"`
}

// deleteKeyEnvelope reports an idempotent deploy-key delete.
type deleteKeyEnvelope struct {
	Deleted bool `json:"deleted"`
}

// handler serves the application deploy routes for one DeployService.
type handler struct {
	svc    DeployService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated application endpoints on r:
//
//	POST   /v1/applications
//	GET    /v1/applications
//	GET    /v1/applications/{id}
//	PUT    /v1/applications/{id}
//	DELETE /v1/applications/{id}
//	GET    /v1/applications/{id}/env
//	PUT    /v1/applications/{id}/env
//	GET    /v1/applications/{id}/storages
//	PUT    /v1/applications/{id}/storages
//	POST   /v1/applications/{id}/stop
//	POST   /v1/applications/{id}/start
//	POST   /v1/applications/{id}/deploy
//	GET    /v1/applications/{id}/deployments
//	POST   /v1/applications/{id}/rollback
//	POST   /v1/applications/{id}/deploy-key
//	DELETE /v1/applications/{id}/deploy-key
//
// auth wraps the group (the server passes its RequireAuth); a nil svc or
// FEATURE_APPLICATIONS=false mounts nothing, so the control plane can call
// Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc DeployService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/applications", h.createApplication)
		protected.Get("/v1/applications", h.listApplications)
		protected.Get("/v1/applications/{id}", h.getApplication)
		protected.Put("/v1/applications/{id}", h.updateApplication)
		protected.Delete("/v1/applications/{id}", h.deleteApplication)
		protected.Get("/v1/applications/{id}/env", h.getEnv)
		protected.Put("/v1/applications/{id}/env", h.putEnv)
		protected.Get("/v1/applications/{id}/storages", h.getStorages)
		protected.Put("/v1/applications/{id}/storages", h.putStorages)
		protected.Post("/v1/applications/{id}/stop", h.stop)
		protected.Post("/v1/applications/{id}/start", h.start)
		protected.Post("/v1/applications/{id}/deploy", h.deploy)
		protected.Get("/v1/applications/{id}/deployments", h.list)
		protected.Post("/v1/applications/{id}/rollback", h.rollback)
		protected.Post("/v1/applications/{id}/deploy-key", h.createDeployKey)
		protected.Delete("/v1/applications/{id}/deploy-key", h.deleteDeployKey)
	})
}

// createApplication serves POST /applications: validates the payload, seals the
// values marked as secrets and stores the application with its configuration in
// one transaction (201).
func (h *handler) createApplication(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createApplicationRequest
	if !decodeBody(w, r, &req) {
		return
	}
	serverID, ok := applicationServerID(w, req.ServerID)
	if !ok {
		return
	}
	environmentID, ok := optionalUUID(w, "environment_id", req.EnvironmentID)
	if !ok {
		return
	}
	githubAppID, ok := optionalUUID(w, "github_app_id", req.GitHubAppID)
	if !ok {
		return
	}

	application, err := h.svc.CreateApplication(r.Context(), userID, CreateApplicationInput{
		Name:              req.Name,
		EnvironmentID:     environmentID,
		Provider:          req.Provider,
		Repo:              req.Repo,
		CloneURL:          req.CloneURL,
		SourceType:        req.SourceType,
		GitHubAppID:       githubAppID,
		Branch:            req.Branch,
		BuildPack:         req.BuildPack,
		ImageRef:          req.ImageRef,
		RegistryUsername:  req.RegistryUsername,
		RegistryPassword:  req.RegistryPassword,
		BaseDomain:        req.BaseDomain,
		Port:              req.Port,
		HostPort:          req.HostPort,
		ServerID:          serverID,
		Env:               toEnvEntries(req.Env),
		Storage:           toStorages(req.Storage),
		DockerfileContent: req.DockerfileContent,
		BuildArgs:         req.BuildArgs,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	// BE-4.4: a provider-connected application gets its push hook installed
	// now, exactly as POST /v1/applications/{id}/webhooks would. The install
	// must not fail the create: the row is already committed, so turning a
	// provider failure into a create error would leave an application the
	// caller believes was never stored. Service.InstallHook bounds the call
	// and logs a failure with the configured logger; the response carries the
	// outcome so the caller knows automatic deploys are off and which route
	// retries the install. A pasted public URL has no provider hook.
	var webhook *webhookOutcome
	if supportedSourceProvider(application.Provider) && strings.TrimSpace(application.Repo) != "" {
		attempted, err := h.svc.InstallHook(r.Context(), userID, application.ID, r)
		if attempted {
			webhook = &webhookOutcome{Installed: err == nil}
			if err != nil {
				webhook.Error = "provider hook not installed; retry with the application webhook endpoint"
			}
		}
	}
	writeJSON(w, http.StatusCreated, applicationEnvelope{
		Application: newApplicationResponse(application),
		Webhook:     webhook,
	})
}

// listApplications serves GET /applications: the caller's own rows, newest
// first, optionally scoped by ?environment_id= or ?project_id=.
func (h *handler) listApplications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter, ok := applicationFilter(w, r)
	if !ok {
		return
	}
	applications, err := h.svc.ListApplications(r.Context(), userID, filter)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]applicationListItem, 0, len(applications))
	for _, application := range applications {
		response = append(response, newApplicationListItem(application))
	}
	writeJSON(w, http.StatusOK, applicationListEnvelope{Applications: response})
}

// getApplication serves GET /applications/{id}. Another user's application
// answers 404, so IDs cannot be probed.
func (h *handler) getApplication(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	application, err := h.svc.GetApplication(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, applicationEnvelope{Application: newApplicationResponse(application)})
}

// updateApplication serves PUT /applications/{id} with the partial body.
func (h *handler) updateApplication(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var req updateApplicationRequest
	if !decodeBody(w, r, &req) {
		return
	}
	in := UpdateApplicationInput{
		Name:              req.Name,
		Branch:            req.Branch,
		BuildPack:         req.BuildPack,
		ImageRef:          req.ImageRef,
		RegistryUsername:  req.RegistryUsername,
		RegistryPassword:  req.RegistryPassword,
		BaseDomain:        req.BaseDomain,
		Port:              req.Port,
		HostPort:          req.HostPort,
		GitHubAppID:       req.GitHubAppID,
		DockerfileContent: req.DockerfileContent,
		BuildArgs:         req.BuildArgs,
	}
	if req.ServerID != nil {
		serverID, parsed := applicationServerID(w, *req.ServerID)
		if !parsed {
			return
		}
		in.ServerID = &serverID
	}
	if req.EnvironmentID != nil {
		environmentID, parsed := optionalUUID(w, "environment_id", *req.EnvironmentID)
		if !parsed {
			return
		}
		in.EnvironmentID = &environmentID
	}

	application, err := h.svc.UpdateApplication(r.Context(), userID, appID, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, applicationEnvelope{Application: newApplicationResponse(application)})
}

// deleteApplication serves DELETE /applications/{id}: the service stops the
// current container best effort and removes the row (204, no body).
func (h *handler) deleteApplication(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteApplication(r.Context(), userID, appID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getEnv serves GET /applications/{id}/env: plain values and secret references.
func (h *handler) getEnv(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	entries, err := h.svc.GetEnv(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, envListEnvelope{Env: wireEnvEntries(entries)})
}

// putEnv serves PUT /applications/{id}/env: replaces the whole collection.
func (h *handler) putEnv(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var req envListEnvelope
	if !decodeBody(w, r, &req) {
		return
	}
	entries, err := h.svc.ReplaceEnv(r.Context(), userID, appID, toEnvEntries(req.Env))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, envListEnvelope{Env: wireEnvEntries(entries)})
}

// getStorages serves GET /applications/{id}/storages.
func (h *handler) getStorages(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	storages, err := h.svc.GetStorages(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storageListEnvelope{Storage: wireStorages(storages)})
}

// putStorages serves PUT /applications/{id}/storages: replaces the collection.
func (h *handler) putStorages(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var req storageListEnvelope
	if !decodeBody(w, r, &req) {
		return
	}
	storages, err := h.svc.ReplaceStorages(r.Context(), userID, appID, toStorages(req.Storage))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storageListEnvelope{Storage: wireStorages(storages)})
}

// stop serves POST /applications/{id}/stop: stops the container of the newest
// deployment and answers with that deployment.
func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	h.control(w, r, false)
}

// start serves POST /applications/{id}/start: restarts that container.
func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	h.control(w, r, true)
}

// control runs one manual container operation (start when start is true).
func (h *handler) control(w http.ResponseWriter, r *http.Request, start bool) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var (
		deployment Deployment
		err        error
	)
	if start {
		deployment, err = h.svc.Start(r.Context(), userID, appID)
	} else {
		deployment, err = h.svc.Stop(r.Context(), userID, appID)
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
}

// deploy serves POST .../deploy: validates, persists a queued deployment and
// answers as soon as the job is queued (202) — the state machine runs on the
// worker pool and streams its progress on the deployment's log channel.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	deployment, err := h.svc.Deploy(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
}

// list serves GET .../deployments. The response is newest first; ?limit=
// bounds the page to at most that many rows (the dashboard latest-state read
// uses ?limit=1). An absent limit returns the full history; a present limit
// must be at least 1 — a non-numeric, zero or negative limit is a 400, so an
// explicit ?limit=0 can never silently mean "unbounded".
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}
	limit, ok := limitParam(w, r)
	if !ok {
		return
	}

	var deployments []Deployment
	var err error
	if limit > 0 {
		deployments, err = h.svc.ListDeploymentsLimit(r.Context(), userID, appID, limit)
	} else {
		deployments, err = h.svc.ListDeployments(r.Context(), userID, appID)
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]deploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		response = append(response, newDeploymentResponse(deployment))
	}
	writeJSON(w, http.StatusOK, deploymentListEnvelope{Deployments: response})
}

// limitParam parses the ?limit= page size of a listing. An absent value
// selects the unbounded read; a present value must be at least 1 —
// non-numeric, zero or negative is a 400.
func limitParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid limit"})
		return 0, false
	}
	return limit, true
}

// rollback serves POST .../rollback with an optional deployment_id body.
func (h *handler) rollback(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	var req rollbackRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	var target uuid.UUID
	if strings.TrimSpace(req.DeploymentID) != "" {
		parsed, err := uuid.Parse(req.DeploymentID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid deployment id"})
			return
		}
		target = parsed
	}

	deployment, err := h.svc.Rollback(r.Context(), userID, appID, target)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
}

// createDeployKey serves POST .../deploy-key: generates an ed25519 keypair for
// the application and registers its public half with the Git host. Repeating
// the call returns the key that already exists (regenerating would strand the
// registered one).
func (h *handler) createDeployKey(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	key, err := h.svc.CreateDeployKey(r.Context(), userID, appID)
	if err != nil {
		// A conflict here is the concurrent-create race (the service is
		// idempotent for an application that already has a key), not a
		// deployment: the shared ErrConflict text would be wrong.
		if errors.Is(err, ErrConflict) {
			writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy key already exists for this application"})
			return
		}
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, deployKeyEnvelope{DeployKey: newDeployKeyResponse(key)})
}

// deleteDeployKey serves DELETE .../deploy-key: removes the key from the Git
// host and then from the database. An application without a key answers 200
// with deleted=false.
func (h *handler) deleteDeployKey(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	deleted, err := h.svc.DeleteDeployKey(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteKeyEnvelope{Deleted: deleted})
}

// currentUser resolves the authenticated user, answering 401 when absent.
func (h *handler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	userID, ok := h.userID(r.Context())
	if !ok || userID == uuid.Nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return userID, true
}

// writeServiceError maps service sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, teams.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrSourceNotImplemented):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deployment is already in progress"})
	case errors.Is(err, ErrDeployInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy is in progress"})
	case errors.Is(err, ErrDomainConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "domain already in use on the target server"})
	case errors.Is(err, ErrPreviewsOpen):
		writeJSON(w, http.StatusConflict, errorBody{Message: "close the open previews first"})
	case errors.Is(err, ErrNameConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrNotConnected):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "applications are disabled"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable"})
	case errors.Is(err, ErrProvider):
		h.logger.Error("deploy: Git host call failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "provider unavailable"})
	default:
		h.logger.Error("deploy: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// applicationIDParam parses the {id} path parameter, answering 400 on a bad
// UUID.
func applicationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid application id"})
		return uuid.Nil, false
	}
	return id, true
}

// decodeOptionalBody decodes a JSON body when one is present; an empty body
// leaves dst untouched. Failures answer 400.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	return decodeJSONBody(w, body, dst)
}

// decodeBody decodes a required JSON body: an empty or malformed body answers
// 400, so a collection replace can never wipe a collection by accident.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return decodeJSONBody(w, body, dst)
}

// decodeJSONBody decodes body into dst, rejecting unknown fields.
func decodeJSONBody(w http.ResponseWriter, body []byte, dst any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// requestTarget resolves the authenticated user and the {id} path parameter in
// one step, answering 401/400 as appropriate.
func (h *handler) requestTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, appID, true
}

// applicationServerID parses a request's server_id: an empty string means "no
// server assigned" (uuid.Nil), anything else must be a UUID.
func applicationServerID(w http.ResponseWriter, raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, true
	}
	serverID, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return serverID, true
}

// optionalUUID parses an optional UUID field: empty means unset (uuid.Nil,
// still valid), anything else must parse.
func optionalUUID(w http.ResponseWriter, field, raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + field})
		return uuid.Nil, false
	}
	return id, true
}

// applicationFilter parses the ?environment_id= and ?project_id= list filters.
// At most one may be present; a malformed UUID is a 400.
func applicationFilter(w http.ResponseWriter, r *http.Request) (ApplicationFilter, bool) {
	var filter ApplicationFilter
	rawEnv := strings.TrimSpace(r.URL.Query().Get("environment_id"))
	rawProject := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if rawEnv != "" {
		envID, err := uuid.Parse(rawEnv)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid environment id"})
			return ApplicationFilter{}, false
		}
		filter.EnvironmentID = envID
	}
	if rawProject != "" {
		projectID, err := uuid.Parse(rawProject)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid project id"})
			return ApplicationFilter{}, false
		}
		filter.ProjectID = projectID
	}
	return filter, true
}

// toEnvEntries maps the wire environment rows to the domain view. The two
// types are field-identical on purpose, so the conversion fails to compile the
// moment either side grows a field the other does not carry.
func toEnvEntries(rows []envEntryRequest) []EnvEntry {
	entries := make([]EnvEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, EnvEntry(row))
	}
	return entries
}

// wireEnvEntries maps the domain environment back to the wire (always a JSON
// array, even when empty).
func wireEnvEntries(entries []EnvEntry) []envEntryRequest {
	rows := make([]envEntryRequest, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, envEntryRequest(entry))
	}
	return rows
}

// toStorages maps the wire storage rows to the domain model.
func toStorages(rows []storageRequest) []Storage {
	storages := make([]Storage, 0, len(rows))
	for _, row := range rows {
		storages = append(storages, Storage{
			Name:          row.Name,
			HostPath:      row.HostPath,
			ContainerPath: row.ContainerPath,
		})
	}
	return storages
}

// wireStorages maps the domain storages back to the wire (always a JSON array).
func wireStorages(storages []Storage) []storageRequest {
	rows := make([]storageRequest, 0, len(storages))
	for _, storage := range storages {
		rows = append(rows, storageRequest{
			Name:          storage.Name,
			HostPath:      storage.HostPath,
			ContainerPath: storage.ContainerPath,
		})
	}
	return rows
}

// nonNilBuildArgs keeps the wire field a JSON object (never null) so the FE
// reuses one shape for the editor draft.
func nonNilBuildArgs(args map[string]string) map[string]string {
	if args == nil {
		return map[string]string{}
	}
	return args
}

// newApplicationResponse maps a domain application to its wire representation.
func newApplicationResponse(application Application) applicationResponse {
	response := applicationResponse{
		ID:              application.ID.String(),
		Name:            application.Name,
		EnvironmentID:   application.EnvironmentID.String(),
		EnvironmentName: application.EnvironmentName,
		ProjectID:       application.ProjectID.String(),
		ProjectName:     application.ProjectName,
		Provider:        application.Provider,
		Repo:            application.Repo,
		// The clone URL is display data: strip any embedded credentials so a
		// legacy token-bearing URL can never be echoed to API clients
		// (creation validation already refuses userinfo on new rows).
		CloneURL:          RedactCloneURL(application.CloneURL),
		SourceType:        application.SourceType,
		DockerfileContent: application.DockerfileContent,
		BuildArgs:         nonNilBuildArgs(application.BuildArgs),
		Branch:            application.Branch,
		BuildPack:         application.BuildPack,
		ImageRef:          application.ImageRef,
		// The registry credential is never returned: only whether one is
		// stored, so the UI can show "configured" without seeing it.
		HasRegistryCredential: application.RegistryUsername != "" ||
			application.RegistryPasswordCiphertext != "",
		BaseDomain:         application.BaseDomain,
		BaseDomainDisabled: application.BaseDomainDisabled,
		Port:               application.Port,
		HostPort:           application.HostPort,
		ServerName:         application.ServerName,
		CreatedAt:          application.CreatedAt,
		UpdatedAt:          application.UpdatedAt,
	}
	if application.ServerID != uuid.Nil {
		serverID := application.ServerID.String()
		response.ServerID = &serverID
	}
	// The link is empty (not the zero UUID) when the application has none.
	if application.GitHubAppID != uuid.Nil {
		response.GitHubAppID = application.GitHubAppID.String()
	}
	return response
}

// newApplicationListItem maps a domain application to its list representation
// (detail fields excluded, see applicationListItem).
func newApplicationListItem(application Application) applicationListItem {
	response := applicationListItem{
		ID:              application.ID.String(),
		Name:            application.Name,
		EnvironmentID:   application.EnvironmentID.String(),
		EnvironmentName: application.EnvironmentName,
		ProjectID:       application.ProjectID.String(),
		ProjectName:     application.ProjectName,
		Provider:        application.Provider,
		Repo:            application.Repo,
		CloneURL:        RedactCloneURL(application.CloneURL),
		SourceType:      application.SourceType,
		Branch:          application.Branch,
		BuildPack:       application.BuildPack,
		ImageRef:        application.ImageRef,
		HasRegistryCredential: application.RegistryUsername != "" ||
			application.RegistryPasswordCiphertext != "",
		BaseDomain:         application.BaseDomain,
		BaseDomainDisabled: application.BaseDomainDisabled,
		Port:               application.Port,
		HostPort:           application.HostPort,
		ServerName:         application.ServerName,
		CreatedAt:          application.CreatedAt,
		UpdatedAt:          application.UpdatedAt,
	}
	if application.ServerID != uuid.Nil {
		serverID := application.ServerID.String()
		response.ServerID = &serverID
	}
	// The link is empty (not the zero UUID) when the application has none,
	// mirroring the detail mapper.
	if application.GitHubAppID != uuid.Nil {
		response.GitHubAppID = application.GitHubAppID.String()
	}
	return response
}

// newDeployKeyResponse maps a stored deploy key to its wire representation.
// Only the public half travels: the sealed private key stays on the server.
func newDeployKeyResponse(key DeployKey) deployKeyResponse {
	return deployKeyResponse{
		ID:            key.ID.String(),
		ApplicationID: key.ApplicationID.String(),
		Provider:      key.Provider,
		Repo:          key.Repo,
		ProviderKeyID: key.ProviderKeyID,
		Fingerprint:   key.Fingerprint,
		PublicKey:     key.PublicKey,
		CreatedAt:     key.CreatedAt,
	}
}

// newDeploymentResponse maps a domain deployment to its wire representation.
func newDeploymentResponse(d Deployment) deploymentResponse {
	response := deploymentResponse{
		ID:            d.ID.String(),
		ApplicationID: d.ApplicationID.String(),
		Kind:          d.Kind,
		State:         d.State,
		ImageTag:      d.ImageTag,
		RegistryImage: d.RegistryImage,
		Digest:        d.Digest,
		Error:         d.Error,
		Attempt:       d.Attempt,
		ContainerID:   d.ContainerID,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
	if d.RollbackFrom != uuid.Nil {
		response.RollbackFrom = d.RollbackFrom.String()
	}
	if !d.StartedAt.IsZero() {
		started := d.StartedAt
		response.StartedAt = &started
	}
	if !d.FinishedAt.IsZero() {
		finished := d.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
