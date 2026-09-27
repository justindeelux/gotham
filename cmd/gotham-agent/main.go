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
	log.Info("starting gotham-agent",
		slog.String("version", version),
		slog.String("node_id", cfg.NodeID),
		slog.String("cp_addr", cfg.CPAddr),
		slog.String("listen_addr", cfg.ListenAddr),
	)

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
		var startErr error
		serverOnce.Do(func() {
			certPath := ""
			if len(response.GetCert()) == 0 {
				log.Warn("control plane returned no certificate; using a self-signed certificate")
			} else {
				written, err := agent.SaveAgentCert(cfg.CertDir, response.GetCert())
				if err != nil {
					startErr = err
					return
				}
				certPath = written
			}

			keyPEM, err := agent.LoadOrGenerateKey(cfg.KeyFile, cfg.CertDir)
			if err != nil {
				startErr = err
				return
			}
			creds, err := agent.ServerCredentials(response.GetCert(), keyPEM, cfg.CA)
			if err != nil {
				startErr = err
				return
			}
			server, err := agent.NewServer(cfg.ListenAddr, creds, agent.NewDockerServer(docker, log), log,
				agent.WithBuildService(agent.NewBuildServer(docker, log)))
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
  gotham-agent version      Print the version
  gotham-agent help         Show this help

Configuration is read from GOTHAM_AGENT_* environment variables.
`, version)
}
