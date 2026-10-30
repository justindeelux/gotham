package docker

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
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

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
	listQuery   string
	versionHits int
	createdID   string
	startFail   bool
	removeFail  bool
	logsBody    []byte
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
		listQuery:   s.listQuery,
		versionHits: s.versionHits,
		createdID:   s.createdID,
		startFail:   s.startFail,
		logsBody:    append([]byte(nil), s.logsBody...),
	}
}

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

	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		state.listQuery = r.URL.RawQuery
		state.mu.Unlock()
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

	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"Id": "sha256:deadbeef",
			"RepoDigests": []string{
				"unrelated/other@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				inspectRepoDigest + "@" + inspectDigest,
			},
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

func newFakeDockerClient(t *testing.T, apiURL string) *DockerClient {
	t.Helper()
	client, err := NewDockerClient(strings.Replace(apiURL, "http://", "tcp://", 1))
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

	containers, err := client.ListContainers(context.Background(), true, nil, false)
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

func TestDockerClientListVolumes(t *testing.T) {
	var query string
	mux := http.NewServeMux()
	mux.HandleFunc("/system/df", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(t, w, map[string]any{
			"Volumes": []map[string]any{
				{
					"Name":      "gotham-app-6f1c2f7e-0000-0000-0000-000000000001-data",
					"CreatedAt": "2026-10-30T08:00:00Z",
					"UsageData": map[string]any{"Size": 2048, "RefCount": 0},
				},
				{"Name": "unused", "CreatedAt": "2026-10-30T09:00:00Z"},
			},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	volumes, err := client.ListVolumes(context.Background())
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if query != "type=volume" {
		t.Errorf("query = %q; want type=volume", query)
	}
	if len(volumes) != 2 {
		t.Fatalf("len(volumes) = %d; want 2", len(volumes))
	}
	got := volumes[0]
	if got.GetName() != "gotham-app-6f1c2f7e-0000-0000-0000-000000000001-data" {
		t.Errorf("name = %q", got.GetName())
	}
	if got.GetSizeBytes() != 2048 || got.GetRefCount() != 0 {
		t.Errorf("usage = %d/%d; want 2048/0", got.GetSizeBytes(), got.GetRefCount())
	}
	if got.GetCreatedAt() == nil || got.GetCreatedAt().AsTime().UTC().Format(time.RFC3339) != "2026-10-30T08:00:00Z" {
		t.Errorf("CreatedAt = %v", got.GetCreatedAt())
	}
	if vols := volumes[1].GetSizeBytes(); vols != 0 {
		t.Errorf("volume without UsageData size = %d; want 0", vols)
	}
}

func TestDockerClientListContainersMountMetadata(t *testing.T) {
	managed := filepath.Join(t.TempDir(), "app", "data")
	var inspectCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{{
			"Id": "abc123", "Names": []string{"/shop-web-1"}, "Image": "nginx",
			"State": "exited",
			"Mounts": []map[string]any{
				{"Type": "bind", "Source": managed, "Destination": "/data", "RW": true},
				{"Type": "volume", "Name": "shop_data", "Source": "/var/lib/docker/volumes/shop_data/_data", "Destination": "/cache", "RW": false},
				{"Type": "volume", "Name": "plain_vol", "Source": "/var/lib/docker/volumes/plain_vol/_data", "Destination": "/plain", "RW": true},
				{"Type": "volume", "Name": "broken_vol", "Source": "/var/lib/docker/volumes/broken_vol/_data", "Destination": "/broken", "RW": true},
				{"Type": "volume", "Name": "relative_vol", "Source": "/var/lib/docker/volumes/relative_vol/_data", "Destination": "/relative", "RW": true},
				{"Type": "volume", "Name": "nfs_vol", "Source": "/var/lib/docker/volumes/nfs_vol/_data", "Destination": "/nfs", "RW": true},
				{"Type": "volume", "Name": "noexec_vol", "Source": "/var/lib/docker/volumes/noexec_vol/_data", "Destination": "/noexec", "RW": true},
				{"Type": "tmpfs", "Destination": "/tmp", "RW": true},
			},
		}})
	})
	volume := func(body map[string]any) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			inspectCalls++
			writeJSON(t, w, body)
		}
	}
	mux.HandleFunc("/volumes/shop_data", volume(map[string]any{
		"Driver": "local", "Options": map[string]string{"type": "none", "o": "bind", "device": managed},
	}))
	mux.HandleFunc("/volumes/plain_vol", volume(map[string]any{
		"Driver": "local", "Options": map[string]string{},
	}))
	mux.HandleFunc("/volumes/broken_vol", func(w http.ResponseWriter, _ *http.Request) {
		inspectCalls++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"engine unavailable"}`))
	})
	mux.HandleFunc("/volumes/relative_vol", volume(map[string]any{
		"Driver": "local", "Options": map[string]string{"type": "none", "o": "bind", "device": "relative/dir"},
	}))
	mux.HandleFunc("/volumes/nfs_vol", volume(map[string]any{
		"Driver": "nfs", "Options": map[string]string{"type": "nfs", "o": "addr=10.0.0.1", "device": "/export"},
	}))
	mux.HandleFunc("/volumes/noexec_vol", volume(map[string]any{
		"Driver": "local", "Options": map[string]string{"type": "ext4", "o": "noexec", "device": "/dev/sdb1"},
	}))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	containers, err := client.ListContainers(context.Background(), true, nil, true)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if inspectCalls != 6 {
		t.Errorf("volume inspects = %d, want 6 (one per distinct named volume)", inspectCalls)
	}
	mounts := containers[0].GetMounts()
	if len(mounts) != 8 {
		t.Fatalf("mounts = %+v, want 8", mounts)
	}
	if got := mounts[0]; got.GetType() != "bind" || got.GetVolumeName() != "" || got.GetVolumeDevice() != "" {
		t.Errorf("bind mount = %+v; want type bind and no volume metadata", got)
	}
	if got := mounts[1]; got.GetType() != "volume" || got.GetVolumeName() != "shop_data" || got.GetVolumeDevice() != managed {
		t.Errorf("managed volume = %+v; want device %q, not its mountpoint", got, managed)
	}
	if !mounts[1].GetReadOnly() {
		t.Errorf("managed volume read_only = false; want true")
	}
	if got := mounts[2]; got.GetVolumeName() != "plain_vol" || got.GetVolumeDevice() != "" || got.GetVolumeDeviceUnverified() {
		t.Errorf("plain volume = %+v; want no device and not unverified", got)
	}
	if got := mounts[3]; got.GetVolumeDevice() != "" || !got.GetVolumeDeviceUnverified() {
		t.Errorf("broken volume = %+v; want empty device marked unverified", got)
	}
	if got := mounts[4]; got.GetVolumeDevice() != "" {
		t.Errorf("relative device = %+v; want refused", got)
	}
	if got := mounts[5]; got.GetVolumeDevice() != "" {
		t.Errorf("non-local driver = %+v; want refused", got)
	}
	if got := mounts[6]; got.GetVolumeDevice() != "" {
		t.Errorf("non-bind options = %+v; want refused", got)
	}
	if got := mounts[7]; got.GetType() != "tmpfs" || got.GetVolumeName() != "" {
		t.Errorf("tmpfs mount = %+v; want type tmpfs and no host path", got)
	}
}

func TestDockerClientListContainersSkipsDevicesUnlessAsked(t *testing.T) {
	var inspectCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{{
			"Id": "abc123", "Names": []string{"/shop-web-1"}, "Image": "nginx", "State": "running",
			"Mounts": []map[string]any{
				{"Type": "volume", "Name": "shop_data", "Source": "/docker/shop_data", "Destination": "/cache", "RW": true},
			},
		}})
	})
	mux.HandleFunc("/volumes/shop_data", func(w http.ResponseWriter, _ *http.Request) {
		inspectCalls++
		writeJSON(t, w, map[string]any{"Driver": "local"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	containers, err := client.ListContainers(context.Background(), true, nil, false)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if inspectCalls != 0 {
		t.Errorf("volume inspects = %d, want 0 when devices are not requested", inspectCalls)
	}
	if got := containers[0].GetMounts()[0]; got.GetVolumeDevice() != "" || got.GetVolumeDeviceUnverified() {
		t.Errorf("mount = %+v; want no device and not unverified", got)
	}
}

func TestDockerClientListContainersLabelFilter(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	labels := map[string]string{
		"com.docker.compose.project": "gotham-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182",
		"com.docker.compose.service": "web",
	}
	if _, err := client.ListContainers(context.Background(), false, labels, false); err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	query := state.snapshot().listQuery
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("parse query %q: %v", query, err)
	}
	if got := values.Get("all"); got != "0" {
		t.Fatalf("all = %q, want 0 (running only)", got)
	}
	var filters map[string][]string
	if err := json.Unmarshal([]byte(values.Get("filters")), &filters); err != nil {
		t.Fatalf("decode filters %q: %v", values.Get("filters"), err)
	}
	want := []string{
		"com.docker.compose.project=gotham-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182",
		"com.docker.compose.service=web",
	}
	if got := filters["label"]; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("label filters = %v, want %v", got, want)
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

const (
	inspectRepoDigest = "docker.io/library/alpine"
	inspectDigest     = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestDockerClientPullImageAuth(t *testing.T) {
	server, state := newFakeDockerServer(t)
	client := newFakeDockerClient(t, server.URL)

	digest, err := client.PullImage(context.Background(), "registry.example.com/acme/app:1.2", "robot", "s3cret-token")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != "" {
		t.Errorf("digest = %q; the fake engine reports no digest for this repository", digest)
	}
	auth := decodeRegistryAuth(t, state.snapshot().pullAuth)
	if auth["username"] != "robot" || auth["password"] != "s3cret-token" {
		t.Errorf("auth config = %v; want the per-pull credential", auth)
	}
	if auth["serveraddress"] != "registry.example.com" {
		t.Errorf("serveraddress = %q; want the image registry host", auth["serveraddress"])
	}

	digest, err = client.PullImage(context.Background(), "alpine:latest", "robot", "s3cret-token")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != inspectDigest {
		t.Errorf("digest = %q; want %q", digest, inspectDigest)
	}

	if _, err := client.PullImage(context.Background(), "alpine:latest", "", ""); err != nil {
		t.Fatalf("anonymous PullImage: %v", err)
	}
	if got := state.snapshot().pullAuth; got != anonymousRegistryAuth {
		t.Errorf("anonymous pull auth = %q; want the anonymous config", got)
	}
}

func TestDockerClientPullImageHubSpellings(t *testing.T) {
	const digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const digestC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	mux := http.NewServeMux()
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		var digests []string
		switch {
		case strings.Contains(r.URL.Path, "alpine"):
			digests = []string{"alpine@" + digestA}
		case strings.Contains(r.URL.Path, "app"):
			digests = []string{"docker.io/acme/app@" + digestB}
		case strings.Contains(r.URL.Path, "private"):
			digests = []string{"acme/app@" + digestC, "registry.example.com/acme/private@" + digestB}
		}
		writeJSON(t, w, map[string]any{"Id": "sha256:deadbeef", "RepoDigests": digests})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	digest, err := client.PullImage(context.Background(), "docker.io/library/alpine:3.20", "", "")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != digestA {
		t.Errorf("digest = %q; want the familiar-name entry %q", digest, digestA)
	}
	digest, err = client.PullImage(context.Background(), "index.docker.io/acme/app:1", "", "")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != digestB {
		t.Errorf("digest = %q; want the normalized entry %q", digest, digestB)
	}
	digest, err = client.PullImage(context.Background(), "registry.example.com/acme/private:1", "", "")
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if digest != digestB {
		t.Errorf("digest = %q; want the exact-name entry, not the familiar %q", digest, digestB)
	}
}

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

func TestDockerClientDaemonDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	client := newFakeDockerClient(t, url)

	if _, err := client.Version(context.Background()); !errors.Is(err, ErrDockerUnavailable) {
		t.Fatalf("closed daemon = %v; want ErrDockerUnavailable", err)
	}
}

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

func TestDecodeLogStreamTruncatedAfterValidFrame(t *testing.T) {
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
