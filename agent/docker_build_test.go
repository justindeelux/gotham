package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// newTestDockerClient returns a DockerClient bound to server.
func newTestDockerClient(t *testing.T, handler http.Handler) *DockerClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewDockerClient(server.URL)
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	return client
}

// writeJSONStream writes Docker-style JSON messages followed by a newline.
func writeJSONStream(t *testing.T, w http.ResponseWriter, messages ...any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	for _, message := range messages {
		if err := encoder.Encode(message); err != nil {
			t.Errorf("encode stream message: %v", err)
		}
	}
}

func TestDockerClientBuild(t *testing.T) {
	var (
		mu           sync.Mutex
		gotQuery     url.Values
		gotContent   string
		gotBody      []byte
		requestCount int
	)

	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build" {
			t.Errorf("path = %q, want /build", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		mu.Lock()
		gotQuery = r.URL.Query()
		gotContent = r.Header.Get("Content-Type")
		gotBody = body
		requestCount++
		mu.Unlock()

		writeJSONStream(t, w,
			map[string]string{"stream": "Step 1/2 : FROM scratch\n"},
			map[string]string{"stream": "Step 2/2 : COPY hello /hello\n"},
			map[string]string{"stream": "Successfully built sha256:abc\n"},
		)
	}))

	var logs strings.Builder
	err := client.Build(context.Background(), BuildOptions{
		Tag:        "127.0.0.1:5000/gotham/web:dep-1",
		BuildArgs:  map[string]string{"NODE_ENV": "production"},
		Context:    strings.NewReader("tar-bytes"),
		Dockerfile: "deploy/Dockerfile",
	}, func(data []byte) error {
		logs.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
	if gotQuery.Get("t") != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Errorf("t = %q", gotQuery.Get("t"))
	}
	if gotQuery.Get("dockerfile") != "deploy/Dockerfile" {
		t.Errorf("dockerfile = %q", gotQuery.Get("dockerfile"))
	}
	if gotQuery.Get("rm") != "1" {
		t.Errorf("rm = %q", gotQuery.Get("rm"))
	}
	if gotQuery.Get("buildargs") != `{"NODE_ENV":"production"}` {
		t.Errorf("buildargs = %q", gotQuery.Get("buildargs"))
	}
	if gotContent != "application/x-tar" {
		t.Errorf("content-type = %q", gotContent)
	}
	if string(gotBody) != "tar-bytes" {
		t.Errorf("body = %q", gotBody)
	}

	want := "Step 1/2 : FROM scratch\nStep 2/2 : COPY hello /hello\nSuccessfully built sha256:abc\n"
	if logs.String() != want {
		t.Errorf("logs = %q, want %q", logs.String(), want)
	}
}

func TestDockerClientBuildDefaultsDockerfile(t *testing.T) {
	var gotDockerfile string
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDockerfile = r.URL.Query().Get("dockerfile")
		writeJSONStream(t, w, map[string]string{"stream": "ok\n"})
	}))

	if err := client.Build(context.Background(), BuildOptions{
		Tag:     "gotham/web:dep-1",
		Context: strings.NewReader("tar"),
	}, func([]byte) error { return nil }); err != nil {
		t.Fatalf("build: %v", err)
	}
	if gotDockerfile != "Dockerfile" {
		t.Fatalf("dockerfile = %q, want Dockerfile", gotDockerfile)
	}
}

func TestDockerClientBuildReportsDaemonError(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"cannot connect to daemon"}`))
	}))

	err := client.Build(context.Background(), BuildOptions{
		Tag:     "gotham/web:dep-1",
		Context: strings.NewReader("tar"),
	}, func([]byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err = %v, want status 500", err)
	}
}

func TestDockerClientBuildReportsStreamError(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStream(t, w, map[string]string{"stream": "Step 1/1 : FROM nowhere:latest\n"},
			map[string]string{"error": "failed to solve: manifest unknown"})
	}))

	err := client.Build(context.Background(), BuildOptions{
		Tag:     "gotham/web:dep-1",
		Context: strings.NewReader("tar"),
	}, func([]byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "failed to solve") {
		t.Fatalf("err = %v, want stream error", err)
	}
}

func TestDockerClientBuildAbortsWithEmitError(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStream(t, w, map[string]string{"stream": "Step 1/1 : FROM scratch\n"})
	}))

	emitErr := errors.New("stream closed")
	err := client.Build(context.Background(), BuildOptions{
		Tag:     "gotham/web:dep-1",
		Context: strings.NewReader("tar"),
	}, func([]byte) error { return emitErr })
	if !errors.Is(err, emitErr) {
		t.Fatalf("err = %v, want %v", err, emitErr)
	}
}

func TestDockerClientBuildRequiresTagAndContext(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request expected")
	}))

	if err := client.Build(context.Background(), BuildOptions{Context: strings.NewReader("tar")},
		func([]byte) error { return nil }); err == nil {
		t.Error("missing tag: want error")
	}
	if err := client.Build(context.Background(), BuildOptions{Tag: "gotham/web:dep-1"},
		func([]byte) error { return nil }); err == nil {
		t.Error("missing context: want error")
	}
}

func TestDockerClientPushImage(t *testing.T) {
	var (
		gotPath    string
		gotTag     string
		gotAuthHdr string
	)
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTag = r.URL.Query().Get("tag")
		gotAuthHdr = r.Header.Get(registryAuthHeader)
		writeJSONStream(t, w,
			map[string]string{"status": "The push refers to repository [127.0.0.1:5000/gotham/web]"},
			map[string]string{"status": "dep-1: digest: sha256:abc size: 512"},
		)
	}))

	var logs strings.Builder
	err := client.PushImage(context.Background(), "127.0.0.1:5000/gotham/web", "dep-1",
		func(data []byte) error {
			logs.Write(data)
			return nil
		})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotPath != "/images/127.0.0.1:5000/gotham/web/push" {
		t.Errorf("path = %q", gotPath)
	}
	if gotTag != "dep-1" {
		t.Errorf("tag = %q", gotTag)
	}
	// Docker 28+ rejects a push with no X-Registry-Auth header at all
	// (moby/moby#50614); the agent must always send the anonymous config.
	if gotAuthHdr != anonymousRegistryAuth {
		t.Errorf("%s = %q, want the anonymous auth config %q", registryAuthHeader, gotAuthHdr, anonymousRegistryAuth)
	}
	want := "The push refers to repository [127.0.0.1:5000/gotham/web]\ndep-1: digest: sha256:abc size: 512\n"
	if logs.String() != want {
		t.Errorf("logs = %q, want %q", logs.String(), want)
	}
}

func TestDockerClientPushImageReportsError(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStream(t, w, map[string]string{"error": "denied: requested access to the resource is denied"})
	}))

	err := client.PushImage(context.Background(), "127.0.0.1:5000/gotham/web", "dep-1",
		func([]byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err = %v, want push error", err)
	}
}

func TestDockerClientImageDigest(t *testing.T) {
	var gotPath string
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSONStream(t, w, map[string]any{
			"Id": "sha256:config",
			"RepoDigests": []string{
				"gotham/web@sha256:other",
				"127.0.0.1:5000/gotham/web@sha256:feedface",
			},
		})
	}))

	digest, err := client.ImageDigest(context.Background(), "127.0.0.1:5000/gotham/web:dep-1")
	if err != nil {
		t.Fatalf("image digest: %v", err)
	}
	if digest != "sha256:feedface" {
		t.Errorf("digest = %q", digest)
	}
	if gotPath != "/images/127.0.0.1:5000/gotham/web:dep-1/json" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDockerClientImageDigestMissing(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStream(t, w, map[string]any{"Id": "sha256:config", "RepoDigests": []string{}})
	}))

	if _, err := client.ImageDigest(context.Background(), "gotham/web:dep-1"); err == nil {
		t.Fatal("want error when no digest is recorded")
	}
}

// registryTestEngine is a fake Docker engine that supports registry
// bootstrap: inspect, pull, create, start and remove of the registry
// container. hostPort is the configured host port ("" for an auto-assigned
// one), publishedPort is what the daemon reports once running.
type registryTestEngine struct {
	mu            sync.Mutex
	exists        bool
	running       bool
	managed       bool
	hostPort      string
	publishedPort string
	created       dockerCreateBody
	pulls         int
	starts        int
	creates       int
	removals      int
	startFails    bool
}

// inspectPayload renders a GET /containers/{name}/json response.
func (e *registryTestEngine) inspectPayload() map[string]any {
	startedAt := "0000-00-00T00:00:00Z"
	if e.running {
		startedAt = "2026-01-01T00:00:00.000000000Z"
	}
	labels := map[string]string{}
	if e.managed {
		labels = map[string]string{"gotham.managed": "true", "gotham.role": "registry"}
	}
	ports := map[string]any{}
	if e.running && e.publishedPort != "" {
		ports[registryPort+"/tcp"] = []map[string]string{
			{"HostIp": registryHostIP, "HostPort": e.publishedPort},
		}
	}
	return map[string]any{
		"State":  map[string]any{"Running": e.running, "StartedAt": startedAt},
		"Config": map[string]any{"Labels": labels},
		"HostConfig": map[string]any{
			"PortBindings": map[string]any{
				registryPort + "/tcp": []map[string]string{
					{"HostIp": registryHostIP, "HostPort": e.hostPort},
				},
			},
		},
		"NetworkSettings": map[string]any{"Ports": ports},
	}
}

func (e *registryTestEngine) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/"+registryContainerName+"/json":
			if !e.exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"No such container"}`))
				return
			}
			writeJSONStream(t, w, e.inspectPayload())

		case r.Method == http.MethodDelete && r.URL.Path == "/containers/"+registryContainerName:
			if !e.exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			e.removals++
			e.exists, e.running, e.managed = false, false, false
			e.hostPort, e.publishedPort = "", ""
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			e.pulls++
			if got := r.URL.Query().Get("fromImage"); got != registryImage {
				t.Errorf("fromImage = %q, want %q", got, registryImage)
			}
			writeJSONStream(t, w, map[string]string{"status": "Pulling from library/" + registryImage})

		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			e.creates++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read create body: %v", err)
			}
			if err := json.Unmarshal(body, &e.created); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if e.exists {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"Conflict"}`))
				return
			}
			e.exists = true
			e.managed = true
			e.hostPort = ""
			if bindings := e.created.HostConfig.PortBindings[registryPort+"/tcp"]; len(bindings) == 1 {
				e.hostPort = bindings[0].HostPort
			}
			writeJSONStream(t, w, map[string]string{"Id": "registry-1"})

		case r.Method == http.MethodPost && r.URL.Path == "/containers/"+registryContainerName+"/start":
			if e.startFails {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"port is already allocated"}`))
				return
			}
			e.starts++
			e.running = true
			e.publishedPort = e.hostPort
			if e.publishedPort == "" {
				e.publishedPort = "32768"
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

// registryPortInRange reports whether port is inside the probed range.
func registryPortInRange(t *testing.T, port string) int {
	t.Helper()
	value, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("port %q is not numeric: %v", port, err)
	}
	if value < registryPreferredPort || value >= registryPreferredPort+registryPortRange {
		t.Fatalf("port %d outside probed range %d-%d", value, registryPreferredPort, registryPreferredPort+registryPortRange-1)
	}
	return value
}

func TestDockerClientEnsureRegistryBootstraps(t *testing.T) {
	engine := &registryTestEngine{}
	client := newTestDockerClient(t, engine.handler(t))

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	registryPortInRange(t, engine.hostPort)
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q, want 127.0.0.1:%s", address, engine.hostPort)
	}
	if engine.pulls != 1 || engine.creates != 1 || engine.starts != 1 {
		t.Fatalf("pulls = %d, creates = %d, starts = %d; want 1 each", engine.pulls, engine.creates, engine.starts)
	}

	if engine.created.Image != registryImage {
		t.Errorf("image = %q, want %q", engine.created.Image, registryImage)
	}
	if got := engine.created.Labels["gotham.managed"]; got != "true" {
		t.Errorf("gotham.managed label = %q", got)
	}
	if len(engine.created.HostConfig.Binds) != 1 || engine.created.HostConfig.Binds[0] != registryVolumeName+":/var/lib/registry" {
		t.Errorf("binds = %v", engine.created.HostConfig.Binds)
	}
	bindings := engine.created.HostConfig.PortBindings[registryPort+"/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != registryHostIP || bindings[0].HostPort == "" {
		t.Errorf("port bindings = %+v, want an explicit loopback host port", bindings)
	}
	if engine.created.HostConfig.RestartPolicy == nil || engine.created.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("restart policy = %+v", engine.created.HostConfig.RestartPolicy)
	}
}

func TestDockerClientEnsureRegistryReusesRunningContainer(t *testing.T) {
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "5001",
	}
	client := newTestDockerClient(t, engine.handler(t))

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	if address != "127.0.0.1:5001" {
		t.Fatalf("address = %q", address)
	}
	if engine.pulls != 0 || engine.creates != 0 || engine.starts != 0 || engine.removals != 0 {
		t.Fatalf("pulls = %d, creates = %d, starts = %d, removals = %d; want 0",
			engine.pulls, engine.creates, engine.starts, engine.removals)
	}
}

func TestDockerClientEnsureRegistryRestartsStoppedContainer(t *testing.T) {
	engine := &registryTestEngine{exists: true, running: false, managed: true, hostPort: "5001"}
	client := newTestDockerClient(t, engine.handler(t))

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	if address != "127.0.0.1:5001" {
		t.Fatalf("address = %q", address)
	}
	if engine.starts != 1 {
		t.Fatalf("starts = %d, want 1", engine.starts)
	}
	if engine.pulls != 0 || engine.creates != 0 || engine.removals != 0 {
		t.Fatalf("pulls = %d, creates = %d, removals = %d; want 0", engine.pulls, engine.creates, engine.removals)
	}
}

func TestDockerClientEnsureRegistryRepairsAutoAssignedPort(t *testing.T) {
	// A container created with an auto-assigned host port is unreachable from
	// some daemons, so it must be replaced by one on an explicit probed port.
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "", publishedPort: "32768",
	}
	client := newTestDockerClient(t, engine.handler(t))

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	registryPortInRange(t, engine.hostPort)
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q, want 127.0.0.1:%s", address, engine.hostPort)
	}
	if engine.removals != 1 || engine.pulls != 1 || engine.creates != 1 || engine.starts != 1 {
		t.Fatalf("removals = %d, pulls = %d, creates = %d, starts = %d; want 1 each",
			engine.removals, engine.pulls, engine.creates, engine.starts)
	}
}

func TestDockerClientEnsureRegistryRejectsUnmanagedContainer(t *testing.T) {
	engine := &registryTestEngine{
		exists: true, running: true, managed: false,
		hostPort: "5001", publishedPort: "5001",
	}
	client := newTestDockerClient(t, engine.handler(t))

	_, err := client.EnsureRegistry(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not managed by gotham") {
		t.Fatalf("err = %v, want unmanaged container error", err)
	}
	if engine.removals != 0 || engine.creates != 0 {
		t.Fatalf("removals = %d, creates = %d; want 0", engine.removals, engine.creates)
	}
}

func TestDockerClientEnsureRegistryDropsContainerThatNeverStarted(t *testing.T) {
	engine := &registryTestEngine{
		exists: true, running: false, managed: true,
		hostPort: "5001", startFails: true,
	}
	client := newTestDockerClient(t, engine.handler(t))

	if _, err := client.EnsureRegistry(context.Background()); err == nil {
		t.Fatal("first call: want start error")
	}
	if engine.removals != 1 {
		t.Fatalf("removals = %d, want 1", engine.removals)
	}

	// The next call bootstraps a fresh registry.
	engine.startFails = false
	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	registryPortInRange(t, engine.hostPort)
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q, want 127.0.0.1:%s", address, engine.hostPort)
	}
}

func TestDockerClientEnsureRegistryRequiresPublishedPort(t *testing.T) {
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "",
	}
	client := newTestDockerClient(t, engine.handler(t))

	_, err := client.EnsureRegistry(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no published port") {
		t.Fatalf("err = %v, want missing port error", err)
	}
}

func TestStripImageTag(t *testing.T) {
	for _, testCase := range []struct {
		ref  string
		want string
	}{
		{"127.0.0.1:5000/gotham/web:dep-1", "127.0.0.1:5000/gotham/web"},
		{"gotham/web:dep-1", "gotham/web"},
		{"gotham/web", "gotham/web"},
		{"localhost:5000/web", "localhost:5000/web"},
	} {
		if got := stripImageTag(testCase.ref); got != testCase.want {
			t.Errorf("stripImageTag(%q) = %q, want %q", testCase.ref, got, testCase.want)
		}
	}
}
