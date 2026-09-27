package agentv1_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const testCPVersion = "0.1.0-test"

func TestRegisterRequestProtoRoundtrip(t *testing.T) {
	t.Parallel()

	want := &agentv1.RegisterRequest{
		NodeId:        "node-1",
		Os:            "linux",
		DockerVersion: "27.3.1",
		Arch:          "arm64",
		TotalMem:      8 << 30,
		TotalDisk:     100 << 30,
		Csr:           []byte("-----BEGIN CERTIFICATE REQUEST-----\ncsr\n-----END CERTIFICATE REQUEST-----\n"),
	}

	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got agentv1.RegisterRequest
	if err := proto.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(want, &got) {
		t.Fatalf("roundtrip mismatch: want %v, got %v", want, &got)
	}
}

func TestCreateContainerRequestProtoRoundtrip(t *testing.T) {
	t.Parallel()

	want := &agentv1.CreateContainerRequest{
		Image:      "ghcr.io/justindeelux/gotham:latest",
		Name:       "gotham-web",
		Env:        []string{"NODE_ENV=production", "PORT=3000"},
		Command:    []string{"serve", "--verbose"},
		Entrypoint: []string{"/usr/local/bin/entrypoint"},
		Labels:     map[string]string{"gotham.app": "web", "gotham.deploy": "42"},
		Ports:      []string{"80:3000"},
		Volumes:    []string{"/srv/data:/data"},
		Networks:   []string{"gotham-public"},
	}

	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got agentv1.CreateContainerRequest
	if err := proto.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(want, &got) {
		t.Fatalf("roundtrip mismatch: want %v, got %v", want, &got)
	}
}

func TestAgentServiceRegister(t *testing.T) {
	t.Parallel()

	client := agentv1.NewAgentServiceClient(startTestServer(t))

	resp, err := client.Register(context.Background(), &agentv1.RegisterRequest{
		NodeId:        "node-1",
		Os:            "linux",
		DockerVersion: "27.3.1",
		Arch:          "amd64",
		TotalMem:      4 << 30,
		TotalDisk:     40 << 30,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.GetCpVersion() != testCPVersion {
		t.Fatalf("cp_version = %q, want %q", resp.GetCpVersion(), testCPVersion)
	}
	if len(resp.GetCert()) == 0 {
		t.Fatal("cert is empty")
	}
}

func TestAgentServiceHeartbeatClientStream(t *testing.T) {
	t.Parallel()

	client := agentv1.NewAgentServiceClient(startTestServer(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Heartbeat(ctx)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	samples := []*agentv1.HeartbeatRequest{
		{CpuUsage: 0.25, MemUsage: 0.50, DiskUsage: 0.60, ContainerCount: 3, SentAt: timestamppb.Now()},
		{CpuUsage: 0.75, MemUsage: 0.80, DiskUsage: 0.90, ContainerCount: 5, SentAt: timestamppb.Now()},
	}
	for i, sample := range samples {
		if err := stream.Send(sample); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("close and recv: %v", err)
	}
	if resp.GetReceivedAt() == nil {
		t.Fatal("received_at is not set")
	}
}

func TestDockerServiceStreamLogs(t *testing.T) {
	t.Parallel()

	client := agentv1.NewDockerServiceClient(startTestServer(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamLogs(ctx, &agentv1.StreamLogsRequest{
		ContainerId: "container-1",
		Follow:      true,
		Tail:        2,
	})
	if err != nil {
		t.Fatalf("stream logs: %v", err)
	}

	var got []string
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		got = append(got, string(chunk.GetData()))
	}

	want := []string{"first line\n", "second line\n"}
	if !slices.Equal(got, want) {
		t.Fatalf("chunks = %q, want %q", got, want)
	}
}

func TestBuildImageRequestProtoRoundtrip(t *testing.T) {
	t.Parallel()

	meta := &agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{
			AppId:      "web",
			DeployId:   "dep-1",
			Dockerfile: "deploy/Dockerfile",
			BuildArgs:  map[string]string{"NODE_ENV": "production"},
		}},
	}
	raw, err := proto.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	var gotMeta agentv1.BuildImageRequest
	if err := proto.Unmarshal(raw, &gotMeta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if !proto.Equal(meta, &gotMeta) {
		t.Fatalf("meta roundtrip mismatch: want %v, got %v", meta, &gotMeta)
	}

	chunk := &agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_ContextChunk{ContextChunk: []byte("tar-bytes")},
	}
	raw, err = proto.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	var gotChunk agentv1.BuildImageRequest
	if err := proto.Unmarshal(raw, &gotChunk); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if !proto.Equal(chunk, &gotChunk) {
		t.Fatalf("chunk roundtrip mismatch: want %v, got %v", chunk, &gotChunk)
	}
}

func TestBuildImageResponseProtoRoundtrip(t *testing.T) {
	t.Parallel()

	want := &agentv1.BuildImageResponse{
		Event: &agentv1.BuildImageResponse_Result{
			Result: &agentv1.BuildImageResult{
				ImageTag:      "gotham/web:dep-1",
				RegistryImage: "127.0.0.1:5000/gotham/web:dep-1",
				Digest:        "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				RegistryAddr:  "127.0.0.1:5000",
			},
		},
	}

	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got agentv1.BuildImageResponse
	if err := proto.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(want, &got) {
		t.Fatalf("roundtrip mismatch: want %v, got %v", want, &got)
	}
}

func TestBuildServiceBuildImage(t *testing.T) {
	t.Parallel()

	client := agentv1.NewBuildServiceClient(startTestServer(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.BuildImage(ctx)
	if err != nil {
		t.Fatalf("build image: %v", err)
	}

	if err := stream.Send(&agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{
			AppId:    "web",
			DeployId: "dep-1",
		}},
	}); err != nil {
		t.Fatalf("send meta: %v", err)
	}
	for _, chunk := range [][]byte{[]byte("hello "), []byte("world")} {
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
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
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

	wantLogs := []string{"building gotham/web:dep-1\n"}
	if !slices.Equal(logs, wantLogs) {
		t.Fatalf("logs = %q, want %q", logs, wantLogs)
	}
	if result == nil {
		t.Fatal("result is missing")
	}
	if result.GetImageTag() != "gotham/web:dep-1" {
		t.Fatalf("image_tag = %q, want %q", result.GetImageTag(), "gotham/web:dep-1")
	}
	if result.GetRegistryImage() != "127.0.0.1:5000/gotham/web:dep-1" {
		t.Fatalf("registry_image = %q", result.GetRegistryImage())
	}
	if result.GetRegistryAddr() != "127.0.0.1:5000" {
		t.Fatalf("registry_addr = %q", result.GetRegistryAddr())
	}
	sum := sha256.Sum256([]byte("hello world"))
	if want := "sha256:" + hex.EncodeToString(sum[:]); result.GetDigest() != want {
		t.Fatalf("digest = %q, want %q", result.GetDigest(), want)
	}
}

// startTestServer serves the stub AgentService, DockerService and
// BuildService over an in-memory bufconn listener and returns a dialed client
// connection. Teardown is registered with t.Cleanup and reports any serve
// error.
func startTestServer(t *testing.T) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, stubAgentServer{})
	agentv1.RegisterDockerServiceServer(srv, stubDockerServer{})
	agentv1.RegisterBuildServiceServer(srv, stubBuildServer{})

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		<-serveErr
		t.Fatalf("dial bufconn: %v", err)
	}

	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
		srv.Stop()
		if err := <-serveErr; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return conn
}

type stubAgentServer struct {
	agentv1.UnimplementedAgentServiceServer
}

func (stubAgentServer) Register(_ context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if req.GetNodeId() == "" {
		return nil, errors.New("node_id is required")
	}
	return &agentv1.RegisterResponse{
		Cert:      []byte("-----BEGIN CERTIFICATE-----\nstub\n-----END CERTIFICATE-----\n"),
		CpVersion: testCPVersion,
	}, nil
}

func (stubAgentServer) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	for {
		if _, err := stream.Recv(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
}

type stubDockerServer struct {
	agentv1.UnimplementedDockerServiceServer
}

func (stubDockerServer) StreamLogs(req *agentv1.StreamLogsRequest, stream grpc.ServerStreamingServer[agentv1.LogChunk]) error {
	if req.GetContainerId() == "" {
		return errors.New("container_id is required")
	}
	for _, data := range []string{"first line\n", "second line\n"} {
		if err := stream.Send(&agentv1.LogChunk{Data: []byte(data)}); err != nil {
			return err
		}
	}
	return nil
}

type stubBuildServer struct {
	agentv1.UnimplementedBuildServiceServer
}

func (stubBuildServer) BuildImage(stream grpc.BidiStreamingServer[agentv1.BuildImageRequest, agentv1.BuildImageResponse]) error {
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return errors.New("build stream is missing build meta")
	}
	if err != nil {
		return err
	}
	meta := first.GetMeta()
	if meta == nil {
		return errors.New("first request must carry build meta")
	}

	var contextBytes []byte
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		contextBytes = append(contextBytes, request.GetContextChunk()...)
	}

	imageTag := "gotham/" + meta.GetAppId() + ":" + meta.GetDeployId()
	if err := stream.Send(&agentv1.BuildImageResponse{
		Event: &agentv1.BuildImageResponse_Log{
			Log: &agentv1.BuildLogChunk{Data: []byte("building " + imageTag + "\n")},
		},
	}); err != nil {
		return err
	}
	sum := sha256.Sum256(contextBytes)
	return stream.Send(&agentv1.BuildImageResponse{
		Event: &agentv1.BuildImageResponse_Result{
			Result: &agentv1.BuildImageResult{
				ImageTag:      imageTag,
				RegistryImage: "127.0.0.1:5000/" + imageTag,
				Digest:        "sha256:" + hex.EncodeToString(sum[:]),
				RegistryAddr:  "127.0.0.1:5000",
			},
		},
	})
}
