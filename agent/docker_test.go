package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
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
	removed     []string
	createName  string
	createBody  dockerCreateBody
	pullImage   string
	pullAuth    string
	logsQuery   string
	versionHits int
	// createdID is the id the create endpoint returns (default created123).
	createdID string
	// startFail makes the start endpoint answer 500.
	startFail bool
	// removeFail makes the delete endpoint answer 500.
	removeFail bool
	// logsBody, when non-nil, replaces the multiplexed log frames with the
	// exact bytes written (for short/raw/truncated stream tests).
	logsBody []byte
}

func (s *fakeDockerState) snapshot() fakeDockerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fakeDockerState{
		started:     append([]string(nil), s.started...),
		stopped:     append([]string(nil), s.stopped...),
		restarted:   append([]string(nil), s.restarted...),
		removed:     append([]string(nil), s.removed...),
		createName:  s.createName,
		createBody:  s.createBody,
		pullImage:   s.pullImage,
		pullAuth:    s.pullAuth,
		logsQuery:   s.logsQuery,
		versionHits: s.versionHits,
		createdID:   s.createdID,
		startFail:   s.startFail,
		logsBody:    append([]byte(nil), s.logsBody...),
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
		id := state.createdID
		if id == "" {
			id = "created123"
		}
		state.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]string{"Id": id})
	})

	mux.HandleFunc("/images/create", func(w http.ResponseWriter, r *http.Request) {
		image := r.URL.Query().Get("fromImage")
		state.mu.Lock()
		state.pullImage = image
		state.pullAuth = r.Header.Get("X-Registry-Auth")
		state.mu.Unlock()
		if image == "missing:latest" {
			_, _ = w.Write([]byte(`{"error":"manifest unknown"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"Pulling from library/alpine"}` + "\n"))
		_, _ = w.Write([]byte(`{"status":"Download complete"}` + "\n"))
	})

	// Image inspect backing digest resolution after a pull.
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"Id":          "sha256:deadbeef",
			"RepoDigests": []string{inspectRepoDigest + "@" + inspectDigest},
		})
	})

	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/containers/")
		if r.Method == http.MethodDelete {
			state.mu.Lock()
			fail := state.removeFail
			if !fail {
				state.removed = append(state.removed, rest)
			}
			state.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"remove failed"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		id, action, found := strings.Cut(rest, "/")
		if !found {
			http.NotFound(w, r)
			return
		}
		if action == "logs" {
			state.mu.Lock()
			state.logsQuery = r.URL.RawQuery
			body := append([]byte(nil), state.logsBody...)
			state.mu.Unlock()
			if body != nil {
				_, _ = w.Write(body)
				return
			}
			writeLogFrames(w, 1, "hello ")
			writeLogFrames(w, 2, "world\n")
			return
		}
		if id == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such container"}`))
			return
		}
		if action == "start" {
			state.mu.Lock()
			fail := state.startFail
			state.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"driver failed programming external connectivity"}`))
				return
			}
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

	digest, err := client.PullImage(context.Background(), "alpine:latest", "", "")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if got := state.snapshot().pullImage; got != "alpine:latest" {
		t.Errorf("fromImage = %q; want alpine:latest", got)
	}
	if digest == "" {
		t.Error("PullImage digest = empty; want the engine-reported digest")
	}
}

func TestDockerClientPullImageError(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	_, err := client.PullImage(context.Background(), "missing:latest", "", "")
	if err == nil {
		t.Fatal("PullImage(missing) = nil; want error")
	}
	if !strings.Contains(err.Error(), "manifest unknown") {
		t.Errorf("error = %v; want it to mention manifest unknown", err)
	}
}

// inspectRepoDigest/inspectDigest are the RepoDigest the fake engine reports.
const (
	inspectRepoDigest = "docker.io/library/alpine"
	inspectDigest     = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// TestDockerClientPullImageAuth is the credential-scoping guard: a per-pull
// credential is sent as the Docker auth config for the image's own registry
// and the resolved digest is answered; an anonymous pull sends the anonymous
// config instead of the node credential.
func TestDockerClientPullImageAuth(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	digest, err := client.PullImage(context.Background(), "registry.example.com/team/app:1.2", "robot", "s3cret-token")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != inspectDigest {
		t.Errorf("digest = %q; want %q", digest, inspectDigest)
	}
	auth := decodeRegistryAuth(t, state.snapshot().pullAuth)
	if auth["username"] != "robot" || auth["password"] != "s3cret-token" {
		t.Errorf("auth config = %v; want the per-pull credential", auth)
	}
	if auth["serveraddress"] != "registry.example.com" {
		t.Errorf("serveraddress = %q; want the image registry host", auth["serveraddress"])
	}

	if _, err := client.PullImage(context.Background(), "alpine:latest", "", ""); err != nil {
		t.Fatalf("anonymous PullImage: %v", err)
	}
	if got := state.snapshot().pullAuth; got != anonymousRegistryAuth {
		t.Errorf("anonymous pull auth = %q; want the anonymous config", got)
	}
}

// TestDockerClientPullImageErrorRedactsCredential is the redaction guard: a
// failed pull with a credential never echoes it in the error.
func TestDockerClientPullImageErrorRedactsCredential(t *testing.T) {
	server, _ := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	_, err := client.PullImage(context.Background(), "missing:latest", "robot", "s3cret-token")
	if err == nil {
		t.Fatal("PullImage(missing) = nil; want error")
	}
	if strings.Contains(err.Error(), "s3cret-token") || strings.Contains(err.Error(), "robot") {
		t.Errorf("error = %v; must not carry the credential", err)
	}
}

func decodeRegistryAuth(t *testing.T, header string) map[string]string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode auth header: %v", err)
	}
	var auth map[string]string
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatalf("unmarshal auth config: %v", err)
	}
	return auth
}

// createContainerRequestForTest builds a request exercising every mapped field.
func createContainerRequestForTest() *agentv1.CreateContainerRequest {
	return &agentv1.CreateContainerRequest{
		Image:         "nginx:latest",
		Name:          "web",
		Env:           []string{"K=V"},
		Command:       []string{"run"},
		Entrypoint:    []string{"/entry"},
		Labels:        map[string]string{"app": "web"},
		Ports:         []string{"8080:80", "127.0.0.1:9090:90"},
		Volumes:       []string{"/data:/var/lib/data"},
		Networks:      []string{"gotham"},
		RestartPolicy: "unless-stopped",
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
	if _, ok := body.ExposedPorts["90/tcp"]; !ok {
		t.Errorf("ExposedPorts = %v; want 90/tcp", body.ExposedPorts)
	}
	if body.HostConfig == nil {
		t.Fatal("HostConfig = nil; want bind mounts and port bindings")
	}
	if len(body.HostConfig.Binds) != 1 || body.HostConfig.Binds[0] != "/data:/var/lib/data" {
		t.Errorf("Binds = %v; want [/data:/var/lib/data]", body.HostConfig.Binds)
	}
	portBindings := body.HostConfig.PortBindings["80/tcp"]
	if len(portBindings) != 1 || portBindings[0].HostPort != "8080" || portBindings[0].HostIP != "" {
		t.Errorf("PortBindings[80/tcp] = %v; want host port 8080 without a host ip", portBindings)
	}
	loopback := body.HostConfig.PortBindings["90/tcp"]
	if len(loopback) != 1 || loopback[0].HostIP != "127.0.0.1" || loopback[0].HostPort != "9090" {
		t.Errorf("PortBindings[90/tcp] = %v; want 127.0.0.1:9090", loopback)
	}
	if body.HostConfig.RestartPolicy == nil || body.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("RestartPolicy = %v; want unless-stopped", body.HostConfig.RestartPolicy)
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
	for message := range chunks {
		if message.Err != nil {
			t.Fatalf("unexpected stream error: %v", message.Err)
		}
		got = append(got, string(message.Data))
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

// TestDockerClientRunImageCleansUpFailedStart is the A3-3/D1-3 guard: a start
// failure must remove the container it just created, so no orphan is left and
// an immediate retry on the same name succeeds.
func TestDockerClientRunImageCleansUpFailedStart(t *testing.T) {
	server, state := newFakeDockerServer(t)
	state.mu.Lock()
	state.startFail = true
	state.mu.Unlock()
	client := newFakeDockerClient(t, server.URL)

	if _, err := client.RunImage(context.Background(), createContainerRequestForTest()); err == nil {
		t.Fatal("RunImage with failing start = nil error; want failure")
	}
	if removed := state.snapshot().removed; len(removed) != 1 || removed[0] != "created123" {
		t.Fatalf("removed = %v; want [created123] (orphan cleanup)", removed)
	}

	state.mu.Lock()
	state.startFail = false
	state.mu.Unlock()
	id, err := client.RunImage(context.Background(), createContainerRequestForTest())
	if err != nil {
		t.Fatalf("retry RunImage: %v", err)
	}
	if id != "created123" {
		t.Errorf("retry id = %q; want created123", id)
	}
}

// TestDockerClientRunImageCleanupFailureNamesContainer is the U4 guard: when the
// cleanup removal also fails, the error must name the orphan container so it can
// be found and deleted.
func TestDockerClientRunImageCleanupFailureNamesContainer(t *testing.T) {
	server, state := newFakeDockerServer(t)
	state.mu.Lock()
	state.startFail = true
	state.removeFail = true
	state.mu.Unlock()
	client := newFakeDockerClient(t, server.URL)

	if _, err := client.RunImage(context.Background(), createContainerRequestForTest()); err == nil {
		t.Fatal("RunImage with failing start and cleanup = nil; want failure")
	} else if !strings.Contains(err.Error(), "container created123 left behind") {
		t.Errorf("error = %q; want the orphan container id in the cleanup failure", err)
	}
}

// TestDockerClientLogsShortRawStream is the A3-4 guard: a raw stream shorter
// than a multiplexed header must still be delivered, not dropped.
func TestDockerClientLogsShortRawStream(t *testing.T) {
	server, state := newFakeDockerServer(t)
	state.mu.Lock()
	state.logsBody = []byte("hi\n")
	state.mu.Unlock()
	client := newFakeDockerClient(t, server.URL)

	chunks, err := client.Logs(context.Background(), "abc123", false, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	var got []byte
	for message := range chunks {
		if message.Err != nil {
			t.Fatalf("short raw stream error = %v; want clean end", message.Err)
		}
		got = append(got, message.Data...)
	}
	if string(got) != "hi\n" {
		t.Errorf("logs = %q; want %q", got, "hi\n")
	}
}

// TestDockerClientLogsTruncatedFrameReportsError is the A3-5 guard: a stream
// that ends inside a frame is a failure, not a clean success.
func TestDockerClientLogsTruncatedFrameReportsError(t *testing.T) {
	server, state := newFakeDockerServer(t)
	header := make([]byte, 8)
	header[0] = 1
	binary.BigEndian.PutUint32(header[4:], 32)
	state.mu.Lock()
	state.logsBody = append(header, []byte("abc")...)
	state.mu.Unlock()
	client := newFakeDockerClient(t, server.URL)

	chunks, err := client.Logs(context.Background(), "abc123", false, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	var got []byte
	var streamErr error
	for message := range chunks {
		got = append(got, message.Data...)
		if message.Err != nil {
			streamErr = message.Err
		}
	}
	if string(got) != "abc" {
		t.Errorf("partial payload = %q; want abc", got)
	}
	if streamErr == nil {
		t.Fatal("truncated frame reported as a clean end; want a terminal error")
	}
}

// TestDockerClientHonorsContext proves the Docker client cancels an in-flight
// request when its context expires, so a hung daemon cannot block past the
// caller's deadline.
func TestDockerClientHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.Version(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Version with expired ctx = %v; want DeadlineExceeded", err)
	}
}

// TestDecodeLogStreamFollowsShortRawLine is the U1 guard: on a following raw
// (TTY) stream that writes fewer than eight bytes and stays open, the bytes must
// be delivered without waiting for a full header.
func TestDecodeLogStreamFollowsShortRawLine(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan LogMessage)
	go decodeLogStream(ctx, reader, out)

	if _, err := writer.Write([]byte("ok\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case message := <-out:
		if message.Err != nil {
			t.Fatalf("stream error = %v; want clean data", message.Err)
		}
		if string(message.Data) != "ok\n" {
			t.Errorf("data = %q; want %q", message.Data, "ok\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("short raw line withheld while the stream is still open")
	}
}

// TestDockerClientNotFoundTaxonomy is the U2/U3 guard: a Docker 404 is
// classified by scope — a missing container is ErrDockerNotFound, a missing
// image or repository is ErrDockerImageNotFound.
func TestDockerClientNotFoundTaxonomy(t *testing.T) {
	mux := http.NewServeMux()
	notFound := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/containers/abc/start", notFound(`{"message":"No such container: abc"}`))
	mux.HandleFunc("/containers/create", notFound(`{"message":"No such image: nope:latest"}`))
	mux.HandleFunc("/images/create", notFound(`{"message":"pull access denied for nope, repository does not exist"}`))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)
	ctx := context.Background()

	if err := client.Start(ctx, "abc"); !errors.Is(err, ErrDockerNotFound) {
		t.Errorf("container 404 = %v; want ErrDockerNotFound", err)
	}
	if _, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{Image: "nope:latest"}); !errors.Is(err, ErrDockerImageNotFound) {
		t.Errorf("create with a missing image = %v; want ErrDockerImageNotFound", err)
	}
	if _, err := client.PullImage(ctx, "nope", "", ""); !errors.Is(err, ErrDockerImageNotFound) {
		t.Errorf("pull of a missing repository = %v; want ErrDockerImageNotFound", err)
	}
}

// TestDockerClientDaemonDown is the U3 guard for the daemon-down mapping: a
// closed listener is ErrDockerUnavailable, not Internal.
func TestDockerClientDaemonDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	client := newFakeDockerClient(t, url)

	if _, err := client.Version(context.Background()); !errors.Is(err, ErrDockerUnavailable) {
		t.Fatalf("closed daemon = %v; want ErrDockerUnavailable", err)
	}
}

// TestNotFoundErrorClassification is the U2/U3 guard: the image sentinel is
// scoped to the pull and create paths, the create match is image-specific, and
// the build helpers keep the container classification.
func TestNotFoundErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		message string
		want    error
	}{
		{"container", "/containers/abc/start", "No such container: abc", ErrDockerNotFound},
		{"create missing image", "/containers/create", "No such image: nope:latest", ErrDockerImageNotFound},
		{"create with name", "/containers/create?name=web", "No such image: nope:latest", ErrDockerImageNotFound},
		{"create missing network", "/containers/create", "network gotham-net not found", ErrDockerNotFound},
		{"pull missing repository", "/images/create", "pull access denied for nope", ErrDockerImageNotFound},
		{"tag build helper", "/images/nope/tag", "No such image: nope:latest", ErrDockerNotFound},
		{"push build helper", "/images/nope/push", "manifest unknown", ErrDockerNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notFoundError("POST", tt.path, tt.message)
			if !errors.Is(err, tt.want) {
				t.Fatalf("notFoundError(%q) = %v; want %v", tt.path, err, tt.want)
			}
		})
	}
}

// TestDecodeLogStreamTruncatedAfterValidFrame is the U4 guard: a partial header
// after at least one valid multiplexed frame is a truncated stream, not raw
// data with a clean end.
func TestDecodeLogStreamTruncatedAfterValidFrame(t *testing.T) {
	// A valid frame (stream 1, size 1, payload "a") followed by a partial
	// header [1,0,0] and EOF.
	source := bytes.NewReader([]byte{1, 0, 0, 0, 0, 0, 0, 1, 'a', 1, 0, 0})
	out := make(chan LogMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(out)
		decodeLogStream(ctx, source, out)
	}()

	var got []byte
	var streamErr error
	for message := range out {
		got = append(got, message.Data...)
		if message.Err != nil {
			streamErr = message.Err
		}
	}
	if string(got) != "a" {
		t.Errorf("data = %q; want %q", got, "a")
	}
	if streamErr == nil {
		t.Fatal("truncated header after a valid frame reported as a clean end")
	}
}

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		hostIP    string
		host      string
		container string
		wantErr   bool
	}{
		{name: "host and container", spec: "8080:80", host: "8080", container: "80"},
		{name: "host ip, host and container", spec: "127.0.0.1:8080:8080", hostIP: "127.0.0.1", host: "8080", container: "8080"},
		{name: "container only", spec: "80", container: "80"},
		{name: "ephemeral host port", spec: "0:80", host: "0", container: "80"},
		{name: "max ports", spec: "65535:65535", host: "65535", container: "65535"},
		{name: "empty", spec: "", wantErr: true},
		{name: "non numeric", spec: "abc:def", wantErr: true},
		{name: "non numeric host", spec: "http:80", wantErr: true},
		{name: "invalid host ip", spec: "not-an-ip:8080:80", wantErr: true},
		{name: "bracketed ipv4", spec: "[127.0.0.1]:8080:80", wantErr: true},
		{name: "non canonical ipv4", spec: "127.000.000.001:8080:80", wantErr: true},
		{name: "ipv6 unsupported", spec: "::1:8080:80", wantErr: true},
		{name: "zero container port", spec: "8080:0", wantErr: true},
		{name: "container port too high", spec: "8080:65536", wantErr: true},
		{name: "host port too high", spec: "65536:80", wantErr: true},
		{name: "empty host ip", spec: ":8080:80", wantErr: true},
		{name: "empty host port", spec: "127.0.0.1::80", wantErr: true},
		{name: "too many parts", spec: "127.0.0.1:8080:80:1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostIP, host, container, err := parsePortSpec(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePortSpec(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidPortMapping) {
				t.Fatalf("parsePortSpec(%q) error = %v, want ErrInvalidPortMapping", tt.spec, err)
			}
			if err == nil && (hostIP != tt.hostIP || host != tt.host || container != tt.container) {
				t.Errorf("parsePortSpec(%q) = (%q, %q, %q); want (%q, %q, %q)",
					tt.spec, hostIP, host, container, tt.hostIP, tt.host, tt.container)
			}
		})
	}
}
