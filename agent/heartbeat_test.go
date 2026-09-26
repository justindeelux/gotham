package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
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
	if heartbeat.GetContainerCount() != wantContainers {
		t.Errorf("container count = %d; want %d", heartbeat.GetContainerCount(), wantContainers)
	}
	if heartbeat.GetSentAt() == nil {
		t.Error("SentAt is nil")
	}
}
