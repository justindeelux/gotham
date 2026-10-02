package e2e

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/proxy"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeBindDocker is a dockerClient that records nothing; it exists only so the
// agent's DockerService accepts the request and the validation under test is
// the only thing that can reject it.
type fakeBindDocker struct{}

func (fakeBindDocker) Version(context.Context) (string, error) { return "test", nil }
func (fakeBindDocker) ListContainers(context.Context, bool) ([]*agentv1.ContainerInfo, error) {
	return nil, nil
}
func (fakeBindDocker) Start(context.Context, string) error   { return nil }
func (fakeBindDocker) Stop(context.Context, string) error    { return nil }
func (fakeBindDocker) Restart(context.Context, string) error { return nil }
func (fakeBindDocker) Remove(context.Context, string) error  { return nil }
func (fakeBindDocker) RemoveVolume(context.Context, string) error {
	return nil
}
func (fakeBindDocker) PullImage(context.Context, string) error {
	return nil
}
func (fakeBindDocker) CreateContainer(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	return "created", nil
}
func (fakeBindDocker) RunImage(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	return "created", nil
}
func (fakeBindDocker) Logs(context.Context, string, bool, int64) (<-chan agent.LogMessage, error) {
	ch := make(chan agent.LogMessage)
	close(ch)
	return ch, nil
}

// TestAgentAcceptsTraefikProxyVolumes is the FX-5a regression for the proxy:
// the node's own Traefik mounts (internal/proxy.TraefikVolumesFor) must pass
// the managed-volume validator, or every node loses routing and TLS.
func TestAgentAcceptsTraefikProxyVolumes(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterDockerServiceServer(server, agent.NewDockerServer(fakeBindDocker{}, testLogger(t)))
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
	client := agentv1.NewDockerServiceClient(conn)

	response, err := client.RunImage(context.Background(), &agentv1.CreateContainerRequest{
		Image:   proxy.TraefikImage,
		Name:    proxy.TraefikContainerName,
		Labels:  map[string]string{"gotham.managed": "true", "gotham.component": "proxy"},
		Volumes: proxy.TraefikVolumesFor(proxy.TraefikDir, proxy.TraefikAcmeDir),
	})
	if err != nil {
		t.Fatalf("proxy RunImage = %v, want the Traefik volumes accepted", err)
	}
	if response.GetContainerId() == "" {
		t.Error("proxy RunImage returned no container id")
	}

	// An application bind outside the managed root is still refused.
	if _, err := client.RunImage(context.Background(), &agentv1.CreateContainerRequest{
		Image:   "nginx",
		Volumes: []string{"/etc:/etc"},
		Labels:  map[string]string{"gotham.app_id": uuid.New().String()},
	}); status.Code(err) == 0 {
		t.Fatal("out-of-root application bind was accepted")
	}
}
