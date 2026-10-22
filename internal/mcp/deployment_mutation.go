package mcp

import (
	"context"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type deploymentReceipt struct {
	ResourceID string    `json:"resource_id"`
	AcceptedAt time.Time `json:"accepted_at"`
	Replayed   bool      `json:"replayed"`
	Outcome    string    `json:"outcome"`
}

type applicationDeployResult struct {
	Receipt    *deploymentReceipt  `json:"receipt"`
	Deployment *deploymentMetadata `json:"deployment,omitempty"`
	Accepted   bool                `json:"accepted"`
}

func registerApplicationDeployTool(server *sdk.Server, client *Client) {
	tool := &sdk.Tool{
		Name:        "application_deploy",
		Description: "Deploy an explicit application once using a required stable request_id. A 202 means accepted/queued, never finished. Reuse the same request_id after timeout or response loss; never automatically generate a fresh key. Replay guarantee 24 hours, tombstones 7 days; reconcile manually after expiry. Running means pipeline completion, not live health or public URL readiness. A deliberate new deploy after terminal failure needs a separately authorized fresh request.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}
	sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, in applicationDeployInput) (*sdk.CallToolResult, applicationDeployResult, error) {
		var out applicationDeployResult
		app, err := discoveryID(in.ApplicationID)
		if err != nil {
			return nil, out, err
		}
		err = client.mutateJSON(ctx, "/api/v1/mcp/applications/"+app+"/deploy", in.RequestID, struct{}{}, &out)
		if err != nil {
			return nil, applicationDeployResult{}, err
		}
		if out.Receipt == nil || (out.Receipt.Outcome != "accepted" && out.Receipt.Outcome != "accepted_not_confirmed") {
			return nil, applicationDeployResult{}, &Error{Code: "invalid_response"}
		}
		if _, err := discoveryID(out.Receipt.ResourceID); err != nil {
			return nil, applicationDeployResult{}, &Error{Code: "invalid_response"}
		}
		if out.Deployment != nil {
			if out.Receipt.ResourceID != out.Deployment.ID || out.Deployment.ApplicationID != app {
				return nil, applicationDeployResult{}, &Error{Code: "invalid_response"}
			}
			out.Deployment.setTerminal()
		}
		out.Accepted = true
		return nil, out, nil
	})
}
