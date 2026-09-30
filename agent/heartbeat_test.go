package agent

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeAgentService is an in-memory AgentServiceServer.
type fakeAgentService struct {
	agentv1.UnimplementedAgentServiceServer

	cpVersion   string
	cert        []byte
	registerErr error
	heartbeatCh chan *agentv1.HeartbeatRequest

	mu         sync.Mutex
	registers  []*agentv1.RegisterRequest
	heartbeats []*agentv1.HeartbeatRequest
	// heartbeatNodes is the node id carried in the heartbeat stream metadata,
	// aligned with heartbeats (the request itself carries no node id).
	heartbeatNodes []string
}

func (f *fakeAgentService) Register(_ context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	f.mu.Lock()
	f.registers = append(f.registers, req)
	f.mu.Unlock()
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return &agentv1.RegisterResponse{Cert: f.cert, CpVersion: f.cpVersion}, nil
}

func (f *fakeAgentService) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
		}
		if err != nil {
			return err
		}
		f.mu.Lock()
		f.heartbeats = append(f.heartbeats, req)
		nodeID := ""
		if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
			if values := md.Get(nodeIDMetadataKey); len(values) > 0 {
				nodeID = values[0]
			}
		}
		f.heartbeatNodes = append(f.heartbeatNodes, nodeID)
		f.mu.Unlock()
		if f.heartbeatCh != nil {
			select {
			case f.heartbeatCh <- req:
			default:
			}
		}
	}
}

func (f *fakeAgentService) registerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registers)
}

// startFakeCP starts an in-process control plane and returns dial options that
// route the agent to it.
func startFakeCP(t *testing.T, fake *fakeAgentService) []grpc.DialOption {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	}
}

func TestAgentRegisterAndHeartbeat(t *testing.T) {
	fake := &fakeAgentService{
		cpVersion:   "0.2.0",
		cert:        []byte("CERTIFICATE"),
		heartbeatCh: make(chan *agentv1.HeartbeatRequest, 16),
	}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:     "passthrough:///bufnet",
		NodeID:     "node-1",
		CertDir:    t.TempDir(),
		DockerSock: defaultDockerSock,
		LogLevel:   "error",
	}
	certDir := cfg.CertDir
	docker := &fakeDockerClient{
		version:    "24.0.5",
		containers: []*agentv1.ContainerInfo{{Id: "a"}, {Id: "b"}, {Id: "c"}},
	}
	runner := NewAgent(cfg, discardLogger(), docker,
		WithHeartbeatInterval(20*time.Millisecond),
		WithDialOptions(dialOptions...),
	)

	registered := make(chan *agentv1.RegisterResponse, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx, func(response *agentv1.RegisterResponse) error {
			select {
			case registered <- response:
			default:
			}
			return nil
		})
	}()

	select {
	case response := <-registered:
		if string(response.GetCert()) != "CERTIFICATE" {
			t.Errorf("cert = %q; want CERTIFICATE", response.GetCert())
		}
		if response.GetCpVersion() != "0.2.0" {
			t.Errorf("cp version = %q; want 0.2.0", response.GetCpVersion())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not register")
	}

	select {
	case heartbeat := <-fake.heartbeatCh:
		assertHeartbeat(t, heartbeat, 3)
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat received")
	}

	fake.mu.Lock()
	if len(fake.registers) == 0 {
		fake.mu.Unlock()
		t.Fatal("Register was never called")
	}
	register := fake.registers[0]
	fake.mu.Unlock()

	if register.GetNodeId() != "node-1" {
		t.Errorf("NodeId = %q; want node-1", register.GetNodeId())
	}
	if register.GetArch() != runtime.GOARCH {
		t.Errorf("Arch = %q; want %q", register.GetArch(), runtime.GOARCH)
	}
	if !strings.HasPrefix(register.GetOs(), runtime.GOOS) {
		t.Errorf("Os = %q; want prefix %q", register.GetOs(), runtime.GOOS)
	}
	if register.GetDockerVersion() != "24.0.5" {
		t.Errorf("DockerVersion = %q; want 24.0.5", register.GetDockerVersion())
	}

	// The agent must send a CSR bound to its node identity so the CP can issue
	// a certificate for the agent's own keypair.
	if len(register.GetCsr()) == 0 {
		t.Fatal("RegisterRequest.csr is empty")
	}
	csr := parseCSRRequest(t, register.GetCsr())
	if csr.Subject.CommonName != "node-1" {
		t.Errorf("CSR common name = %q; want node-1", csr.Subject.CommonName)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "node-1" {
		t.Errorf("CSR DNS SANs = %v; want [node-1]", csr.DNSNames)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature is invalid: %v", err)
	}

	// The certificate returned by the CP is persisted next to the key, and the
	// key generated for the CSR is written once.
	if data, err := os.ReadFile(filepath.Join(certDir, certFileName)); err != nil {
		t.Errorf("agent.crt not persisted: %v", err)
	} else if string(data) != "CERTIFICATE" {
		t.Errorf("agent.crt = %q; want CERTIFICATE", data)
	}
	if _, err := os.Stat(filepath.Join(certDir, keyFileName)); err != nil {
		t.Errorf("agent.key not persisted: %v", err)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v; want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestAgentRetriesRegisterUntilCanceled(t *testing.T) {
	fake := &fakeAgentService{registerErr: errors.New("control plane unreachable")}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:     "passthrough:///bufnet",
		NodeID:     "node-retry",
		CertDir:    t.TempDir(),
		DockerSock: defaultDockerSock,
		LogLevel:   "error",
	}
	runner := NewAgent(cfg, discardLogger(), nil,
		WithBackoff(5*time.Millisecond, 10*time.Millisecond),
		WithDialOptions(dialOptions...),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := runner.Run(ctx, nil); err != nil {
		t.Fatalf("Run = %v; want nil after cancel", err)
	}
	if count := fake.registerCount(); count < 2 {
		t.Errorf("register attempts = %d; want >= 2", count)
	}
}

func assertHeartbeat(t *testing.T, heartbeat *agentv1.HeartbeatRequest, wantContainers int64) {
	t.Helper()
	for name, value := range map[string]float64{
		"cpu":  heartbeat.GetCpuUsage(),
		"mem":  heartbeat.GetMemUsage(),
		"disk": heartbeat.GetDiskUsage(),
	} {
		if value < 0 || value > 1 {
			t.Errorf("%s usage = %v; want 0..1", name, value)
		}
	}
	// The I/O rates are zero on the first sample and on platforms that cannot
	// report them; they must never be negative or NaN.
	for name, value := range map[string]float64{
		"net rx":     heartbeat.GetNetRxBps(),
		"net tx":     heartbeat.GetNetTxBps(),
		"disk read":  heartbeat.GetDiskReadBps(),
		"disk write": heartbeat.GetDiskWriteBps(),
	} {
		if math.IsNaN(value) || value < 0 {
			t.Errorf("%s rate = %v; want a finite, non-negative rate", name, value)
		}
	}
	if heartbeat.GetContainerCount() != wantContainers {
		t.Errorf("container count = %d; want %d", heartbeat.GetContainerCount(), wantContainers)
	}
	if heartbeat.GetSentAt() == nil {
		t.Error("SentAt is nil")
	}
}

// parseCSRRequest decodes the first PKCS#10 certificate request in a PEM blob.
func parseCSRRequest(t *testing.T, csrPEM []byte) *x509.CertificateRequest {
	t.Helper()

	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("decode CSR: not PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse CSR: %v", err)
	}
	return csr
}
