package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeDockerClient is a configurable dockerClient for server tests.
type fakeDockerClient struct {
	containers []*agentv1.ContainerInfo
	version    string
	err        error
	logs       []byte
	started    []string
	stopped    []string
	restarted  []string
	removed    []string
	createdID  string
}

func (f *fakeDockerClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

func (f *fakeDockerClient) ListContainers(context.Context, bool) ([]*agentv1.ContainerInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.containers, nil
}

func (f *fakeDockerClient) Start(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, id)
	return nil
}

func (f *fakeDockerClient) Stop(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDockerClient) Restart(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.restarted = append(f.restarted, id)
	return nil
}

func (f *fakeDockerClient) Remove(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeDockerClient) PullImage(context.Context, string) error {
	return f.err
}

func (f *fakeDockerClient) CreateContainer(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.createdID != "" {
		return f.createdID, nil
	}
	return "created", nil
}

func (f *fakeDockerClient) RunImage(_ context.Context, _ *agentv1.CreateContainerRequest) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	id := f.createdID
	if id == "" {
		id = "created"
	}
	f.started = append(f.started, id)
	return id, nil
}

func (f *fakeDockerClient) Logs(context.Context, string, bool, int64) (<-chan []byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	chunks := make(chan []byte, 1)
	chunks <- f.logs
	close(chunks)
	return chunks, nil
}

// discardLogger returns a logger that drops every record.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newDockerServiceClient starts an in-process DockerService server backed by
// fake and returns a client connected over bufconn.
func newDockerServiceClient(t *testing.T, fake *fakeDockerClient) agentv1.DockerServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterDockerServiceServer(server, NewDockerServer(fake, discardLogger()))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

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
	return agentv1.NewDockerServiceClient(conn)
}

func TestDockerServerListContainers(t *testing.T) {
	fake := &fakeDockerClient{containers: []*agentv1.ContainerInfo{{Id: "abc", Name: "web"}}}
	client := newDockerServiceClient(t, fake)

	response, err := client.ListContainers(context.Background(), &agentv1.ListContainersRequest{All: true})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(response.GetContainers()) != 1 || response.GetContainers()[0].GetId() != "abc" {
		t.Errorf("containers = %v; want one with id abc", response.GetContainers())
	}
}

func TestDockerServerContainerActions(t *testing.T) {
	fake := &fakeDockerClient{}
	client := newDockerServiceClient(t, fake)
	ctx := context.Background()
	request := &agentv1.ContainerActionRequest{ContainerId: "abc"}

	if _, err := client.StartContainer(ctx, request); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	if _, err := client.StopContainer(ctx, request); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if _, err := client.RestartContainer(ctx, request); err != nil {
		t.Fatalf("RestartContainer: %v", err)
	}
	if _, err := client.RemoveContainer(ctx, request); err != nil {
		t.Fatalf("RemoveContainer: %v", err)
	}

	if len(fake.started) != 1 || fake.started[0] != "abc" {
		t.Errorf("started = %v; want [abc]", fake.started)
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "abc" {
		t.Errorf("stopped = %v; want [abc]", fake.stopped)
	}
	if len(fake.restarted) != 1 || fake.restarted[0] != "abc" {
		t.Errorf("restarted = %v; want [abc]", fake.restarted)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "abc" {
		t.Errorf("removed = %v; want [abc]", fake.removed)
	}
}

func TestDockerServerRunImage(t *testing.T) {
	fake := &fakeDockerClient{createdID: "run1"}
	client := newDockerServiceClient(t, fake)

	response, err := client.RunImage(context.Background(), &agentv1.CreateContainerRequest{Image: "nginx"})
	if err != nil {
		t.Fatalf("RunImage: %v", err)
	}
	if response.GetContainerId() != "run1" {
		t.Errorf("container id = %q; want run1", response.GetContainerId())
	}
	if len(fake.started) != 1 || fake.started[0] != "run1" {
		t.Errorf("started = %v; want [run1]", fake.started)
	}
}

func TestDockerServerValidation(t *testing.T) {
	fake := &fakeDockerClient{}
	client := newDockerServiceClient(t, fake)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
		code codes.Code
	}{
		{
			name: "start without id",
			call: func() error {
				_, err := client.StartContainer(ctx, &agentv1.ContainerActionRequest{})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "remove without id",
			call: func() error {
				_, err := client.RemoveContainer(ctx, &agentv1.ContainerActionRequest{})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "create without image",
			call: func() error {
				_, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "pull without image",
			call: func() error {
				_, err := client.PullImage(ctx, &agentv1.PullImageRequest{})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "logs without id",
			call: func() error {
				stream, err := client.StreamLogs(ctx, &agentv1.StreamLogsRequest{})
				if err != nil {
					return err
				}
				_, err = stream.Recv()
				return err
			},
			code: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if status.Code(err) != tt.code {
				t.Errorf("code = %v; want %v (err=%v)", status.Code(err), tt.code, err)
			}
		})
	}
}

func TestDockerServerErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{"internal", errors.New("boom"), codes.Internal},
		{"canceled", context.Canceled, codes.Canceled},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeDockerClient{err: tt.err}
			client := newDockerServiceClient(t, fake)

			_, err := client.ListContainers(context.Background(), &agentv1.ListContainersRequest{})
			if status.Code(err) != tt.code {
				t.Errorf("code = %v; want %v (err=%v)", status.Code(err), tt.code, err)
			}
		})
	}
}

func TestDockerServerStreamLogs(t *testing.T) {
	fake := &fakeDockerClient{logs: []byte("payload")}
	client := newDockerServiceClient(t, fake)

	stream, err := client.StreamLogs(context.Background(), &agentv1.StreamLogsRequest{
		ContainerId: "abc",
		Follow:      true,
		Tail:        10,
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}

	var data []byte
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		data = append(data, chunk.GetData()...)
	}
	if string(data) != "payload" {
		t.Errorf("logs = %q; want payload", data)
	}
}

func TestServerServeAndShutdown(t *testing.T) {
	fake := &fakeDockerClient{containers: []*agentv1.ContainerInfo{{Id: "abc"}}}
	// Development mode: no certificate and no CA → plaintext listener, which
	// is what the control plane's insecure dial expects.
	creds, err := ServerCredentials(nil, nil, "")
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	server, err := NewServer("127.0.0.1:0", creds, NewDockerServer(fake, discardLogger()), discardLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	conn, err := grpc.NewClient(server.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := agentv1.NewDockerServiceClient(conn)
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	response, err := client.ListContainers(callCtx, &agentv1.ListContainersRequest{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(response.GetContainers()) != 1 {
		t.Errorf("containers = %v; want one", response.GetContainers())
	}

	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
