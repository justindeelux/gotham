package agentv1_test

import (
	"context"
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

// startTestServer serves the stub AgentService and DockerService over an
// in-memory bufconn listener and returns a dialed client connection. Teardown
// is registered with t.Cleanup and reports any serve error.
func startTestServer(t *testing.T) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, stubAgentServer{})
	agentv1.RegisterDockerServiceServer(srv, stubDockerServer{})

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
