package mcp

import (
	"context"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer verifies current authorization before exposing the SDK lifecycle.
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

// registerTools is extended by subsequent read and mutation work packages.
func registerTools(server *sdk.Server, client *Client) {
	registerObservationTools(server, client)
}

func Serve(ctx context.Context, cfg Config, version string, logger *slog.Logger) error {
	server, err := NewServer(ctx, cfg, version, logger)
	if err != nil {
		return err
	}
	return server.Run(ctx, &sdk.StdioTransport{MaxLineLength: maxBody})
}
