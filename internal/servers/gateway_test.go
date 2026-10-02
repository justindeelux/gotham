package servers

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/updatecore"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	service, st, _ := newTestServiceWithAuthority(t)
	return service, st
}

// newTestServiceWithAuthority returns the service and store together with the
// CA the service signs agent certificates with, so tests can verify them.
func newTestServiceWithAuthority(t *testing.T) (*ServerService, *store.Store, *Authority) {
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
	return service, st, authority
}

// startTestGateway serves g (insecure) over an in-memory connection and returns
// a client connection.
func startTestGateway(t *testing.T, service *ServerService) *grpc.ClientConn {
	t.Helper()
	_, conn := startTestGatewayInstance(t, service)
	return conn
}

// startTestGatewayInstance is startTestGateway but also returns the gateway, so
// tests can tighten its per-peer rate limits.
func startTestGatewayInstance(t *testing.T, service *ServerService) (*Gateway, *grpc.ClientConn) {
	t.Helper()
	return startTestGatewayWithConfig(t, GatewayConfig{
		Service: service,
		Logger:  discardLogger(),
	})
}

// startTestGatewayWithConfig serves cfg over an in-memory connection.
func startTestGatewayWithConfig(t *testing.T, cfg GatewayConfig) (*Gateway, *grpc.ClientConn) {
	t.Helper()

	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	gateway, err := NewGateway(cfg)
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
	return gateway, conn
}

// uniqueNodeID returns a node id that will not collide with other tests.
func uniqueNodeID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestGatewayRegisterCreatesServer(t *testing.T) {
	service, st := newTestService(t)
	conn := startTestGateway(t, service)

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
	// A certificate is only ever issued from a CSR bound to the node identity;
	// this gateway has no authority configured, so the response carries none.
	if len(resp.GetCert()) != 0 {
		t.Error("Register returned a certificate without a CSR or a CA")
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
	if len(second.GetCert()) != 0 {
		t.Error("second Register returned a certificate without a CSR")
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

func TestGatewayRegisterRejectsInvalidCSR(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)

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

	conn, err := grpc.NewClient(
		gateway.listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(authority.Pool(), "")),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()

	// The CSR is validated before the registry write, so no row is created.
	_, err = agentv1.NewAgentServiceClient(conn).Register(callCtx, &agentv1.RegisterRequest{
		NodeId: uniqueNodeID("node-bad-csr"),
		Csr:    []byte("not a csr"),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Register(invalid CSR) = %v, want InvalidArgument", err)
	}
}

func TestGatewayHeartbeatUpdatesMetrics(t *testing.T) {
	service, st := newTestService(t)
	conn := startTestGateway(t, service)

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
	base := time.Now().UTC().Truncate(time.Second)
	first := &agentv1.HeartbeatRequest{
		CpuUsage:       0.25,
		MemUsage:       0.5,
		DiskUsage:      0.75,
		ContainerCount: 3,
		NetRxBps:       2048,
		NetTxBps:       1024,
		DiskReadBps:    4096,
		DiskWriteBps:   8192,
		SentAt:         timestamppb.New(base),
	}
	second := &agentv1.HeartbeatRequest{
		CpuUsage:       0.3,
		MemUsage:       0.6,
		DiskUsage:      0.8,
		ContainerCount: 4,
		NetRxBps:       4096,
		NetTxBps:       2048,
		DiskReadBps:    8192,
		DiskWriteBps:   16384,
		SentAt:         timestamppb.New(base.Add(10 * time.Second)),
	}
	if err := stream.Send(first); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if err := stream.Send(second); err != nil {
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

	// The heartbeat path aggregates time-series samples on the server wall
	// clock: two messages sent back-to-back persist a single sample, while the
	// servers row keeps the newest snapshot. The sample is stamped with the
	// message's sent_at.
	var count int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM server_metrics WHERE server_id = $1", row.ID).Scan(&count); err != nil {
		t.Fatalf("count server_metrics: %v", err)
	}
	if count != 1 {
		t.Errorf("server_metrics rows = %d, want 1 (samples inside %s are dropped)", count, heartbeatSampleInterval)
	}
	var (
		cpu float64
		rx  float64
	)
	if err := st.DB.QueryRow(ctx,
		"SELECT cpu_usage, net_rx_bps FROM server_metrics WHERE server_id = $1 AND recorded_at = $2",
		row.ID, base).Scan(&cpu, &rx); err != nil {
		t.Fatalf("read the first sample: %v", err)
	}
	if cpu != 0.25 || rx != 2048 {
		t.Errorf("first sample = cpu %v net_rx %v, want 0.25 / 2048", cpu, rx)
	}
}

// TestGatewayHeartbeatRecordsAgentVersion proves the version map is fed by the
// heartbeat path (which resolves the node first) and refuses an unknown node,
// and that RequestUpdate does not write it.
func TestGatewayHeartbeatRecordsAgentVersion(t *testing.T) {
	service, st := newTestService(t)
	conn := startTestGateway(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := agentv1.NewAgentServiceClient(conn)

	nodeID := uniqueNodeID("node-agentver")
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
		_ = st.DeleteServer(cleanupCtx, row.ID)
	})

	// A registered node's heartbeat records its version.
	stream, err := client.Heartbeat(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{AgentVersion: "v1.2.0", SentAt: timestamppb.Now()}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("close and recv: %v", err)
	}
	found := false
	for _, version := range service.KnownAgentVersions() {
		if version.NodeID == nodeID && version.Version == "v1.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("version map = %+v, want %s=v1.2.0", service.KnownAgentVersions(), nodeID)
	}

	// An unknown node's heartbeat is refused (ErrNotFound) and never recorded.
	unknown := uniqueNodeID("node-unknown")
	stream, err = client.Heartbeat(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, unknown))
	if err != nil {
		t.Fatalf("Heartbeat(unknown): %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{AgentVersion: "v9.9.9", SentAt: timestamppb.Now()}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("close and recv: %v", err)
	}
	for _, version := range service.KnownAgentVersions() {
		if version.NodeID == unknown {
			t.Fatalf("unknown node %s was recorded: %+v", unknown, version)
		}
	}
}

// fakeAgentUpdater is a controllable AgentUpdateOfferer for gateway tests.
type fakeAgentUpdater struct {
	release *updatecore.Release
	target  string
	err     error
	calls   int
}

func (f *fakeAgentUpdater) Offer(context.Context, string, string, string) (*updatecore.Release, error) {
	f.calls++
	return f.release, f.err
}

func (f *fakeAgentUpdater) TargetVersion(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.target != "" {
		return f.target, nil
	}
	if f.release != nil {
		return f.release.Version, nil
	}
	return "", nil
}

func TestGatewayRequestUpdate(t *testing.T) {
	release := &updatecore.Release{
		Version:              "v1.2.0",
		Channel:              "stable",
		Arch:                 "amd64",
		AssetName:            "gotham-agent-linux-amd64",
		AssetURL:             "https://releases.example.com/gotham-agent-linux-amd64",
		ManifestURL:          "https://releases.example.com/gotham-agent-manifest-amd64.txt",
		ManifestSignatureURL: "https://releases.example.com/gotham-agent-manifest-amd64.txt.sig",
		SHA256:               "abc123",
	}

	t.Run("offers a newer verified release", func(t *testing.T) {
		updater := &fakeAgentUpdater{release: release}
		service := NewService(Config{Secret: "gateway-test-secret", Version: "test", Logger: discardLogger(), Updater: updater})
		conn := startTestGateway(t, service)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ctx = metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, "node-update")

		resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{
			AgentVersion: "v1.0.0", Os: "linux", Arch: "amd64",
		})
		if err != nil {
			t.Fatalf("RequestUpdate: %v", err)
		}
		if !resp.GetUpdateAvailable() || resp.GetLatestVersion() != "v1.2.0" {
			t.Fatalf("resp = %+v, want an offer for v1.2.0", resp)
		}
		if resp.GetAssetUrl() != release.AssetURL || resp.GetManifestUrl() != release.ManifestURL ||
			resp.GetManifestSignatureUrl() != release.ManifestSignatureURL || resp.GetSha256() != release.SHA256 ||
			resp.GetChannel() != "stable" {
			t.Fatalf("offer material = %+v", resp)
		}
		if resp.GetRollout() {
			t.Error("rollout = true without an operator trigger")
		}
		// M2: RequestUpdate carries no authenticated node identity, so it must
		// not write the version map; only the heartbeat path does.
		if versions := service.KnownAgentVersions(); len(versions) != 0 {
			t.Errorf("version map = %+v, want empty (RequestUpdate must not record)", versions)
		}
	})

	t.Run("no update when the updater finds none", func(t *testing.T) {
		service := NewService(Config{Secret: "gateway-test-secret", Version: "test", Logger: discardLogger(), Updater: &fakeAgentUpdater{}})
		conn := startTestGateway(t, service)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "v1.0.0", Arch: "amd64"})
		if err != nil {
			t.Fatalf("RequestUpdate: %v", err)
		}
		if resp.GetUpdateAvailable() {
			t.Errorf("update_available = true, want false")
		}
	})

	t.Run("checker error reports no update", func(t *testing.T) {
		service := NewService(Config{
			Secret: "gateway-test-secret", Version: "test", Logger: discardLogger(),
			Updater: &fakeAgentUpdater{err: errors.New("releases API down")},
		})
		conn := startTestGateway(t, service)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "v1.0.0", Arch: "amd64"})
		if err != nil {
			t.Fatalf("RequestUpdate: %v", err)
		}
		if resp.GetUpdateAvailable() {
			t.Errorf("update_available = true on a checker error, want false")
		}
	})

	t.Run("feature off reports no update", func(t *testing.T) {
		service := NewService(Config{Secret: "gateway-test-secret", Version: "test", Logger: discardLogger()})
		conn := startTestGateway(t, service)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "v1.0.0", Arch: "amd64"})
		if err != nil {
			t.Fatalf("RequestUpdate: %v", err)
		}
		if resp.GetUpdateAvailable() {
			t.Errorf("update_available = true with no updater, want false")
		}
	})

	t.Run("active rollout is flagged", func(t *testing.T) {
		updater := &fakeAgentUpdater{release: release}
		service := NewService(Config{Secret: "gateway-test-secret", Version: "test", Logger: discardLogger(), Updater: updater})
		service.StartAgentRollout("v1.2.0")
		conn := startTestGateway(t, service)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "v1.0.0", Arch: "amd64"})
		if err != nil {
			t.Fatalf("RequestUpdate: %v", err)
		}
		if !resp.GetRollout() {
			t.Errorf("rollout = false during an active rollout")
		}
	})
}

// startAuthorityGateway serves service over TLS with authority and returns the
// listener address.
func startAuthorityGateway(t *testing.T, service *ServerService, authority *Authority) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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
	return gateway.listener.Addr().String()
}

// TestGatewayHeartbeatRejectsIdentityMismatch rejects a heartbeat whose
// metadata node id contradicts the authenticated peer certificate (FX-3 item 3).
func TestGatewayHeartbeatRejectsIdentityMismatch(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientCertPEM, clientKeyPEM, err := authority.IssueClientCert("attacker")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	keyPair, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{keyPair},
		RootCAs:      authority.Pool(),
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	streamCtx := metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, "victim")
	stream, err := agentv1.NewAgentServiceClient(conn).Heartbeat(streamCtx)
	if err == nil {
		_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
		_, err = stream.CloseAndRecv()
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Heartbeat with a mismatched identity = %v, want PermissionDenied", err)
	}
}

// TestGatewayRegisterWithCSRIssuesBoundCert is the FX-3 item-1 end-to-end path:
// a CSR bound to the registered node id yields a usable certificate, and the
// registry row is created.
func TestGatewayRegisterWithCSRIssuesBoundCert(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(authority.Pool(), "")))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	nodeID := uniqueNodeID("node-csr")
	csr := testCSR(t, testKey(t), &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: nodeID},
		DNSNames: []string{nodeID},
	})
	resp, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{
		NodeId: nodeID,
		Os:     "linux",
		Csr:    csr,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(resp.GetCert()) == 0 {
		t.Fatal("Register with a bound CSR returned no certificate")
	}
	cert := parseCertPEM(t, resp.GetCert())
	if _, err := cert.Verify(x509.VerifyOptions{
		DNSName:   nodeID,
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("issued certificate failed verification: %v", err)
	}

	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = st.DeleteServer(cleanupCtx, row.ID)
	})
}

// TestGatewayRegisterRejectsIdentityMismatch is the FX-3 item-3 guard: a peer
// that presents a client certificate cannot register under another node id.
func TestGatewayRegisterRejectsIdentityMismatch(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientCertPEM, clientKeyPEM, err := authority.IssueClientCert("attacker")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	keyPair, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{keyPair},
		RootCAs:      authority.Pool(),
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := agentv1.NewAgentServiceClient(conn)
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: "victim", Os: "linux"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Register as victim = %v, want PermissionDenied", err)
	}

	// The authenticated identity itself is accepted.
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: "attacker", Os: "linux"}); err != nil {
		t.Fatalf("Register as the authenticated identity: %v", err)
	}
	identity := "attacker"
	row, err := st.GetServerByNodeID(ctx, &identity)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = st.DeleteServer(cleanupCtx, row.ID)
	})
}

// TestGatewayRegisterRejectsLongNodeID is the FX-3 item-4 cap on node_id.
func TestGatewayRegisterRejectsLongNodeID(t *testing.T) {
	service, _ := newTestService(t)
	conn := startTestGateway(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{
		NodeId: strings.Repeat("a", maxNodeIDLength+1),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Register(long node id) = %v, want InvalidArgument", err)
	}
}

// TestGatewayRegisterRateLimited is the FX-3 item-4 per-peer registration cap.
func TestGatewayRegisterRateLimited(t *testing.T) {
	service, _ := newTestService(t)
	gateway, conn := startTestGatewayInstance(t, service)
	gateway.registerLimiter = newPeerRateLimiter(rate.Limit(0), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := agentv1.NewAgentServiceClient(conn)
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: uniqueNodeID("rl-1")}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: uniqueNodeID("rl-2")}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second Register = %v, want ResourceExhausted", err)
	}
}

// TestGatewayHeartbeatRateLimited is the FX-3 item-4 per-peer heartbeat cap.
func TestGatewayHeartbeatRateLimited(t *testing.T) {
	service, _ := newTestService(t)
	gateway, conn := startTestGatewayInstance(t, service)
	gateway.heartbeatLimiter = newPeerRateLimiter(rate.Limit(0), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nodeID := uniqueNodeID("node-hb-rl")
	client := agentv1.NewAgentServiceClient(conn)
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: nodeID}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	stream, err := client.Heartbeat(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	// The second message exhausts the burst; the server closes the stream with
	// ResourceExhausted, surfaced on the next send or close.
	_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Heartbeat after burst = %v, want ResourceExhausted", err)
	}
}

// TestGatewayRegisterRejectsReservedNodeID is the FX-3 R1 short-term guard: a
// peer cannot enroll the control plane's own listener identity, which would mint
// a CP-impersonation certificate.
func TestGatewayRegisterRejectsReservedNodeID(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gateway, err := NewGateway(GatewayConfig{
		Addr:      "127.0.0.1:0",
		Hosts:     []string{"cp.example.com"},
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

	conn, err := grpc.NewClient(gateway.listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(authority.Pool(), "")))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := agentv1.NewAgentServiceClient(conn)

	reserved := []string{"cp.example.com", "localhost", "127.0.0.1"}
	for _, nodeID := range reserved {
		_, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"})
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("Register(%q) = %v, want PermissionDenied", nodeID, err)
		}
		if _, lookupErr := st.GetServerByNodeID(ctx, &nodeID); lookupErr == nil {
			t.Errorf("a registry row was created for the reserved id %q", nodeID)
		}
	}

	// A non-reserved id is unaffected.
	nodeID := uniqueNodeID("node-not-reserved")
	if _, err := client.Register(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"}); err != nil {
		t.Fatalf("Register(%q) = %v, want success", nodeID, err)
	}
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = st.DeleteServer(cleanupCtx, row.ID)
	})
}

// TestGatewayHeartbeatIdleDeadline proves an idle Heartbeat stream is closed
// rather than held open indefinitely (FX-3 R2).
func TestGatewayHeartbeatIdleDeadline(t *testing.T) {
	service, _ := newTestService(t)
	_, conn := startTestGatewayWithConfig(t, GatewayConfig{
		Service:       service,
		Logger:        discardLogger(),
		HeartbeatIdle: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := agentv1.NewAgentServiceClient(conn).Heartbeat(ctx)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	// Stay quiet past the idle deadline; the server must close the stream
	// rather than hold it open.
	time.Sleep(3 * time.Second)
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("idle Heartbeat = %v, want DeadlineExceeded", err)
	}
}

// TestGatewayLimitsStreamsAndMessages pins the gRPC caps (FX-3 R2). The
// oversized-message path is exercised against the served gateway; the stream cap
// is asserted on the constants and wired by construction.
func TestGatewayLimitsStreamsAndMessages(t *testing.T) {
	if maxConcurrentStreams != 64 || maxRecvMsgSize != 1<<20 {
		t.Fatalf("gateway caps = %d/%d, want 64/1MiB", maxConcurrentStreams, maxRecvMsgSize)
	}
	service, _ := newTestService(t)
	conn := startTestGateway(t, service)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{
		NodeId: uniqueNodeID("node-big"),
		Os:     strings.Repeat("x", 2<<20),
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized Register = %v, want ResourceExhausted", err)
	}
}

// TestLoadAuthorityRejectsLooseKeyPermissions is the FX-3 R3 guard: a CA key
// readable beyond its owner is refused.
func TestLoadAuthorityRejectsLooseKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateAuthority(dir); err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	keyPath := filepath.Join(dir, caKeyFile)
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := LoadAuthority(dir); err == nil {
		t.Error("LoadAuthority(0644 key) = nil error, want refusal")
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatalf("chmod back: %v", err)
	}
	if _, err := LoadAuthority(dir); err != nil {
		t.Errorf("LoadAuthority(0600 key) = %v, want success", err)
	}
}

// TestRegisterErrorLogsUnexpected is the FX-3 item-6 guard: an unexpected
// registry failure is logged before it is collapsed to Internal.
func TestRegisterErrorLogsUnexpected(t *testing.T) {
	var buf bytes.Buffer
	gateway := &Gateway{logger: slog.New(slog.NewTextHandler(&buf, nil))}

	if err := gateway.registerError("node-1", errors.New("db exploded")); status.Code(err) != codes.Internal {
		t.Fatalf("registerError = %v, want Internal", err)
	}
	if !strings.Contains(buf.String(), "db exploded") {
		t.Errorf("unexpected error was not logged: %q", buf.String())
	}

	buf.Reset()
	if err := gateway.registerError("node-1", fmt.Errorf("%w: bad", ErrValidation)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("registerError(validation) = %v, want InvalidArgument", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a domain error was logged: %q", buf.String())
	}
}
