package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/servers"
)

// startLocalAgent boots the node agent on the local Docker daemon and returns
// its dial target together with the CA that signed its certificate, so a test
// can reach it exactly the way the control plane does in production (mTLS,
// server name = node ID).
//
// The certificate is issued from a CSR signed by a throwaway CA, the same way
// the Phase 2 gateway does during registration, and both the DockerService
// (container lifecycle) and the BuildService (BuildImage, Phase 4 builds) are
// registered. The server stops when the test ends, so background goroutines
// never outlive the run.
func startLocalAgent(t *testing.T, ctx context.Context, engine *agent.DockerClient, nodeID string) (string, *servers.Authority) {
	t.Helper()
	return startLocalAgentWithOptions(t, ctx, engine, nodeID)
}

// startLocalAgentWithOptions is startLocalAgent with extra service options,
// such as WithProxyService for the Phase 6 proxy smoke.
func startLocalAgentWithOptions(t *testing.T, ctx context.Context, engine *agent.DockerClient, nodeID string, options ...agent.ServerOption) (string, *servers.Authority) {
	t.Helper()
	return startLocalAgentWithProxyRoot(t, ctx, engine, nodeID, "", options...)
}

// startLocalAgentWithProxyRoot is startLocalAgentWithOptions plus the proxy
// directory the node may mount. A test whose CP proxy service runs out of a
// temporary config dir passes it here so the agent's proxy branch accepts the
// same root; an empty proxyRoot keeps the production default.
func startLocalAgentWithProxyRoot(t *testing.T, ctx context.Context, engine *agent.DockerClient, nodeID, proxyRoot string, options ...agent.ServerOption) (string, *servers.Authority) {
	t.Helper()

	// 1. The throwaway CA and the agent's key/CSR/certificate.
	authority, err := servers.LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("load CA: %v", err)
	}
	certDir := t.TempDir()
	signer, keyPath, err := agent.EnsureKey(certDir)
	if err != nil {
		t.Fatalf("agent key: %v", err)
	}
	csrPEM, err := agent.GenerateCSR(nodeID, signer)
	if err != nil {
		t.Fatalf("agent CSR: %v", err)
	}
	certPEM, err := authority.IssueAgentCertFromCSR(csrPEM, nodeID)
	if err != nil {
		t.Fatalf("issue agent certificate: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read agent key: %v", err)
	}
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, authority.CACertPEM(), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	serverCreds, err := agent.ServerCredentials(certPEM, keyPEM, caPath, false)
	if err != nil {
		t.Fatalf("agent server credentials: %v", err)
	}

	// 2. Serve the requested agent services over mTLS on loopback.
	logger := testLogger(t)
	services := append([]agent.ServerOption{agent.WithBuildService(agent.NewBuildServer(engine, logger))}, options...)
	var dockerOptions []agent.DockerServerOption
	if proxyRoot != "" {
		dockerOptions = append(dockerOptions, agent.WithProxyVolumeRoot(proxyRoot))
	}
	grpcServer, err := agent.NewServer("127.0.0.1:0", serverCreds, agent.NewDockerServer(engine, logger, dockerOptions...), logger, services...)
	if err != nil {
		t.Fatalf("agent server: %v", err)
	}
	agentCtx, agentCancel := context.WithCancel(ctx)
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(agentCtx) }()
	t.Cleanup(func() {
		agentCancel()
		select {
		case err := <-serveErr:
			if err != nil {
				t.Errorf("agent serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("agent gRPC server did not stop")
		}
	})

	return grpcServer.Addr().String(), authority
}
