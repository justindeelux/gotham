package mcp

import (
	"context"
	"strconv"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// deploymentMetadata excludes raw pipeline errors, logs, source and configuration.
type deploymentMetadata struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	Kind          string     `json:"kind"`
	State         string     `json:"state"`
	Attempt       int32      `json:"attempt"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Terminal      bool       `json:"terminal"`
}

type deploymentStatusInput struct {
	ApplicationID string `json:"application_id" jsonschema:"Explicit application UUID"`
	DeploymentID  string `json:"deployment_id" jsonschema:"Exact deployment UUID returned by deploy"`
}
type deploymentsListInput struct {
	ApplicationID string `json:"application_id"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Newest releases to return; default 20 maximum 100"`
}
type deploymentsListResult struct {
	Deployments []deploymentMetadata `json:"deployments"`
	WindowLimit int                  `json:"window_limit"`
	Coverage    string               `json:"coverage"`
}
type applicationDeployInput struct {
	ApplicationID string `json:"application_id"`
	RequestID     string `json:"request_id" jsonschema:"Required stable retry identity; reuse after timeout; never generate a fresh key automatically"`
}

func (d *deploymentMetadata) setTerminal() {
	d.Terminal = d.State == "running" || d.State == "failed"
}

func registerDeploymentTools(server *sdk.Server, client *Client) {
	sdk.AddTool(server, readTool("deployments_list", "Read newest pipeline releases, bounded to 100. This window is not full history; use deployment_status for an exact older ID. Pipeline running does not establish live health or public URL readiness."), func(ctx context.Context, _ *sdk.CallToolRequest, in deploymentsListInput) (*sdk.CallToolResult, deploymentsListResult, error) {
		var out deploymentsListResult
		app, err := discoveryID(in.ApplicationID)
		if err != nil {
			return nil, out, err
		}
		if in.Limit < 0 || in.Limit > 100 {
			return nil, out, &Error{Code: "invalid_request"}
		}
		limit := in.Limit
		if limit == 0 {
			limit = 20
		}
		if err := client.readJSON(ctx, "/api/v1/applications/"+app+"/deployments?limit="+strconv.Itoa(limit), &out); err != nil {
			return nil, deploymentsListResult{}, err
		}
		out.Deployments = append([]deploymentMetadata{}, out.Deployments[:min(limit, len(out.Deployments))]...)
		for i := range out.Deployments {
			if out.Deployments[i].ApplicationID != app {
				return nil, deploymentsListResult{}, &Error{Code: "invalid_response"}
			}
			if _, err := discoveryID(out.Deployments[i].ID); err != nil {
				return nil, deploymentsListResult{}, &Error{Code: "invalid_response"}
			}
			out.Deployments[i].setTerminal()
		}
		out.WindowLimit, out.Coverage = limit, "newest_window_not_complete_history"
		return nil, out, nil
	})
	sdk.AddTool(server, readTool("deployment_status", "Read one exact application/deployment pair once. Terminal is true only for running/failed; running means pipeline completion, not live application health or URL readiness. Clients choose bounded polling; canceling a read does not cancel the job."), func(ctx context.Context, _ *sdk.CallToolRequest, in deploymentStatusInput) (*sdk.CallToolResult, deploymentMetadata, error) {
		var body struct {
			Deployment deploymentMetadata `json:"deployment"`
		}
		app, err := discoveryID(in.ApplicationID)
		if err != nil {
			return nil, body.Deployment, err
		}
		dep, err := discoveryID(in.DeploymentID)
		if err != nil {
			return nil, body.Deployment, err
		}
		if err := client.readJSON(ctx, "/api/v1/applications/"+app+"/deployments/"+dep, &body); err != nil {
			return nil, deploymentMetadata{}, err
		}
		if body.Deployment.ID != dep || body.Deployment.ApplicationID != app {
			return nil, deploymentMetadata{}, &Error{Code: "invalid_response"}
		}
		body.Deployment.setTerminal()
		return nil, body.Deployment, nil
	})
	registerApplicationDeployTool(server, client)
}
