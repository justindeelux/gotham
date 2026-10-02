package agent

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestValidateContainerVolumes pins the node-side confinement: an absolute
// bind must live inside <root>/<appID>; named volumes pass; dangerous targets
// and unidentified binds are refused.
func TestValidateContainerVolumes(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())

	cases := []struct {
		name    string
		volumes []string
		labels  map[string]string
		wantErr bool
	}{
		{
			name:    "bind inside the app directory",
			volumes: []string{filepath.Join(appDir, "data") + ":/var/lib/app"},
			labels:  map[string]string{labelAppID: appID.String()},
		},
		{
			name:    "bind outside the managed root",
			volumes: []string{"/data/app:/var/lib/app"},
			labels:  map[string]string{labelAppID: appID.String()},
			wantErr: true,
		},
		{
			name:    "docker socket",
			volumes: []string{"/var/run/docker.sock:/var/run/docker.sock"},
			labels:  map[string]string{labelAppID: appID.String()},
			wantErr: true,
		},
		{
			name:    "etc escape",
			volumes: []string{"/etc:/etc"},
			labels:  map[string]string{labelAppID: appID.String()},
			wantErr: true,
		},
		{
			name:    "traversal out of the app directory",
			volumes: []string{filepath.Join(appDir, "..", "..", "etc") + ":/etc"},
			labels:  map[string]string{labelAppID: appID.String()},
			wantErr: true,
		},
		{
			name:    "bind without an application label",
			volumes: []string{filepath.Join(appDir, "data") + ":/var/lib/app"},
			labels:  map[string]string{},
			wantErr: true,
		},
		{
			name:    "non-uuid application label",
			volumes: []string{filepath.Join(appDir, "data") + ":/var/lib/app"},
			labels:  map[string]string{labelAppID: "../.."},
			wantErr: true,
		},
		{
			name:    "named volume",
			volumes: []string{"gotham-data:/var/lib/app"},
			labels:  map[string]string{labelAppID: appID.String()},
		},
		{
			name:    "no volumes",
			volumes: nil,
			labels:  map[string]string{labelAppID: appID.String()},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContainerVolumes(root, &agentv1.CreateContainerRequest{
				Volumes: tc.volumes,
				Labels:  tc.labels,
			})
			if tc.wantErr && !errors.Is(err, ErrInvalidVolumeBind) {
				t.Fatalf("err = %v, want ErrInvalidVolumeBind", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want accepted", err)
			}
		})
	}
}

// TestDockerServerRejectsOutOfRootBind drives the rule through the gRPC
// surface and pins the InvalidArgument mapping, so the control plane sees bad
// input rather than an internal failure.
func TestDockerServerRejectsOutOfRootBind(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterDockerServiceServer(server, NewDockerServer(&fakeDockerClient{}, discardLogger(),
		WithManagedVolumeRoot(root)))
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

	outside := &agentv1.CreateContainerRequest{
		Image:   "nginx",
		Volumes: []string{"/etc:/etc"},
		Labels:  map[string]string{labelAppID: appID.String()},
	}
	if _, err := client.RunImage(context.Background(), outside); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("out-of-root bind code = %v, want InvalidArgument (err=%v)", status.Code(err), err)
	}

	inside := &agentv1.CreateContainerRequest{
		Image:   "nginx",
		Volumes: []string{filepath.Join(root, appID.String(), "data") + ":/var/lib/app"},
		Labels:  map[string]string{labelAppID: appID.String()},
	}
	if _, err := client.RunImage(context.Background(), inside); err != nil {
		t.Fatalf("in-root bind run = %v, want accepted", err)
	}
}
