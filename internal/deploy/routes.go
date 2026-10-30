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

	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/runtimewatch"
)

const maxBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

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
	CommitSHA     string     `json:"commit_sha"`
	CommitMessage string     `json:"commit_message"`
	CommitAuthor  string     `json:"commit_author"`
	CommittedAt   string     `json:"committed_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type deploymentEnvelope struct {
	Receipt    *requestreplay.Result `json:"receipt,omitempty"`
	Deployment deploymentResponse    `json:"deployment"`
}

type deploymentListEnvelope struct {
	Deployments []deploymentResponse `json:"deployments"`
}

type recentDeploymentResponse struct {
	deploymentResponse
	ApplicationName   string `json:"application_name"`
	ApplicationRepo   string `json:"application_repo"`
	ApplicationBranch string `json:"application_branch"`
}

type recentDeploymentListEnvelope struct {
	Deployments []recentDeploymentResponse `json:"deployments"`
}

const defaultRecentDeploymentLimit = 20

type rollbackRequest struct {
	DeploymentID string `json:"deployment_id"`
}

type applicationResponse struct {
	ID                    string               `json:"id"`
	Name                  string               `json:"name"`
	EnvironmentID         string               `json:"environment_id"`
	EnvironmentName       string               `json:"environment_name"`
	ProjectID             string               `json:"project_id"`
	ProjectName           string               `json:"project_name"`
	Provider              string               `json:"provider"`
	Repo                  string               `json:"repo"`
	CloneURL              string               `json:"clone_url"`
	SourceType            string               `json:"source_type"`
	GitHubAppID           string               `json:"github_app_id"`
	Branch                string               `json:"branch"`
	BuildPack             string               `json:"build_pack"`
	ImageRef              string               `json:"image_ref"`
	HasRegistryCredential bool                 `json:"has_registry_credential"`
	PrimaryDomain         string               `json:"primary_domain"`
	DockerfileContent     string               `json:"dockerfile_content"`
	BuildArgs             map[string]string    `json:"build_args"`
	ComposeContent        string               `json:"compose_content"`
	ComposeFile           string               `json:"compose_file"`
	ComposeService        string               `json:"compose_service"`
	ServerID              *string              `json:"server_id"`
	ServerName            string               `json:"server_name"`
	PendingChanges        []string             `json:"pending_changes"`
	RuntimeHealth         *runtimewatch.Health `json:"runtime_health,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type applicationEnvelope struct {
	Receipt     *requestreplay.Result `json:"receipt,omitempty"`
	Application applicationResponse   `json:"application"`
	Webhook     *webhookOutcome       `json:"webhook,omitempty"`
}

type webhookOutcome struct {
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}

type applicationListEnvelope struct {
	Applications []applicationListItem `json:"applications"`
}

// deleteWarningsEnvelope reports data the delete could not remove. It is only
// returned when there is something to report, so a clean delete stays a 204.
type deleteWarningsEnvelope struct {
	Warnings []string `json:"warnings"`
}

type applicationListItem struct {
	ID                    string               `json:"id"`
	Name                  string               `json:"name"`
	EnvironmentID         string               `json:"environment_id"`
	EnvironmentName       string               `json:"environment_name"`
	ProjectID             string               `json:"project_id"`
	ProjectName           string               `json:"project_name"`
	Provider              string               `json:"provider"`
	Repo                  string               `json:"repo"`
	CloneURL              string               `json:"clone_url"`
	SourceType            string               `json:"source_type"`
	GitHubAppID           string               `json:"github_app_id"`
	Branch                string               `json:"branch"`
	BuildPack             string               `json:"build_pack"`
	ImageRef              string               `json:"image_ref"`
	HasRegistryCredential bool                 `json:"has_registry_credential"`
	PrimaryDomain         string               `json:"primary_domain"`
	ComposeFile           string               `json:"compose_file"`
	ComposeService        string               `json:"compose_service"`
	ServerID              *string              `json:"server_id"`
	ServerName            string               `json:"server_name"`
	PendingChanges        []string             `json:"pending_changes"`
	RuntimeHealth         *runtimewatch.Health `json:"runtime_health,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type envEntryRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type envListEnvelope struct {
	Env []envEntryRequest `json:"env"`
}

type storageRequest struct {
	Name          string `json:"name"`
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
}

type storageListEnvelope struct {
	Storage []storageRequest `json:"storage"`
}

type volumeMountResponse struct {
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
	Service     string `json:"service,omitempty"`
	Container   string `json:"container,omitempty"`
	State       string `json:"state"`
}

type volumeFileManagerResponse struct {
	ServerID string `json:"server_id"`
	Root     string `json:"root"`
	Path     string `json:"path"`
}

type volumeResponse struct {
	Name        string                     `json:"name"`
	Kind        string                     `json:"kind"`
	HostPath    string                     `json:"host_path,omitempty"`
	State       string                     `json:"state"`
	Mounts      []volumeMountResponse      `json:"mounts"`
	FileManager *volumeFileManagerResponse `json:"file_manager,omitempty"`
	OpenReason  string                     `json:"open_reason,omitempty"`
}

type volumeListEnvelope struct {
	SourceType string           `json:"source_type"`
	Runtime    string           `json:"runtime"`
	Reason     string           `json:"reason,omitempty"`
	Volumes    []volumeResponse `json:"volumes"`
}

type createApplicationRequest struct {
	Name              string            `json:"name"`
	EnvironmentID     string            `json:"environment_id"`
	Provider          string            `json:"provider"`
	Repo              string            `json:"repo"`
	CloneURL          string            `json:"clone_url"`
	SourceType        string            `json:"source_type"`
	GitHubAppID       string            `json:"github_app_id"`
	Branch            string            `json:"branch"`
	BuildPack         string            `json:"build_pack"`
	ImageRef          string            `json:"image_ref"`
	RegistryUsername  string            `json:"registry_username"`
	RegistryPassword  string            `json:"registry_password"`
	ServerID          string            `json:"server_id"`
	Env               []envEntryRequest `json:"env"`
	Storage           []storageRequest  `json:"storage"`
	DockerfileContent string            `json:"dockerfile_content"`
	BuildArgs         map[string]string `json:"build_args"`
	ComposeContent    string            `json:"compose_content"`
	ComposeFile       string            `json:"compose_file"`
	ComposeService    string            `json:"compose_service"`
}

type updateApplicationRequest struct {
	Name              *string            `json:"name"`
	EnvironmentID     *string            `json:"environment_id"`
	Branch            *string            `json:"branch"`
	BuildPack         *string            `json:"build_pack"`
	ImageRef          *string            `json:"image_ref"`
	RegistryUsername  *string            `json:"registry_username"`
	RegistryPassword  *string            `json:"registry_password"`
	ServerID          *string            `json:"server_id"`
	GitHubAppID       *string            `json:"github_app_id"`
	DockerfileContent *string            `json:"dockerfile_content"`
	BuildArgs         *map[string]string `json:"build_args"`
	ComposeContent    *string            `json:"compose_content"`
	ComposeFile       *string            `json:"compose_file"`
	ComposeService    *string            `json:"compose_service"`
	MonitorEnabled    *bool              `json:"monitor_enabled"`
	MonitorGrace      *int               `json:"monitor_grace_seconds"`
}

type errorBody struct {
	Message string `json:"message"`
}

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

type deployKeyEnvelope struct {
	DeployKey deployKeyResponse `json:"deploy_key"`
}

type deleteKeyEnvelope struct {
	Deleted bool `json:"deleted"`
}

type gitCredentialRequest struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

type gitCredentialResponse struct {
	HasCredential bool   `json:"has_credential"`
	Username      string `json:"username,omitempty"`
}

type gitConnectionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Host    string `json:"host,omitempty"`
}

type handler struct {
	svc    DeployService
	userID UserIDFunc
	logger *slog.Logger
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc DeployService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/applications", h.createApplication)
		protected.With(requestreplay.RequireStrict).Post("/v1/mcp/applications", h.createApplication)
		protected.With(requestreplay.RequireStrict).Post("/v1/mcp/applications/{id}/deploy", h.deploy)
		protected.Get("/v1/applications", h.listApplications)
		protected.Get("/v1/applications/{id}", h.getApplication)
		protected.Put("/v1/applications/{id}", h.updateApplication)
		protected.Get("/v1/deployments", h.listRecentDeployments)
		protected.Delete("/v1/applications/{id}", h.deleteApplication)
		protected.Get("/v1/applications/{id}/env", h.getEnv)
		protected.Put("/v1/applications/{id}/env", h.putEnv)
		protected.Get("/v1/applications/{id}/storages", h.getStorages)
		protected.Put("/v1/applications/{id}/storages", h.putStorages)
		protected.Get("/v1/applications/{id}/volumes", h.getVolumes)
		protected.Get("/v1/applications/{id}/runtime", h.getRuntime)
		protected.Get("/v1/applications/{id}/logs", h.getApplicationLogs)
		protected.Post("/v1/applications/{id}/stop", h.stop)
		protected.Post("/v1/applications/{id}/start", h.start)
		protected.Post("/v1/applications/{id}/deploy", h.deploy)
		protected.Get("/v1/applications/{id}/deployments", h.list)
		protected.Get("/v1/applications/{id}/deployments/{deployment_id}", h.getDeployment)
		protected.Get("/v1/applications/{id}/deployments/{deployment_id}/logs", h.getDeploymentLog)
		protected.Post("/v1/applications/{id}/rollback", h.rollback)
		protected.Post("/v1/applications/{id}/deploy-key", h.createDeployKey)
		protected.Get("/v1/applications/{id}/deploy-key", h.getDeployKey)
		protected.Delete("/v1/applications/{id}/deploy-key", h.deleteDeployKey)
		protected.Put("/v1/applications/{id}/git-credential", h.putGitCredential)
		protected.Get("/v1/applications/{id}/git-credential", h.getGitCredential)
		protected.Delete("/v1/applications/{id}/git-credential", h.deleteGitCredential)
		protected.Post("/v1/applications/{id}/test-connection", h.testConnection)
		protected.Get("/v1/applications/{id}/domains", h.listDomains)
		protected.Post("/v1/applications/{id}/domains", h.addDomain)
		protected.Put("/v1/applications/{id}/domains/{domainId}", h.updateDomain)
		protected.Delete("/v1/applications/{id}/domains/{domainId}", h.removeDomain)
		protected.Post("/v1/applications/{id}/domains/{domainId}/primary", h.setPrimaryDomain)
	})
}

func (h *handler) createApplication(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var replayOK bool
	r, replayOK = requestreplay.Begin(w, r, userID, "create_application", uuid.Nil)
	if !replayOK {
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
		ServerID:          serverID,
		Env:               toEnvEntries(req.Env),
		Storage:           toStorages(req.Storage),
		DockerfileContent: req.DockerfileContent,
		BuildArgs:         req.BuildArgs,
		ComposeContent:    req.ComposeContent,
		ComposeFile:       req.ComposeFile,
		ComposeService:    req.ComposeService,
	})
	if err != nil {
		if requestreplay.WriteError(w, err) {
			return
		}
		if !errors.Is(err, ErrNotFound) && requestreplay.WriteAccepted(w, r.Context(), 201) {
			return
		}
		h.writeServiceError(w, err)
		return
	}
	var webhook *webhookOutcome
	var hookUnconfirmed bool
	if !requestreplay.Replayed(r.Context()) && supportedSourceProvider(application.Provider) && strings.TrimSpace(application.Repo) != "" {
		attempted, err := h.svc.InstallHook(r.Context(), userID, application.ID, r)
		if attempted {
			hookUnconfirmed = errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
			webhook = &webhookOutcome{Installed: err == nil}
			if err != nil {
				webhook.Error = "provider hook not installed; retry with the application webhook endpoint"
			}
		}
	}
	if result := requestreplay.ResultFrom(r.Context()); result != nil {
		if !result.Replayed {
			outcome := "none"
			if webhook != nil {
				outcome = "installed"
				if !webhook.Installed {
					outcome = "warning"
				}
				if hookUnconfirmed || r.Context().Err() != nil {
					outcome = "not_confirmed"
				}
			}
			if saver, ok := h.svc.(interface {
				SaveRequestHook(context.Context, string) error
			}); ok {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
				if err := saver.SaveRequestHook(ctx, outcome); err != nil {
					h.logger.Warn("deploy: webhook receipt outcome not confirmed", "application_id", application.ID)
				}
				cancel()
			}
		}
		switch result.Hook {
		case "installed":
			webhook = &webhookOutcome{Installed: true}
		case "warning":
			webhook = &webhookOutcome{Error: "provider hook not installed; retry with the application webhook endpoint"}
		case "not_confirmed":
			webhook = &webhookOutcome{Error: "provider hook outcome not confirmed; reconcile before retrying"}
		}
	}
	writeJSON(w, http.StatusCreated, applicationEnvelope{
		Receipt:     requestreplay.ResultFrom(r.Context()),
		Application: newApplicationResponse(application),
		Webhook:     webhook,
	})
}

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
		GitHubAppID:       req.GitHubAppID,
		DockerfileContent: req.DockerfileContent,
		BuildArgs:         req.BuildArgs,
		ComposeContent:    req.ComposeContent,
		ComposeFile:       req.ComposeFile,
		ComposeService:    req.ComposeService,
		MonitorEnabled:    req.MonitorEnabled,
		MonitorGrace:      req.MonitorGrace,
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

func (h *handler) deleteApplication(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	warnings, err := h.svc.DeleteApplication(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if len(warnings) > 0 {
		writeJSON(w, http.StatusOK, deleteWarningsEnvelope{Warnings: warnings})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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

func (h *handler) getVolumes(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	listing, err := h.svc.ListVolumes(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, volumeListEnvelope{
		SourceType: listing.SourceType,
		Runtime:    listing.Runtime,
		Reason:     listing.RuntimeReason,
		Volumes:    wireVolumes(listing.Volumes),
	})
}

func wireVolumes(volumes []VolumeEntry) []volumeResponse {
	rows := make([]volumeResponse, 0, len(volumes))
	for _, volume := range volumes {
		mounts := make([]volumeMountResponse, 0, len(volume.Mounts))
		for _, mount := range volume.Mounts {
			mounts = append(mounts, volumeMountResponse(mount))
		}
		row := volumeResponse{
			Name:       volume.Name,
			Kind:       volume.Kind,
			HostPath:   volume.HostPath,
			State:      volume.State,
			Mounts:     mounts,
			OpenReason: volume.OpenReason,
		}
		if volume.FileManager != nil {
			row.FileManager = &volumeFileManagerResponse{
				ServerID: volume.FileManager.ServerID.String(),
				Root:     volume.FileManager.Root,
				Path:     volume.FileManager.Path,
			}
		}
		rows = append(rows, row)
	}
	return rows
}

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

func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	h.control(w, r, false)
}

func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	h.control(w, r, true)
}

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

func (h *handler) deploy(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}
	var replayOK bool
	r, replayOK = requestreplay.Begin(w, r, userID, "deploy", appID)
	if !replayOK {
		return
	}
	if requestreplay.From(r.Context()) != nil {
		var body map[string]any
		if !decodeOptionalBody(w, r, &body) {
			return
		}
		if len(body) != 0 {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "deploy request must be empty"})
			return
		}
	}

	deployment, err := h.svc.Deploy(r.Context(), userID, appID)
	if err != nil {
		if requestreplay.WriteError(w, err) {
			return
		}
		if !errors.Is(err, ErrNotFound) && requestreplay.WriteAccepted(w, r.Context(), 202) {
			return
		}
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deploymentEnvelope{Receipt: requestreplay.ResultFrom(r.Context()), Deployment: newDeploymentResponse(deployment)})
}

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

func (h *handler) listRecentDeployments(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	limit, ok := limitParam(w, r)
	if !ok {
		return
	}
	if limit == 0 {
		limit = defaultRecentDeploymentLimit
	}
	deployments, err := h.svc.ListRecentDeployments(r.Context(), userID, limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]recentDeploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		response = append(response, newRecentDeploymentResponse(deployment))
	}
	writeJSON(w, http.StatusOK, recentDeploymentListEnvelope{Deployments: response})
}

type deploymentLogEnvelope struct {
	Log string `json:"log"`
}

func (h *handler) getDeploymentLog(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}
	deploymentID, err := uuid.Parse(chi.URLParam(r, "deployment_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid deployment id"})
		return
	}
	if r.URL.Query().Has("tail") || r.URL.Query().Has("max_bytes") {
		options, ok := logOptionsParam(w, r, false)
		if !ok {
			return
		}
		out, err := h.svc.GetDeploymentLogs(r.Context(), userID, appID, deploymentID, options)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	log, err := h.svc.GetDeploymentBuildLog(r.Context(), userID, appID, deploymentID)
	if err != nil {
		if errors.Is(err, ErrValidation) {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
			return
		}
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deploymentLogEnvelope{Log: log})
}

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

func (h *handler) createDeployKey(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	key, err := h.svc.CreateDeployKey(r.Context(), userID, appID)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy key already exists for this application"})
			return
		}
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, deployKeyEnvelope{DeployKey: newDeployKeyResponse(key)})
}

func (h *handler) getDeployKey(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	key, err := h.svc.GetDeployKey(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deployKeyEnvelope{DeployKey: newDeployKeyResponse(key)})
}

func (h *handler) putGitCredential(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var req gitCredentialRequest
	if !decodeBody(w, r, &req) {
		return
	}
	state, err := h.svc.SetGitCredential(r.Context(), userID, appID, req.Username, req.Token)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newGitCredentialResponse(state))
}

func (h *handler) getGitCredential(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	state, err := h.svc.GetGitCredential(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newGitCredentialResponse(state))
}

func (h *handler) deleteGitCredential(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	deleted, err := h.svc.DeleteGitCredential(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteKeyEnvelope{Deleted: deleted})
}

func (h *handler) testConnection(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	result, err := h.svc.TestGitConnection(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newGitConnectionResponse(result))
}

type applicationDomainResponse struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Domain        string    `json:"domain"`
	IsPrimary     bool      `json:"is_primary"`
	Disabled      bool      `json:"disabled"`
	Port          int32     `json:"port"`
	Service       string    `json:"service"`
	Protocol      string    `json:"protocol"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type domainListEnvelope struct {
	Domains []applicationDomainResponse `json:"domains"`
}

type domainEnvelope struct {
	Domain applicationDomainResponse `json:"domain"`
}

type addDomainRequest struct {
	Domain   string `json:"domain"`
	Port     int32  `json:"port"`
	Service  string `json:"service"`
	Protocol string `json:"protocol"`
}

func newApplicationDomainResponse(domain ApplicationDomain) applicationDomainResponse {
	return applicationDomainResponse{
		ID:            domain.ID.String(),
		ApplicationID: domain.ApplicationID.String(),
		Domain:        domain.Domain,
		IsPrimary:     domain.IsPrimary,
		Disabled:      domain.Disabled,
		Port:          domain.Port,
		Service:       domain.Service,
		Protocol:      domain.Protocol,
		CreatedAt:     domain.CreatedAt,
		UpdatedAt:     domain.UpdatedAt,
	}
}

func (h *handler) listDomains(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	domains, err := h.svc.ListDomains(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]applicationDomainResponse, 0, len(domains))
	for _, domain := range domains {
		response = append(response, newApplicationDomainResponse(domain))
	}
	writeJSON(w, http.StatusOK, domainListEnvelope{Domains: response})
}

func (h *handler) addDomain(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	var req addDomainRequest
	if !decodeBody(w, r, &req) {
		return
	}
	domain, err := h.svc.AddDomain(r.Context(), userID, appID, DomainInput(req))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, domainEnvelope{Domain: newApplicationDomainResponse(domain)})
}

func (h *handler) updateDomain(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	domainID, err := uuid.Parse(chi.URLParam(r, "domainId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid domain id"})
		return
	}
	var req addDomainRequest
	if !decodeBody(w, r, &req) {
		return
	}
	domain, err := h.svc.UpdateDomain(r.Context(), userID, appID, domainID, DomainInput(req))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, domainEnvelope{Domain: newApplicationDomainResponse(domain)})
}

func (h *handler) removeDomain(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	domainID, err := uuid.Parse(chi.URLParam(r, "domainId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid domain id"})
		return
	}
	if err := h.svc.RemoveDomain(r.Context(), userID, appID, domainID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) setPrimaryDomain(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	domainID, err := uuid.Parse(chi.URLParam(r, "domainId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid domain id"})
		return
	}
	if err := h.svc.SetPrimaryDomain(r.Context(), userID, appID, domainID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrSourceNotImplemented):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deployment is already in progress"})
	case errors.Is(err, ErrDeployInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy is in progress"})
	case errors.Is(err, ErrBackupInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a backup or restore is in progress"})
	case errors.Is(err, ErrDomainConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "domain already in use by another application"})
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

func applicationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid application id"})
		return uuid.Nil, false
	}
	return id, true
}

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

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return decodeJSONBody(w, body, dst)
}

func decodeJSONBody(w http.ResponseWriter, body []byte, dst any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

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

func toEnvEntries(rows []envEntryRequest) []EnvEntry {
	entries := make([]EnvEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, EnvEntry(row))
	}
	return entries
}

func wireEnvEntries(entries []EnvEntry) []envEntryRequest {
	rows := make([]envEntryRequest, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, envEntryRequest(entry))
	}
	return rows
}

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

func nonNilBuildArgs(args map[string]string) map[string]string {
	if args == nil {
		return map[string]string{}
	}
	return args
}

func primaryDomainName(application Application) string {
	if application.PrimaryDomain == nil {
		return ""
	}
	return application.PrimaryDomain.Domain
}

func newApplicationResponse(application Application) applicationResponse {
	response := applicationResponse{
		ID:                application.ID.String(),
		Name:              application.Name,
		EnvironmentID:     application.EnvironmentID.String(),
		EnvironmentName:   application.EnvironmentName,
		ProjectID:         application.ProjectID.String(),
		ProjectName:       application.ProjectName,
		Provider:          application.Provider,
		Repo:              application.Repo,
		CloneURL:          RedactCloneURL(application.CloneURL),
		SourceType:        application.SourceType,
		DockerfileContent: application.DockerfileContent,
		BuildArgs:         nonNilBuildArgs(application.BuildArgs),
		Branch:            application.Branch,
		BuildPack:         application.BuildPack,
		ImageRef:          application.ImageRef,
		HasRegistryCredential: application.RegistryUsername != "" ||
			application.RegistryPasswordCiphertext != "",
		ComposeContent: application.ComposeContent,
		ComposeFile:    application.ComposeFile,
		ComposeService: application.ComposeService,
		PrimaryDomain:  primaryDomainName(application),
		ServerName:     application.ServerName,
		PendingChanges: append([]string{}, application.PendingChanges...),
		RuntimeHealth:  application.RuntimeHealth,
		CreatedAt:      application.CreatedAt,
		UpdatedAt:      application.UpdatedAt,
	}
	if application.ServerID != uuid.Nil {
		serverID := application.ServerID.String()
		response.ServerID = &serverID
	}
	if application.GitHubAppID != uuid.Nil {
		response.GitHubAppID = application.GitHubAppID.String()
	}
	return response
}

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
		ComposeFile:    application.ComposeFile,
		ComposeService: application.ComposeService,
		PrimaryDomain:  primaryDomainName(application),
		ServerName:     application.ServerName,
		PendingChanges: append([]string{}, application.PendingChanges...),
		RuntimeHealth:  application.RuntimeHealth,
		CreatedAt:      application.CreatedAt,
		UpdatedAt:      application.UpdatedAt,
	}
	if application.ServerID != uuid.Nil {
		serverID := application.ServerID.String()
		response.ServerID = &serverID
	}
	if application.GitHubAppID != uuid.Nil {
		response.GitHubAppID = application.GitHubAppID.String()
	}
	return response
}

func newGitCredentialResponse(state GitCredentialState) gitCredentialResponse {
	return gitCredentialResponse(state)
}

func newGitConnectionResponse(result GitConnectionResult) gitConnectionResponse {
	return gitConnectionResponse(result)
}

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
		CommitSHA:     d.CommitSHA,
		CommitMessage: d.CommitMessage,
		CommitAuthor:  d.CommitAuthor,
		CommittedAt:   d.CommittedAt,
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

func newRecentDeploymentResponse(d ApplicationDeployment) recentDeploymentResponse {
	return recentDeploymentResponse{
		deploymentResponse: newDeploymentResponse(d.Deployment),
		ApplicationName:    d.ApplicationName,
		ApplicationRepo:    d.ApplicationRepo,
		ApplicationBranch:  d.ApplicationBranch,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
