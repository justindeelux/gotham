package containers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeRegistry is an in-memory Registry.
type fakeRegistry struct {
	mu     sync.Mutex
	items  map[uuid.UUID]*servers.Server
	getErr error
	gets   int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{items: make(map[uuid.UUID]*servers.Server)}
}

func (f *fakeRegistry) seed() *servers.Server {
	server := &servers.Server{ID: uuid.New(), Name: "node-1", IP: "10.0.0.5", Port: 22, Status: servers.StatusReady}
	f.mu.Lock()
	f.items[server.ID] = server
	f.mu.Unlock()
	return server
}

func (f *fakeRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	server, ok := f.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	return server, nil
}

// mockDockerClient is a scriptable DockerClient with call counters.
type mockDockerClient struct {
	mu sync.Mutex

	lists     int
	starts    int
	stops     int
	restarts  int
	pulls     int
	runs      int
	listResp  *agentv1.ListContainersResponse
	listErr   error
	actionErr error
	pullErr   error
	runID     string
	runErr    error
	runSeen   *agentv1.CreateContainerRequest
}

func (m *mockDockerClient) ListContainers(context.Context, *agentv1.ListContainersRequest, ...grpc.CallOption) (*agentv1.ListContainersResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists++
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResp != nil {
		return m.listResp, nil
	}
	return &agentv1.ListContainersResponse{}, nil
}

func (m *mockDockerClient) StartContainer(context.Context, *agentv1.ContainerActionRequest, ...grpc.CallOption) (*agentv1.ContainerActionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts++
	return &agentv1.ContainerActionResponse{}, m.actionErr
}

func (m *mockDockerClient) StopContainer(context.Context, *agentv1.ContainerActionRequest, ...grpc.CallOption) (*agentv1.ContainerActionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stops++
	return &agentv1.ContainerActionResponse{}, m.actionErr
}

func (m *mockDockerClient) RestartContainer(context.Context, *agentv1.ContainerActionRequest, ...grpc.CallOption) (*agentv1.ContainerActionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restarts++
	return &agentv1.ContainerActionResponse{}, m.actionErr
}

func (m *mockDockerClient) PullImage(context.Context, *agentv1.PullImageRequest, ...grpc.CallOption) (*agentv1.PullImageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pulls++
	return &agentv1.PullImageResponse{}, m.pullErr
}

func (m *mockDockerClient) CreateContainer(context.Context, *agentv1.CreateContainerRequest, ...grpc.CallOption) (*agentv1.ContainerActionResponse, error) {
	return &agentv1.ContainerActionResponse{}, nil
}

func (m *mockDockerClient) RunImage(_ context.Context, req *agentv1.CreateContainerRequest, _ ...grpc.CallOption) (*agentv1.ContainerActionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs++
	m.runSeen = req
	if m.runErr != nil {
		return nil, m.runErr
	}
	return &agentv1.ContainerActionResponse{ContainerId: m.runID}, nil
}

func (m *mockDockerClient) StreamLogs(context.Context, *agentv1.StreamLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return nil, nil
}

func (m *mockDockerClient) counts() (lists, starts, pulls, runs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lists, m.starts, m.pulls, m.runs
}

// fakeCache is a scriptable Cache with call counters and error injection.
type fakeCache struct {
	mu          sync.Mutex
	items       map[uuid.UUID][]Container
	getErr      error
	setErr      error
	invalErr    error
	gets        int
	sets        int
	invalidates int
}

func newFakeCache() *fakeCache {
	return &fakeCache{items: make(map[uuid.UUID][]Container)}
}

func (f *fakeCache) Get(_ context.Context, id uuid.UUID) ([]Container, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	items, ok := f.items[id]
	return items, ok, nil
}

func (f *fakeCache) Set(_ context.Context, id uuid.UUID, containers []Container) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.items[id] = containers
	return nil
}

func (f *fakeCache) Invalidate(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidates++
	if f.invalErr != nil {
		return f.invalErr
	}
	delete(f.items, id)
	return nil
}

// fixture wires a service with fakes and a dialer returning mock.
func fixture(registry *fakeRegistry, mock *mockDockerClient, cache *fakeCache) *Service {
	dial := func(context.Context, *servers.Server) (DockerClient, error) {
		return mock, nil
	}
	return NewService(Config{Registry: registry, Dial: dial, Cache: cache})
}

func listResponse() *agentv1.ListContainersResponse {
	return &agentv1.ListContainersResponse{Containers: []*agentv1.ContainerInfo{
		{
			Id:        "abc123",
			Name:      "web",
			Image:     "nginx:latest",
			Status:    "Up 2 hours",
			State:     "running",
			CreatedAt: timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
			Labels:    map[string]string{portsLabel: "8080:80, 8443:443"},
		},
		{Id: "def456", Name: "db", Image: "postgres:16", State: "exited"},
	}}
}

func TestListMapsDTO(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{listResp: listResponse()}
	svc := fixture(registry, mock, newFakeCache())

	containers, err := svc.List(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("containers = %d, want 2", len(containers))
	}
	first := containers[0]
	if first.ID != "abc123" || first.Name != "web" || first.Image != "nginx:latest" {
		t.Errorf("first = %+v", first)
	}
	if first.State != "running" || first.Status != "Up 2 hours" {
		t.Errorf("first state/status = %q/%q", first.State, first.Status)
	}
	if len(first.Ports) != 2 || first.Ports[0] != "8080:80" || first.Ports[1] != "8443:443" {
		t.Errorf("first ports = %v", first.Ports)
	}
	if first.Created == nil || !first.Created.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("first created = %v", first.Created)
	}
	second := containers[1]
	if second.Ports == nil || len(second.Ports) != 0 {
		t.Errorf("second ports = %v, want non-nil empty", second.Ports)
	}
	if second.Created != nil {
		t.Errorf("second created = %v, want nil", second.Created)
	}
}

func TestListServesCache(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{listResp: listResponse()}
	cache := newFakeCache()
	svc := fixture(registry, mock, cache)

	if _, err := svc.List(context.Background(), server.ID); err != nil {
		t.Fatalf("first List: %v", err)
	}
	if _, err := svc.List(context.Background(), server.ID); err != nil {
		t.Fatalf("second List: %v", err)
	}
	if lists, _, _, _ := mock.counts(); lists != 1 {
		t.Errorf("agent lists = %d, want 1 (second served from cache)", lists)
	}
}

func TestListDegradesWhenCacheFails(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{listResp: listResponse()}
	cache := newFakeCache()
	cache.getErr = errors.New("connection refused")
	cache.setErr = errors.New("connection refused")
	svc := fixture(registry, mock, cache)

	containers, err := svc.List(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("List with broken cache: %v", err)
	}
	if len(containers) != 2 {
		t.Errorf("containers = %d, want 2", len(containers))
	}
	if lists, _, _, _ := mock.counts(); lists != 1 {
		t.Errorf("agent lists = %d, want 1", lists)
	}
}

func TestMutationsInvalidateCache(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{listResp: listResponse(), runID: "new-id"}
	cache := newFakeCache()
	svc := fixture(registry, mock, cache)

	if _, err := svc.List(context.Background(), server.ID); err != nil {
		t.Fatalf("List: %v", err)
	}

	mutations := map[string]func() error{
		"start":   func() error { return svc.Start(context.Background(), server.ID, "abc123") },
		"stop":    func() error { return svc.Stop(context.Background(), server.ID, "abc123") },
		"restart": func() error { return svc.Restart(context.Background(), server.ID, "abc123") },
		"pull":    func() error { return svc.Pull(context.Background(), server.ID, "nginx:latest") },
		"run": func() error {
			_, err := svc.Run(context.Background(), server.ID, RunOptions{Image: "nginx:latest"})
			return err
		},
	}
	for name, mutate := range mutations {
		cache.mu.Lock()
		cache.items[server.ID] = []Container{{ID: "stale"}}
		cache.invalidates = 0
		cache.mu.Unlock()

		if err := mutate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cache.mu.Lock()
		_, stillCached := cache.items[server.ID]
		invalidates := cache.invalidates
		cache.mu.Unlock()
		if stillCached {
			t.Errorf("%s: cache entry survived mutation", name)
		}
		if invalidates != 1 {
			t.Errorf("%s: invalidates = %d, want 1", name, invalidates)
		}
	}

	// After invalidation the next List goes back to the agent.
	mock.mu.Lock()
	mock.lists = 0
	mock.mu.Unlock()
	if _, err := svc.List(context.Background(), server.ID); err != nil {
		t.Fatalf("List after mutations: %v", err)
	}
	if lists, _, _, _ := mock.counts(); lists != 1 {
		t.Errorf("agent lists = %d, want 1 after invalidation", lists)
	}
}

func TestRunForwardsOptions(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{runID: "new-id"}
	svc := fixture(registry, mock, newFakeCache())

	opts := RunOptions{
		Image:   "nginx:latest",
		Name:    "web",
		Env:     []string{"FOO=bar"},
		Command: []string{"nginx", "-g", "daemon off;"},
		Ports:   []string{"8080:80"},
		Volumes: []string{"/data:/data"},
	}
	id, err := svc.Run(context.Background(), server.ID, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if id != "new-id" {
		t.Errorf("id = %q, want new-id", id)
	}
	mock.mu.Lock()
	seen := mock.runSeen
	mock.mu.Unlock()
	if seen.GetImage() != "nginx:latest" || seen.GetName() != "web" {
		t.Errorf("run request = %+v", seen)
	}
	if len(seen.GetPorts()) != 1 || seen.GetPorts()[0] != "8080:80" {
		t.Errorf("run ports = %v", seen.GetPorts())
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{}
	svc := fixture(registry, mock, newFakeCache())
	ctx := context.Background()

	if err := svc.Start(ctx, server.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("Start empty id: %v, want ErrValidation", err)
	}
	if err := svc.Stop(ctx, server.ID, "  "); !errors.Is(err, ErrValidation) {
		t.Errorf("Stop blank id: %v, want ErrValidation", err)
	}
	if err := svc.Restart(ctx, server.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("Restart empty id: %v, want ErrValidation", err)
	}
	if err := svc.Pull(ctx, server.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("Pull empty image: %v, want ErrValidation", err)
	}
	if _, err := svc.Run(ctx, server.ID, RunOptions{}); !errors.Is(err, ErrValidation) {
		t.Errorf("Run empty image: %v, want ErrValidation", err)
	}
	if _, _, pulls, runs := mock.counts(); pulls != 0 || runs != 0 {
		t.Errorf("agent pulls/runs = %d/%d, want 0/0 (rejected before dial)", pulls, runs)
	}
}

func TestUnknownServer(t *testing.T) {
	registry := newFakeRegistry()
	mock := &mockDockerClient{listResp: listResponse()}
	svc := fixture(registry, mock, newFakeCache())

	unknown := uuid.New()
	if _, err := svc.List(context.Background(), unknown); !errors.Is(err, ErrServerNotFound) {
		t.Errorf("List unknown: %v, want ErrServerNotFound", err)
	}
	if err := svc.Start(context.Background(), unknown, "abc"); !errors.Is(err, ErrServerNotFound) {
		t.Errorf("Start unknown: %v, want ErrServerNotFound", err)
	}
	if lists, starts, _, _ := mock.counts(); lists != 0 || starts != 0 {
		t.Errorf("agent lists/starts = %d/%d, want 0/0", lists, starts)
	}
}

func TestNoDialerReportsUnavailable(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	svc := NewService(Config{Registry: registry, Cache: newFakeCache()})

	if _, err := svc.List(context.Background(), server.ID); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("List without dialer: %v, want ErrAgentUnavailable", err)
	}
	if err := svc.Stop(context.Background(), server.ID, "abc"); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Stop without dialer: %v, want ErrAgentUnavailable", err)
	}

	// SetDial plugs the client in at merge time.
	svc.SetDial(func(context.Context, *servers.Server) (DockerClient, error) {
		return &mockDockerClient{listResp: listResponse()}, nil
	})
	if _, err := svc.List(context.Background(), server.ID); err != nil {
		t.Errorf("List after SetDial: %v", err)
	}
}

func TestRPCErrorMapping(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{}
	svc := fixture(registry, mock, newFakeCache())
	ctx := context.Background()

	mock.actionErr = status.Error(codes.NotFound, "no such container")
	if err := svc.Start(ctx, server.ID, "missing"); !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Start not found: %v, want ErrContainerNotFound", err)
	}
	mock.actionErr = status.Error(codes.Unavailable, "connection refused")
	if err := svc.Stop(ctx, server.ID, "abc"); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Stop unavailable: %v, want ErrAgentUnavailable", err)
	}
	mock.actionErr = status.Error(codes.InvalidArgument, "bad id")
	if err := svc.Restart(ctx, server.ID, "abc"); !errors.Is(err, ErrValidation) {
		t.Errorf("Restart invalid: %v, want ErrValidation", err)
	}
	mock.pullErr = status.Error(codes.DeadlineExceeded, "slow pull")
	if err := svc.Pull(ctx, server.ID, "nginx:latest"); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Pull timeout: %v, want ErrAgentUnavailable", err)
	}
	mock.listErr = status.Error(codes.Internal, "boom")
	if _, err := svc.List(ctx, server.ID); err == nil || errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("List internal: %v, want passthrough error", err)
	}
}

func TestPortsFromLabels(t *testing.T) {
	if ports := portsFromLabels(nil); ports == nil || len(ports) != 0 {
		t.Errorf("nil labels = %v, want non-nil empty", ports)
	}
	ports := portsFromLabels(map[string]string{portsLabel: "8080:80,, 8443:443 "})
	if len(ports) != 2 || ports[0] != "8080:80" || ports[1] != "8443:443" {
		t.Errorf("ports = %v", ports)
	}
}

func TestNopCache(t *testing.T) {
	var cache Cache = NopCache{}
	id := uuid.New()
	if _, ok, err := cache.Get(context.Background(), id); err != nil || ok {
		t.Errorf("NopCache.Get = %v/%v, want miss", ok, err)
	}
	if err := cache.Set(context.Background(), id, []Container{{ID: "x"}}); err != nil {
		t.Errorf("NopCache.Set: %v", err)
	}
	if err := cache.Invalidate(context.Background(), id); err != nil {
		t.Errorf("NopCache.Invalidate: %v", err)
	}
}

func TestRedisCacheDegradesWhenUnreachable(t *testing.T) {
	cache := NewRedisCache("127.0.0.1:1")
	defer func() { _ = cache.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id := uuid.New()
	if _, ok, err := cache.Get(ctx, id); err == nil || ok {
		t.Errorf("Get unreachable = %v/%v, want miss with error", ok, err)
	}
	if err := cache.Set(ctx, id, []Container{{ID: "x"}}); err == nil {
		t.Error("Set unreachable = nil, want error")
	}
	if err := cache.Invalidate(ctx, id); err == nil {
		t.Error("Invalidate unreachable = nil, want error")
	}
}

func TestNewDefaultServiceNilRegistry(t *testing.T) {
	if svc := NewDefaultService(nil, ""); svc != nil {
		t.Errorf("NewDefaultService(nil) = %v, want nil", svc)
	}
}

// Compile-time assertion that the service implements its interface.
var _ ContainerService = (*Service)(nil)
