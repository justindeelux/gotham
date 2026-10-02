// Command gotham-agent is the Gotham node agent entrypoint.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/justindeelux/gotham/agent"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/credentials"
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

// shutdownGrace bounds how long a shutdown waits for goroutines to unwind.
const shutdownGrace = 5 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches subcommands and returns the process exit code. With no
// arguments it runs the agent, matching the systemd unit.
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

// runServe loads configuration, connects to the control plane, serves the
// DockerService and streams heartbeats until SIGINT or SIGTERM arrives.
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

	docker, err := agent.NewDockerClient(cfg.DockerSock)
	if err != nil {
		log.Error("docker client", "error", err)
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner := agent.NewAgent(cfg, log, docker)

	var (
		serverOnce sync.Once
		waitGroup  sync.WaitGroup
	)
	runErr := make(chan error, 1)

	startServer := func(response *agentv1.RegisterResponse) error {
		// Persist a freshly issued certificate on every registration so a
		// renewal is visible to the running listener, which reads the file on
		// each handshake. A persistence failure falls back to the in-memory
		// certificate (reload disabled).
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
				// File-backed credentials reload on every handshake, so a
				// re-issued certificate takes effect without a restart.
				creds, err = agent.ServerCredentialsFromFiles(certPath, agent.KeyPath(cfg.KeyFile, cfg.CertDir), cfg.CA)
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
			server, err := agent.NewServer(cfg.ListenAddr, creds, agent.NewDockerServer(docker, log), log,
				agent.WithBuildService(agent.NewBuildServer(docker, log)),
				agent.WithProxyService(agent.NewProxyServer(agent.ProxyServerConfig{Logger: log})),
				agent.WithComposeService(agent.NewComposeServer(agent.ComposeServerConfig{
					Root:       cfg.ComposeRoot,
					DockerHost: cfg.DockerSock,
					Logger:     log,
				})))
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

// usage prints the top-level command help.
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
