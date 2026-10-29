package services

import (
	"log/slog"
	"time"
)

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
	Template        string            `json:"template,omitempty"`
	ComposeYAML     string            `json:"compose_yaml,omitempty"`
	Env             map[string]string `json:"env"`
	Domains         []DomainRoute     `json:"domains"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

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
		Template:        service.TemplateSlug,
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
