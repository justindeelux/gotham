package mcp

import (
	"context"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewServer(ctx context.Context, cfg Config, version string, logger *slog.Logger) (*sdk.Server, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.Context(ctx); err != nil {
		return nil, err
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "gotham", Version: version}, &sdk.ServerOptions{
		Logger:       logger,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	registerTools(server, client)
	return server, nil
}

// NewServerWithClient builds the tool surface for an authenticated caller.
// Mutation tools are registered only when the token holds the deploy scope, so
// a read token cannot even list them; the REST layer still authorizes each call.
func NewServerWithClient(client *Client, allowMutations bool, version string, logger *slog.Logger) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "gotham", Version: version}, &sdk.ServerOptions{
		Logger:       logger,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	registerToolsForScopes(server, client, allowMutations)
	return server
}

func registerTools(server *sdk.Server, client *Client) {
	registerToolsForScopes(server, client, true)
}

func registerToolsForScopes(server *sdk.Server, client *Client, allowMutations bool) {
	registerObservationTools(server, client)
	registerDiscoveryTools(server, client)
	registerLogTools(server, client)
	registerDeploymentTools(server, client, allowMutations)
	if allowMutations {
		registerCreateTools(server, client)
	}
}

func Serve(ctx context.Context, cfg Config, version string, logger *slog.Logger) error {
	server, err := NewServer(ctx, cfg, version, logger)
	if err != nil {
		return err
	}
	return server.Run(ctx, &sdk.StdioTransport{MaxLineLength: maxBody})
}
