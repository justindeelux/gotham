package servers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// defaultGatewayTestDSN points at the dev database from deploy/compose.dev.yml.
const defaultGatewayTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// gatewayTestDSN mirrors the store integration test env override.
func gatewayTestDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultGatewayTestDSN
}

// discardLogger silences service logging during tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestService builds a ServerService backed by the dev database, skipping
// the test when Postgres is unavailable.
func newTestService(t *testing.T) (*ServerService, *store.Store) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := gatewayTestDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}

	service := NewService(Config{
		Store:     st,
		Authority: authority,
		Secret:    "gateway-test-secret",
		Version:   "test",
		Logger:    discardLogger(),
	})
	return service, st
}

// startTestGateway serves g (insecure) over an in-memory connection and returns
// a client connection.
func startTestGateway(t *testing.T, service *ServerService, authority *Authority) *grpc.ClientConn {
	t.Helper()

	gateway, err := NewGateway(GatewayConfig{
		Service:   service,
		Authority: authority,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}

	listener := bufconn.Listen(1024 * 1024)
	go func() {
		_ = gateway.server.Serve(listener)
	}()
	t.Cleanup(func() {
		gateway.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// uniqueNodeID returns a node id that will not collide with other tests.
func uniqueNodeID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestGatewayRegisterCreatesServer(t *testing.T) {
	service, st := newTestService(t)
	conn := startTestGateway(t, service, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nodeID := uniqueNodeID("node-register")
	resp, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{
		NodeId:        nodeID,
		Os:            "linux",
		DockerVersion: "24.0.7",
		Arch:          "amd64",
		TotalMem:      1024,
		TotalDisk:     2048,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(resp.GetCert()) == 0 {
		t.Error("Register returned an empty certificate")
	}
	if resp.GetCpVersion() != "test" {
		t.Errorf("cp_version = %q, want test", resp.GetCpVersion())
	}

	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	if row.Os == nil || *row.Os != "linux" {
		t.Errorf("os = %v, want linux", row.Os)
	}
	if row.DockerVersion == nil || *row.DockerVersion != "24.0.7" {
		t.Errorf("docker_version = %v, want 24.0.7", row.DockerVersion)
	}
	if row.Status != StatusReady {
		t.Errorf("status = %q, want %q", row.Status, StatusReady)
	}

	// A second Register for the same node id must update, not duplicate.
	second, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{
		NodeId:        nodeID,
		Os:            "linux",
		DockerVersion: "25.0.0",
		Arch:          "amd64",
	})
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if len(second.GetCert()) == 0 {
		t.Error("second Register returned an empty certificate")
	}
	reregistered, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID after re-register: %v", err)
	}
	if reregistered.ID != row.ID {
		t.Errorf("re-register created a new server id %s, want %s", reregistered.ID.String(), row.ID.String())
	}
	if reregistered.DockerVersion == nil || *reregistered.DockerVersion != "25.0.0" {
		t.Errorf("docker_version after re-register = %v, want 25.0.0", reregistered.DockerVersion)
	}
}

func TestGatewayHeartbeatUpdatesMetrics(t *testing.T) {
	service, st := newTestService(t)
	conn := startTestGateway(t, service, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nodeID := uniqueNodeID("node-heartbeat")
	client := agentv1.NewAgentServiceClient(conn)
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	streamCtx := metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID)
	stream, err := client.Heartbeat(streamCtx)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{CpuUsage: 0.25, MemUsage: 0.5, DiskUsage: 0.75, ContainerCount: 3}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{CpuUsage: 0.3, MemUsage: 0.6, DiskUsage: 0.8, ContainerCount: 4}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if resp.GetReceivedAt() == nil {
		t.Error("HeartbeatResponse.received_at is nil")
	}

	updated, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID after heartbeat: %v", err)
	}
	if updated.CpuUsage == nil || *updated.CpuUsage != 0.3 {
		t.Errorf("cpu_usage = %v, want 0.3", updated.CpuUsage)
	}
	if updated.ContainerCount == nil || *updated.ContainerCount != 4 {
		t.Errorf("container_count = %v, want 4", updated.ContainerCount)
	}
	if updated.Status != StatusReady {
		t.Errorf("status = %q, want %q", updated.Status, StatusReady)
	}
	if !updated.LastSeen.Valid || updated.LastSeen.Time.IsZero() {
		t.Errorf("last_seen = %+v, want a set timestamp", updated.LastSeen)
	}
}

func TestGatewayUpdateServiceSkeleton(t *testing.T) {
	// The UpdateService skeleton touches no database, so no store is required.
	service := NewService(Config{Secret: "gateway-test-secret", Version: "test", Logger: discardLogger()})
	conn := startTestGateway(t, service, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "0.1.0"})
	if err != nil {
		t.Fatalf("RequestUpdate: %v", err)
	}
	if resp.GetUpdateAvailable() {
		t.Error("update_available = true, want false (skeleton)")
	}
	if resp.GetLatestVersion() != updateLatestVersion {
		t.Errorf("latest_version = %q, want %q", resp.GetLatestVersion(), updateLatestVersion)
	}
}
