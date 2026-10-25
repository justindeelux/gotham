package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/server"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/updates"
)

var version = "dev"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitUsage
	}

	switch args[0] {
	case "mcp":
		return runMCP(args[1:])
	case "serve":
		return runServe()
	case "instance":
		return runInstance(args[1:])
	case "admin":
		return runAdmin(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "ca":
		return runCA(args[1:])
	case "update":
		return runUpdate(args[1:])
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
	oauthService := buildOAuthService(snap.OAuth, authService, logger)
	tokenService := auth.NewAPITokenService(authStore, logger)

	authority, err := servers.LoadAuthority(snap.CA.Dir)
	if err != nil {
		logger.Error("failed to load certificate authority", "error", err)
		return exitError
	}
	if authority == nil {
		if !snap.GRPC.Insecure {
			logger.Error("no CA found and GOTHAM_GRPC_INSECURE is not set; refusing to serve the agent channel in plaintext",
				"ca_dir", snap.CA.Dir)
			return exitError
		}
		logger.Warn("no CA found; gRPC gateway runs without TLS because GOTHAM_GRPC_INSECURE is set (development only)",
			"ca_dir", snap.CA.Dir)
	} else {
		logger.Info("certificate authority loaded", "ca_dir", snap.CA.Dir)
	}

	serverSecret, _, _ := server.ResolveSecretKey(snap.SecretKey, snap.CA.Dir)
	authService.SetSecretKey(serverSecret)
	serverService := servers.NewService(servers.Config{
		Store:     authStore,
		Authority: authority,
		Secret:    serverSecret,
		Version:   version,
		Logger:    logger,
		Updater:   agentUpdater(logger),
	})

	if err := serverService.EnsureLocalhost(ctx); err != nil {
		logger.Error("failed to ensure localhost node", "error", err)
		return exitError
	}

	grpcHosts := snap.GRPC.Hosts
	if len(grpcHosts) == 0 {
		saved, err := servers.LoadHosts(snap.CA.Dir)
		if err != nil {
			logger.Warn("failed to read the gRPC SAN host list", "error", err)
		}
		grpcHosts = saved
	}
	if len(grpcHosts) > 0 {
		logger.Info("gRPC listener certificate hosts", "hosts", grpcHosts)
	}

	enrollmentToken := snap.GRPC.EnrollmentToken
	switch {
	case enrollmentToken != "":
	case snap.GRPC.AllowOpenEnrollment:
		logger.Warn("open enrollment is enabled (GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT); " +
			"any peer that can reach the gRPC listener may enroll an unused node id")
	case authority != nil && snap.CA.Dir != "":
		generated, tokenPath, genErr := servers.LoadOrCreateEnrollmentToken(snap.CA.Dir)
		if genErr != nil {
			logger.Error("could not provide a bootstrap enrollment token; set GOTHAM_GRPC_ENROLLMENT_TOKEN "+
				"or GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT", "error", genErr)
			return exitError
		}
		enrollmentToken = generated
		logger.Info("generated a bootstrap enrollment token; pass it to new nodes with GOTHAM_AGENT_ENROLLMENT_TOKEN "+
			"(or install-agent.sh --enrollment-token-file)", "path", tokenPath)
	}
	legacyLeafDeadline, err := servers.ParseLegacyLeafDeadline(snap.GRPC.LegacyLeafUntil)
	if err != nil {
		logger.Error("invalid GOTHAM_GRPC_LEGACY_LEAF_UNTIL", "error", err)
		return exitError
	}
	if !legacyLeafDeadline.IsZero() {
		legacyNodes, countErr := serverService.LegacyLeafNodeCount(ctx)
		if countErr != nil {
			logger.Warn("failed to count nodes with a legacy certificate", "error", countErr)
		}
		unobservedNodes, unobservedErr := serverService.UnobservedLeafNodeCount(ctx)
		if unobservedErr != nil {
			logger.Warn("failed to count nodes with an unclassified certificate", "error", unobservedErr)
		}
		logger.Warn("node certificates issued before mutual TLS (serverAuth-only) stop being accepted after the sunset; "+
			"unobserved nodes may still be legacy (they renew when they next connect)",
			"deadline", legacyLeafDeadline.Format(time.RFC3339),
			"legacy_nodes", legacyNodes,
			"unobserved_nodes", unobservedNodes,
			"override", config.EnvGRPCLegacyLeafUntil)
	}
	gateway, err := servers.NewGateway(servers.GatewayConfig{
		Addr:                snap.GRPC.Addr,
		Authority:           authority,
		Service:             serverService,
		Logger:              logger,
		Hosts:               grpcHosts,
		EnrollmentToken:     enrollmentToken,
		AllowOpenEnrollment: snap.GRPC.AllowOpenEnrollment,
		LegacyLeafDeadline:  legacyLeafDeadline,
	})
	if err != nil {
		logger.Error("failed to create grpc gateway", "error", err)
		return exitError
	}
	if err := gateway.Start(ctx); err != nil {
		logger.Error("failed to start grpc gateway", "error", err)
		return exitError
	}
	defer gateway.Stop()
	logger.Info("grpc gateway started", slog.String("addr", gateway.Addr()))

	srv, err := server.New(cfg, logger, authService, oauthService, tokenService, serverService, authStore)
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

func agentUpdater(logger *slog.Logger) servers.AgentUpdateOfferer {
	updater, err := updates.AgentUpdaterFromEnv(logger)
	if err != nil {
		logger.Warn("updates: agent update offers disabled", "error", err)
		return nil
	}
	if updater == nil {
		return nil
	}
	return updater
}

func buildOAuthService(oauthCfg config.OAuth, authService *auth.Service, logger *slog.Logger) server.OAuthService {
	providers := make([]auth.OAuthProvider, 0, 1)
	if provider := auth.NewGitHubProvider(
		oauthCfg.GitHub.ClientID,
		oauthCfg.GitHub.ClientSecret,
		oauthCfg.GitHub.RedirectURL,
	); provider != nil {
		providers = append(providers, provider)
	}

	if len(providers) == 0 {
		logger.Info("oauth: no providers configured")
		return nil
	}
	logger.Info("oauth: providers enabled", "providers", len(providers))
	return auth.NewOAuthService(authService, logger, providers...)
}

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

	if err := store.MigrateFailFast(ctx, cfg.Database.DSN, command); err != nil {
		fmt.Fprintf(os.Stderr, "migrate %s: %v\n", command, err)
		return exitError
	}
	return exitOK
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `gotham %s

Usage:
  gotham serve              Start the control plane
  gotham mcp serve          Start the MCP stdio adapter
  gotham admin create       Create the first account (--email; see gotham admin help)
  gotham admin reset-password
                            Replace a password and revoke its sessions
  gotham migrate [verb]     Run database migrations (up, down, status; default up)
  gotham instance set-url <url>
                            Try a panel URL through the instance service
  gotham ca init            Create the gRPC mTLS certificate authority
  gotham update [command]   Check, apply or roll back a self-update
  gotham version            Print the version
  gotham help               Show this help

Configuration is read from gotham.yaml in the working directory and can be
overridden with GOTHAM_* environment variables.
`, version)
}
