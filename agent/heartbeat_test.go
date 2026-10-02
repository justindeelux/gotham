package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"math/big"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	// registerGate, when non-nil, blocks Register until it is closed or the
	// call's context is canceled.
	registerGate chan struct{}
	// heartbeatFail closes the stream with an error after the first message.
	heartbeatFail bool

	mu         sync.Mutex
	registers  []*agentv1.RegisterRequest
	heartbeats []*agentv1.HeartbeatRequest
	// heartbeatNodes is the node id carried in the heartbeat stream metadata,
	// aligned with heartbeats (the request itself carries no node id).
	heartbeatNodes []string
}

func (f *fakeAgentService) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	f.mu.Lock()
	f.registers = append(f.registers, req)
	f.mu.Unlock()
	if f.registerGate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.registerGate:
		}
	}
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
		if f.heartbeatFail {
			return errors.New("heartbeat stream reset")
		}
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

// newAgentConn builds a client connection with the agent's own dial options
// (including the fake CP's bufconn dialer).
func newAgentConn(t *testing.T, runner *Agent) *grpc.ClientConn {
	t.Helper()
	options, err := runner.dialOptionsFor()
	if err != nil {
		t.Fatalf("dialOptionsFor: %v", err)
	}
	conn, err := grpc.NewClient(runner.cfg.CPAddr, options...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestAgentRegisterDeadlineBounded is the FX-3 item-10 guard: a stalled control
// plane cannot pin a registration attempt past the per-attempt deadline.
func TestAgentRegisterDeadlineBounded(t *testing.T) {
	fake := &fakeAgentService{registerGate: make(chan struct{})}
	defer close(fake.registerGate)
	dialOptions := startFakeCP(t, fake)

	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-deadline",
		CertDir: t.TempDir(),
	}, discardLogger(), nil,
		WithDialOptions(dialOptions...),
		WithRegisterTimeout(100*time.Millisecond),
	)
	client := agentv1.NewAgentServiceClient(newAgentConn(t, runner))

	start := time.Now()
	_, err := runner.register(context.Background(), client)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("register = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("register took %v, want it bounded by the deadline", elapsed)
	}
}

// TestAgentHeartbeatCancelsStreamOnReturn is the FX-3 item-11 guard: the
// heartbeat stream's own context is canceled when the call returns, so a
// reconnect cannot leak the stream.
func TestAgentHeartbeatCancelsStreamOnReturn(t *testing.T) {
	fake := &fakeAgentService{heartbeatFail: true}
	dialOptions := startFakeCP(t, fake)

	var captured context.Context
	interceptor := grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		captured = ctx
		return streamer(ctx, desc, cc, method, opts...)
	})

	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-hb-cancel",
		CertDir: t.TempDir(),
	}, discardLogger(), nil,
		WithHeartbeatInterval(5*time.Millisecond),
		WithDialOptions(append(dialOptions, interceptor)...),
	)
	client := agentv1.NewAgentServiceClient(newAgentConn(t, runner))

	if err := runner.heartbeat(context.Background(), client); err == nil {
		t.Fatal("heartbeat returned nil, want the server's stream error")
	}
	if captured == nil {
		t.Fatal("the stream interceptor did not capture a context")
	}
	deadline := time.Now().Add(2 * time.Second)
	for captured.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if captured.Err() == nil {
		t.Fatal("the heartbeat stream context was not canceled on return")
	}
}

// TestRenewalDelaySchedulesBeforeExpiry pins the FX-3 item-7 renewal schedule.
func TestRenewalDelaySchedulesBeforeExpiry(t *testing.T) {
	certPEM, _, err := generateSelfSigned()
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}
	runner := NewAgent(Config{CertDir: t.TempDir()}, discardLogger(), nil, WithRenewBefore(30*24*time.Hour))

	got := runner.renewalDelay(certPEM)
	// selfSignedValidity is 365 days; renewing 30 days early leaves ~335.
	if got < 330*24*time.Hour || got > 340*24*time.Hour {
		t.Fatalf("renewalDelay = %v, want ~335d", got)
	}
	if runner.renewalDelay(nil) != 0 {
		t.Error("renewalDelay(empty) != 0")
	}
	if runner.renewalDelay([]byte("garbage")) != 0 {
		t.Error("renewalDelay(garbage) != 0")
	}
}

// TestAgentReRegistersBeforeCertificateExpiry is the FX-3 item-7 end-to-end
// behaviour: a short-lived certificate makes the agent re-register before it
// lapses rather than waiting for a stream failure.
func TestAgentReRegistersBeforeCertificateExpiry(t *testing.T) {
	fake := &fakeAgentService{cert: shortLivedCertPEM(t, 2*time.Second)}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-renew",
		CertDir: t.TempDir(),
	}
	runner := NewAgent(cfg, discardLogger(), nil,
		WithHeartbeatInterval(20*time.Millisecond),
		WithBackoff(5*time.Millisecond, 20*time.Millisecond),
		WithRenewBefore(time.Second),
		WithDialOptions(dialOptions...),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- runner.Run(ctx, nil) }()

	deadline := time.Now().Add(4 * time.Second)
	for fake.registerCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if count := fake.registerCount(); count < 2 {
		t.Fatalf("register attempts = %d; want the agent to re-register before expiry", count)
	}
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// shortLivedCertPEM returns a self-signed certificate valid for validity.
func shortLivedCertPEM(t *testing.T, validity time.Duration) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "gotham-agent"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
