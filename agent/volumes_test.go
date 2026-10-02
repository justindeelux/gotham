package agent

import (
	"context"
	"errors"
	"net"
	"os"
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

// volumeRequest builds a create request carrying volumes and labels.
func volumeRequest(labels map[string]string, volumes ...string) *agentv1.CreateContainerRequest {
	return &agentv1.CreateContainerRequest{Volumes: volumes, Labels: labels}
}

// proxyRequest builds a request carrying the managed proxy identity, which the
// proxy branch requires before it may mount the node's proxy directory.
func proxyRequest(volumes ...string) *agentv1.CreateContainerRequest {
	return &agentv1.CreateContainerRequest{
		Name:    defaultTraefikContainerName,
		Volumes: volumes,
		Labels:  map[string]string{labelComponent: componentProxy, "gotham.managed": "true"},
	}
}

// TestValidateContainerVolumes pins the node-side rule per component: an
// application bind is a direct child of <root>/<appID> and its named volumes
// are namespaced; a proxy may mount the node's proxy directory; other managed
// containers (database, backup) may use named volumes but no host bind.
func TestValidateContainerVolumes(t *testing.T) {
	root := t.TempDir()
	proxyRoot := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	appLabels := map[string]string{labelAppID: appID.String()}
	managedLabels := map[string]string{"gotham.managed": "true"}

	cases := []struct {
		name    string
		req     *agentv1.CreateContainerRequest
		wantErr bool
	}{
		{name: "app direct child", req: volumeRequest(appLabels, filepath.Join(appDir, "data")+":/var/lib/app")},
		{name: "app outside the root", req: volumeRequest(appLabels, "/data/app:/var/lib/app"), wantErr: true},
		{name: "app docker socket", req: volumeRequest(appLabels, "/var/run/docker.sock:/var/run/docker.sock"), wantErr: true},
		{name: "app etc", req: volumeRequest(appLabels, "/etc:/etc"), wantErr: true},
		{name: "app traversal", req: volumeRequest(appLabels, filepath.Join(appDir, "..", "..", "etc")+":/etc"), wantErr: true},
		{name: "app nested child", req: volumeRequest(appLabels, filepath.Join(appDir, "a", "b")+":/data"), wantErr: true},
		{name: "app directory itself", req: volumeRequest(appLabels, appDir+":/data"), wantErr: true},
		{name: "app namespaced named volume", req: volumeRequest(appLabels, appNamedVolumePrefix+appID.String()+"-data:/var/lib/app")},
		{name: "app bare named volume", req: volumeRequest(appLabels, "shared:/var/lib/app"), wantErr: true},
		{name: "app bind without a label", req: volumeRequest(map[string]string{}, filepath.Join(appDir, "data")+":/data"), wantErr: true},
		{name: "app non-uuid label", req: volumeRequest(map[string]string{labelAppID: "../.."}, filepath.Join(appDir, "data")+":/data"), wantErr: true},
		{name: "proxy config directory", req: proxyRequest(filepath.Join(proxyRoot, "conf") + ":/etc/traefik:ro")},
		{name: "proxy acme child", req: proxyRequest(filepath.Join(proxyRoot, "conf", "acme") + ":/acme")},
		{name: "proxy outside its root", req: proxyRequest("/data/conf:/etc/traefik"), wantErr: true},
		{name: "proxy etc", req: proxyRequest("/etc:/etc/traefik"), wantErr: true},
		{name: "proxy label without the proxy identity", req: volumeRequest(map[string]string{labelComponent: componentProxy}, filepath.Join(proxyRoot, "conf")+":/etc/traefik"), wantErr: true},
		{name: "database named volume", req: volumeRequest(managedLabels, "gotham-db-x:/var/lib/postgresql/data")},
		{name: "database absolute bind", req: volumeRequest(managedLabels, "/data/x:/var/lib/postgresql/data"), wantErr: true},
		{name: "no volumes", req: volumeRequest(appLabels)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContainerVolumes(root, proxyRoot, tc.req)
			if tc.wantErr && !errors.Is(err, ErrInvalidVolumeBind) {
				t.Fatalf("err = %v, want ErrInvalidVolumeBind", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want accepted", err)
			}
		})
	}
}

// TestValidateContainerVolumesRejectsSymlinks pins the fail-closed walk on the
// node: a symlink at the bind path (live or dangling) or an app directory that
// is itself a symlink is refused.
func TestValidateContainerVolumesRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	proxyRoot := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	labels := map[string]string{labelAppID: appID.String()}

	live := filepath.Join(appDir, "live")
	if err := os.Symlink("/etc", live); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateContainerVolumes(root, proxyRoot, volumeRequest(labels, live+":/data")); !errors.Is(err, ErrInvalidVolumeBind) {
		t.Fatalf("live symlink err = %v, want ErrInvalidVolumeBind", err)
	}
	dangling := filepath.Join(appDir, "dangling")
	if err := os.Symlink("/nonexistent", dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := validateContainerVolumes(root, proxyRoot, volumeRequest(labels, dangling+":/data")); !errors.Is(err, ErrInvalidVolumeBind) {
		t.Fatalf("dangling symlink err = %v, want ErrInvalidVolumeBind", err)
	}

	// The app directory itself is a symlink.
	otherRoot := t.TempDir()
	otherApp := uuid.New()
	if err := os.Symlink("/etc", filepath.Join(otherRoot, otherApp.String())); err != nil {
		t.Fatalf("symlink app dir: %v", err)
	}
	otherLabels := map[string]string{labelAppID: otherApp.String()}
	if err := validateContainerVolumes(otherRoot, proxyRoot, volumeRequest(otherLabels, filepath.Join(otherRoot, otherApp.String(), "child")+":/data")); !errors.Is(err, ErrInvalidVolumeBind) {
		t.Fatalf("symlinked app dir err = %v, want ErrInvalidVolumeBind", err)
	}
}

// TestValidateContainerVolumesFailsClosedOnLstatError pins that a non-ENOENT
// Lstat error is refused (here ENOTDIR: the app path is a regular file).
func TestValidateContainerVolumesFailsClosedOnLstatError(t *testing.T) {
	root := t.TempDir()
	proxyRoot := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	if err := os.WriteFile(appDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	labels := map[string]string{labelAppID: appID.String()}
	if err := validateContainerVolumes(root, proxyRoot, volumeRequest(labels, filepath.Join(appDir, "child")+":/data")); !errors.Is(err, ErrInvalidVolumeBind) {
		t.Fatalf("err = %v, want ErrInvalidVolumeBind for a non-directory component", err)
	}
}

// TestDockerServerRejectsOutOfRootBind drives the rule through the gRPC
// surface and pins the InvalidArgument mapping, so the control plane sees bad
// input rather than an internal failure.
func TestDockerServerRejectsOutOfRootBind(t *testing.T) {
	root := t.TempDir()
	proxyRoot := t.TempDir()
	appID := uuid.New()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterDockerServiceServer(server, NewDockerServer(&fakeDockerClient{}, discardLogger(),
		WithManagedVolumeRoot(root), WithProxyVolumeRoot(proxyRoot)))
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

	proxy := &agentv1.CreateContainerRequest{
		Image:   "traefik",
		Name:    defaultTraefikContainerName,
		Volumes: []string{filepath.Join(proxyRoot, "conf") + ":/etc/traefik:ro"},
		Labels:  map[string]string{labelComponent: componentProxy, "gotham.managed": "true"},
	}
	if _, err := client.RunImage(context.Background(), proxy); err != nil {
		t.Fatalf("proxy bind run = %v, want accepted", err)
	}
}
