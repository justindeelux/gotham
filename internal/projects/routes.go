package projects

import (
	"bytes"
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

	"github.com/justindeelux/gotham/internal/deploy"
	servicespkg "github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
)

// maxBodyBytes bounds project request bodies (they are tiny).
const maxBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors services.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// resourceCountsResponse is the wire representation of resource counts.
type resourceCountsResponse struct {
	Applications int `json:"applications"`
	Services     int `json:"services"`
	Databases    int `json:"databases"`
}

// projectResponse is the wire representation of a project.
type projectResponse struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	EnvironmentCount int                    `json:"environment_count"`
	ResourceCounts   resourceCountsResponse `json:"resource_counts"`
}

// environmentResponse is the wire representation of an environment.
type environmentResponse struct {
	ID             string                 `json:"id"`
	ProjectID      string                 `json:"project_id"`
	Name           string                 `json:"name"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	ResourceCounts resourceCountsResponse `json:"resource_counts"`
}

// envelope types.
type (
	projectListEnvelope struct {
		Projects []projectResponse `json:"projects"`
	}
	projectEnvelope struct {
		Project projectResponse `json:"project"`
	}
	projectDetailEnvelope struct {
		Project      projectResponse       `json:"project"`
		Environments []environmentResponse `json:"environments"`
	}
	projectCreateEnvelope struct {
		Project      projectResponse       `json:"project"`
		Environments []environmentResponse `json:"environments"`
	}
	environmentListEnvelope struct {
		Environments []environmentResponse `json:"environments"`
	}
	environmentEnvelope struct {
		Environment environmentResponse `json:"environment"`
	}
)

// request bodies.
type (
	createProjectRequest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	updateProjectRequest struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	createEnvironmentRequest struct {
		Name string `json:"name"`
	}
	updateEnvironmentRequest struct {
		Name string `json:"name"`
	}
)

// environmentResourceApplication is one application of the resources
// surface. It mirrors deploy's list item field for field, plus the preview
// marker: is_preview flags a PR-preview sibling, preview_of names its base
// application (empty when none).
type environmentResourceApplication struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	EnvironmentID      string    `json:"environment_id"`
	EnvironmentName    string    `json:"environment_name"`
	ProjectID          string    `json:"project_id"`
	ProjectName        string    `json:"project_name"`
	Provider           string    `json:"provider"`
	Repo               string    `json:"repo"`
	CloneURL           string    `json:"clone_url"`
	Branch             string    `json:"branch"`
	BuildPack          string    `json:"build_pack"`
	BaseDomain         string    `json:"base_domain"`
	BaseDomainDisabled bool      `json:"base_domain_disabled"`
	Port               int32     `json:"port"`
	HostPort           int32     `json:"host_port"`
	ServerID           string    `json:"server_id"`
	ServerName         string    `json:"server_name"`
	IsPreview          bool      `json:"is_preview"`
	PreviewOf          string    `json:"preview_of,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// environmentResourceService is one service of the resources surface: the
// services package's own response type, so the shapes cannot drift.
type environmentResourceService = servicespkg.ServiceResponse

// environmentResourceDatabase is one database of the resources surface. It
// mirrors databases' list item field for field.
type environmentResourceDatabase struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	EnvironmentID   string    `json:"environment_id"`
	EnvironmentName string    `json:"environment_name"`
	ProjectID       string    `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	Engine          string    `json:"engine"`
	Version         string    `json:"version,omitempty"`
	Status          string    `json:"status"`
	ServerID        string    `json:"server_id"`
	ServerName      string    `json:"server_name"`
	PublicPort      int32     `json:"public_port"`
	Volume          string    `json:"volume"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// environmentResourcesEnvelope is the GET /environments/{id}/resources body.
type environmentResourcesEnvelope struct {
	Environment  environmentResponse              `json:"environment"`
	Project      projectResponse                  `json:"project"`
	Applications []environmentResourceApplication `json:"applications"`
	Services     []environmentResourceService     `json:"services"`
	Databases    []environmentResourceDatabase    `json:"databases"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the project routes for one ProjectService.
type handler struct {
	svc    ProjectService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated project endpoints under /api:
//
//	GET    /v1/projects
//	POST   /v1/projects
//	GET    /v1/projects/{id}
//	PATCH  /v1/projects/{id}
//	DELETE /v1/projects/{id}
//	GET    /v1/projects/{id}/environments
//	POST   /v1/projects/{id}/environments
//	PATCH  /v1/environments/{id}
//	DELETE /v1/environments/{id}
//	GET    /v1/environments/{id}/resources
//	GET    /v1/projects/{id}/variables
//	PUT    /v1/projects/{id}/variables
//	GET    /v1/environments/{id}/variables
//	PUT    /v1/environments/{id}/variables
//
// auth wraps the group: the server passes its team chain (RequireAuth,
// RequireTeam and the owner/admin gate for mutating methods, so a read_only
// member reads and every mutation is refused before the handler). A nil svc
// mounts nothing, so the control plane can call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ProjectService) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/projects", h.listProjects)
		protected.Post("/v1/projects", h.createProject)
		protected.Get("/v1/projects/{id}", h.getProject)
		protected.Patch("/v1/projects/{id}", h.updateProject)
		protected.Delete("/v1/projects/{id}", h.deleteProject)
		protected.Get("/v1/projects/{id}/environments", h.listEnvironments)
		protected.Post("/v1/projects/{id}/environments", h.createEnvironment)
		protected.Patch("/v1/environments/{id}", h.updateEnvironment)
		protected.Delete("/v1/environments/{id}", h.deleteEnvironment)
		protected.Get("/v1/environments/{id}/resources", h.getEnvironmentResources)
		protected.Get("/v1/projects/{id}/variables", h.getProjectVariables)
		protected.Put("/v1/projects/{id}/variables", h.replaceProjectVariables)
		protected.Get("/v1/environments/{id}/variables", h.getEnvironmentVariables)
		protected.Put("/v1/environments/{id}/variables", h.replaceEnvironmentVariables)
	})
}

// listProjects serves GET /v1/projects.
func (h *handler) listProjects(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	projects, err := h.svc.ListProjects(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectListEnvelope{Projects: newProjectList(projects)})
}

// createProject serves POST /v1/projects: 201 with the stored project and
// its production environment, created in one transaction.
func (h *handler) createProject(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createProjectRequest
	if !decodeBody(w, r, &req) {
		return
	}
	project, environment, err := h.svc.CreateProject(r.Context(), userID, req.Name, req.Description)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectCreateEnvelope{
		Project:      newProjectResponse(project),
		Environments: []environmentResponse{newEnvironmentResponse(environment)},
	})
}

// getProject serves GET /v1/projects/{id}.
func (h *handler) getProject(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	project, environments, err := h.svc.GetProject(r.Context(), userID, projectID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectDetailEnvelope{
		Project:      newProjectResponse(project),
		Environments: newEnvironmentList(environments),
	})
}

// updateProject serves PATCH /v1/projects/{id}.
func (h *handler) updateProject(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	var req updateProjectRequest
	if !decodeBody(w, r, &req) {
		return
	}
	project, err := h.svc.UpdateProject(r.Context(), userID, projectID, req.Name, req.Description)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectEnvelope{Project: newProjectResponse(project)})
}

// deleteProject serves DELETE /v1/projects/{id}: 204 on success.
func (h *handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(r.Context(), userID, projectID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listEnvironments serves GET /v1/projects/{id}/environments.
func (h *handler) listEnvironments(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	environments, err := h.svc.ListEnvironments(r.Context(), userID, projectID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environmentListEnvelope{Environments: newEnvironmentList(environments)})
}

// createEnvironment serves POST /v1/projects/{id}/environments: 201 with
// the stored environment.
func (h *handler) createEnvironment(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.projectParams(w, r)
	if !ok {
		return
	}
	var req createEnvironmentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	environment, err := h.svc.CreateEnvironment(r.Context(), userID, projectID, req.Name)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, environmentEnvelope{Environment: newEnvironmentResponse(environment)})
}

// updateEnvironment serves PATCH /v1/environments/{id}.
func (h *handler) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	userID, environmentID, ok := h.environmentParams(w, r)
	if !ok {
		return
	}
	var req updateEnvironmentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	environment, err := h.svc.UpdateEnvironment(r.Context(), userID, environmentID, req.Name)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environmentEnvelope{Environment: newEnvironmentResponse(environment)})
}

// deleteEnvironment serves DELETE /v1/environments/{id}: 204 on success.
func (h *handler) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	userID, environmentID, ok := h.environmentParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteEnvironment(r.Context(), userID, environmentID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getEnvironmentResources serves GET /environments/{id}/resources: the
// environment, its project and the workloads attached to it. Previews are
// excluded unless ?previews=1.
func (h *handler) getEnvironmentResources(w http.ResponseWriter, r *http.Request) {
	userID, environmentID, ok := h.environmentParams(w, r)
	if !ok {
		return
	}
	includePreviews := strings.TrimSpace(r.URL.Query().Get("previews")) == "1"
	resources, err := h.svc.GetEnvironmentResources(r.Context(), userID, environmentID, includePreviews)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	applications := make([]environmentResourceApplication, 0, len(resources.Applications))
	previewIDs := make([]uuid.UUID, 0)
	for _, application := range resources.Applications {
		if application.IsPreview {
			previewIDs = append(previewIDs, application.ID)
		}
	}
	// The base-application mapping is a second read only when previews are
	// on the page; a failure here must not fail the whole envelope, so an
	// unmapped preview renders with its tag and no parent.
	bases := map[uuid.UUID]uuid.UUID{}
	if len(previewIDs) > 0 {
		if mapped, err := h.svc.PreviewBases(r.Context(), userID, previewIDs); err == nil {
			bases = mapped
		} else {
			h.logger.Error("projects: preview bases unavailable", "error", err)
		}
	}
	for _, application := range resources.Applications {
		serverID := ""
		if application.ServerID != uuid.Nil {
			serverID = application.ServerID.String()
		}
		previewOf := ""
		if base, ok := bases[application.ID]; ok && base != uuid.Nil {
			previewOf = base.String()
		}
		applications = append(applications, environmentResourceApplication{
			ID:              application.ID.String(),
			Name:            application.Name,
			EnvironmentID:   application.EnvironmentID.String(),
			EnvironmentName: application.EnvironmentName,
			ProjectID:       application.ProjectID.String(),
			ProjectName:     application.ProjectName,
			Provider:        application.Provider,
			Repo:            application.Repo,
			// Display data: never echo embedded credentials (see
			// deploy.RedactCloneURL).
			CloneURL:           deploy.RedactCloneURL(application.CloneURL),
			Branch:             application.Branch,
			BuildPack:          application.BuildPack,
			BaseDomain:         application.BaseDomain,
			BaseDomainDisabled: application.BaseDomainDisabled,
			Port:               application.Port,
			HostPort:           application.HostPort,
			ServerID:           serverID,
			ServerName:         application.ServerName,
			IsPreview:          application.IsPreview,
			PreviewOf:          previewOf,
			CreatedAt:          application.CreatedAt,
			UpdatedAt:          application.UpdatedAt,
		})
	}
	services := make([]environmentResourceService, 0, len(resources.Services))
	for _, service := range resources.Services {
		services = append(services, servicespkg.NewServiceResponse(service, false))
	}
	databases := make([]environmentResourceDatabase, 0, len(resources.Databases))
	for _, database := range resources.Databases {
		databases = append(databases, environmentResourceDatabase{
			ID:              database.ID.String(),
			Name:            database.Name,
			EnvironmentID:   database.EnvironmentID.String(),
			EnvironmentName: database.EnvironmentName,
			ProjectID:       database.ProjectID.String(),
			ProjectName:     database.ProjectName,
			Engine:          database.Engine,
			Version:         database.Version,
			Status:          string(database.Status),
			ServerID:        database.ServerID.String(),
			ServerName:      database.ServerName,
			PublicPort:      database.PublicPort,
			Volume:          database.StoragePath,
			CreatedAt:       database.CreatedAt,
			UpdatedAt:       database.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, environmentResourcesEnvelope{
		Environment:  newEnvironmentResponse(resources.Environment),
		Project:      newProjectResponse(resources.Project),
		Applications: applications,
		Services:     services,
		Databases:    databases,
	})
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

// projectParams resolves the authenticated user and the {id} path parameter,
// answering 401/400 as needed.
func (h *handler) projectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid project id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, projectID, true
}

// environmentParams resolves the authenticated user and the environment {id}
// path parameter, answering 401/400 as needed.
func (h *handler) environmentParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	environmentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid environment id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, environmentID, true
}

// writeServiceError maps service sentinels to HTTP responses. The conflict
// and refusal bodies are the exact strings of the API contract.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden), errors.Is(err, teams.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrProjectExists):
		writeJSON(w, http.StatusConflict, errorBody{Message: "project name already exists"})
	case errors.Is(err, ErrEnvironmentExists):
		writeJSON(w, http.StatusConflict, errorBody{Message: "environment name already exists"})
	case errors.Is(err, ErrProjectNotEmpty):
		writeJSON(w, http.StatusConflict, errorBody{Message: "project still has resources (including previews)"})
	case errors.Is(err, ErrEnvironmentNotEmpty):
		writeJSON(w, http.StatusConflict, errorBody{Message: "environment still has resources (including previews)"})
	case errors.Is(err, ErrLastEnvironment):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a project needs at least one environment"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		h.logger.Error("projects: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// newProjectResponse maps a domain project to its wire representation.
func newProjectResponse(project Project) projectResponse {
	return projectResponse{
		ID:               project.ID.String(),
		Name:             project.Name,
		Description:      project.Description,
		CreatedAt:        project.CreatedAt,
		UpdatedAt:        project.UpdatedAt,
		EnvironmentCount: project.EnvironmentCount,
		ResourceCounts:   resourceCountsResponse(project.Resources),
	}
}

// newProjectList maps domain projects, never rendering a null list.
func newProjectList(projects []Project) []projectResponse {
	response := make([]projectResponse, 0, len(projects))
	for _, project := range projects {
		response = append(response, newProjectResponse(project))
	}
	return response
}

// newEnvironmentResponse maps a domain environment to its wire
// representation.
func newEnvironmentResponse(environment Environment) environmentResponse {
	return environmentResponse{
		ID:             environment.ID.String(),
		ProjectID:      environment.ProjectID.String(),
		Name:           environment.Name,
		CreatedAt:      environment.CreatedAt,
		UpdatedAt:      environment.UpdatedAt,
		ResourceCounts: resourceCountsResponse(environment.Resources),
	}
}

// newEnvironmentList maps domain environments, never rendering a null list.
func newEnvironmentList(environments []Environment) []environmentResponse {
	response := make([]environmentResponse, 0, len(environments))
	for _, environment := range environments {
		response = append(response, newEnvironmentResponse(environment))
	}
	return response
}

// decodeBody decodes a required JSON body into dst. An empty or malformed
// body answers 400.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "request body is required"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
