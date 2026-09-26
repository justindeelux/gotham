package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeDockerState records what the fake Docker API received.
type fakeDockerState struct {
	mu          sync.Mutex
	started     []string
	stopped     []string
	restarted   []string
	createName  string
	createBody  dockerCreateBody
	pullImage   string
	logsQuery   string
	versionHits int
}

func (s *fakeDockerState) snapshot() fakeDockerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fakeDockerState{
		started:     append([]string(nil), s.started...),
		stopped:     append([]string(nil), s.stopped...),
		restarted:   append([]string(nil), s.restarted...),
		createName:  s.createName,
		createBody:  s.createBody,
		pullImage:   s.pullImage,
		logsQuery:   s.logsQuery,
		versionHits: s.versionHits,
	}
}

// newFakeDockerServer starts an httptest server that mimics the Docker Engine
// endpoints the agent uses.
func newFakeDockerServer(t *testing.T) (*httptest.Server, *fakeDockerState) {
	t.Helper()
	state := &fakeDockerState{}
	mux := http.NewServeMux()

	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		state.mu.Lock()
		state.versionHits++
		state.mu.Unlock()
		writeJSON(t, w, map[string]string{"Version": "24.0.5"})
	})

	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{
				"Id":      "abc123",
				"Names":   []string{"/web"},
				"Image":   "nginx:latest",
				"Status":  "Up 2 minutes",
				"State":   "running",
				"Created": 1700000000,
				"Labels":  map[string]string{"app": "web"},
			},
		})
	})

	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, r *http.Request) {
		var body dockerCreateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state.mu.Lock()
		state.createName = r.URL.Query().Get("name")
		state.createBody = body
		state.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]string{"Id": "created123"})
	})

	mux.HandleFunc("/images/create", func(w http.ResponseWriter, r *http.Request) {
		image := r.URL.Query().Get("fromImage")
		state.mu.Lock()
		state.pullImage = image
		state.mu.Unlock()
		if image == "missing:latest" {
			_, _ = w.Write([]byte(`{"error":"manifest unknown"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"Pulling from library/alpine"}` + "\n"))
		_, _ = w.Write([]byte(`{"status":"Download complete"}` + "\n"))
	})

	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/containers/")
		id, action, found := strings.Cut(rest, "/")
		if !found {
			http.NotFound(w, r)
			return
		}
		if action == "logs" {
			state.mu.Lock()
			state.logsQuery = r.URL.RawQuery
			state.mu.Unlock()
			writeLogFrames(w, 1, "hello ")
			writeLogFrames(w, 2, "world\n")
			return
		}
		if id == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such container"}`))
			return
		}
		state.mu.Lock()
		switch action {
		case "start":
			state.started = append(state.started, id)
		case "stop":
			state.stopped = append(state.stopped, id)
		case "restart":
			state.restarted = append(state.restarted, id)
		default:
			state.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		state.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, state
}

// newFakeDockerClient points a DockerClient at the fake server using the
// DOCKER_HOST override path.
func newFakeDockerClient(t *testing.T, apiURL string) *DockerClient {
	t.Helper()
	t.Setenv(envDockerHost, strings.Replace(apiURL, "http://", "tcp://", 1))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	client, err := NewDockerClient(cfg.DockerSock)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// writeLogFrames writes one Docker multiplexed log frame.
func writeLogFrames(w http.ResponseWriter, streamType byte, payload string) {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	_, _ = w.Write(header)
	_, _ = w.Write([]byte(payload))
}

func TestDockerClientVersion(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "24.0.5" {
		t.Errorf("Version = %q; want %q", version, "24.0.5")
	}
}

func TestDockerClientListContainers(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	containers, err := client.ListContainers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("len(containers) = %d; want 1", len(containers))
	}
	got := containers[0]
	if got.GetId() != "abc123" || got.GetName() != "web" || got.GetImage() != "nginx:latest" {
		t.Errorf("unexpected container: %+v", got)
	}
	if got.GetState() != "running" || got.GetLabels()["app"] != "web" {
		t.Errorf("unexpected container state/labels: %+v", got)
	}
	if got.GetCreatedAt() == nil || got.GetCreatedAt().AsTime().Unix() != 1700000000 {
		t.Errorf("CreatedAt = %v; want unix 1700000000", got.GetCreatedAt())
	}
}

func TestDockerClientLifecycle(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)
	ctx := context.Background()

	for _, call := range []struct {
		name string
		fn   func(context.Context, string) error
	}{
		{"Start", client.Start},
		{"Stop", client.Stop},
		{"Restart", client.Restart},
	} {
		if err := call.fn(ctx, "abc123"); err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
	}

	snapshot := state.snapshot()
	if len(snapshot.started) != 1 || snapshot.started[0] != "abc123" {
		t.Errorf("started = %v; want [abc123]", snapshot.started)
	}
	if len(snapshot.stopped) != 1 || snapshot.stopped[0] != "abc123" {
		t.Errorf("stopped = %v; want [abc123]", snapshot.stopped)
	}
	if len(snapshot.restarted) != 1 || snapshot.restarted[0] != "abc123" {
		t.Errorf("restarted = %v; want [abc123]", snapshot.restarted)
	}
}

func TestDockerClientLifecycleError(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	err := client.Start(context.Background(), "missing")
	if err == nil {
		t.Fatal("Start(missing) = nil; want error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v; want it to mention status 404", err)
	}
}

func TestDockerClientPullImage(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	if err := client.PullImage(context.Background(), "alpine:latest"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if got := state.snapshot().pullImage; got != "alpine:latest" {
		t.Errorf("fromImage = %q; want alpine:latest", got)
	}
}

func TestDockerClientPullImageError(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	err := client.PullImage(context.Background(), "missing:latest")
	if err == nil {
		t.Fatal("PullImage(missing) = nil; want error")
	}
	if !strings.Contains(err.Error(), "manifest unknown") {
		t.Errorf("error = %v; want it to mention manifest unknown", err)
	}
}

// createContainerRequestForTest builds a request exercising every mapped field.
func createContainerRequestForTest() *agentv1.CreateContainerRequest {
	return &agentv1.CreateContainerRequest{
		Image:      "nginx:latest",
		Name:       "web",
		Env:        []string{"K=V"},
		Command:    []string{"run"},
		Entrypoint: []string{"/entry"},
		Labels:     map[string]string{"app": "web"},
		Ports:      []string{"8080:80"},
		Volumes:    []string{"/data:/var/lib/data"},
		Networks:   []string{"gotham"},
	}
}

func TestDockerClientCreateContainer(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	req := createContainerRequestForTest()
	id, err := client.CreateContainer(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if id != "created123" {
		t.Errorf("id = %q; want created123", id)
	}

	snapshot := state.snapshot()
	if snapshot.createName != "web" {
		t.Errorf("create name = %q; want web", snapshot.createName)
	}
	body := snapshot.createBody
	if body.Image != "nginx:latest" {
		t.Errorf("Image = %q; want nginx:latest", body.Image)
	}
	if len(body.Env) != 1 || body.Env[0] != "K=V" {
		t.Errorf("Env = %v; want [K=V]", body.Env)
	}
	if len(body.Cmd) != 1 || body.Cmd[0] != "run" {
		t.Errorf("Cmd = %v; want [run]", body.Cmd)
	}
	if len(body.Entrypoint) != 1 || body.Entrypoint[0] != "/entry" {
		t.Errorf("Entrypoint = %v; want [/entry]", body.Entrypoint)
	}
	if body.Labels["app"] != "web" {
		t.Errorf("Labels = %v; want app=web", body.Labels)
	}
	if _, ok := body.ExposedPorts["80/tcp"]; !ok {
		t.Errorf("ExposedPorts = %v; want 80/tcp", body.ExposedPorts)
	}
	if body.HostConfig == nil {
		t.Fatal("HostConfig = nil; want bind mounts and port bindings")
	}
	if len(body.HostConfig.Binds) != 1 || body.HostConfig.Binds[0] != "/data:/var/lib/data" {
		t.Errorf("Binds = %v; want [/data:/var/lib/data]", body.HostConfig.Binds)
	}
	portBindings := body.HostConfig.PortBindings["80/tcp"]
	if len(portBindings) != 1 || portBindings[0].HostPort != "8080" {
		t.Errorf("PortBindings = %v; want 8080", portBindings)
	}
	if _, ok := body.NetworkingConfig.EndpointsConfig["gotham"]; !ok {
		t.Errorf("NetworkingConfig = %v; want network gotham", body.NetworkingConfig)
	}
}

func TestDockerClientRunImage(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	id, err := client.RunImage(context.Background(), createContainerRequestForTest())
	if err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	if id != "created123" {
		t.Errorf("id = %q; want created123", id)
	}
	started := state.snapshot().started
	if len(started) != 1 || started[0] != "created123" {
		t.Errorf("started = %v; want [created123]", started)
	}
}

func TestDockerClientLogs(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chunks, err := client.Logs(ctx, "abc123", true, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	var got []string
	for chunk := range chunks {
		got = append(got, string(chunk))
	}
	want := []string{"hello ", "world\n"}
	if len(got) != len(want) {
		t.Fatalf("chunks = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	query := state.snapshot().logsQuery
	if !strings.Contains(query, "follow=1") || !strings.Contains(query, "tail=all") {
		t.Errorf("logs query = %q; want follow=1 and tail=all", query)
	}
}

func TestSplitPortSpec(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		host      string
		container string
		ok        bool
	}{
		{"host and container", "8080:80", "8080", "80", true},
		{"container only", "80", "", "80", true},
		{"empty", "", "", "", false},
		{"non numeric", "abc:def", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, container, ok := splitPortSpec(tt.spec)
			if ok != tt.ok || host != tt.host || container != tt.container {
				t.Errorf("splitPortSpec(%q) = (%q, %q, %v); want (%q, %q, %v)",
					tt.spec, host, container, ok, tt.host, tt.container, tt.ok)
			}
		})
	}
}
