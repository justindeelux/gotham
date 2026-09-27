package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeSource is a canned ApplicationSource.
type fakeSource struct {
	apps []ProxiedApplication
	err  error
}

// ListProxiedApplications returns the canned rows.
func (f fakeSource) ListProxiedApplications(context.Context) ([]ProxiedApplication, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.apps, nil
}

// fakeAgent records the ProxyService calls a sync makes.
type fakeAgent struct {
	events *[]string
	calls  []*agentv1.WriteProxyConfigRequest
	// respond overrides the default response when set.
	respond func(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error)
	closed  bool
}

// writeProxyConfigDefault records the call and reports a successful write;
// the ping only succeeds for the verified call.
func (f *fakeAgent) writeProxyConfigDefault(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	written := make([]string, 0, len(in.GetFiles()))
	for _, file := range in.GetFiles() {
		written = append(written, file.GetPath())
	}
	return &agentv1.WriteProxyConfigResponse{Written: written, Reloaded: in.GetVerify()}, nil
}

func (f *fakeAgent) WriteProxyConfig(_ context.Context, in *agentv1.WriteProxyConfigRequest, _ ...grpc.CallOption) (*agentv1.WriteProxyConfigResponse, error) {
	f.calls = append(f.calls, in)
	f.record("write-verify=" + boolString(in.GetVerify()))
	if f.respond != nil {
		return f.respond(in)
	}
	return f.writeProxyConfigDefault(in)
}

func (f *fakeAgent) Close() error {
	f.closed = true
	f.record("close")
	return nil
}

// record appends one event when the fixture shares an event log.
func (f *fakeAgent) record(event string) {
	if f.events != nil {
		*f.events = append(*f.events, event)
	}
}

// fakeContainers is a canned ContainerService that records lifecycle calls.
type fakeContainers struct {
	containers.ContainerService

	events  *[]string
	list    []containers.Container
	listErr error
	started []string
	pulled  []string
	runs    []containers.RunOptions
	runErr  error
}

func (f *fakeContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	f.record("list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeContainers) Start(_ context.Context, _ uuid.UUID, containerID string) error {
	f.record("start=" + containerID)
	f.started = append(f.started, containerID)
	return nil
}

func (f *fakeContainers) Pull(_ context.Context, _ uuid.UUID, image string) error {
	f.record("pull=" + image)
	f.pulled = append(f.pulled, image)
	return nil
}

func (f *fakeContainers) Run(_ context.Context, _ uuid.UUID, opts containers.RunOptions) (string, error) {
	f.record("run=" + opts.Name)
	f.runs = append(f.runs, opts)
	if f.runErr != nil {
		return "", f.runErr
	}
	return "traefik-container-id", nil
}

// record appends one event when the fixture shares an event log.
func (f *fakeContainers) record(event string) {
	if f.events != nil {
		*f.events = append(*f.events, event)
	}
}

// syncFixture wires a SyncService over the fakes.
type syncFixture struct {
	service    *SyncService
	containers *fakeContainers
	agent      *fakeAgent
	serverID   uuid.UUID
	events     *[]string
}

// newSyncFixture builds a fixture with one running Traefik container by
// default; tests override the container list and agent response as needed.
func newSyncFixture(t *testing.T, apps []ProxiedApplication, serverID uuid.UUID) *syncFixture {
	t.Helper()
	events := &[]string{}
	agent := &fakeAgent{events: events}
	containerService := &fakeContainers{
		events: events,
		list:   []containers.Container{{ID: "existing-traefik", Name: TraefikContainerName, State: "running"}},
	}
	service := NewService(Config{
		Applications: fakeSource{apps: apps},
		Containers:   containerService,
		Dial: func(context.Context, uuid.UUID) (AgentClient, error) {
			return agent, nil
		},
		Logger: discardLogger(),
	})
	return &syncFixture{
		service:    service,
		containers: containerService,
		agent:      agent,
		serverID:   serverID,
		events:     events,
	}
}

// discardLogger keeps service tests quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// proxiedApp builds a routable row for serverID.
func proxiedApp(serverID uuid.UUID, domain string, hostPort int32) ProxiedApplication {
	return ProxiedApplication{
		ID:         uuid.New(),
		ServerID:   serverID,
		BaseDomain: domain,
		Port:       3000,
		HostPort:   hostPort,
	}
}

func TestSyncServerWritesVerifiedConfigToRunningTraefik(t *testing.T) {
	serverA := uuid.New()
	serverB := uuid.New()
	app := proxiedApp(serverA, "App.Example.com", 18080)
	other := proxiedApp(serverB, "other.example.com", 18081)

	fixture := newSyncFixture(t, []ProxiedApplication{app, other}, serverA)
	if err := fixture.service.SyncServer(context.Background(), serverA); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}

	if len(fixture.agent.calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(fixture.agent.calls))
	}
	call := fixture.agent.calls[0]
	if !call.GetVerify() {
		t.Error("existing container: verify = false, want true")
	}
	files := filesByPath(call.GetFiles())
	if len(files) != 2 || files["traefik.yml"] == "" || files["dynamic/gotham.yml"] == "" {
		t.Fatalf("written files = %#v", files)
	}
	if !strings.Contains(files["dynamic/gotham.yml"], "Host(`app.example.com`)") {
		t.Errorf("normalized domain missing from dynamic config:\n%s", files["dynamic/gotham.yml"])
	}
	if !strings.Contains(files["dynamic/gotham.yml"], "http://172.17.0.1:18080") {
		t.Errorf("backend target missing from dynamic config:\n%s", files["dynamic/gotham.yml"])
	}
	if strings.Contains(files["dynamic/gotham.yml"], "other.example.com") {
		t.Errorf("another node's route leaked into the config:\n%s", files["dynamic/gotham.yml"])
	}
	if len(fixture.containers.pulled) != 0 || len(fixture.containers.runs) != 0 {
		t.Errorf("running container was bootstrapped again: pulled=%v runs=%d",
			fixture.containers.pulled, len(fixture.containers.runs))
	}
	if !fixture.agent.closed {
		t.Error("agent client was not closed")
	}
	if got := strings.Join(*fixture.events, ","); got != "list,write-verify=true,close" {
		t.Errorf("events = %v, want [list write-verify=true close]", got)
	}
}

func TestSyncServerBootstrapsTraefikBeforeFirstVerifiedWrite(t *testing.T) {
	serverID := uuid.New()
	app := proxiedApp(serverID, "app.example.com", 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = nil

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}

	want := []string{
		"list",
		"write-verify=false",
		"pull=" + TraefikImage,
		"run=" + TraefikContainerName,
		"write-verify=true",
		"close",
	}
	if got := strings.Join(*fixture.events, ","); got != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if len(fixture.containers.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(fixture.containers.runs))
	}
	run := fixture.containers.runs[0]
	if run.Image != TraefikImage || run.Name != TraefikContainerName {
		t.Errorf("run image/name = %q/%q", run.Image, run.Name)
	}
	if len(run.Ports) != len(TraefikPorts) || len(run.Volumes) != len(TraefikVolumes) {
		t.Errorf("run ports/volumes = %v/%v", run.Ports, run.Volumes)
	}
	if run.Labels["gotham.component"] != "proxy" {
		t.Errorf("run labels = %v", run.Labels)
	}
	if len(fixture.agent.calls) != 2 {
		t.Fatalf("agent calls = %d, want 2", len(fixture.agent.calls))
	}
	if fixture.agent.calls[0].GetVerify() {
		t.Error("pre-bootstrap write must be unverified")
	}
	if !fixture.agent.calls[1].GetVerify() {
		t.Error("post-bootstrap write must verify")
	}
}

func TestSyncServerStartsStoppedTraefik(t *testing.T) {
	serverID := uuid.New()
	app := proxiedApp(serverID, "app.example.com", 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{{ID: "stopped-traefik", Name: TraefikContainerName, State: "exited"}}

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	if len(fixture.containers.started) != 1 || fixture.containers.started[0] != "stopped-traefik" {
		t.Fatalf("started = %v, want [stopped-traefik]", fixture.containers.started)
	}
	if len(fixture.containers.pulled) != 0 || len(fixture.containers.runs) != 0 {
		t.Errorf("stopped container was recreated: pulled=%v runs=%d",
			fixture.containers.pulled, len(fixture.containers.runs))
	}
	want := []string{"list", "write-verify=false", "start=stopped-traefik", "write-verify=true", "close"}
	if got := strings.Join(*fixture.events, ","); got != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestSyncServerReloadNotConfirmed(t *testing.T) {
	serverID := uuid.New()
	app := proxiedApp(serverID, "app.example.com", 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return &agentv1.WriteProxyConfigResponse{PingError: "connection refused"}, nil
	}

	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrReload) {
		t.Fatalf("err = %v, want ErrReload", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want the ping detail", err)
	}
}

func TestSyncServerRejectsUnroutableApplication(t *testing.T) {
	serverID := uuid.New()
	app := proxiedApp(serverID, "app.example.com", 0)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if len(fixture.agent.calls) != 0 || len(*fixture.events) != 0 {
		t.Errorf("an invalid row reached the node: agent=%d container=%v",
			len(fixture.agent.calls), *fixture.events)
	}
}

func TestSyncServerWithoutDialer(t *testing.T) {
	serverID := uuid.New()
	service := NewService(Config{
		Applications: fakeSource{apps: []ProxiedApplication{proxiedApp(serverID, "app.example.com", 18080)}},
		Containers:   &fakeContainers{},
		Logger:       discardLogger(),
	})
	if err := service.SyncServer(context.Background(), serverID); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
}

func TestSyncServerUnknownNode(t *testing.T) {
	serverID := uuid.New()
	service := NewService(Config{
		Applications: fakeSource{apps: []ProxiedApplication{proxiedApp(serverID, "app.example.com", 18080)}},
		Containers:   &fakeContainers{},
		Dial: func(context.Context, uuid.UUID) (AgentClient, error) {
			return nil, servers.ErrNotFound
		},
		Logger: discardLogger(),
	})
	if err := service.SyncServer(context.Background(), serverID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("err = %v, want ErrServerNotFound", err)
	}
}

func TestSyncServerAgentUnavailableRPC(t *testing.T) {
	serverID := uuid.New()
	app := proxiedApp(serverID, "app.example.com", 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return nil, status.Error(codes.Unavailable, "agent down")
	}
	if err := fixture.service.SyncServer(context.Background(), serverID); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
}

func TestSyncAllReportsPerNodeOutcome(t *testing.T) {
	serverA := uuid.New()
	serverB := uuid.New()
	apps := []ProxiedApplication{
		proxiedApp(serverA, "one.example.com", 18080),
		proxiedApp(serverA, "two.example.com", 18081), // second row, same node
		proxiedApp(serverB, "three.example.com", 18082),
	}

	agents := map[uuid.UUID]*fakeAgent{}
	dial := func(_ context.Context, serverID uuid.UUID) (AgentClient, error) {
		agent, ok := agents[serverID]
		if !ok {
			return nil, servers.ErrNotFound
		}
		return agent, nil
	}
	containerService := &fakeContainers{}
	agents[serverA] = &fakeAgent{}
	agents[serverB] = &fakeAgent{}
	agents[serverB].respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return nil, status.Error(codes.Unavailable, "agent down")
	}
	service := NewService(Config{
		Applications: fakeSource{apps: apps},
		Containers:   containerService,
		Dial:         dial,
		Logger:       discardLogger(),
	})

	results, err := service.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v, want one per node", results)
	}
	if results[0].ServerID != serverA || results[0].Error != "" {
		t.Errorf("server A result = %#v, want success", results[0])
	}
	if results[1].ServerID != serverB || results[1].Error == "" {
		t.Errorf("server B result = %#v, want failure", results[1])
	}
	if agents[serverA].closed != true {
		t.Error("server A client was not closed")
	}
}

func TestSyncAllSourceFailure(t *testing.T) {
	service := NewService(Config{
		Applications: fakeSource{err: errors.New("db down")},
		Containers:   &fakeContainers{},
		Logger:       discardLogger(),
	})
	if _, err := service.SyncAll(context.Background()); err == nil {
		t.Fatal("SyncAll = nil error, want lookup failure")
	}
}

// filesByPath indexes the request files by path.
func filesByPath(files []*agentv1.ProxyConfigFile) map[string]string {
	out := make(map[string]string, len(files))
	for _, file := range files {
		out[file.GetPath()] = string(file.GetContent())
	}
	return out
}

// boolString renders a bool for the event log.
func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
