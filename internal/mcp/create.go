package mcp

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectCreateInput struct {
	RequestID   string `json:"request_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type environmentCreateInput struct {
	RequestID string `json:"request_id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}
type applicationCreateInput struct {
	RequestID string `json:"request_id"`
	applicationCreateRequest
}

type applicationCreateRequest struct {
	Name              string            `json:"name"`
	EnvironmentID     string            `json:"environment_id"`
	ServerID          string            `json:"server_id"`
	SourceType        string            `json:"source_type"`
	Provider          string            `json:"provider,omitempty"`
	Repo              string            `json:"repo,omitempty"`
	CloneURL          string            `json:"clone_url,omitempty"`
	GitHubAppID       string            `json:"github_app_id,omitempty"`
	Branch            string            `json:"branch,omitempty"`
	BuildPack         string            `json:"build_pack,omitempty"`
	DockerfileContent string            `json:"dockerfile_content,omitempty"`
	BuildArgs         map[string]string `json:"build_args,omitempty"`
	ImageRef          string            `json:"image_ref,omitempty"`
	RegistryUsername  string            `json:"registry_username,omitempty"`
	RegistryPassword  string            `json:"registry_password,omitempty"`
	ComposeContent    string            `json:"compose_content,omitempty"`
	ComposeFile       string            `json:"compose_file,omitempty"`
	ComposeService    string            `json:"compose_service,omitempty"`
}

// String UUIDs match the SDK JSON schema as well as the native wire format.
type mutationReceipt struct {
	ResourceID string    `json:"resource_id"`
	RelatedID  string    `json:"related_id,omitempty"`
	AcceptedAt time.Time `json:"accepted_at"`
	Replayed   bool      `json:"replayed"`
	Hook       string    `json:"webhook_outcome,omitempty"`
	Outcome    string    `json:"outcome"`
}

type projectCreated struct {
	Receipt      *mutationReceipt      `json:"receipt"`
	Project      *projectMetadata      `json:"project,omitempty"`
	Environments []environmentMetadata `json:"environments,omitempty"`
}
type environmentCreated struct {
	Receipt     *mutationReceipt     `json:"receipt"`
	Environment *environmentMetadata `json:"environment,omitempty"`
}
type createdApplicationMetadata struct {
	applicationMetadata
	EnvironmentName string `json:"environment_name"`
}
type applicationCreated struct {
	Receipt        *mutationReceipt            `json:"receipt"`
	Application    *createdApplicationMetadata `json:"application,omitempty"`
	WebhookOutcome string                      `json:"webhook_outcome"`
}

func createTool(name, description string) *sdk.Tool {
	return &sdk.Tool{Name: name, Description: description + " Reuse the same request_id after any uncertain result; replay is guaranteed for 24 hours with tombstones for 7 days.", Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool)}}
}

func registerCreateTools(server *sdk.Server, client *Client) {
	server.AddReceivingMiddleware(safeCreateErrors)
	sdk.AddTool(server, createTool("project_create", "Create a project and return its actual production environment."), func(ctx context.Context, _ *sdk.CallToolRequest, in projectCreateInput) (*sdk.CallToolResult, projectCreated, error) {
		var out projectCreated
		if strings.TrimSpace(in.Name) == "" {
			return nil, out, &Error{Code: "invalid_request"}
		}
		err := client.mutateJSON(ctx, "/api/v1/mcp/projects", in.RequestID, struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}{in.Name, in.Description}, &out)
		if err == nil {
			err = validateCreateReceipt(out.Receipt)
			if out.Receipt != nil {
				out.Receipt.Hook = ""
			}
		}
		return nil, out, err
	})
	sdk.AddTool(server, createTool("environment_create", "Create an environment in an explicit project UUID."), func(ctx context.Context, _ *sdk.CallToolRequest, in environmentCreateInput) (*sdk.CallToolResult, environmentCreated, error) {
		var out environmentCreated
		id, err := discoveryID(in.ProjectID)
		if err != nil || strings.TrimSpace(in.Name) == "" {
			return nil, out, &Error{Code: "invalid_request"}
		}
		err = client.mutateJSON(ctx, "/api/v1/mcp/projects/"+id+"/environments", in.RequestID, struct {
			Name string `json:"name"`
		}{in.Name}, &out)
		if err == nil {
			err = validateCreateReceipt(out.Receipt)
			if out.Receipt != nil {
				out.Receipt.Hook = ""
			}
		}
		return nil, out, err
	})
	tool := createTool("application_create", "Create an application on an explicit environment and managed server UUID. Never deploys or creates domains. Provider connections must preexist; providerless private Git credentials must be configured outside MCP before deploy. Creation does not prove clone readiness.")
	tool.InputSchema = applicationCreateSchema()
	sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, in applicationCreateInput) (*sdk.CallToolResult, applicationCreated, error) {
		var body struct {
			Receipt     *mutationReceipt            `json:"receipt"`
			Application *createdApplicationMetadata `json:"application"`
		}
		for _, id := range []string{in.EnvironmentID, in.ServerID} {
			if _, err := discoveryID(id); err != nil {
				return nil, applicationCreated{}, err
			}
		}
		if in.GitHubAppID != "" {
			if _, err := discoveryID(in.GitHubAppID); err != nil {
				return nil, applicationCreated{}, err
			}
		}
		err := client.mutateJSON(ctx, "/api/v1/mcp/applications", in.RequestID, in.applicationCreateRequest, &body)
		if err == nil {
			err = validateCreateReceipt(body.Receipt)
		}
		if body.Receipt != nil {
			switch body.Receipt.Hook {
			case "none", "installed", "warning", "not_confirmed":
			default:
				body.Receipt.Hook = "not_confirmed"
			}
		}
		out := applicationCreated{Receipt: body.Receipt, Application: body.Application, WebhookOutcome: "not_confirmed"}
		if body.Receipt != nil {
			switch body.Receipt.Hook {
			case "none", "installed", "warning", "not_confirmed":
				out.WebhookOutcome = body.Receipt.Hook
			}
		}
		return nil, out, err
	})
}

func validateCreateReceipt(receipt *mutationReceipt) error {
	if receipt == nil {
		return &Error{Code: "invalid_response"}
	}
	id, err := uuid.Parse(receipt.ResourceID)
	if err != nil || id == uuid.Nil {
		return &Error{Code: "invalid_response"}
	}
	switch receipt.Outcome {
	case "accepted", "accepted_not_confirmed":
	default:
		return &Error{Code: "invalid_response"}
	}
	return nil
}

// Each variant permits only fields the native source actually consumes. Backend owns content validation.
func applicationCreateSchema() map[string]any {
	text := func() map[string]any { return map[string]any{"type": "string", "minLength": 1} }
	common := map[string]any{"request_id": map[string]any{"type": "string", "pattern": "^[A-Za-z0-9._:-]{1,128}$"}, "name": text(), "environment_id": text(), "server_id": text()}
	git := []string{"provider", "repo", "clone_url", "branch", "build_pack"}
	variants := []any{}
	add := func(source string, allowed, required []string, provider string) {
		props := map[string]any{}
		for k, v := range common {
			props[k] = v
		}
		props["source_type"] = map[string]any{"const": source}
		for _, k := range allowed {
			props[k] = map[string]any{"type": "string"}
		}
		if provider != "*" {
			props["provider"] = map[string]any{"const": provider}
		}
		if source == "dockerfile" {
			props["build_args"] = map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
		}
		req := append([]string{"request_id", "name", "environment_id", "server_id", "source_type"}, required...)
		for _, k := range required {
			if k != "provider" {
				props[k] = text()
			}
		}
		variant := map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
		if source == "git_public" {
			props["provider"] = map[string]any{"type": "string", "not": map[string]any{"enum": []string{"github", "gitlab"}}}
		}
		if source == "image" {
			variant["dependentRequired"] = map[string]any{"registry_username": []string{"registry_password"}, "registry_password": []string{"registry_username"}}
		}
		variants = append(variants, variant)
	}
	add("git_public", git, []string{"clone_url"}, "*")
	add("git_private", git, []string{"clone_url"}, "")
	add("github_app", append(append([]string{}, git...), "github_app_id"), []string{"provider", "repo", "clone_url"}, "github")
	add("gitlab_app", git, []string{"provider", "repo", "clone_url"}, "gitlab")
	add("dockerfile", []string{"dockerfile_content", "build_args", "build_pack"}, []string{"dockerfile_content"}, "")
	add("image", []string{"image_ref", "registry_username", "registry_password"}, []string{"image_ref"}, "")
	add("compose", []string{"compose_content", "compose_service"}, []string{"compose_content", "compose_service"}, "")
	add("compose", []string{"provider", "repo", "clone_url", "branch", "compose_file", "compose_service"}, []string{"clone_url", "compose_file", "compose_service"}, "*")
	return map[string]any{"type": "object", "oneOf": variants}
}
