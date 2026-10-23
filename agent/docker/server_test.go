package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type fakeDockerClient struct {
	containers         []*agentv1.ContainerInfo
	listAll            bool
	listLabels         map[string]string
	listResolveDevices bool
	version            string
	err                error
	logs               []byte
	logStreamErr       error
	started            []string
	stopped            []string
	restarted          []string
	removed            []string
	removedVolumes     []string
	createdID          string
}

func (f *fakeDockerClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

func (f *fakeDockerClient) ListContainers(_ context.Context, all bool, labels map[string]string, resolveDevices bool) ([]*agentv1.ContainerInfo, error) {
	f.listAll = all
	f.listLabels = labels
	f.listResolveDevices = resolveDevices
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

func (f *fakeDockerClient) RemoveVolume(_ context.Context, name string) error {
	if f.err != nil {
		return f.err
	}
	f.removedVolumes = append(f.removedVolumes, name)
	return nil
}

func (f *fakeDockerClient) PullImage(context.Context, string, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "sha256:testdigest", nil
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

func (f *fakeDockerClient) Logs(context.Context, string, bool, int64) (<-chan LogMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	chunks := make(chan LogMessage, 2)
	if len(f.logs) > 0 {
		chunks <- LogMessage{Data: f.logs}
	}
	if f.logStreamErr != nil {
		chunks <- LogMessage{Err: f.logStreamErr}
	}
	close(chunks)
	return chunks, nil
}

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

	labels := map[string]string{"com.docker.compose.project": "gotham-11111111-2222-4333-8444-555555555555"}
	response, err := client.ListContainers(context.Background(), &agentv1.ListContainersRequest{All: true, Labels: labels})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(response.GetContainers()) != 1 || response.GetContainers()[0].GetId() != "abc" {
		t.Errorf("containers = %v; want one with id abc", response.GetContainers())
	}
	if !fake.listAll || fake.listLabels["com.docker.compose.project"] != labels["com.docker.compose.project"] {
		t.Errorf("docker client got (all=%v, labels=%v), want (true, %v)", fake.listAll, fake.listLabels, labels)
	}
}

func TestDockerServerListContainersResolveDevices(t *testing.T) {
	for _, want := range []bool{true, false} {
		fake := &fakeDockerClient{}
		client := newDockerServiceClient(t, fake)
		if _, err := client.ListContainers(context.Background(), &agentv1.ListContainersRequest{ResolveVolumeDevices: want}); err != nil {
			t.Fatalf("ListContainers(resolve=%v): %v", want, err)
		}
		if fake.listResolveDevices != want {
			t.Errorf("docker client got resolveDevices=%v, want %v", fake.listResolveDevices, want)
		}
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
			name: "create negative pids",
			call: func() error {
				_, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{Image: "nginx", PidsLimit: -1})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "run negative memory",
			call: func() error {
				_, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{Image: "nginx", MemoryBytes: -1})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "create pids over the cap",
			call: func() error {
				_, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{Image: "nginx", PidsLimit: maxContainerPidsLimit + 1})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "run cpus over the cap",
			call: func() error {
				_, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{Image: "nginx", NanoCpus: maxContainerNanoCPUs + 1})
				return err
			},
			code: codes.InvalidArgument,
		},
		{
			name: "resource caps at the boundary are accepted",
			call: func() error {
				_, err := client.CreateContainer(ctx, &agentv1.CreateContainerRequest{
					Image:       "nginx",
					PidsLimit:   maxContainerPidsLimit,
					MemoryBytes: maxContainerMemoryBytes,
					NanoCpus:    maxContainerNanoCPUs,
				})
				return err
			},
			code: codes.OK,
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
		{"invalid port mapping", fmt.Errorf("%w: %q", ErrInvalidPortMapping, "80:0"), codes.InvalidArgument},
		{"docker not found", fmt.Errorf("%w: missing", ErrDockerNotFound), codes.NotFound},
		{"image not found", fmt.Errorf("%w: missing", ErrDockerImageNotFound), codes.InvalidArgument},
		{"daemon unavailable", fmt.Errorf("%w: connection refused", ErrDockerUnavailable), codes.Unavailable},
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

func TestDockerServerStreamLogsBrokenStream(t *testing.T) {
	fake := &fakeDockerClient{logs: []byte("partial"), logStreamErr: errors.New("connection reset")}
	client := newDockerServiceClient(t, fake)

	stream, err := client.StreamLogs(context.Background(), &agentv1.StreamLogsRequest{
		ContainerId: "abc",
		Follow:      true,
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("first Recv: %v", err)
	}
	if string(chunk.GetData()) != "partial" {
		t.Errorf("first chunk = %q; want partial", chunk.GetData())
	}

	if _, err := stream.Recv(); status.Code(err) != codes.Internal {
		t.Errorf("terminal code = %v; want Internal (err=%v)", status.Code(err), err)
	}
}
