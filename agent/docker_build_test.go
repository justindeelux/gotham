package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// newTestDockerClient returns a DockerClient bound to server with a fresh
// registry state directory.
func newTestDockerClient(t *testing.T, handler http.Handler) *DockerClient {
	t.Helper()
	return newTestDockerClientWithStateDir(t, handler, t.TempDir())
}

// newTestDockerClientWithStateDir returns a DockerClient bound to server using
// stateDir for registry credentials, so a test can pre-seed and inspect it.
func newTestDockerClientWithStateDir(t *testing.T, handler http.Handler, stateDir string) *DockerClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewDockerClient(server.URL, WithRegistryStateDir(stateDir))
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	return client
}

// seedRegistryState pre-creates the registry credential in a fresh state dir so
// a pre-existing container is treated as reusable, and returns the dir and the
// htpasswd mount source the container must report.
func seedRegistryState(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	_, htpasswdPath, _, err := prepareRegistryAuth(dir)
	if err != nil {
		t.Fatalf("seed registry state: %v", err)
	}
	return dir, htpasswdPath
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
	networkExists bool
	networkCreate int
	networkMode   string
	// htpasswdSource is the bind source the running container reports for the
	// htpasswd mount. Empty means no mount is reported (a container that must
	// be recreated).
	htpasswdSource string
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
	networkMode := e.networkMode
	if networkMode == "" {
		networkMode = registryNetworkName
	}
	ports := map[string]any{}
	if e.running && e.publishedPort != "" {
		ports[registryPort+"/tcp"] = []map[string]string{
			{"HostIp": registryHostIP, "HostPort": e.publishedPort},
		}
	}
	var mounts []map[string]string
	if e.htpasswdSource != "" {
		mounts = append(mounts, map[string]string{
			"Source":      e.htpasswdSource,
			"Destination": registryHtpasswdPath,
		})
	}
	return map[string]any{
		"State":  map[string]any{"Running": e.running, "StartedAt": startedAt},
		"Config": map[string]any{"Labels": labels},
		"HostConfig": map[string]any{
			"NetworkMode": networkMode,
			"PortBindings": map[string]any{
				registryPort + "/tcp": []map[string]string{
					{"HostIp": registryHostIP, "HostPort": e.hostPort},
				},
			},
		},
		"NetworkSettings": map[string]any{
			"Ports":    ports,
			"Networks": map[string]any{networkMode: map[string]any{}},
		},
		"Mounts": mounts,
	}
}

func (e *registryTestEngine) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+registryNetworkName:
			if !e.networkExists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"network not found"}`))
				return
			}
			writeJSONStream(t, w, map[string]any{
				"Name":   registryNetworkName,
				"Labels": map[string]string{"gotham.managed": "true"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/networks/create":
			e.networkCreate++
			e.networkExists = true
			writeJSONStream(t, w, map[string]any{"Id": "network-1"})

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
			e.htpasswdSource = ""
			for _, bind := range e.created.HostConfig.Binds {
				if strings.HasSuffix(bind, ":"+registryHtpasswdPath+":ro") {
					e.htpasswdSource = strings.TrimSuffix(bind, ":"+registryHtpasswdPath+":ro")
				}
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
	binds := engine.created.HostConfig.Binds
	var hasVolume, hasHtpasswd bool
	for _, bind := range binds {
		switch {
		case strings.HasPrefix(bind, registryVolumeName+":/var/lib/registry"):
			hasVolume = true
		case strings.HasSuffix(bind, ":"+registryHtpasswdPath+":ro"):
			hasHtpasswd = true
		}
	}
	if !hasVolume {
		t.Errorf("binds = %v; want the registry data volume", binds)
	}
	if !hasHtpasswd {
		t.Errorf("binds = %v; want the htpasswd file mounted read-only", binds)
	}
	if engine.created.HostConfig.NetworkMode != registryNetworkName {
		t.Errorf("network mode = %q; want %q", engine.created.HostConfig.NetworkMode, registryNetworkName)
	}
	if !containsString(engine.created.Env, "REGISTRY_AUTH=htpasswd") {
		t.Errorf("env = %v; want REGISTRY_AUTH=htpasswd", engine.created.Env)
	}
	if !containsString(engine.created.Env, "REGISTRY_AUTH_HTPASSWD_PATH="+registryHtpasswdPath) {
		t.Errorf("env = %v; want the htpasswd path", engine.created.Env)
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
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "5001", htpasswdSource: htpasswdPath,
	}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

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

// TestDockerClientEnsureRegistryRecreatesOnStaleCredentialMount isolates the
// mount-source guard: the credential is seeded and matches (reused=true), but
// the running container mounts a different htpasswd path, so only
// registryMountsPath can trigger recreation.
func TestDockerClientEnsureRegistryRecreatesOnStaleCredentialMount(t *testing.T) {
	stateDir, _ := seedRegistryState(t)
	staleSource := filepath.Join(t.TempDir(), registryHtpasswdFilename)
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "5001",
		htpasswdSource: staleSource,
	}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q", address)
	}
	if engine.removals != 1 || engine.pulls != 1 || engine.creates != 1 || engine.starts != 1 {
		t.Fatalf("removals = %d, pulls = %d, creates = %d, starts = %d; want 1 each",
			engine.removals, engine.pulls, engine.creates, engine.starts)
	}
	if engine.htpasswdSource == staleSource {
		t.Errorf("recreated container still mounts the stale source %q", staleSource)
	}
}

// TestDockerClientEnsureRegistryRecreatesOnRewrittenCredential isolates the
// !reused guard: the credential is seeded and the mount path still matches, but
// the htpasswd file is deleted, so prepareRegistryAuth must rewrite it (a new
// inode) and the container must be recreated to pick it up.
func TestDockerClientEnsureRegistryRecreatesOnRewrittenCredential(t *testing.T) {
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "5001", htpasswdSource: htpasswdPath,
	}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

	if err := os.Remove(htpasswdPath); err != nil {
		t.Fatalf("remove htpasswd: %v", err)
	}

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q", address)
	}
	if engine.removals != 1 || engine.pulls != 1 || engine.creates != 1 || engine.starts != 1 {
		t.Fatalf("removals = %d, pulls = %d, creates = %d, starts = %d; want 1 each",
			engine.removals, engine.pulls, engine.creates, engine.starts)
	}
}

func TestDockerClientEnsureRegistryRestartsStoppedContainer(t *testing.T) {
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{exists: true, running: false, managed: true, hostPort: "5001", htpasswdSource: htpasswdPath}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

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

// TestDockerClientEnsureRegistryIsolatesLegacyContainer checks a registry left
// on the default bridge by an older agent is recreated on the dedicated
// network, so workloads can no longer reach it by container IP.
func TestDockerClientEnsureRegistryIsolatesLegacyContainer(t *testing.T) {
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "5001", networkMode: "bridge",
	}
	client := newTestDockerClient(t, engine.handler(t))

	address, err := client.EnsureRegistry(context.Background())
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	if address != "127.0.0.1:"+engine.hostPort {
		t.Fatalf("address = %q", address)
	}
	if engine.removals != 1 || engine.pulls != 1 || engine.creates != 1 || engine.starts != 1 {
		t.Fatalf("removals = %d, pulls = %d, creates = %d, starts = %d; want 1 each",
			engine.removals, engine.pulls, engine.creates, engine.starts)
	}
	if engine.created.HostConfig.NetworkMode != registryNetworkName {
		t.Errorf("recreated network mode = %q; want %q", engine.created.HostConfig.NetworkMode, registryNetworkName)
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
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{
		exists: true, running: false, managed: true,
		hostPort: "5001", startFails: true, htpasswdSource: htpasswdPath,
	}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

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
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{
		exists: true, running: true, managed: true,
		hostPort: "5001", publishedPort: "", htpasswdSource: htpasswdPath,
	}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

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

// TestRegistryAuthFilesArePrivate checks the credential is persisted 0600 and
// the htpasswd only carries a bcrypt hash, never the plaintext password.
func TestRegistryAuthFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	auth, htpasswdPath, reused, err := prepareRegistryAuth(dir)
	if err != nil {
		t.Fatalf("prepare registry auth: %v", err)
	}
	if reused {
		t.Error("first call reported the credential as reused")
	}
	if auth.Username != registryAuthUser || len(auth.Password) < 16 {
		t.Fatalf("auth = %+v", auth)
	}

	for _, path := range []string{filepath.Join(dir, registryCredentialFilename), htpasswdPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		// Literal 0600: comparing against the constant would keep the test
		// green if the constant changed.
		if got := info.Mode().Perm(); got != os.FileMode(0o600) {
			t.Errorf("%s mode = %#o, want %#o", path, got, os.FileMode(0o600))
		}
	}

	htpasswd, err := os.ReadFile(htpasswdPath)
	if err != nil {
		t.Fatalf("read htpasswd: %v", err)
	}
	if strings.Contains(string(htpasswd), auth.Password) {
		t.Error("htpasswd contains the plaintext password")
	}
	if !strings.HasPrefix(string(htpasswd), registryAuthUser+":$2") {
		t.Errorf("htpasswd = %q; want a gotham bcrypt entry", string(htpasswd))
	}

	// A matching credential must not be rewritten (the bind mount's inode and
	// bytes stay put), and must stay stable across reloads. Compare the bytes:
	// an in-place rewrite keeps the inode, so only a fresh bcrypt salt in the
	// content reveals one.
	before, err := os.ReadFile(htpasswdPath)
	if err != nil {
		t.Fatalf("read htpasswd before reload: %v", err)
	}
	again, againPath, reusedAgain, err := prepareRegistryAuth(dir)
	if err != nil {
		t.Fatalf("reload registry auth: %v", err)
	}
	if again.Password != auth.Password || again.Username != auth.Username {
		t.Error("reload changed the credential")
	}
	if againPath != htpasswdPath || !reusedAgain {
		t.Errorf("reload path/reused = %q/%v; want %q/true", againPath, reusedAgain, htpasswdPath)
	}
	after, err := os.ReadFile(htpasswdPath)
	if err != nil {
		t.Fatalf("read htpasswd after reload: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a matching credential was rewritten: the htpasswd bytes changed")
	}
}

// TestPrepareRegistryAuthRejectsRelativeStateDirStillResolves checks a relative
// state dir is absolutised, so the htpasswd bind source is not treated as a
// named volume by Docker.
func TestPrepareRegistryAuthRejectsRelativeStateDirStillResolves(t *testing.T) {
	root := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, htpasswdPath, _, err := prepareRegistryAuth("./data/agent")
	if err != nil {
		t.Fatalf("prepare relative state dir: %v", err)
	}
	if !filepath.IsAbs(htpasswdPath) {
		t.Fatalf("htpasswd path = %q; want absolute", htpasswdPath)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	if !strings.HasPrefix(htpasswdPath, cwd) {
		t.Errorf("htpasswd path = %q; want it under %q", htpasswdPath, cwd)
	}
}

// TestRegistryAuthHeaderScope checks the node credential is only sent to the
// node registry, never to another registry (for example the registry:2
// bootstrap pull from Docker Hub).
func TestRegistryAuthHeaderScope(t *testing.T) {
	client, err := NewDockerClient("http://unused", WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	client.registryAuth = registryAuth{Address: "127.0.0.1:5000", Username: "gotham", Password: "s3cret"}

	header, err := client.registryAuthHeader("127.0.0.1:5000/gotham/web:dep-1")
	if err != nil {
		t.Fatalf("registryAuthHeader: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if payload["username"] != "gotham" || payload["password"] != "s3cret" || payload["serveraddress"] != "127.0.0.1:5000" {
		t.Errorf("header payload = %v", payload)
	}

	for _, image := range []string{"registry:2", "docker.io/library/nginx:latest", "nginx:alpine"} {
		got, err := client.registryAuthHeader(image)
		if err != nil {
			t.Fatalf("registryAuthHeader(%q): %v", image, err)
		}
		if got != anonymousRegistryAuth {
			t.Errorf("registryAuthHeader(%q) = %q; want the anonymous config", image, got)
		}
	}
}

// TestDockerClientPushImageUsesCredential checks a push carries the node
// credential in X-Registry-Auth, not the anonymous config.
func TestDockerClientPushImageUsesCredential(t *testing.T) {
	var gotAuth string
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(registryAuthHeader)
		writeJSONStream(t, w, map[string]string{"status": "pushed\n"})
	}))
	client.registryAuth = registryAuth{Address: "127.0.0.1:5000", Username: "gotham", Password: "s3cret"}

	if err := client.PushImage(context.Background(), "127.0.0.1:5000/gotham/web", "dep-1", func([]byte) error { return nil }); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth == anonymousRegistryAuth || gotAuth == "" {
		t.Fatalf("push auth header = %q; want the node credential", gotAuth)
	}
}

// TestEnsureRegistryRequiresStateDir checks the registry bootstrap fails closed
// rather than serving an unauthenticated registry when no state dir is set.
func TestEnsureRegistryRequiresStateDir(t *testing.T) {
	client := newTestDockerClientWithoutStateDir(t)
	if _, err := client.EnsureRegistry(context.Background()); err == nil || !strings.Contains(err.Error(), "state dir") {
		t.Fatalf("err = %v, want a state-dir error", err)
	}
}

// TestEnsureRegistryCredentialRequiresLiveRegistry checks the lazy credential
// load only attaches the credential when a live, gotham-managed registry
// actually publishes the address named by the image, so a stale reference or a
// same-named container cannot be handed the node credential.
func TestEnsureRegistryCredentialRequiresLiveRegistry(t *testing.T) {
	image := "127.0.0.1:5000/gotham/web:dep"

	newClient := func(managed bool, port string) *DockerClient {
		stateDir, _ := seedRegistryState(t)
		engine := &registryTestEngine{
			exists: true, running: true, managed: managed,
			hostPort: port, publishedPort: port,
		}
		return newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)
	}

	// No container: no credential.
	absent := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	if err := absent.ensureRegistryCredentialFor(context.Background(), image); err != nil {
		t.Fatalf("absent registry: %v", err)
	}
	if absent.registryAuth.Username != "" {
		t.Error("credential attached without a live registry")
	}

	// A non-gotham container with the right port must not get the credential.
	unmanaged := newClient(false, "5000")
	if err := unmanaged.ensureRegistryCredentialFor(context.Background(), image); err != nil {
		t.Fatalf("unmanaged registry: %v", err)
	}
	if unmanaged.registryAuth.Username != "" {
		t.Error("credential attached to a container gotham does not manage")
	}

	// A managed registry on a different port than the image: no credential.
	mismatch := newClient(true, "5001")
	if err := mismatch.ensureRegistryCredentialFor(context.Background(), image); err != nil {
		t.Fatalf("mismatched registry: %v", err)
	}
	if mismatch.registryAuth.Username != "" {
		t.Error("credential attached for a mismatched registry address")
	}

	// A managed registry publishing the image's address: credential attached.
	live := newClient(true, "5000")
	if err := live.ensureRegistryCredentialFor(context.Background(), image); err != nil {
		t.Fatalf("live registry: %v", err)
	}
	if live.registryAuth.Username == "" || live.registryAuth.Address != "127.0.0.1:5000" {
		t.Errorf("registry auth = %+v; want the live credential", live.registryAuth)
	}
}

// TestEnsureRegistryCredentialPullPathDoesNotWrite checks the pull path never
// generates or rewrites the credential: with no stored credential it stays
// anonymous and creates no files.
func TestEnsureRegistryCredentialPullPathDoesNotWrite(t *testing.T) {
	stateDir := t.TempDir()
	engine := &registryTestEngine{exists: true, running: true, managed: true, hostPort: "5000", publishedPort: "5000"}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

	if err := client.ensureRegistryCredentialFor(context.Background(), "127.0.0.1:5000/gotham/web:dep"); err != nil {
		t.Fatalf("pull path: %v", err)
	}
	if client.registryAuth.Username != "" {
		t.Error("pull path attached a credential")
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("pull path wrote %d files to the state dir; want none", len(entries))
	}
}

// TestEnsureRegistryCredentialStaysAnonymousWhenHtpasswdStale pins the
// registryHtpasswdMatches guard in persistedRegistryAuth: a stored credential
// whose htpasswd no longer matches must not be attached, and the pull path must
// not repair or recreate anything.
func TestEnsureRegistryCredentialStaysAnonymousWhenHtpasswdStale(t *testing.T) {
	stateDir, htpasswdPath := seedRegistryState(t)
	engine := &registryTestEngine{exists: true, running: true, managed: true, hostPort: "5000", publishedPort: "5000"}
	client := newTestDockerClientWithStateDir(t, engine.handler(t), stateDir)

	const corrupted = "gotham:not-a-bcrypt-hash\n"
	if err := os.WriteFile(htpasswdPath, []byte(corrupted), 0o600); err != nil {
		t.Fatalf("corrupt htpasswd: %v", err)
	}

	if err := client.ensureRegistryCredentialFor(context.Background(), "127.0.0.1:5000/gotham/web:dep"); err != nil {
		t.Fatalf("pull path: %v", err)
	}
	if client.registryAuth.Username != "" {
		t.Error("pull path attached a credential although the htpasswd is stale")
	}
	if engine.removals != 0 || engine.creates != 0 {
		t.Errorf("pull path recreated the registry: removals=%d creates=%d", engine.removals, engine.creates)
	}
	content, err := os.ReadFile(htpasswdPath)
	if err != nil {
		t.Fatalf("read htpasswd: %v", err)
	}
	if string(content) != corrupted {
		t.Error("pull path rewrote the stale htpasswd")
	}
}

// TestRegistryIsolated pins the isolation predicate: only the dedicated
// registry network counts, so a container on the default bridge is recreated.
func TestRegistryIsolated(t *testing.T) {
	var isolated containerRuntimeInfo
	isolated.HostConfig.NetworkMode = registryNetworkName
	if !registryIsolated(&isolated) {
		t.Error("dedicated network must be isolated")
	}

	var bridged containerRuntimeInfo
	bridged.HostConfig.NetworkMode = "bridge"
	bridged.NetworkSettings.Networks = map[string]struct{}{"bridge": {}}
	if registryIsolated(&bridged) {
		t.Error("default bridge must not be isolated")
	}

	var multi containerRuntimeInfo
	multi.NetworkSettings.Networks = map[string]struct{}{
		registryNetworkName: {},
		"bridge":            {},
	}
	if registryIsolated(&multi) {
		t.Error("a container on two networks must not be isolated")
	}
}

// TestRegistryBindingExplicit pins the loopback requirement: a wildcard, empty
// or non-loopback host IP must be recreated rather than trusted.
func TestRegistryBindingExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		hostIP string
		want   bool
	}{
		{name: "loopback", hostIP: registryHostIP, want: true},
		{name: "wildcard", hostIP: "0.0.0.0", want: false},
		{name: "empty", hostIP: "", want: false},
		{name: "other", hostIP: "10.0.0.1", want: false},
	} {
		var info containerRuntimeInfo
		info.HostConfig.PortBindings = map[string][]dockerPortBinding{
			registryPort + "/tcp": {{HostIP: tc.hostIP, HostPort: "5001"}},
		}
		if got := registryBindingExplicit(&info); got != tc.want {
			t.Errorf("%s: registryBindingExplicit = %v; want %v", tc.name, got, tc.want)
		}
	}
	var none containerRuntimeInfo
	if registryBindingExplicit(&none) {
		t.Error("a missing binding must not be explicit")
	}
}

// TestEnsureRegistryNetworkConflictRechecksLabel checks a 409 from the network
// create is followed by a label verification, so a concurrent create that lost
// the race to an unrelated network is still refused.
func TestEnsureRegistryNetworkConflictRechecksLabel(t *testing.T) {
	gets := 0
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+registryNetworkName:
			gets++
			if gets == 1 {
				http.NotFound(w, r)
				return
			}
			writeJSONStream(t, w, map[string]any{
				"Name":   registryNetworkName,
				"Labels": map[string]string{"operator": "true"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/networks/create":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Conflict"}`))
		default:
			http.NotFound(w, r)
		}
	}))

	_, err := client.EnsureRegistry(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not managed by gotham") {
		t.Fatalf("err = %v; want an unmanaged-network error after the 409", err)
	}
}

// TestEnsureRegistryRejectsUnmanagedNetwork checks a same-named network that
// gotham did not create is refused rather than adopted.
func TestEnsureRegistryRejectsUnmanagedNetwork(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/networks/"+registryNetworkName {
			writeJSONStream(t, w, map[string]any{
				"Name":   registryNetworkName,
				"Labels": map[string]string{"operator": "true"},
			})
			return
		}
		http.NotFound(w, r)
	}))

	_, err := client.EnsureRegistry(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not managed by gotham") {
		t.Fatalf("err = %v; want an unmanaged-network error", err)
	}
}

// newTestDockerClientWithoutStateDir returns a client with no registry state
// directory, for the fail-closed assertion.
func newTestDockerClientWithoutStateDir(t *testing.T) *DockerClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(server.Close)
	client, err := NewDockerClient(server.URL)
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	return client
}

// containsString reports whether values contains want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
