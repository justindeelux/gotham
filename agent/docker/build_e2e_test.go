package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestDockerBuildLive(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	if testing.Short() {
		t.Skip("skipping live Docker build in -short mode")
	}

	client, err := NewDockerClient("", WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	const tag = "gotham/e2e-build:e2e-deploy"
	defer removeTestImage(t, client, tag)

	var archive bytes.Buffer
	writeLiveContext(t, &archive)

	var logs bytes.Buffer
	buildErr := client.Build(ctx, BuildOptions{
		Tag:     tag,
		Context: bytes.NewReader(archive.Bytes()),
	}, func(data []byte) error {
		logs.Write(data)
		return nil
	})
	if buildErr != nil {
		t.Fatalf("build: %v\nbuild output:\n%s", buildErr, logs.String())
	}

	response, err := client.do(ctx, http.MethodGet, "/images/"+tag+"/json", nil)
	if err != nil {
		t.Fatalf("inspect built image: %v\nbuild output:\n%s", err, logs.String())
	}
	defer func() { _ = response.Body.Close() }()

	var info struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatalf("decode image inspect: %v", err)
	}
	if !strings.HasPrefix(info.ID, "sha256:") {
		t.Fatalf("image id = %q, want sha256 digest", info.ID)
	}
}

func TestBuildServerLive(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live build RPCs")
	}
	if testing.Short() {
		t.Skip("skipping live build RPC in -short mode")
	}

	docker, err := NewDockerClient("", WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := docker.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	const (
		appID    = "e2e-live"
		deployID = "d1"
	)
	imageTag := imageRepoPrefix + appID + ":" + deployID
	defer removeTestImage(t, docker, imageTag)

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterBuildServiceServer(server, NewBuildServer(docker, discardLogger()))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(dialCtx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(dialCtx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	stream, err := agentv1.NewBuildServiceClient(conn).BuildImage(ctx)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}

	var archive bytes.Buffer
	writeLiveContext(t, &archive)
	if err := stream.Send(&agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{
			AppId:    appID,
			DeployId: deployID,
		}},
	}); err != nil {
		t.Fatalf("send meta: %v", err)
	}
	if err := stream.Send(&agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_ContextChunk{ContextChunk: archive.Bytes()},
	}); err != nil {
		t.Fatalf("send context: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}

	var logs bytes.Buffer
	var result *agentv1.BuildImageResult
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v\nbuild output:\n%s", err, logs.String())
		}
		switch event := response.GetEvent().(type) {
		case *agentv1.BuildImageResponse_Log:
			logs.Write(event.Log.GetData())
		case *agentv1.BuildImageResponse_Result:
			result = event.Result
		default:
			t.Fatalf("unexpected event %T", event)
		}
	}

	if result == nil {
		t.Fatalf("result is missing\nbuild output:\n%s", logs.String())
	}
	if result.GetImageTag() != imageTag {
		t.Errorf("image_tag = %q, want %q", result.GetImageTag(), imageTag)
	}
	if !strings.HasPrefix(result.GetDigest(), "sha256:") {
		t.Errorf("digest = %q, want sha256 prefix", result.GetDigest())
	}
	if result.GetRegistryAddr() == "" {
		t.Fatal("registry_addr is empty")
	}
	if want := result.GetRegistryAddr() + "/" + imageTag; result.GetRegistryImage() != want {
		t.Errorf("registry_image = %q, want %q", result.GetRegistryImage(), want)
	}

	defer removeTestImage(t, docker, result.GetRegistryImage())
}

func TestRegistryLiveAuthAndIsolation(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live registry test")
	}
	if testing.Short() {
		t.Skip("skipping live registry test in -short mode")
	}

	stateDir := t.TempDir()
	client, err := NewDockerClient("", WithRegistryStateDir(stateDir))
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	address, err := client.EnsureRegistry(ctx)
	if err != nil {
		t.Fatalf("ensure registry: %v", err)
	}
	auth, _, _, err := prepareRegistryAuth(stateDir)
	if err != nil {
		t.Fatalf("read registry credential: %v", err)
	}

	response, err := http.Get("http://" + address + "/v2/") //nolint:gosec // loopback test URL
	if err != nil {
		t.Fatalf("unauthenticated registry request: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d; want 401", response.StatusCode)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/v2/", nil)
	if err != nil {
		t.Fatalf("build authenticated request: %v", err)
	}
	request.SetBasicAuth(auth.Username, auth.Password)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("authenticated registry request: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d; want 200", response.StatusCode)
	}

	info, err := client.inspectRegistryContainer(ctx)
	if err != nil {
		t.Fatalf("inspect registry: %v", err)
	}
	if !registryIsolated(info) || info.HostConfig.NetworkMode == "bridge" {
		t.Fatalf("registry network mode = %q; want %q", info.HostConfig.NetworkMode, registryNetworkName)
	}
}

func writeLiveContext(t *testing.T, dst *bytes.Buffer) {
	t.Helper()
	writer := tar.NewWriter(dst)
	files := map[string]string{
		"Dockerfile": "FROM scratch\nCOPY hello /hello\n",
		"hello":      "hello\n",
	}
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
}

func removeTestImage(t *testing.T, client *DockerClient, ref string) {
	t.Helper()
	response, err := client.do(context.Background(), http.MethodDelete, "/images/"+ref, nil)
	if err != nil {
		t.Logf("cleanup image %s: %v", ref, err)
		return
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}
