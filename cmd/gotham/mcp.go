package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/justindeelux/gotham/internal/mcp"
)

func runMCP(args []string) int {
	if len(args) != 1 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: gotham mcp serve")
		return exitUsage
	}
	cfg, err := mcp.ConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := mcp.Serve(ctx, cfg, version, logger); err != nil {
		var safe *mcp.Error
		if errors.As(err, &safe) {
			logger.Error("MCP server stopped", "code", safe.Code)
		} else {
			logger.Error("MCP server stopped")
		}
		return exitError
	}
	return exitOK
}
