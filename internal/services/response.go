package services

import (
	"log/slog"
	"time"
)

// ServiceResponse is the wire representation of a service. compose_yaml is
// omitted from list responses (a document may be a megabyte); env is always
// included because its values are already embedded in the rendered documents
// the owner can read, and the UI needs them to prefill the editor.
// project_name is the Gotham project; the compose project name rides
// compose_project.
type ServiceResponse struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Status          Status            `json:"status"`
	ServerID        string            `json:"server_id"`
	ServerName      string            `json:"server_name"`
	EnvironmentID   string            `json:"environment_id"`
	EnvironmentName string            `json:"environment_name"`
	ProjectID       string            `json:"project_id"`
	ProjectName     string            `json:"project_name"`
	ComposeProject  string            `json:"compose_project"`
	ComposeYAML     string            `json:"compose_yaml,omitempty"`
	Env             map[string]string `json:"env"`
	Domains         []DomainRoute     `json:"domains"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// NewServiceResponse maps a domain service to its wire representation. The
// domain map is computed from the stored document and environment; a document
// that no longer renders reports no routes instead of failing the read (the
// deploy path is where the error surfaces). The projects resources surface
// reuses this constructor, so the shapes cannot drift.
func NewServiceResponse(service Service, withCompose bool) ServiceResponse {
	response := ServiceResponse{
		ID:              service.ID.String(),
		Name:            service.Name,
		Status:          service.Status,
		ServerID:        service.ServerID.String(),
		ServerName:      service.ServerName,
		EnvironmentID:   service.EnvironmentID.String(),
		EnvironmentName: service.EnvironmentName,
		ProjectID:       service.ProjectID.String(),
		ProjectName:     service.ProjectName,
		ComposeProject:  ProjectName(service.ID),
		Env:             service.Env,
		Domains:         []DomainRoute{},
		CreatedAt:       service.CreatedAt,
		UpdatedAt:       service.UpdatedAt,
	}
	if withCompose {
		response.ComposeYAML = service.ComposeYAML
	}
	if response.Env == nil {
		response.Env = map[string]string{}
	}
	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		slog.Default().Warn("services: stored document does not render",
			"service_id", service.ID.String(), "error", Redact(err.Error(), service.Env))
		return response
	}
	response.Domains = rendered.Spec.Domains
	return response
}
