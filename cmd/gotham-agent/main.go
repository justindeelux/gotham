package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/justindeelux/gotham/agent"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/credentials"
)

var version = "dev"

func newTerminalConfig(cfg agent.Config, log *slog.Logger, containers agent.ContainerExec) agent.TerminalConfig {
	return agent.TerminalConfig{
		Logger:              log,
		Shell:               cfg.TerminalShell,
		Containers:          containers,
		MaxSessions:         cfg.TerminalMaxSessions,
		IntegrationDisabled: !cfg.TerminalIntegration,
	}
}

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const shutdownGrace = 5 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return runServe()
	}
	switch args[0] {
	case "serve", "run":
		return runServe()
	case "update":
		return runUpdate(args[1:])
	case "version", "-v", "--version":
		fmt.Printf("gotham-agent %s\n", version)
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
	cfg, err := agent.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	log := agent.NewLogger(os.Stderr, cfg.LogLevel)
	cfg.Version = version
	log.Info("starting gotham-agent",
		slog.String("version", version),
		slog.String("node_id", cfg.NodeID),
		slog.String("cp_addr", cfg.CPAddr),
		slog.String("listen_addr", cfg.ListenAddr),
	)
	if dir, err := defaultDockerConfig(cfg.CertDir); err != nil {
		log.Warn("docker CLI config directory unavailable; compose deployments may fail", "error", err)
	} else if dir != "" {
		log.Info("docker CLI config directory", "dir", dir)
	}

	health, err := agent.StartHealthServer(cfg.HealthAddr, log)
	if err != nil {
		log.Warn("health endpoint unavailable; the update wrapper health check will fail", "error", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			_ = health.Close(shutdownCtx)
		}()
	}

	docker, err := agent.NewDockerClient(cfg.DockerSock, agent.WithRegistryStateDir(cfg.CertDir), agent.WithLogger(log))
	if err != nil {
		log.Error("docker client", "error", err)
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner := agent.NewAgent(cfg, log, docker)

	var fileServer *agent.FileServer
	// Register the file service even when it is off: the control plane then
	// reports "turned off on this node" instead of mistaking it for an old agent.
	var fileService agentv1.FileServiceServer = agent.NewDisabledFileServer()
	if cfg.Files {
		opened, err := agent.NewFileServer(cfg, log)
		if err != nil {
			log.Error("file service", "error", err)
			return exitError
		}
		fileServer = opened
		fileService = opened
		defer func() { _ = fileServer.Close() }()
	}

	var (
		serverOnce sync.Once
		reloadable bool
		waitGroup  sync.WaitGroup
	)
	runErr := make(chan error, 1)

	startServer := func(response *agentv1.RegisterResponse) error {
		certPEM := response.GetCert()
		certPath := ""
		if len(certPEM) > 0 {
			if written, err := agent.SaveAgentCert(cfg.CertDir, certPEM); err != nil {
				log.Warn("failed to persist agent certificate", "error", err)
			} else {
				certPath = written
			}
		}

		var startErr error
		serverOnce.Do(func() {
			keyPEM, err := agent.LoadOrGenerateKey(cfg.KeyFile, cfg.CertDir)
			if err != nil {
				startErr = err
				return
			}

			var creds credentials.TransportCredentials
			switch {
			case len(certPEM) > 0 && certPath != "":
				creds, err = agent.ServerCredentialsFromFiles(certPath, agent.KeyPath(cfg.KeyFile, cfg.CertDir), cfg.CA)
				reloadable = true
			case len(certPEM) > 0:
				creds, err = agent.ServerCredentials(certPEM, keyPEM, cfg.CA, false)
			case cfg.CA == "":
				log.Warn("control plane returned no certificate; serving plaintext on loopback in development mode")
				creds, err = agent.ServerCredentials(nil, nil, "", cfg.Insecure)
			default:
				log.Warn("control plane returned no certificate; using a self-signed certificate")
				creds, err = agent.ServerCredentials(nil, nil, cfg.CA, false)
			}
			if err != nil {
				startErr = err
				return
			}
			allowAnyPeer := false
			if creds != nil && cfg.CA == "" {
				if !cfg.Insecure {
					startErr = fmt.Errorf("agent: refusing a TLS listener without GOTHAM_AGENT_CA; it cannot authenticate the control plane")
					return
				}
				allowAnyPeer = true
				log.Warn("agent: DEV-INSECURE (GOTHAM_AGENT_INSECURE=true): the listener does not require the control-plane identity")
			}
			toolUse := agent.NewToolUseTracker()
			serverOptions := []agent.ServerOption{
				agent.WithBuildService(agent.NewBuildServer(docker, log, agent.WithToolUseTracker(toolUse))),
				agent.WithProxyService(agent.NewProxyServer(agent.ProxyServerConfig{Logger: log})),
				agent.WithToolService(agent.NewToolServer(agent.WithToolUse(toolUse))),
				agent.WithComposeService(agent.NewComposeServer(agent.ComposeServerConfig{
					Root:       cfg.ComposeRoot,
					DockerHost: cfg.DockerSock,
					VolumeRoot: cfg.ManagedVolumeRoot,
					Logger:     log,
				})),
			}
			serverOptions = append(serverOptions, agent.WithFileService(fileService))
			terminalConfig := newTerminalConfig(cfg, log, docker)
			if fileServer != nil {
				terminalConfig.Roots = fileServer
			}
			serverOptions = append(serverOptions, agent.WithTerminalService(agent.NewTerminalServer(terminalConfig)))
			if allowAnyPeer {
				serverOptions = append(serverOptions, agent.WithoutControlPlanePeer())
			}
			server, err := agent.NewServer(cfg.ListenAddr, creds, agent.NewDockerServer(docker, log,
				agent.WithManagedVolumeRoot(cfg.ManagedVolumeRoot),
				agent.WithArchiveDeniedPaths(cfg.ComposeRoot, cfg.BackupRoot, cfg.CertDir, cfg.DockerSock)), log, serverOptions...)
			if err != nil {
				startErr = err
				return
			}

			log.Info("docker service listening", "addr", server.Addr().String(), "cert", certPath)
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				if err := server.Serve(ctx); err != nil {
					log.Error("docker service stopped with error", "error", err)
				}
			}()
		})
		if !reloadable && len(certPEM) > 0 {
			log.Warn("certificate reload is disabled for this process; a renewed certificate needs a restart",
				"cert", certPath)
		}
		return startErr
	}

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		runErr <- runner.Run(ctx, startServer)
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case err := <-runErr:
		if err != nil {
			log.Error("agent stopped with error", "error", err)
			return exitError
		}
		return exitOK
	}

	done := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		log.Warn("graceful shutdown timed out")
	}

	log.Info("agent stopped")
	return exitOK
}

// defaultDockerConfig points the docker CLI at the service-owned config
// directory when the systemd sandbox hides ~/.docker (ProtectHome). Without a
// readable config dir the CLI skips its compose plugin, so `docker compose -f`
// is parsed as `docker -f`. Fresh installs get DOCKER_CONFIG from the unit;
// this covers existing nodes whose only update is the binary swap.
func defaultDockerConfig(stateDir string) (string, error) {
	if os.Getenv("DOCKER_CONFIG") != "" {
		return "", nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.ReadDir(filepath.Join(home, ".docker")); err == nil {
			return "", nil
		}
	}
	dir, err := filepath.Abs(filepath.Join(stateDir, ".docker"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, os.Setenv("DOCKER_CONFIG", dir)
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `gotham-agent %s

Usage:
  gotham-agent              Run the node agent (same as gotham-agent serve)
  gotham-agent serve        Run the node agent
  gotham-agent update reset Clear the failed-update state so a fixed release is retried
  gotham-agent version      Print the version
  gotham-agent help         Show this help

Configuration is read from GOTHAM_AGENT_* environment variables.
`, version)
}
