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

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/server"
	"github.com/justindeelux/gotham/internal/store"
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
	case "migrate":
		return runMigrate(args[1:])
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
// until SIGINT or SIGTERM. The database is required: the server exits when it
// cannot be reached.
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

	snap := cfg.Snapshot()

	logger.Info("starting gotham",
		slog.String("version", version),
		slog.String("addr", snap.Server.Addr),
		slog.Int("port", snap.Server.Port),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, snap.Database.DSN)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		return exitError
	}
	defer pool.Close()

	privatePEM, publicPEM, err := loadJWTKeys(snap.Auth)
	if err != nil {
		logger.Error("failed to load JWT keys", "error", err)
		return exitError
	}

	signer, err := auth.NewSigner(privatePEM, publicPEM)
	if err != nil {
		logger.Error("failed to build JWT signer", "error", err)
		return exitError
	}
	if signer.Ephemeral() {
		logger.Warn("ephemeral JWT keys — sessions do not survive restart")
	}

	authStore := store.New(pool)
	authService := auth.New(authStore, signer, logger)
	tokenService := auth.NewAPITokenService(authStore, logger)

	srv, err := server.New(cfg, logger, authService, tokenService, authStore)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		return exitError
	}

	if err := srv.Run(ctx); err != nil {
		logger.Error("server stopped with error", "error", err)
		return exitError
	}

	logger.Info("server stopped")
	return exitOK
}

// loadJWTKeys reads the configured Ed25519 PEM keypair. When no paths are
// configured it returns nil slices, which makes auth.NewSigner generate an
// ephemeral keypair. A configured path that cannot be read is an error.
func loadJWTKeys(cfg config.Auth) (privatePEM, publicPEM []byte, err error) {
	if cfg.JWTPrivateKeyPath != "" {
		privatePEM, err = os.ReadFile(cfg.JWTPrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read auth.jwt_private_key_path: %w", err)
		}
	}
	if cfg.JWTPublicKeyPath != "" {
		publicPEM, err = os.ReadFile(cfg.JWTPublicKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read auth.jwt_public_key_path: %w", err)
		}
	}
	return privatePEM, publicPEM, nil
}

// runMigrate applies the embedded database migrations. It accepts an optional
// verb (default "up") and returns the process exit code.
func runMigrate(args []string) int {
	command := store.MigrateUp
	switch len(args) {
	case 0:
	case 1:
		command = args[0]
	default:
		fmt.Fprintln(os.Stderr, "usage: gotham migrate [up|down|status]")
		return exitUsage
	}
	if !store.IsMigrateCommand(command) {
		fmt.Fprintf(os.Stderr, "unknown migrate command %q\n\n", command)
		fmt.Fprintln(os.Stderr, "usage: gotham migrate [up|down|status]")
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := store.Migrate(ctx, cfg.Database.DSN, command); err != nil {
		fmt.Fprintf(os.Stderr, "migrate %s: %v\n", command, err)
		return exitError
	}
	return exitOK
}

// usage prints the top-level command help.
func usage(w io.Writer) {
	fmt.Fprintf(w, `gotham %s

Usage:
  gotham serve              Start the control plane
  gotham migrate [verb]     Run database migrations (up, down, status; default up)
  gotham version            Print the version
  gotham help               Show this help

Configuration is read from gotham.yaml in the working directory and can be
overridden with GOTHAM_* environment variables.
`, version)
}
