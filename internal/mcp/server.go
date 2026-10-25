package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

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
	server := newSDKServer(version, logger)
	registerTools(server, client)
	return server, nil
}

// NewServerWithClient builds the tool surface for an authenticated caller.
// Mutation tools are registered only when the token holds the deploy scope, so
// a read token cannot even list them; the REST layer still authorizes each call.
func NewServerWithClient(client *Client, allowMutations bool, version string, logger *slog.Logger) *sdk.Server {
	server := newSDKServer(version, logger)
	registerToolsForScopes(server, client, allowMutations)
	return server
}

func newSDKServer(version string, logger *slog.Logger) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "gotham", Version: version}, &sdk.ServerOptions{
		Logger:       logger,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	server.AddReceivingMiddleware(recoverPanics(logger))
	return server
}

func registerTools(server *sdk.Server, client *Client) {
	registerToolsForScopes(server, client, true)
}

// recoverPanics keeps a tool panic from killing the process. The SDK runs
// handlers on its own goroutine, so the HTTP recoverer cannot see them. Only
// the panic type is logged: panic values may contain caller material.
func recoverPanics(logger *slog.Logger) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (result sdk.Result, err error) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if logger != nil {
					logger.Error("mcp: recovered handler panic",
						"method", method,
						"panic_type", fmt.Sprintf("%T", recovered),
						"stack", string(debug.Stack()))
				}
				if method == "tools/call" {
					call := &sdk.CallToolResult{}
					call.SetError(&Error{Code: "internal_error"})
					result, err = call, nil
					return
				}
				result, err = nil, errors.New("internal error")
			}()
			return next(ctx, method, req)
		}
	}
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
