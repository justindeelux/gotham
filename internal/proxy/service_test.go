package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// discardLogger keeps service tests quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeSource is a canned, mutable ApplicationSource.
type fakeSource struct {
	mu   sync.Mutex
	apps []ProxiedApplication
	err  error
}

// ListProxiedApplications returns the canned rows.
func (f *fakeSource) ListProxiedApplications(context.Context) ([]ProxiedApplication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]ProxiedApplication{}, f.apps...), nil
}

// setApps replaces the canned rows.
func (f *fakeSource) setApps(apps ...ProxiedApplication) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = apps
}

// fakeNodes is a canned NodeSource.
type fakeNodes struct {
	nodes []uuid.UUID
	err   error
}

// ListNodes returns the canned nodes.
func (f fakeNodes) ListNodes(context.Context) ([]uuid.UUID, error) {
	return f.nodes, f.err
}

// fakeHistory is an in-memory HistoryStore, newest first.
// fakeHistory is an in-memory HistoryStore mirroring the store's sequencing
// semantics: one active version, at most one pending version and superseded
// predecessors (newest first).
type fakeHistory struct {
	mu         sync.Mutex
	active     *ConfigVersion
	pending    *ConfigVersion
	superseded []ConfigVersion
	prepareErr error
	promoteErr error
	abortErr   error
	prepares   int
	promotes   int
	aborts     int
}

// PrepareConfigVersion mirrors the transactional preparation.
func (f *fakeHistory) PrepareConfigVersion(_ context.Context, _ uuid.UUID, files []File, contentHash string) (ConfigVersion, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepares++
	if f.prepareErr != nil {
		return ConfigVersion{}, false, f.prepareErr
	}
	switch {
	case f.active != nil && f.active.ContentHash == contentHash && f.pending == nil:
		return *f.active, false, nil
	case f.active != nil && f.active.ContentHash == contentHash:
		// Restoration of the active content while a pending push exists.
		return *f.active, true, nil
	case f.pending != nil && f.pending.ContentHash == contentHash:
		return *f.pending, true, nil
	}
	f.pending = &ConfigVersion{
		ID:          uuid.New(),
		Files:       append([]File{}, files...),
		ContentHash: contentHash,
		Pending:     true,
	}
	return *f.pending, true, nil
}

// PromoteConfigVersion activates a version, clears any pending record and
// supersedes the previous active version.
func (f *fakeHistory) PromoteConfigVersion(_ context.Context, _ uuid.UUID, versionID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promoteErr != nil {
		return f.promoteErr
	}
	f.promotes++
	var target *ConfigVersion
	switch {
	case f.pending != nil && f.pending.ID == versionID:
		target = f.pending
	case f.active != nil && f.active.ID == versionID:
		target = f.active
	default:
		return fmt.Errorf("fakeHistory: unknown version %s", versionID)
	}
	target.Pending = false
	if f.active != nil && f.active.ID != target.ID {
		retired := *f.active
		retired.SupersededAt = time.Now()
		f.superseded = append([]ConfigVersion{retired}, f.superseded...)
	}
	f.pending = nil
	f.active = target
	return nil
}

// AbortConfigVersion drops a pending record.
func (f *fakeHistory) AbortConfigVersion(_ context.Context, _ uuid.UUID, versionID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborts++
	if f.abortErr != nil {
		return f.abortErr
	}
	if f.pending != nil && f.pending.ID == versionID {
		f.pending = nil
	}
	return nil
}

// PreviousConfigVersion mirrors the revert-target rule: a pending push means
// the active version is the actual prior.
func (f *fakeHistory) PreviousConfigVersion(context.Context, uuid.UUID) (ConfigVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending != nil {
		if f.active != nil {
			return *f.active, nil
		}
		return ConfigVersion{}, ErrVersionNotFound
	}
	if len(f.superseded) > 0 {
		return f.superseded[0], nil
	}
	if f.active != nil {
		return *f.active, nil
	}
	return ConfigVersion{}, ErrVersionNotFound
}

// activeHash returns the active content hash (test helper).
func (f *fakeHistory) activeHash() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active == nil {
		return ""
	}
	return f.active.ContentHash
}

// fakeAgent records the ProxyService calls a sync makes.
type fakeAgent struct {
	events  *[]string
	calls   []*agentv1.WriteProxyConfigRequest
	respond func(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error)
	closed  bool
}

// writeProxyConfigDefault records the call and reports a successful ping for
// the verified write.
func (f *fakeAgent) writeProxyConfigDefault(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	written := make([]string, 0, len(in.GetFiles()))
	for _, file := range in.GetFiles() {
		written = append(written, file.GetPath())
	}
	return &agentv1.WriteProxyConfigResponse{Written: written, Reloaded: in.GetVerify()}, nil
}

// WriteProxyConfig records the call and answers through respond when set.
func (f *fakeAgent) WriteProxyConfig(_ context.Context, in *agentv1.WriteProxyConfigRequest, _ ...grpc.CallOption) (*agentv1.WriteProxyConfigResponse, error) {
	f.calls = append(f.calls, in)
	f.record("write-verify=" + boolString(in.GetVerify()))
	if f.respond != nil {
		return f.respond(in)
	}
	return f.writeProxyConfigDefault(in)
}

// Close records the release.
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
	removed []string
	pulled  []string
	runs    []containers.RunOptions
	runErr  error
}

// List returns the canned containers.
func (f *fakeContainers) List(context.Context, uuid.UUID) ([]containers.Container, error) {
	f.record("list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

// Start records a container start.
func (f *fakeContainers) Start(_ context.Context, _ uuid.UUID, containerID string) error {
	f.record("start=" + containerID)
	f.started = append(f.started, containerID)
	return nil
}

// Remove records a container removal.
func (f *fakeContainers) Remove(_ context.Context, _ uuid.UUID, containerID string) error {
	f.record("remove=" + containerID)
	f.removed = append(f.removed, containerID)
	return nil
}

// Pull records an image pull.
func (f *fakeContainers) Pull(_ context.Context, _ uuid.UUID, image string) error {
	f.record("pull=" + image)
	f.pulled = append(f.pulled, image)
	return nil
}

// Run records a container create+start and adds it to the canned list, so a
// serialized second sync observes the container the first one created.
func (f *fakeContainers) Run(_ context.Context, _ uuid.UUID, opts containers.RunOptions) (string, error) {
	f.record("run=" + opts.Name)
	f.runs = append(f.runs, opts)
	if f.runErr != nil {
		return "", f.runErr
	}
	id := "traefik-container-id"
	created := containers.Container{
		ID:            id,
		Name:          opts.Name,
		State:         "running",
		Image:         opts.Image,
		Ports:         append([]string{}, opts.Ports...),
		Labels:        opts.Labels,
		RestartPolicy: opts.RestartPolicy,
	}
	if configDir := opts.Labels["gotham.proxy.config_dir"]; configDir != "" {
		created.Mounts = append(created.Mounts, containers.ContainerMount{
			Source: configDir, Destination: TraefikContainerConfigDir, ReadOnly: true,
		})
	}
	if acmeDir := opts.Labels["gotham.proxy.acme_dir"]; acmeDir != "" {
		created.Mounts = append(created.Mounts, containers.ContainerMount{
			Source: acmeDir, Destination: TraefikAcmeMount, ReadOnly: false,
		})
	}
	f.list = append(f.list, created)
	return id, nil
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
	source     *fakeSource
	containers *fakeContainers
	agent      *fakeAgent
	history    *fakeHistory
	serverID   uuid.UUID
	events     *[]string
}

// runningTraefik is the expected proxy container with its production ports,
// labels, mounts and restart policy.
func runningTraefik() containers.Container {
	return containers.Container{
		ID:            "existing-traefik",
		Name:          TraefikContainerName,
		State:         "running",
		Image:         TraefikImage,
		Ports:         append([]string{}, TraefikPorts...),
		Labels:        traefikLabels(TraefikDir, TraefikAcmeDir),
		RestartPolicy: TraefikRestartPolicy,
		Mounts: []containers.ContainerMount{
			{Source: TraefikDir, Destination: TraefikContainerConfigDir, ReadOnly: true},
			{Source: TraefikAcmeDir, Destination: TraefikAcmeMount, ReadOnly: false},
		},
	}
}

// newSyncFixture builds a fixture with one running Traefik container carrying
// the production ports; tests override the container list and agent response.
func newSyncFixture(t *testing.T, apps []ProxiedApplication, serverID uuid.UUID) *syncFixture {
	t.Helper()
	events := &[]string{}
	agent := &fakeAgent{events: events}
	source := &fakeSource{apps: apps}
	history := &fakeHistory{}
	containerService := &fakeContainers{
		events: events,
		list:   []containers.Container{runningTraefik()},
	}
	service := NewService(Config{
		Applications: source,
		History:      history,
		Containers:   containerService,
		Dial: func(context.Context, uuid.UUID) (AgentClient, error) {
			return agent, nil
		},
		Logger: discardLogger(),
	})
	return &syncFixture{
		service:    service,
		source:     source,
		containers: containerService,
		agent:      agent,
		history:    history,
		serverID:   serverID,
		events:     events,
	}
}

// runningApp builds a routable row whose deployment container is `containerID`
// and whose published binding is `published` ("" for none).
func runningApp(serverID uuid.UUID, domain string, containerID string, privatePort, published int32) ProxiedApplication {
	app := ProxiedApplication{
		ID:          uuid.New(),
		ServerID:    serverID,
		BaseDomain:  domain,
		Port:        privatePort,
		ContainerID: containerID,
	}
	if published > 0 {
		app.HostPort = published
	}
	return app
}

// appContainer is an application container publishing its container port 3000
// on the given host port.
const appContainerPort = 3000

func appContainer(id string, published int32) containers.Container {
	return containers.Container{
		ID:            id,
		Name:          "gotham-app-" + id,
		State:         "running",
		Ports:         []string{fmtPort(published, appContainerPort)},
		PortsReported: true,
	}
}

// fmtPort renders a host:container binding.
func fmtPort(host, container int32) string {
	return itoa(host) + ":" + itoa(container)
}

// itoa renders a small int32.
func itoa(value int32) string {
	return strconvItoa(int(value))
}

// strconvItoa is a local alias to keep the fixture self-contained.
func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestSyncServerWritesVerifiedConfigFromLiveEndpoints(t *testing.T) {
	serverA := uuid.New()
	serverB := uuid.New()
	appID := uuid.New()
	app := ProxiedApplication{
		ID: appID, ServerID: serverA, BaseDomain: "App.Example.com",
		Port: 3000, HostPort: 18080, ContainerID: "app-container-1",
	}
	other := runningApp(serverB, "other.example.com", "other-container", 3001, 18081)

	fixture := newSyncFixture(t, []ProxiedApplication{app, other}, serverA)
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("app-container-1", 32768),
	}

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
	dynamic := files["dynamic/gotham.yml"]
	if !strings.Contains(dynamic, "Host(`app.example.com`)") {
		t.Errorf("normalized domain missing:\n%s", dynamic)
	}
	// The route must use the running container's actual published port, not
	// the stale pinned value (BE-6.1 F3).
	if !strings.Contains(dynamic, "http://172.17.0.1:32768") {
		t.Errorf("live published endpoint missing:\n%s", dynamic)
	}
	if strings.Contains(dynamic, "other.example.com") {
		t.Errorf("another node's route leaked:\n%s", dynamic)
	}
	if !fixture.agent.closed {
		t.Error("agent client was not closed")
	}
	if got := strings.Join(*fixture.events, ","); got != "list,write-verify=true,close" {
		t.Errorf("events = %v, want [list write-verify=true close]", got)
	}
	if fixture.history.promotes != 1 {
		t.Errorf("history promotes = %d, want 1", fixture.history.promotes)
	}
}

func TestSyncServerSupportsEphemeralHostPort(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "ephemeral.example.com", "ephemeral-container", 3000, 0)
	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("ephemeral-container", 49152),
	}

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	dynamic := filesByPath(fixture.agent.calls[0].GetFiles())["dynamic/gotham.yml"]
	if !strings.Contains(dynamic, "http://172.17.0.1:49152") {
		t.Errorf("ephemeral published port missing:\n%s", dynamic)
	}
}

// TestSyncServerIsolatesUnreachablePinnedApplications proves a declared
// pinned port is never routed without a positively matched running container
// with a real publication: a missing container (whose old port may already
// belong to another workload), a stopped one and a legacy label-only binding
// are all isolated instead (R1).
func TestSyncServerIsolatesUnreachablePinnedApplications(t *testing.T) {
	serverID := uuid.New()

	cases := map[string]struct {
		container *containers.Container
		want      string
	}{
		"container missing": {nil, "no running deployment container"},
		"container stopped": {&containers.Container{ID: "gone-container", Name: "app", State: "exited", Ports: []string{"18080:3000"}, PortsReported: true}, "not running"},
		"legacy label only": {&containers.Container{ID: "gone-container", Name: "app", State: "running", Ports: []string{"18080:3000"}, PortsReported: false}, "engine-reported"},
		"loopback only": {&containers.Container{ID: "gone-container", Name: "app", State: "running",
			Ports: []string{"127.0.0.1:18080:3000"}, PortsReported: true}, "no reachable published binding"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			app := runningApp(serverID, "pinned.example.com", "gone-container", 3000, 18080)
			fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
			fixture.containers.list = []containers.Container{runningTraefik()}
			if tc.container != nil {
				fixture.containers.list = append(fixture.containers.list, *tc.container)
			}

			err := fixture.service.SyncServer(context.Background(), serverID)
			if !errors.Is(err, ErrPartialSync) {
				t.Fatalf("err = %v, want ErrPartialSync", err)
			}
			var partial *PartialError
			if !errors.As(err, &partial) || len(partial.Diagnostics) != 1 ||
				!strings.Contains(partial.Diagnostics[0].Reason, tc.want) {
				t.Fatalf("diagnostics = %#v, want reason containing %q", err, tc.want)
			}
			dynamic := filesByPath(fixture.agent.calls[0].GetFiles())["dynamic/gotham.yml"]
			if strings.Contains(dynamic, "pinned.example.com") {
				t.Errorf("unverified pinned port was routed:\n%s", dynamic)
			}
		})
	}
}

func TestSyncServerIsolatesPendingAndInvalidRows(t *testing.T) {
	serverID := uuid.New()
	healthy := runningApp(serverID, "healthy.example.com", "healthy-container", 3000, 18080)
	pending := runningApp(serverID, "pending.example.com", "", 3000, 18081)
	invalid := runningApp(serverID, "not a domain", "invalid-container", 3000, 18082)

	fixture := newSyncFixture(t, []ProxiedApplication{pending, invalid, healthy}, serverID)
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("healthy-container", 32768),
		appContainer("invalid-container", 32769),
	}

	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrPartialSync) {
		t.Fatalf("err = %v, want ErrPartialSync", err)
	}
	var partial *PartialError
	if !errors.As(err, &partial) || len(partial.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want pending and invalid", err)
	}
	dynamic := filesByPath(fixture.agent.calls[0].GetFiles())["dynamic/gotham.yml"]
	if !strings.Contains(dynamic, "healthy.example.com") {
		t.Errorf("healthy route missing despite sibling problems:\n%s", dynamic)
	}
	if strings.Contains(dynamic, "pending.example.com") || strings.Contains(dynamic, "not a domain") {
		t.Errorf("unroutable rows leaked into the config:\n%s", dynamic)
	}
}

func TestSyncServerSkipsDisabledLegacyDomainsWithNoWinner(t *testing.T) {
	serverID := uuid.New()
	first := runningApp(serverID, "legacy.example.com", "legacy-container-1", 3000, 18080)
	first.Disabled = true
	second := runningApp(serverID, "legacy.example.com", "legacy-container-2", 3000, 18081)
	second.Disabled = true
	healthy := runningApp(serverID, "healthy.example.com", "healthy-container", 3000, 18082)

	fixture := newSyncFixture(t, []ProxiedApplication{first, second, healthy}, serverID)
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("legacy-container-1", 32768),
		appContainer("legacy-container-2", 32769),
		appContainer("healthy-container", 32770),
	}

	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrPartialSync) {
		t.Fatalf("err = %v, want ErrPartialSync", err)
	}
	var partial *PartialError
	if !errors.As(err, &partial) || len(partial.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want both disabled duplicates", err)
	}
	for _, diagnostic := range partial.Diagnostics {
		if diagnostic.Domain != "legacy.example.com" ||
			!strings.Contains(diagnostic.Reason, "uniqueness migration") {
			t.Fatalf("diagnostic = %#v, want an actionable disabled-domain reason", diagnostic)
		}
	}
	dynamic := filesByPath(fixture.agent.calls[0].GetFiles())["dynamic/gotham.yml"]
	if strings.Contains(dynamic, "legacy.example.com") {
		t.Errorf("a disabled legacy duplicate was routed:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "healthy.example.com") {
		t.Errorf("healthy route missing:\n%s", dynamic)
	}
}

func TestSyncServerHoldsBackAllDuplicateBindings(t *testing.T) {
	serverID := uuid.New()
	first := runningApp(serverID, "dup.example.com", "first-container", 3000, 18080)
	second := runningApp(serverID, "dup.example.com", "second-container", 3000, 18081)

	fixture := newSyncFixture(t, []ProxiedApplication{first, second}, serverID)
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("first-container", 32768),
		appContainer("second-container", 32769),
	}
	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrPartialSync) {
		t.Fatalf("err = %v, want ErrPartialSync", err)
	}
	var partial *PartialError
	if !errors.As(err, &partial) || len(partial.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want both duplicates held back", err)
	}
	for _, diagnostic := range partial.Diagnostics {
		if !strings.Contains(diagnostic.Reason, "held back") {
			t.Fatalf("diagnostic = %#v, want a quarantine reason", diagnostic)
		}
	}
	dynamic := filesByPath(fixture.agent.calls[0].GetFiles())["dynamic/gotham.yml"]
	if strings.Contains(dynamic, "dup.example.com") {
		t.Errorf("a duplicate binding was routed:\n%s", dynamic)
	}
}

func TestSyncServerBootstrapsWithRestartPolicyAndReadOnlyConfig(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}

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
	if run.RestartPolicy != TraefikRestartPolicy {
		t.Errorf("restart policy = %q, want %q", run.RestartPolicy, TraefikRestartPolicy)
	}
	if !strings.HasSuffix(run.Volumes[0], TraefikContainerConfigDir+":ro") {
		t.Errorf("config mount = %q, want a read-only bind", run.Volumes[0])
	}
}

func TestSyncServerRepairsContainerWithWrongPorts(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	// A Gotham-owned container created before the host-IP binding fix: it
	// carries the ownership labels but misses the loopback ping binding.
	legacy := runningTraefik()
	legacy.ID = "legacy-traefik"
	legacy.Ports = []string{"80:80", "443:443"}
	fixture.containers.list = []containers.Container{legacy, appContainer("app-container", 32768)}

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	if len(fixture.containers.removed) != 1 || fixture.containers.removed[0] != "legacy-traefik" {
		t.Fatalf("removed = %v, want the legacy container", fixture.containers.removed)
	}
	want := []string{
		"list",
		"remove=legacy-traefik",
		"write-verify=false",
		"pull=" + TraefikImage,
		"run=" + TraefikContainerName,
		"write-verify=true",
		"close",
	}
	if got := strings.Join(*fixture.events, ","); got != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestSyncServerWaitsForFreshTraefikReadiness(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}

	attempts := 0
	fixture.agent.respond = func(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		if !in.GetVerify() {
			return &agentv1.WriteProxyConfigResponse{}, nil
		}
		attempts++
		if attempts < 3 {
			return &agentv1.WriteProxyConfigResponse{PingError: "connection refused"}, nil
		}
		return &agentv1.WriteProxyConfigResponse{Reloaded: true}, nil
	}

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("verified attempts = %d, want 3", attempts)
	}
}

// TestSyncServerRejectsUnownedSameNameContainer proves a same-name container
// without Gotham ownership labels is never removed: the sync fails with an
// actionable conflict instead (R5).
func TestSyncServerRejectsUnownedSameNameContainer(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	foreign := runningTraefik()
	foreign.ID = "foreign-traefik"
	foreign.Labels = nil
	fixture.containers.list = []containers.Container{foreign, appContainer("app-container", 32768)}

	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(fixture.containers.removed) != 0 {
		t.Fatalf("removed = %v, want the foreign container untouched", fixture.containers.removed)
	}
	if len(fixture.agent.calls) != 0 {
		t.Fatalf("agent calls = %d, want no write before the conflict is resolved", len(fixture.agent.calls))
	}
}

// TestSyncServerRepairsOwnedDrift proves an owned container that differs from
// the desired image/mounts/policy/ports state is safely recreated (R5).
func TestSyncServerRepairsOwnedDrift(t *testing.T) {
	cases := map[string]func(*containers.Container){
		"wrong image": func(c *containers.Container) {
			c.Image = "traefik:v2.11"
		},
		"wrong mount source": func(c *containers.Container) {
			c.Mounts[0].Source = "/somewhere/else"
		},
		"writable config mount": func(c *containers.Container) {
			c.Mounts[0].ReadOnly = false
		},
		"missing policy label": func(c *containers.Container) {
			delete(c.Labels, "gotham.proxy.restart_policy")
		},
		"unknown restart policy": func(c *containers.Container) {
			c.RestartPolicy = ""
		},
		"wrong restart policy": func(c *containers.Container) {
			c.RestartPolicy = "no"
		},
		"wrong ports": func(c *containers.Container) {
			c.Ports = []string{"80:80", "443:443"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			serverID := uuid.New()
			app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)
			fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
			drifted := runningTraefik()
			mutate(&drifted)
			fixture.containers.list = []containers.Container{drifted, appContainer("app-container", 32768)}

			if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
				t.Fatalf("SyncServer: %v", err)
			}
			if len(fixture.containers.removed) != 1 || fixture.containers.removed[0] != drifted.ID {
				t.Fatalf("removed = %v, want the drifted container recreated", fixture.containers.removed)
			}
			if len(fixture.containers.runs) != 1 {
				t.Fatalf("runs = %d, want 1 recreate", len(fixture.containers.runs))
			}
		})
	}
}

func TestSyncServerReloadNotConfirmed(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}
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
	// The ping failure happened after a write attempt: the pending record is
	// retained (ambiguous node state) and the error is a degraded ErrHistory.
	if !errors.Is(err, ErrHistory) {
		t.Fatalf("err = %v, want ErrHistory too", err)
	}
	if fixture.history.pending == nil {
		t.Fatal("pending record was dropped after an ambiguous push failure")
	}
}

func TestSyncServerWithoutDialer(t *testing.T) {
	serverID := uuid.New()
	service := NewService(Config{
		Applications: &fakeSource{apps: []ProxiedApplication{runningApp(serverID, "app.example.com", "c", 3000, 18080)}},
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
		Applications: &fakeSource{apps: []ProxiedApplication{runningApp(serverID, "app.example.com", "c", 3000, 18080)}},
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
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return nil, status.Error(codes.Unavailable, "agent down")
	}
	if err := fixture.service.SyncServer(context.Background(), serverID); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
}

// TestSyncServerSerializesSnapshotThroughWrite proves a delayed older sync
// cannot overwrite a newer deletion (BE-6.1 F4): the first sync is blocked
// inside its write while the source changes and a second sync runs; the final
// document must be the second (empty) one.
func TestSyncServerSerializesSnapshotThroughWrite(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}

	released := make(chan struct{})
	started := make(chan struct{}, 1)
	var once sync.Once
	fixture.agent.respond = func(in *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		once.Do(func() {
			started <- struct{}{}
			<-released
		})
		return &agentv1.WriteProxyConfigResponse{Written: []string{"dynamic/gotham.yml"}, Reloaded: in.GetVerify()}, nil
	}

	var waitGroup sync.WaitGroup
	var firstErr, secondErr error
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		firstErr = fixture.service.SyncServer(context.Background(), serverID)
	}()
	<-started

	// The delete lands while the first sync is inside its write.
	fixture.source.setApps()
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		secondErr = fixture.service.SyncServer(context.Background(), serverID)
	}()
	close(released)
	waitGroup.Wait()

	if firstErr != nil || secondErr != nil {
		t.Fatalf("errors = %v / %v, want nil", firstErr, secondErr)
	}
	last := fixture.agent.calls[len(fixture.agent.calls)-1]
	dynamic := filesByPath(last.GetFiles())["dynamic/gotham.yml"]
	if strings.Contains(dynamic, "app.example.com") {
		t.Fatalf("stale snapshot won over the deletion:\n%s", dynamic)
	}
}

// TestSyncServerConcurrentBootstrapConverges proves two simultaneous first
// syncs serialize: exactly one bootstrap runs and both calls succeed instead
// of racing the same container name (BE-6.1 F4).
func TestSyncServerConcurrentBootstrapConverges(t *testing.T) {
	serverID := uuid.New()
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}

	var waitGroup sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		waitGroup.Add(1)
		go func(i int) {
			defer waitGroup.Done()
			errs[i] = fixture.service.SyncServer(context.Background(), serverID)
		}(i)
	}
	waitGroup.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("sync %d err = %v, want nil", i, err)
		}
	}
	if len(fixture.containers.runs) != 1 || len(fixture.containers.pulled) != 1 {
		t.Fatalf("bootstraps = %d runs / %d pulls, want exactly one",
			len(fixture.containers.runs), len(fixture.containers.pulled))
	}
}

func TestSyncAllIncludesRegisteredNodesWithoutRoutes(t *testing.T) {
	serverID := uuid.New()
	events := &[]string{}
	agent := &fakeAgent{events: events}
	containerService := &fakeContainers{events: events}
	service := NewService(Config{
		Applications: &fakeSource{},
		Nodes:        fakeNodes{nodes: []uuid.UUID{serverID}},
		History:      &fakeHistory{},
		Containers:   containerService,
		Dial: func(context.Context, uuid.UUID) (AgentClient, error) {
			return agent, nil
		},
		Logger: discardLogger(),
	})

	results, err := service.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if len(results) != 1 || results[0].ServerID != serverID || results[0].Error != "" {
		t.Fatalf("results = %#v, want the registered node synced", results)
	}
	// A fresh bootstrap writes the config once before the container starts and
	// once verified afterwards.
	if len(agent.calls) != 2 {
		t.Fatalf("agent calls = %d, want the zero-domain node pushed", len(agent.calls))
	}
	if len(containerService.runs) != 1 {
		t.Fatalf("bootstrap runs = %d, want 1", len(containerService.runs))
	}
}

func TestSyncAllReportsPerNodeOutcome(t *testing.T) {
	serverA := uuid.New()
	serverB := uuid.New()
	apps := []ProxiedApplication{
		runningApp(serverA, "one.example.com", "one", 3000, 18080),
		runningApp(serverA, "two.example.com", "two", 3000, 18081),
		runningApp(serverB, "three.example.com", "three", 3000, 18082),
	}

	agents := map[uuid.UUID]*fakeAgent{}
	dial := func(_ context.Context, serverID uuid.UUID) (AgentClient, error) {
		agent, ok := agents[serverID]
		if !ok {
			return nil, servers.ErrNotFound
		}
		return agent, nil
	}
	containerService := &fakeContainers{list: []containers.Container{
		runningTraefik(),
		appContainer("one", 32768),
		appContainer("two", 32769),
		appContainer("three", 32770),
	}}
	agents[serverA] = &fakeAgent{}
	agents[serverB] = &fakeAgent{}
	agents[serverB].respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return nil, status.Error(codes.Unavailable, "agent down")
	}
	service := NewService(Config{
		Applications: &fakeSource{apps: apps},
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
	byServer := make(map[uuid.UUID]SyncResult, len(results))
	for _, result := range results {
		byServer[result.ServerID] = result
	}
	if result := byServer[serverA]; result.Error != "" {
		t.Errorf("server A result = %#v, want success", result)
	}
	if result := byServer[serverB]; result.Error == "" {
		t.Errorf("server B result = %#v, want failure", result)
	}
}

func TestSyncAllSourceFailure(t *testing.T) {
	service := NewService(Config{
		Applications: &fakeSource{err: errors.New("db down")},
		Containers:   &fakeContainers{},
		Logger:       discardLogger(),
	})
	if _, err := service.SyncAll(context.Background()); err == nil {
		t.Fatal("SyncAll = nil error, want lookup failure")
	}
}

func TestRevertServerRepushesPreviousVersion(t *testing.T) {
	serverID := uuid.New()
	previous := []File{{Name: "dynamic/gotham.yml", Content: []byte("previous")}}
	current := []File{{Name: "dynamic/gotham.yml", Content: []byte("current")}}

	fixture := newSyncFixture(t, nil, serverID)
	fixture.history.active = &ConfigVersion{ID: uuid.New(), Files: current, ContentHash: configHash(current)}
	fixture.history.superseded = []ConfigVersion{{ID: uuid.New(), Files: previous, ContentHash: configHash(previous)}}

	if err := fixture.service.RevertServer(context.Background(), serverID); err != nil {
		t.Fatalf("RevertServer: %v", err)
	}
	if len(fixture.agent.calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(fixture.agent.calls))
	}
	if got := string(fixture.agent.calls[0].GetFiles()[0].GetContent()); got != "previous" {
		t.Fatalf("reverted content = %q, want previous", got)
	}
	if got := fixture.history.activeHash(); got != configHash(previous) {
		t.Fatalf("active hash = %q, want the reverted version promoted", got)
	}
}

func TestRevertServerWithoutPreviousVersion(t *testing.T) {
	serverID := uuid.New()
	fixture := newSyncFixture(t, nil, serverID)

	if err := fixture.service.RevertServer(context.Background(), serverID); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("err = %v, want ErrVersionNotFound", err)
	}
	if len(fixture.agent.calls) != 0 {
		t.Fatal("revert pushed without a previous version")
	}
}

// TestSyncServerUnchangedSyncKeepsActiveSnapshot proves repeated identical
// syncs do not lose or rewrite the active history entry (R2).
func TestSyncServerUnchangedSyncKeepsActiveSnapshot(t *testing.T) {
	serverID := uuid.New()
	fixture := newSyncFixture(t, nil, serverID)

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	active := fixture.history.activeHash()
	if active == "" {
		t.Fatal("first sync did not promote an active version")
	}
	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if got := fixture.history.activeHash(); got != active {
		t.Fatalf("active hash = %q, want the unchanged active %q", got, active)
	}
	if fixture.history.promotes != 1 {
		t.Fatalf("promotes = %d, want 1 (unchanged content records nothing)", fixture.history.promotes)
	}
}

// TestSyncServerPrepareFailureLeavesNodeUntouched proves a history write
// failure fails the sync before any node mutation (R2).
func TestSyncServerPrepareFailureLeavesNodeUntouched(t *testing.T) {
	serverID := uuid.New()
	fixture := newSyncFixture(t, nil, serverID)
	fixture.history.prepareErr = errors.New("db down")

	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrHistory) {
		t.Fatalf("err = %v, want ErrHistory", err)
	}
	if len(fixture.agent.calls) != 0 {
		t.Fatalf("agent calls = %d, want the node untouched", len(fixture.agent.calls))
	}
	if fixture.history.aborts != 0 {
		t.Fatalf("aborts = %d, want none (nothing was prepared)", fixture.history.aborts)
	}
}

// TestSyncServerAmbiguousPushFailureRetainsPendingThenRepairs proves an
// ambiguous push failure keeps the pending record and a later same-content
// sync reconciles it by promoting the pending version (R2).
func TestSyncServerAmbiguousPushFailureRetainsPendingThenRepairs(t *testing.T) {
	serverID := uuid.New()
	fixture := newSyncFixture(t, nil, serverID)

	// First sync: the verified write fails ambiguously after a write attempt.
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return nil, status.Error(codes.Unavailable, "agent down")
	}
	err := fixture.service.SyncServer(context.Background(), serverID)
	if !errors.Is(err, ErrHistory) || !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrHistory and ErrAgentUnavailable", err)
	}
	if fixture.history.pending == nil {
		t.Fatal("ambiguous push failure dropped the pending record")
	}
	if fixture.history.aborts != 0 {
		t.Fatal("ambiguous push failure aborted a possibly-applied intent")
	}
	pendingHash := fixture.history.pending.ContentHash

	// Second sync of the same content: the pending record is reused and
	// promoted after the push succeeds.
	fixture.agent.respond = nil
	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("repair sync: %v", err)
	}
	if fixture.history.pending != nil {
		t.Fatal("pending record survived a confirmed promotion")
	}
	if got := fixture.history.activeHash(); got != pendingHash {
		t.Fatalf("active hash = %q, want the reconciled %q", got, pendingHash)
	}
}

// TestSyncServerPromoteFailureNeverRevertsWrongVersion proves the three-version
// failure sequence: A active, B promoted, C pushed with a promotion failure
// keeps B active and a FRESH service reverts to B (the actual prior), never to
// the older superseded A (R2).
func TestSyncServerPromoteFailureNeverRevertsWrongVersion(t *testing.T) {
	serverID := uuid.New()
	fixture := newSyncFixture(t, nil, serverID)

	// A and B are recorded and promoted normally; their generated content is
	// empty-config identical, so use explicit history entries to force
	// different versions.
	versionA := []File{{Name: "dynamic/gotham.yml", Content: []byte("A")}}
	versionB := []File{{Name: "dynamic/gotham.yml", Content: []byte("B")}}
	versionC := []File{{Name: "dynamic/gotham.yml", Content: []byte("C")}}
	fixture.history.active = &ConfigVersion{ID: uuid.New(), Files: versionB, ContentHash: configHash(versionB)}
	fixture.history.superseded = []ConfigVersion{{ID: uuid.New(), Files: versionA, ContentHash: configHash(versionA)}}

	// Push C with a promotion failure after the node write succeeded.
	fixture.history.promoteErr = errors.New("db down")
	touched, err := fixture.service.push(context.Background(), serverID, fixture.containers.list, versionC)
	if err != nil || !touched {
		t.Fatalf("push C: touched=%v err=%v", touched, err)
	}
	prepared, changed, err := fixture.service.prepareHistory(context.Background(), serverID, versionC)
	if err != nil || !changed {
		t.Fatalf("prepare C: changed=%v err=%v", changed, err)
	}
	if err := fixture.service.pushAndPromote(context.Background(), serverID, fixture.containers.list, versionC, prepared, changed); !errors.Is(err, ErrHistory) {
		t.Fatalf("promote C err = %v, want ErrHistory", err)
	}
	if fixture.history.pending == nil {
		t.Fatal("promotion failure dropped the pending record")
	}

	// A fresh service instance sharing the same history must revert to B.
	fixture.history.promoteErr = nil
	fresh := &SyncService{
		source:      fixture.service.source,
		history:     fixture.history,
		containers:  fixture.containers,
		dial:        fixture.service.dial,
		logger:      discardLogger(),
		backendHost: DefaultBackendHost,
		configDir:   TraefikDir,
		acmeDir:     TraefikAcmeDir,
		timeout:     defaultSyncTimeout,
	}
	if err := fresh.RevertServer(context.Background(), serverID); err != nil {
		t.Fatalf("fresh revert: %v", err)
	}
	last := fixture.agent.calls[len(fixture.agent.calls)-1]
	content := filesByPath(last.GetFiles())["dynamic/gotham.yml"]
	if content != "B" {
		t.Fatalf("revert pushed %q, want the actual prior B (never the older superseded A)", content)
	}
	if fixture.history.pending != nil {
		t.Fatal("revert did not clear the pending record")
	}
}

func TestPortsMatch(t *testing.T) {
	if !portsMatch(TraefikPorts, TraefikPorts) {
		t.Error("identical port sets must match")
	}
	legacy := []string{"80:80", "443:443"}
	if portsMatch(legacy, TraefikPorts) {
		t.Error("a container missing the loopback ping binding must be repaired")
	}
	extra := append(append([]string{}, TraefikPorts...), "9999:9999")
	if portsMatch(extra, TraefikPorts) {
		t.Error("unexpected extra bindings must not count as a match")
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

func TestMountMatchesNormalizesDockerDesktopSources(t *testing.T) {
	mounts := []containers.ContainerMount{{
		Source:      "/host_mnt/private/var/proxy",
		Destination: "/etc/traefik",
		ReadOnly:    true,
	}}
	if !mountMatches(mounts, "/private/var/proxy", "/etc/traefik", true) {
		t.Error("Docker Desktop /host_mnt source must compare equal to the host path")
	}
	if mountMatches(mounts, "/private/var/other", "/etc/traefik", true) {
		t.Error("a different source must not match")
	}
	if mountMatches(mounts, "/private/var/proxy", "/etc/traefik", false) {
		t.Error("a read-only mount must not satisfy a writable expectation")
	}
}
