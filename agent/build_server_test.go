package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeBuildClient is a configurable buildClient for BuildServer tests.
type fakeBuildClient struct {
	registryAddr string
	registryErr  error
	buildErr     error
	tagErr       error
	pushErr      error
	digestErr    error
	digest       string

	buildOpts    *BuildOptions
	contextBytes []byte
	tagSource    string
	tagRepo      string
	tagTag       string
	pushRepo     string
	pushTag      string
	digestRef    string

	toolchainEngine  string
	toolchainTag     string
	toolchainContext []byte
	toolchainArgs    map[string]string
	toolchainErr     error
	toolchainCalls   int

	pruneCalls  int
	pruneApp    string
	pruneDeploy string
	pruneErr    error
}

func (f *fakeBuildClient) EnsureRegistry(context.Context) (string, error) {
	if f.registryErr != nil {
		return "", f.registryErr
	}
	if f.registryAddr == "" {
		return "127.0.0.1:5000", nil
	}
	return f.registryAddr, nil
}

func (f *fakeBuildClient) Build(_ context.Context, opts BuildOptions, emit func([]byte) error) error {
	processed, err := io.ReadAll(opts.Context)
	if err != nil {
		return err
	}
	f.buildOpts = &opts
	f.contextBytes = processed

	for _, line := range []string{"Step 1/2 : FROM scratch\n", "Step 2/2 : COPY hello /hello\n"} {
		if err := emit([]byte(line)); err != nil {
			return err
		}
	}
	return f.buildErr
}

func (f *fakeBuildClient) RunToolchain(_ context.Context, engine string, contextTar io.Reader, tag string, buildArgs map[string]string, emit func([]byte) error) error {
	f.toolchainCalls++
	f.toolchainEngine = engine
	f.toolchainTag = tag
	f.toolchainArgs = buildArgs
	processed, err := io.ReadAll(contextTar)
	if err != nil {
		return err
	}
	f.toolchainContext = processed
	if err := emit([]byte("toolchain " + engine + " on node\n")); err != nil {
		return err
	}
	return f.toolchainErr
}

func (f *fakeBuildClient) TagImage(_ context.Context, source, repository, tag string) error {
	f.tagSource, f.tagRepo, f.tagTag = source, repository, tag
	return f.tagErr
}

func (f *fakeBuildClient) PushImage(_ context.Context, repository, tag string, emit func([]byte) error) error {
	f.pushRepo, f.pushTag = repository, tag
	if err := emit([]byte("pushed " + repository + ":" + tag + "\n")); err != nil {
		return err
	}
	return f.pushErr
}

func (f *fakeBuildClient) ImageDigest(_ context.Context, ref string) (string, error) {
	f.digestRef = ref
	if f.digestErr != nil {
		return "", f.digestErr
	}
	if f.digest == "" {
		return "sha256:deadbeef", nil
	}
	return f.digest, nil
}

func (f *fakeBuildClient) PruneAppImages(_ context.Context, appID, keepDeploy string) error {
	f.pruneCalls++
	f.pruneApp = appID
	f.pruneDeploy = keepDeploy
	return f.pruneErr
}

// newBuildServiceClient starts an in-process BuildService server backed by
// fake and returns a client connected over bufconn alongside the server so
// tests can adjust it before the first RPC.
func newBuildServiceClient(t *testing.T, fake *fakeBuildClient) (agentv1.BuildServiceClient, *BuildServer) {
	t.Helper()
	server := NewBuildServer(fake, discardLogger())

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	agentv1.RegisterBuildServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return agentv1.NewBuildServiceClient(conn), server
}

// runBuild streams meta and chunks to BuildImage, closes the send side, and
// returns the collected log chunks plus the final result (nil when the RPC
// failed first).
func runBuild(t *testing.T, client agentv1.BuildServiceClient, meta *agentv1.BuildMeta, chunks [][]byte) ([]string, *agentv1.BuildImageResult, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.BuildImage(ctx)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}
	if meta != nil {
		if err := stream.Send(&agentv1.BuildImageRequest{
			Part: &agentv1.BuildImageRequest_Meta{Meta: meta},
		}); err != nil {
			t.Fatalf("send meta: %v", err)
		}
	}
	for _, chunk := range chunks {
		if err := stream.Send(&agentv1.BuildImageRequest{
			Part: &agentv1.BuildImageRequest_ContextChunk{ContextChunk: chunk},
		}); err != nil {
			t.Fatalf("send chunk: %v", err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}

	var logs []string
	var result *agentv1.BuildImageResult
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return logs, result, nil
		}
		if err != nil {
			return logs, result, err
		}
		switch event := response.GetEvent().(type) {
		case *agentv1.BuildImageResponse_Log:
			logs = append(logs, string(event.Log.GetData()))
		case *agentv1.BuildImageResponse_Result:
			result = event.Result
		default:
			t.Fatalf("unexpected event %T", event)
		}
	}
}

func TestBuildServerBuildImage(t *testing.T) {
	fake := &fakeBuildClient{digest: "sha256:abc123"}
	client, _ := newBuildServiceClient(t, fake)

	meta := &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}
	logs, result, err := runBuild(t, client, meta, [][]byte{[]byte("hello "), []byte("world")})
	if err != nil {
		t.Fatalf("build image: %v", err)
	}

	if result == nil {
		t.Fatal("result is missing")
	}
	if result.GetImageTag() != "gotham/web:dep-1" {
		t.Fatalf("image_tag = %q", result.GetImageTag())
	}
	if result.GetRegistryImage() != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Fatalf("registry_image = %q", result.GetRegistryImage())
	}
	if result.GetRegistryAddr() != "127.0.0.1:5000" {
		t.Fatalf("registry_addr = %q", result.GetRegistryAddr())
	}
	if result.GetDigest() != "sha256:abc123" {
		t.Fatalf("digest = %q", result.GetDigest())
	}

	if fake.buildOpts == nil {
		t.Fatal("build was not called")
	}
	if fake.buildOpts.Tag != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Fatalf("build tag = %q", fake.buildOpts.Tag)
	}
	if fake.buildOpts.Dockerfile != "Dockerfile" {
		t.Fatalf("dockerfile = %q", fake.buildOpts.Dockerfile)
	}
	if string(fake.contextBytes) != "hello world" {
		t.Fatalf("context = %q", fake.contextBytes)
	}
	if fake.tagSource != "127.0.0.1:5000/gotham/web:dep-1" || fake.tagRepo != "gotham/web" || fake.tagTag != "dep-1" {
		t.Fatalf("tag call = (%q, %q, %q)", fake.tagSource, fake.tagRepo, fake.tagTag)
	}
	if fake.pushRepo != "127.0.0.1:5000/gotham/web" || fake.pushTag != "dep-1" {
		t.Fatalf("push call = (%q, %q)", fake.pushRepo, fake.pushTag)
	}
	if fake.digestRef != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Fatalf("digest ref = %q", fake.digestRef)
	}
	if fake.pruneCalls != 1 || fake.pruneApp != "web" || fake.pruneDeploy != "dep-1" {
		t.Fatalf("prune call = (%d, %q, %q); want (1, web, dep-1)", fake.pruneCalls, fake.pruneApp, fake.pruneDeploy)
	}

	joined := strings.Join(logs, "")
	for _, want := range []string{"using node registry 127.0.0.1:5000\n", "Step 1/2 : FROM scratch\n", "pushing 127.0.0.1:5000/gotham/web:dep-1\n", "pushed 127.0.0.1:5000/gotham/web:dep-1\n"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("logs %q missing %q", joined, want)
		}
	}
}

func TestBuildServerBuildImageUsesDockerfileAndBuildArgs(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	meta := &agentv1.BuildMeta{
		AppId:      "api",
		DeployId:   "d2",
		Dockerfile: "deploy/service.Dockerfile",
		BuildArgs:  map[string]string{"NODE_ENV": "production"},
	}
	_, result, err := runBuild(t, client, meta, nil)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}
	if result == nil {
		t.Fatal("result is missing")
	}
	if fake.buildOpts.Dockerfile != "deploy/service.Dockerfile" {
		t.Fatalf("dockerfile = %q", fake.buildOpts.Dockerfile)
	}
	if fake.buildOpts.BuildArgs["NODE_ENV"] != "production" {
		t.Fatalf("build args = %v", fake.buildOpts.BuildArgs)
	}
}

// TestBuildServerRunsToolchainOnNode pins the remote-build fix: a railpack or
// buildpacks engine is dispatched to the node's toolchain runner against the
// uploaded context, then tagged and pushed like a Dockerfile build.
func TestBuildServerRunsToolchainOnNode(t *testing.T) {
	fake := &fakeBuildClient{digest: "sha256:tool"}
	client, _ := newBuildServiceClient(t, fake)

	meta := &agentv1.BuildMeta{
		AppId:     "web",
		DeployId:  "dep-1",
		Engine:    "railpack",
		BuildArgs: map[string]string{"GO_VERSION": "1.22"},
	}
	logs, result, err := runBuild(t, client, meta, [][]byte{[]byte("source-tar")})
	if err != nil {
		t.Fatalf("build image: %v", err)
	}
	if result == nil || result.GetDigest() != "sha256:tool" {
		t.Fatalf("result = %+v", result)
	}
	if fake.toolchainCalls != 1 {
		t.Fatalf("toolchain calls = %d; want 1", fake.toolchainCalls)
	}
	if fake.buildOpts != nil {
		t.Fatalf("docker build ran for a toolchain engine: %+v", fake.buildOpts)
	}
	if fake.toolchainEngine != "railpack" {
		t.Errorf("engine = %q; want railpack", fake.toolchainEngine)
	}
	if fake.toolchainTag != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Errorf("tag = %q", fake.toolchainTag)
	}
	if string(fake.toolchainContext) != "source-tar" {
		t.Errorf("context = %q", fake.toolchainContext)
	}
	if fake.toolchainArgs["GO_VERSION"] != "1.22" {
		t.Errorf("build args = %v", fake.toolchainArgs)
	}
	// The shared tag/push steps still run for a toolchain build.
	if fake.pushRepo != "127.0.0.1:5000/gotham/web" || fake.pushTag != "dep-1" {
		t.Errorf("push call = (%q, %q)", fake.pushRepo, fake.pushTag)
	}
	joined := strings.Join(logs, "")
	for _, want := range []string{"running railpack build on the node\n", "toolchain railpack on node\n", "pushed 127.0.0.1:5000/gotham/web:dep-1\n"} {
		if !strings.Contains(joined, want) {
			t.Errorf("logs %q missing %q", joined, want)
		}
	}
}

func TestBuildServerRejectsUnknownEngine(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	_, _, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1", Engine: "wasm"}, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.InvalidArgument, err)
	}
	if fake.buildOpts != nil || fake.toolchainCalls != 0 {
		t.Fatal("build must not run for an unsupported engine")
	}
}

func TestBuildServerRejectsChunkBeforeMeta(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	_, _, err := runBuild(t, client, nil, [][]byte{[]byte("payload")})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.InvalidArgument, err)
	}
	if fake.buildOpts != nil {
		t.Fatal("build must not run without meta")
	}
}

func TestBuildServerRejectsMissingMeta(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	_, _, err := runBuild(t, client, nil, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.InvalidArgument, err)
	}
}

func TestBuildServerRejectsUnsafeMeta(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	for _, meta := range []*agentv1.BuildMeta{
		{AppId: "web/app", DeployId: "dep-1"},
		{AppId: "web", DeployId: ""},
		{AppId: "web", DeployId: "dep:1"},
	} {
		_, _, err := runBuild(t, client, meta, nil)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("meta %+v: code = %v, want %v (err %v)", meta, status.Code(err), codes.InvalidArgument, err)
		}
	}
	if fake.buildOpts != nil {
		t.Fatal("build must not run for rejected meta")
	}
}

func TestBuildServerRejectsSecondMeta(t *testing.T) {
	fake := &fakeBuildClient{}
	client, _ := newBuildServiceClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.BuildImage(ctx)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}
	for _, request := range []*agentv1.BuildImageRequest{
		{Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}}},
		{Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{AppId: "other", DeployId: "dep-2"}}},
	} {
		if err := stream.Send(request); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	_ = stream.CloseSend()

	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.InvalidArgument, err)
	}
	if fake.buildOpts != nil {
		t.Fatal("build must not run for a duplicate meta")
	}
}

func TestBuildServerRejectsOversizedContext(t *testing.T) {
	fake := &fakeBuildClient{}
	client, server := newBuildServiceClient(t, fake)
	server.maxContextBytes = 4

	_, _, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"},
		[][]byte{[]byte("1234"), []byte("5")})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.InvalidArgument, err)
	}
	if fake.buildOpts != nil {
		t.Fatal("build must not run for an oversized context")
	}
}

func TestBuildServerRegistryError(t *testing.T) {
	fake := &fakeBuildClient{registryErr: errors.New("daemon unreachable")}
	client, _ := newBuildServiceClient(t, fake)

	_, _, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}, nil)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.Internal, err)
	}
	if !strings.Contains(status.Convert(err).Message(), "daemon unreachable") {
		t.Fatalf("message = %q", status.Convert(err).Message())
	}
	if fake.buildOpts != nil {
		t.Fatal("build must not run without a registry")
	}
}

func TestBuildServerBuildErrorStreamsLogs(t *testing.T) {
	fake := &fakeBuildClient{buildErr: errors.New("dockerfile not found")}
	client, _ := newBuildServiceClient(t, fake)

	logs, _, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}, nil)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.Internal, err)
	}
	if !strings.Contains(status.Convert(err).Message(), "dockerfile not found") {
		t.Fatalf("message = %q", status.Convert(err).Message())
	}
	if len(logs) == 0 {
		t.Fatal("expected build logs before the failure")
	}
}

func TestBuildServerPushError(t *testing.T) {
	fake := &fakeBuildClient{pushErr: errors.New("registry write denied")}
	client, _ := newBuildServiceClient(t, fake)

	_, _, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}, nil)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want %v (err %v)", status.Code(err), codes.Internal, err)
	}
	if !strings.Contains(status.Convert(err).Message(), "registry write denied") {
		t.Fatalf("message = %q", status.Convert(err).Message())
	}
}

// TestBuildServerPruneFailureDoesNotFailBuild pins the best-effort contract:
// image retention runs after a successful push, and a cleanup failure is
// logged, never surfaced as a build failure.
func TestBuildServerPruneFailureDoesNotFailBuild(t *testing.T) {
	fake := &fakeBuildClient{pruneErr: errors.New("daemon busy")}
	client, _ := newBuildServiceClient(t, fake)

	_, result, err := runBuild(t, client, &agentv1.BuildMeta{AppId: "web", DeployId: "dep-1"}, nil)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}
	if result == nil {
		t.Fatal("result is missing")
	}
	if fake.pruneCalls != 1 {
		t.Fatalf("prune calls = %d; want 1", fake.pruneCalls)
	}
}
