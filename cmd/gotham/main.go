// Command gotham is the Gotham control-plane entrypoint.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/server"
)

// version is the reported build version. Released binaries override it with
// -ldflags "-X main.version=<tag>".
var version = "dev"

// Exit codes returned by run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches subcommands and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitUsage
	}

	switch args[0] {
	case "serve":
		return runServe()
	case "version", "-v", "--version":
		fmt.Printf("gotham %s\n", version)
		return exitOK
	case "help", "-h", "--help":
		usage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return exitUsage
	}
}

// runServe loads configuration, builds the logger and HTTP server, and serves
// until SIGINT or SIGTERM.
func runServe() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	logger, levelVar := server.NewLoggerWithLevel(cfg.Log)
	cfg.SetLogger(logger)
	cfg.Watch(func() {
		levelVar.Set(server.ParseLevel(cfg.Snapshot().Log.Level))
	})

	logger.Info("starting gotham",
		slog.String("version", version),
		slog.String("addr", cfg.Server.Addr),
		slog.Int("port", cfg.Server.Port),
	)

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		logger.Error("server stopped with error", "error", err)
		return exitError
	}

	logger.Info("server stopped")
	return exitOK
}

// usage prints the top-level command help.
func usage(w io.Writer) {
	fmt.Fprintf(w, `gotham %s

Usage:
  gotham serve      Start the control plane
  gotham version    Print the version
  gotham help       Show this help

Configuration is read from gotham.yaml in the working directory and can be
overridden with GOTHAM_* environment variables.
`, version)
}
