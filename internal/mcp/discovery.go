package mcp

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// These projections intentionally omit source, environment, registry and build configuration.
type resourceCounts struct {
	Applications int `json:"applications"`
	Services     int `json:"services"`
	Databases    int `json:"databases"`
}
type projectMetadata struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	EnvironmentCount int            `json:"environment_count"`
	ResourceCounts   resourceCounts `json:"resource_counts"`
}
type environmentMetadata struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"project_id"`
	Name           string         `json:"name"`
	ResourceCounts resourceCounts `json:"resource_counts"`
}
type applicationMetadata struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ProjectID     string  `json:"project_id"`
	EnvironmentID string  `json:"environment_id"`
	ServerID      *string `json:"server_id"`
	SourceType    string  `json:"source_type"`
}
type workloadMetadata struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"server_id"`
}
type pipelineMetadata struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	State         string     `json:"state"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}
type runtimeContainer struct {
	ID           string  `json:"id"`
	Service      string  `json:"service"`
	Replica      string  `json:"replica"`
	State        string  `json:"state"`
	Health       string  `json:"health"`
	DeploymentID *string `json:"deployment_id"`
}
type runtimeObservation struct {
	ApplicationID string             `json:"application_id"`
	ObservedAt    *time.Time         `json:"observed_at"`
	Coverage      string             `json:"coverage"`
	Reason        string             `json:"reason,omitempty"`
	Containers    []runtimeContainer `json:"containers"`
}

type discoveryPageInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"Page size; default 20 maximum 100"`
	Offset int `json:"offset,omitempty" jsonschema:"Offset within the fully decoded collection"`
}
type projectInput struct {
	ProjectID string `json:"project_id" jsonschema:"Explicit project UUID; names are not selectors"`
}
type environmentInput struct {
	EnvironmentID string `json:"environment_id" jsonschema:"Explicit environment UUID"`
}
type applicationInput struct {
	ApplicationID string `json:"application_id" jsonschema:"Explicit application UUID"`
}
type environmentsInput struct {
	ProjectID string `json:"project_id"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}
type applicationsInput struct {
	ProjectID     string `json:"project_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Offset        int    `json:"offset,omitempty"`
}
type collectionPage[T any] struct {
	Items              []T  `json:"items"`
	Total              int  `json:"total"`
	NextOffset         *int `json:"next_offset"`
	Truncated          bool `json:"truncated"`
	SnapshotConsistent bool `json:"snapshot_consistent"`
}

func discoveryPage[T any](items []T, limit, offset int) (collectionPage[T], error) {
	if limit < 0 || limit > 100 || offset < 0 {
		return collectionPage[T]{}, &Error{Code: "invalid_request"}
	}
	if limit == 0 {
		limit = 20
	}
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + min(limit, total-offset)
	result := collectionPage[T]{Items: append([]T{}, items[offset:end]...), Total: total, Truncated: end < total}
	if end < total {
		result.NextOffset = &end
	}
	return result, nil
}
func discoveryID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", &Error{Code: "invalid_request"}
	}
	return url.PathEscape(id.String()), nil
}
func sectionError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "upstream_unavailable"}
}
func fatalSection(err error) bool {
	if err == nil {
		return false
	}
	e := sectionError(err)
	return e.Status == 401 || e.Status == 403 || e.Code == "contract_mismatch" || e.Code == "cancelled" || e.Code == "deadline_exceeded"
}
func readTool(name, description string) *sdk.Tool {
	return &sdk.Tool{Name: name, Description: description, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: new(bool)}}
}
func registerDiscoveryTools(server *sdk.Server, client *Client) {
	sdk.AddTool(server, readTool("projects_list", "List project IDs and counts. Pages a bounded decoded collection; no snapshot consistency."), func(ctx context.Context, _ *sdk.CallToolRequest, in discoveryPageInput) (*sdk.CallToolResult, collectionPage[projectMetadata], error) {
		var body struct {
			Projects []projectMetadata `json:"projects"`
		}
		err := client.readJSON(ctx, "/api/v1/projects", &body)
		if err != nil {
			return nil, collectionPage[projectMetadata]{}, err
		}
		out, err := discoveryPage(body.Projects, in.Limit, in.Offset)
		return nil, out, err
	})
	sdk.AddTool(server, readTool("environments_list", "List environments of an explicit project UUID."), func(ctx context.Context, _ *sdk.CallToolRequest, in environmentsInput) (*sdk.CallToolResult, collectionPage[environmentMetadata], error) {
		id, err := discoveryID(in.ProjectID)
		if err != nil {
			return nil, collectionPage[environmentMetadata]{}, err
		}
		var body struct {
			Environments []environmentMetadata `json:"environments"`
		}
		err = client.readJSON(ctx, "/api/v1/projects/"+id+"/environments", &body)
		if err != nil {
			return nil, collectionPage[environmentMetadata]{}, err
		}
		out, err := discoveryPage(body.Environments, in.Limit, in.Offset)
		return nil, out, err
	})
	sdk.AddTool(server, readTool("applications_list", "List safe application metadata. Project and environment filters are mutually exclusive."), func(ctx context.Context, _ *sdk.CallToolRequest, in applicationsInput) (*sdk.CallToolResult, collectionPage[applicationMetadata], error) {
		apps, err := client.discoveryApplications(ctx, in)
		if err != nil {
			return nil, collectionPage[applicationMetadata]{}, err
		}
		out, err := discoveryPage(apps, in.Limit, in.Offset)
		return nil, out, err
	})
	sdk.AddTool(server, readTool("environment_resources", "Read safe inventory and counts for an environment. Each collection is capped at 100; inventory is not live health."), func(ctx context.Context, _ *sdk.CallToolRequest, in environmentInput) (*sdk.CallToolResult, environmentInventory, error) {
		out, err := client.discoveryInventory(ctx, in.EnvironmentID)
		return nil, out, err
	})
	sdk.AddTool(server, readTool("project_overview", "Project counts, bounded inventory of at most four environments and recent pipeline activity. Partial coverage is explicit; use application_overview for live runtime."), func(ctx context.Context, _ *sdk.CallToolRequest, in projectInput) (*sdk.CallToolResult, projectOverview, error) {
		out, err := client.discoveryProject(ctx, in.ProjectID)
		return nil, out, err
	})
	sdk.AddTool(server, readTool("application_overview", "Safe app metadata, five recent pipeline outcomes and fresh app-scoped runtime. Pipeline running does not imply live health or public readiness."), func(ctx context.Context, _ *sdk.CallToolRequest, in applicationInput) (*sdk.CallToolResult, applicationOverview, error) {
		out, err := client.discoveryApplication(ctx, in.ApplicationID)
		return nil, out, err
	})
}
func (c *Client) discoveryApplications(ctx context.Context, in applicationsInput) ([]applicationMetadata, error) {
	if in.ProjectID != "" && in.EnvironmentID != "" {
		return nil, &Error{Code: "invalid_request"}
	}
	query := url.Values{}
	for key, value := range map[string]string{"project_id": in.ProjectID, "environment_id": in.EnvironmentID} {
		if value != "" {
			id, err := discoveryID(value)
			if err != nil {
				return nil, err
			}
			query.Set(key, id)
		}
	}
	var body struct {
		Applications []applicationMetadata `json:"applications"`
	}
	err := c.readJSON(ctx, "/api/v1/applications?"+query.Encode(), &body)
	return body.Applications, err
}

type environmentInventory struct {
	Environment  environmentMetadata                 `json:"environment"`
	Project      projectMetadata                     `json:"project"`
	Applications collectionPage[applicationMetadata] `json:"applications"`
	Services     collectionPage[workloadMetadata]    `json:"services"`
	Databases    collectionPage[workloadMetadata]    `json:"databases"`
}

func (c *Client) discoveryInventory(ctx context.Context, value string) (environmentInventory, error) {
	id, err := discoveryID(value)
	if err != nil {
		return environmentInventory{}, err
	}
	var body struct {
		Environment  environmentMetadata   `json:"environment"`
		Project      projectMetadata       `json:"project"`
		Applications []applicationMetadata `json:"applications"`
		Services     []workloadMetadata    `json:"services"`
		Databases    []workloadMetadata    `json:"databases"`
	}
	err = c.readJSON(ctx, "/api/v1/environments/"+id+"/resources", &body)
	if err != nil {
		return environmentInventory{}, err
	}
	apps, _ := discoveryPage(body.Applications, 100, 0)
	services, _ := discoveryPage(body.Services, 100, 0)
	databases, _ := discoveryPage(body.Databases, 100, 0)
	return environmentInventory{body.Environment, body.Project, apps, services, databases}, nil
}

type inventorySection struct {
	EnvironmentID string                `json:"environment_id"`
	Inventory     *environmentInventory `json:"inventory"`
	Error         *Error                `json:"error"`
}
type projectOverview struct {
	Project              projectMetadata                     `json:"project"`
	Environments         collectionPage[environmentMetadata] `json:"environments"`
	Inventory            []inventorySection                  `json:"inventory"`
	InventoryTruncated   bool                                `json:"inventory_truncated"`
	RecentActivity       []pipelineMetadata                  `json:"recent_activity"`
	ActivityError        *Error                              `json:"activity_error"`
	ActivityWindowLimit  int                                 `json:"activity_window_limit"`
	ActivityCoverage     string                              `json:"activity_coverage"`
	LiveRuntimeInspected bool                                `json:"live_runtime_inspected"`
}

func (c *Client) discoveryProject(ctx context.Context, value string) (projectOverview, error) {
	id, err := discoveryID(value)
	if err != nil {
		return projectOverview{}, err
	}
	var body struct {
		Project      projectMetadata       `json:"project"`
		Environments []environmentMetadata `json:"environments"`
	}
	if err = c.readJSON(ctx, "/api/v1/projects/"+id, &body); err != nil {
		return projectOverview{}, err
	}
	page, _ := discoveryPage(body.Environments, 20, 0)
	out := projectOverview{Project: body.Project, Environments: page, Inventory: []inventorySection{}, InventoryTruncated: len(body.Environments) > 4, RecentActivity: []pipelineMetadata{}, ActivityCoverage: "bounded_team_window_not_complete_project_history", ActivityWindowLimit: 100}
	// Sequential reads keep fan-out below four even on large projects.
	for _, env := range body.Environments[:min(4, len(body.Environments))] {
		inv, readErr := c.discoveryInventory(ctx, env.ID)
		if fatalSection(readErr) {
			return projectOverview{}, readErr
		}
		section := inventorySection{EnvironmentID: env.ID}
		if readErr != nil {
			section.Error = sectionError(readErr)
		} else {
			section.Inventory = &inv
		}
		out.Inventory = append(out.Inventory, section)
	}
	apps, err := c.discoveryApplications(ctx, applicationsInput{ProjectID: id})
	if fatalSection(err) {
		return projectOverview{}, err
	}
	if err != nil {
		out.ActivityError = sectionError(err)
		return out, nil
	}
	appIDs := map[string]bool{}
	for _, app := range apps {
		appIDs[app.ID] = true
	}
	var recent struct {
		Deployments []pipelineMetadata `json:"deployments"`
	}
	err = c.readJSON(ctx, "/api/v1/deployments?limit=100", &recent)
	if fatalSection(err) {
		return projectOverview{}, err
	}
	if err != nil {
		out.ActivityError = sectionError(err)
		return out, nil
	}
	for _, dep := range recent.Deployments {
		if appIDs[dep.ApplicationID] && len(out.RecentActivity) < 20 {
			out.RecentActivity = append(out.RecentActivity, dep)
		}
	}
	return out, nil
}

type applicationOverview struct {
	SnapshotConsistent bool                `json:"snapshot_consistent"`
	Application        applicationMetadata `json:"application"`
	Pipeline           []pipelineMetadata  `json:"pipeline"`
	PipelineError      *Error              `json:"pipeline_error"`
	Runtime            *runtimeObservation `json:"runtime"`
	RuntimeError       *Error              `json:"runtime_error"`
}

func (c *Client) discoveryApplication(ctx context.Context, value string) (applicationOverview, error) {
	id, err := discoveryID(value)
	if err != nil {
		return applicationOverview{}, err
	}
	path := "/api/v1/applications/" + id
	var body struct {
		Application applicationMetadata `json:"application"`
	}
	if err = c.readJSON(ctx, path, &body); err != nil {
		return applicationOverview{}, err
	}
	out := applicationOverview{Application: body.Application, Pipeline: []pipelineMetadata{}}
	var pipeline struct {
		Deployments []pipelineMetadata `json:"deployments"`
	}
	err = c.readJSON(ctx, path+"/deployments?limit=5", &pipeline)
	if fatalSection(err) {
		return applicationOverview{}, err
	}
	if err != nil {
		out.PipelineError = sectionError(err)
	} else {
		out.Pipeline = append(out.Pipeline, pipeline.Deployments[:min(5, len(pipeline.Deployments))]...)
	}
	var runtime runtimeObservation
	err = c.readJSON(ctx, path+"/runtime", &runtime)
	if fatalSection(err) {
		return applicationOverview{}, err
	}
	if err != nil {
		out.RuntimeError = sectionError(err)
	} else {
		runtime.Containers = runtime.Containers[:min(100, len(runtime.Containers))]
		out.Runtime = &runtime
	}
	return out, nil
}
