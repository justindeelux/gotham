package servers

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestAgentGatewayEndToEnd drives the real agent package against the real CP
// gateway and a real database: the agent generates a keypair, sends a CSR in
// Register, receives a CA-signed certificate for that key, persists it, starts
// its DockerService with it, and streams heartbeats the CP records.
func TestAgentGatewayEndToEnd(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gateway, err := NewGateway(GatewayConfig{
		Addr:      "127.0.0.1:0",
		Authority: authority,
		Service:   service,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if err := gateway.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}
	t.Cleanup(gateway.Stop)

	gatewayAddr := gateway.listener.Addr().String()

	// The agent trusts the CP CA by file path.
	caDir := t.TempDir()
	caPath := filepath.Join(caDir, "ca.crt")
	if err := os.WriteFile(caPath, authority.CACertPEM(), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	nodeID := uniqueNodeID("node-e2e")
	certDir := t.TempDir()
	docker := &e2eDockerClient{
		version:    "24.0.7",
		containers: []*agentv1.ContainerInfo{{Id: "e2e-1", Name: "web"}},
	}

	cfg := agent.Config{
		CPAddr:     gatewayAddr,
		NodeID:     nodeID,
		ListenAddr: "127.0.0.1:0",
		CA:         caPath,
		CertDir:    certDir,
		DockerSock: "/var/run/docker.sock",
		LogLevel:   "error",
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	registered := make(chan *agentv1.RegisterResponse, 1)
	dockerAddr := make(chan string, 1)

	// onRegister mirrors cmd/gotham-agent: start the DockerService server with
	// the freshly issued certificate and record the RegisterResponse.
	var serverOnce sync.Once
	onRegister := func(response *agentv1.RegisterResponse) error {
		var startErr error
		serverOnce.Do(func() {
			if len(response.GetCert()) == 0 {
				startErr = errors.New("control plane returned no certificate")
				return
			}
			keyPEM, err := agent.LoadOrGenerateKey("", certDir)
			if err != nil {
				startErr = err
				return
			}
			creds, err := agent.ServerCredentials(response.GetCert(), keyPEM, caPath, false)
			if err != nil {
				startErr = err
				return
			}
			server, err := agent.NewServer("127.0.0.1:0", creds, agent.NewDockerServer(docker, logger), logger)
			if err != nil {
				startErr = err
				return
			}
			go func() { _ = server.Serve(ctx) }()
			dockerAddr <- server.Addr().String()
		})
		if startErr != nil {
			return startErr
		}
		select {
		case registered <- response:
		default:
		}
		return nil
	}

	runner := agent.NewAgent(cfg, logger, docker, agent.WithHeartbeatInterval(200*time.Millisecond))
	runErr := make(chan error, 1)
	go func() { runErr <- runner.Run(ctx, onRegister) }()

	// 1. Register succeeded and returned a certificate for the agent's key.
	var response *agentv1.RegisterResponse
	select {
	case response = <-registered:
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not register with the gateway")
	}
	if len(response.GetCert()) == 0 {
		t.Fatal("RegisterResponse.cert is empty")
	}
	cert := parseCertPEM(t, response.GetCert())
	if _, err := cert.Verify(x509.VerifyOptions{
		DNSName:   nodeID,
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("issued agent certificate failed verification: %v", err)
	}

	// 2. The registry now holds a ready server for the node.
	row := waitForServer(t, st, nodeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})
	if row.NodeID == nil || *row.NodeID != nodeID {
		t.Fatalf("server node_id = %v, want %q", row.NodeID, nodeID)
	}
	if row.Status != StatusReady {
		t.Errorf("server status = %q, want %q", row.Status, StatusReady)
	}
	if row.DockerVersion == nil || *row.DockerVersion != "24.0.7" {
		t.Errorf("docker_version = %v, want 24.0.7", row.DockerVersion)
	}

	// 3. Heartbeats flow through the gateway and update metrics/last_seen.
	updated := waitForHeartbeat(t, st, nodeID)
	if updated.CpuUsage == nil || updated.MemUsage == nil || updated.DiskUsage == nil {
		t.Errorf("metrics not recorded: cpu=%v mem=%v disk=%v", updated.CpuUsage, updated.MemUsage, updated.DiskUsage)
	}
	if !updated.LastSeen.Valid || updated.LastSeen.Time.IsZero() {
		t.Errorf("last_seen = %+v, want a set timestamp", updated.LastSeen)
	}
	if updated.ContainerCount == nil || *updated.ContainerCount != 1 {
		t.Errorf("container_count = %v, want 1", updated.ContainerCount)
	}

	// 4. The agent persisted its key and the CA-signed certificate, and the
	// certificate matches the key it stored.
	agentCert := readAgentCert(t, certDir)
	if !agentCert.PublicKey.(*ecdsa.PublicKey).Equal(cert.PublicKey) {
		t.Error("persisted agent certificate public key mismatch")
	}
	if _, err := agentCert.Verify(x509.VerifyOptions{Roots: authority.Pool()}); err != nil {
		t.Errorf("persisted agent certificate failed verification: %v", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(certDir, "agent.key"))
	if err != nil {
		t.Fatalf("agent.key not persisted: %v", err)
	}
	if block, _ := pem.Decode(keyPEM); block == nil || block.Type != "EC PRIVATE KEY" {
		t.Fatalf("agent.key block = %v, want EC PRIVATE KEY", block)
	}

	// 5. The agent's DockerService is reachable over mTLS using a CP-issued
	// client certificate, proving the issued cert is usable end to end.
	var addr string
	select {
	case addr = <-dockerAddr:
	case <-time.After(5 * time.Second):
		t.Fatal("DockerService did not start")
	}
	assertDockerServiceReachable(t, authority, addr, nodeID)

	t.Logf("e2e loop ok: node=%s gateway=%s docker=%s cert_cn=%s containers=1 cpu=%v last_seen=%s",
		nodeID, gatewayAddr, addr, agentCert.Subject.CommonName, *updated.CpuUsage, updated.LastSeen.Time.Format(time.RFC3339))

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("agent.Run = %v, want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent.Run did not return after cancel")
	}
}

// waitForServer polls the registry until the node's row appears, failing the
// test after a few seconds.
func waitForServer(t *testing.T, st *store.Store, nodeID string) sqlc.Server {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		row, err := st.GetServerByNodeID(ctx, &nodeID)
		cancel()
		if err == nil {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("server %q not registered: %v", nodeID, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForHeartbeat polls until the node records its first heartbeat metrics.
func waitForHeartbeat(t *testing.T, st *store.Store, nodeID string) sqlc.Server {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		row, err := st.GetServerByNodeID(ctx, &nodeID)
		cancel()
		if err == nil && row.CpuUsage != nil && row.LastSeen.Valid {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("no heartbeat recorded for %q", nodeID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// assertDockerServiceReachable dials the agent's DockerService over mTLS with a
// CP-issued client certificate and lists its containers.
func assertDockerServiceReachable(t *testing.T, authority *Authority, addr, serverName string) {
	t.Helper()

	clientCertPEM, clientKeyPEM, err := authority.IssueClientCert("e2e-test-client")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      authority.Pool(),
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatalf("dial DockerService: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := agentv1.NewDockerServiceClient(conn).ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		t.Fatalf("ListContainers over mTLS: %v", err)
	}
	if len(response.GetContainers()) != 1 || response.GetContainers()[0].GetId() != "e2e-1" {
		t.Fatalf("containers = %v, want one with id e2e-1", response.GetContainers())
	}
}

// readAgentCert reads and parses <certDir>/agent.crt.
func readAgentCert(t *testing.T, certDir string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(certDir, "agent.crt"))
	if err != nil {
		t.Fatalf("agent.crt not persisted: %v", err)
	}
	return parseCertPEM(t, data)
}

// e2eDockerClient is a minimal in-memory dockerClient for the agent.
type e2eDockerClient struct {
	version    string
	containers []*agentv1.ContainerInfo
}

func (f *e2eDockerClient) Version(context.Context) (string, error) { return f.version, nil }

func (f *e2eDockerClient) ListContainers(context.Context, bool) ([]*agentv1.ContainerInfo, error) {
	return f.containers, nil
}

func (f *e2eDockerClient) Start(context.Context, string) error   { return nil }
func (f *e2eDockerClient) Stop(context.Context, string) error    { return nil }
func (f *e2eDockerClient) Restart(context.Context, string) error { return nil }
func (f *e2eDockerClient) Remove(context.Context, string) error  { return nil }
func (f *e2eDockerClient) PullImage(context.Context, string) error {
	return nil
}
func (f *e2eDockerClient) CreateContainer(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	return "e2e-created", nil
}
func (f *e2eDockerClient) RunImage(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	return "e2e-created", nil
}
func (f *e2eDockerClient) Logs(context.Context, string, bool, int64) (<-chan []byte, error) {
	chunks := make(chan []byte)
	close(chunks)
	return chunks, nil
}
