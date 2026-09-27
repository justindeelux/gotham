package servers

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeDockerServer is an in-process DockerService implementation with canned
// responses covering all eight RPCs the CP client must expose.
type fakeDockerServer struct {
	agentv1.UnimplementedDockerServiceServer
}

func (fakeDockerServer) ListContainers(context.Context, *agentv1.ListContainersRequest) (*agentv1.ListContainersResponse, error) {
	return &agentv1.ListContainersResponse{
		Containers: []*agentv1.ContainerInfo{
			{Id: "abc123", Name: "web", Image: "nginx:latest", Status: "Up", State: "running"},
		},
	}, nil
}

func (fakeDockerServer) StartContainer(_ context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return &agentv1.ContainerActionResponse{ContainerId: req.GetContainerId()}, nil
}

func (fakeDockerServer) StopContainer(_ context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return &agentv1.ContainerActionResponse{ContainerId: req.GetContainerId()}, nil
}

func (fakeDockerServer) RestartContainer(_ context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return &agentv1.ContainerActionResponse{ContainerId: req.GetContainerId()}, nil
}

func (fakeDockerServer) PullImage(context.Context, *agentv1.PullImageRequest) (*agentv1.PullImageResponse, error) {
	return &agentv1.PullImageResponse{}, nil
}

func (fakeDockerServer) CreateContainer(_ context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, errors.New("image is required")
	}
	return &agentv1.ContainerActionResponse{ContainerId: "created-1"}, nil
}

func (fakeDockerServer) RunImage(_ context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, errors.New("image is required")
	}
	return &agentv1.ContainerActionResponse{ContainerId: "run-1"}, nil
}

func (fakeDockerServer) StreamLogs(_ *agentv1.StreamLogsRequest, stream grpc.ServerStreamingServer[agentv1.LogChunk]) error {
	for _, line := range []string{"first line\n", "second line\n"} {
		if err := stream.Send(&agentv1.LogChunk{Data: []byte(line)}); err != nil {
			return err
		}
	}
	return nil
}

// startFakeDockerServer serves fakeDockerServer over bufconn and returns the
// dial target plus a context dialer option. serverCreds may be nil for an
// insecure listener.
func startFakeDockerServer(t *testing.T, serverCreds credentials.TransportCredentials) (string, grpc.DialOption) {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	opts := make([]grpc.ServerOption, 0, 1)
	if serverCreds != nil {
		opts = append(opts, grpc.Creds(serverCreds))
	}
	srv := grpc.NewServer(opts...)
	agentv1.RegisterDockerServiceServer(srv, fakeDockerServer{})

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveErr; err != nil {
			t.Logf("fake docker serve: %v", err)
		}
		_ = lis.Close()
	})

	dialer := grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
	return "passthrough:///bufnet", dialer
}

// exerciseDockerClient calls every RPC the deliverable requires.
func exerciseDockerClient(t *testing.T, ctx context.Context, client agentv1.DockerServiceClient) {
	t.Helper()

	listed, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(listed.GetContainers()) != 1 || listed.GetContainers()[0].GetId() != "abc123" {
		t.Fatalf("ListContainers = %v, want one container abc123", listed.GetContainers())
	}

	for name, call := range map[string]func() (string, error){
		"StartContainer": func() (string, error) {
			resp, err := client.StartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: "abc123"})
			return resp.GetContainerId(), err
		},
		"StopContainer": func() (string, error) {
			resp, err := client.StopContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: "abc123"})
			return resp.GetContainerId(), err
		},
		"RestartContainer": func() (string, error) {
			resp, err := client.RestartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: "abc123"})
			return resp.GetContainerId(), err
		},
	} {
		id, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if id != "abc123" {
			t.Fatalf("%s id = %q, want abc123", name, id)
		}
	}

	if _, err := client.PullImage(ctx, &agentv1.PullImageRequest{Image: "nginx:latest"}); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	created, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{Image: "nginx:latest", Name: "web"})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if created.GetContainerId() != "created-1" {
		t.Fatalf("CreateContainer id = %q, want created-1", created.GetContainerId())
	}
	run, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{Image: "nginx:latest", Name: "web-run"})
	if err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	if run.GetContainerId() != "run-1" {
		t.Fatalf("RunImage id = %q, want run-1", run.GetContainerId())
	}

	stream, err := client.StreamLogs(ctx, &agentv1.StreamLogsRequest{ContainerId: "abc123", Follow: false, Tail: 2})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	var chunks []string
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("StreamLogs recv: %v", err)
		}
		chunks = append(chunks, string(chunk.GetData()))
	}
	if len(chunks) != 2 || chunks[0] != "first line\n" || chunks[1] != "second line\n" {
		t.Fatalf("StreamLogs chunks = %q, want two log lines", chunks)
	}
}

func TestDialDockerClientInsecureBufconn(t *testing.T) {
	target, dialer := startFakeDockerServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A nil authority selects insecure transport for development.
	client, err := DialDockerClient(ctx, target, nil, WithDockerDialOptions(dialer))
	if err != nil {
		t.Fatalf("DialDockerClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	}()
	if client.Conn() == nil {
		t.Fatal("Conn is nil")
	}

	exerciseDockerClient(t, ctx, client)
}

func TestDialDockerClientMTLSBufconn(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	const nodeID = "test-node"

	// The agent side presents a server-auth certificate and requires a client
	// certificate, mirroring agent.ServerCredentials with a CA configured.
	serverCertPEM, serverKeyPEM, err := authority.IssueServerCert([]string{nodeID})
	if err != nil {
		t.Fatalf("IssueServerCert: %v", err)
	}
	serverPair, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("load server keypair: %v", err)
	}
	target, dialer := startFakeDockerServer(t, credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    authority.Pool(),
		MinVersion:   tls.VersionTLS12,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := DialDockerClient(ctx, target, authority,
		WithDockerServerName(nodeID),
		WithDockerDialOptions(dialer),
	)
	if err != nil {
		t.Fatalf("DialDockerClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	}()

	// The CP-issued ephemeral client cert must satisfy the agent's client
	// verification; exercise the streaming RPC too since it holds a stream.
	exerciseDockerClient(t, ctx, client)
}

func TestDialDockerClientMTLSWrongServerNameFails(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	serverCertPEM, serverKeyPEM, err := authority.IssueServerCert([]string{"real-node"})
	if err != nil {
		t.Fatalf("IssueServerCert: %v", err)
	}
	serverPair, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("load server keypair: %v", err)
	}
	target, dialer := startFakeDockerServer(t, credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverPair},
		MinVersion:   tls.VersionTLS12,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := DialDockerClient(ctx, target, authority,
		WithDockerServerName("wrong-name"),
		WithDockerDialOptions(dialer),
	)
	if err != nil {
		t.Fatalf("DialDockerClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// grpc.NewClient is lazy, so the handshake failure surfaces on first RPC.
	if _, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{}); err == nil {
		t.Fatal("ListContainers with wrong server name = nil error, want TLS verification failure")
	}
}

func TestDialDockerClientValidation(t *testing.T) {
	ctx := context.Background()

	if _, err := DialDockerClient(ctx, "  ", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty target = %v, want ErrValidation", err)
	}
	var nilCtx context.Context // stays nil: exercises the nil-context guard.
	//nolint:staticcheck // intentionally passing a nil Context to test the guard.
	if _, err := DialDockerClient(nilCtx, "passthrough:///bufnet", nil); err == nil {
		t.Fatal("nil context = nil error, want error")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DialDockerClient(cancelled, "passthrough:///bufnet", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context = %v, want context.Canceled", err)
	}
}

func TestNormalizeDockerTarget(t *testing.T) {
	cases := map[string]string{
		"10.0.0.5":              "10.0.0.5:9443",
		"10.0.0.5:9443":         "10.0.0.5:9443",
		"example.com":           "example.com:9443",
		"example.com:8443":      "example.com:8443",
		"passthrough:///bufnet": "passthrough:///bufnet",
	}
	for in, want := range cases {
		if got := normalizeDockerTarget(in); got != want {
			t.Errorf("normalizeDockerTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentTargetFor(t *testing.T) {
	nodeID := "node-42"
	strPtr := func(s string) *string { return &s }

	t.Run("ip and node id", func(t *testing.T) {
		target, name, err := agentTargetFor(&Server{IP: "10.0.0.5", NodeID: strPtr(nodeID)})
		if err != nil {
			t.Fatalf("agentTargetFor: %v", err)
		}
		if target != "10.0.0.5:9443" {
			t.Errorf("target = %q, want 10.0.0.5:9443", target)
		}
		if name != nodeID {
			t.Errorf("serverName = %q, want %q", name, nodeID)
		}
	})

	t.Run("ip only falls back to host", func(t *testing.T) {
		target, name, err := agentTargetFor(&Server{IP: "10.0.0.6"})
		if err != nil {
			t.Fatalf("agentTargetFor: %v", err)
		}
		if target != "10.0.0.6:9443" {
			t.Errorf("target = %q, want 10.0.0.6:9443", target)
		}
		if name != "10.0.0.6" {
			t.Errorf("serverName = %q, want 10.0.0.6", name)
		}
	})

	t.Run("empty ip uses node id as host", func(t *testing.T) {
		target, name, err := agentTargetFor(&Server{NodeID: strPtr("10.0.0.9")})
		if err != nil {
			t.Fatalf("agentTargetFor: %v", err)
		}
		if target != "10.0.0.9:9443" {
			t.Errorf("target = %q, want 10.0.0.9:9443", target)
		}
		if name != "10.0.0.9" {
			t.Errorf("serverName = %q, want 10.0.0.9", name)
		}
	})

	t.Run("no address errors", func(t *testing.T) {
		if _, _, err := agentTargetFor(&Server{}); !errors.Is(err, ErrValidation) {
			t.Fatalf("no address = %v, want ErrValidation", err)
		}
		if _, _, err := agentTargetFor(nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("nil server = %v, want ErrValidation", err)
		}
	})
}

func TestDockerClientCloseNilSafe(t *testing.T) {
	var client *DockerClient
	if err := client.Close(); err != nil {
		t.Fatalf("nil Close = %v, want nil", err)
	}
	if client.Conn() != nil {
		t.Fatal("nil Conn = non-nil, want nil")
	}
}

func TestServerNameFromTarget(t *testing.T) {
	cases := map[string]string{
		"10.0.0.5:9443":    "10.0.0.5",
		"example.com:9443": "example.com",
		"[::1]:9443":       "::1",
	}
	for in, want := range cases {
		if got := serverNameFromTarget(in); got != want {
			t.Errorf("serverNameFromTarget(%q) = %q, want %q", in, got, want)
		}
	}
	if got := strings.TrimSpace(serverNameFromTarget("passthrough:///bufnet")); got != "" {
		t.Errorf("bufnet server name = %q, want empty", got)
	}
}
