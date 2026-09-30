package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// fakeUpdateService answers RequestUpdate with one fixed offer, reporting no
// update once the agent already runs the offered version.
type fakeUpdateService struct {
	agentv1.UnimplementedUpdateServiceServer
	resp *agentv1.UpdateResponse
}

func (f *fakeUpdateService) RequestUpdate(_ context.Context, req *agentv1.UpdateRequest) (*agentv1.UpdateResponse, error) {
	if req.GetAgentVersion() == f.resp.GetLatestVersion() {
		return &agentv1.UpdateResponse{UpdateAvailable: false}, nil
	}
	return f.resp, nil
}

// startFakeCPWithUpdate serves AgentService and UpdateService over bufconn.
func startFakeCPWithUpdate(t *testing.T, agentSvc *fakeAgentService, updateSvc *fakeUpdateService) []grpc.DialOption {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(server, agentSvc)
	agentv1.RegisterUpdateServiceServer(server, updateSvc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	}
}

// reportedVersions returns the last agent_version each node reported.
func reportedVersions(fake *fakeAgentService) map[string]string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	out := map[string]string{}
	for i, hb := range fake.heartbeats {
		if i < len(fake.heartbeatNodes) && fake.heartbeatNodes[i] != "" {
			out[fake.heartbeatNodes[i]] = hb.GetAgentVersion()
		}
	}
	return out
}

// TestTwoAgentsUpdateAllReportNewVersion is the plan's two-agent scenario run
// in-process: two agents at v1.0.0 poll a fake CP that offers v1.2.0, both
// apply it and both report v1.2.0 on their next heartbeat.
func TestTwoAgentsUpdateAllReportNewVersion(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	fakeAgent := &fakeAgentService{
		cpVersion:   "test",
		cert:        []byte("CERTIFICATE"),
		heartbeatCh: make(chan *agentv1.HeartbeatRequest, 64),
	}
	dialOptions := startFakeCPWithUpdate(t, fakeAgent, &fakeUpdateService{resp: release.offer()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for _, node := range []string{"node-a", "node-b"} {
		dir := t.TempDir()
		target := filepath.Join(dir, "gotham-agent")
		if err := os.WriteFile(target, []byte("old "+node), 0o755); err != nil {
			t.Fatalf("write target: %v", err)
		}
		cfg := updaterTestConfig(t, target, noopAgentRestart)
		cfg.CPAddr = "passthrough:///bufnet"
		cfg.NodeID = node
		cfg.CertDir = dir
		cfg.DockerSock = defaultDockerSock
		cfg.UpdateInterval = 20 * time.Millisecond
		runner := NewAgent(cfg, discardLogger(), &fakeDockerClient{},
			WithHeartbeatInterval(20*time.Millisecond),
			WithDialOptions(dialOptions...),
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = runner.Run(ctx, nil)
		}()
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		versions := reportedVersions(fakeAgent)
		if versions["node-a"] == "v1.2.0" && versions["node-b"] == "v1.2.0" {
			cancel()
			wg.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	t.Fatalf("agents did not both report v1.2.0: %v", reportedVersions(fakeAgent))
}

// TestHeartbeatReportsAgentVersion proves the agent includes its version in the
// heartbeat stream.
func TestHeartbeatReportsAgentVersion(t *testing.T) {
	fake := &fakeAgentService{cpVersion: "test", cert: []byte("CERT"), heartbeatCh: make(chan *agentv1.HeartbeatRequest, 16)}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{CPAddr: "passthrough:///bufnet", NodeID: "node-v", CertDir: t.TempDir(), Version: "v9.9.9"}
	runner := NewAgent(cfg, discardLogger(), &fakeDockerClient{}, WithHeartbeatInterval(20*time.Millisecond), WithDialOptions(dialOptions...))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx, nil) }()

	select {
	case heartbeat := <-fake.heartbeatCh:
		if heartbeat.GetAgentVersion() != "v9.9.9" {
			t.Fatalf("agent_version = %q, want v9.9.9", heartbeat.GetAgentVersion())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat received")
	}
}
