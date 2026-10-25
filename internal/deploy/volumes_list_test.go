package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	filespkg "github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func managedVolumeTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(envManagedVolumeRoot, root)
	return root
}

func volumeTestService(t *testing.T, repo *fakeRepository, node *mockNode) *Service {
	t.Helper()
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial:       dialAlways(node),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func fileRoot(name, path string, writable bool) *agentv1.FileRoot {
	return &agentv1.FileRoot{Name: name, Path: path, Writable: writable}
}

func fullAccessRoots() []*agentv1.FileRoot {
	return []*agentv1.FileRoot{fileRoot("system", "/", true)}
}

func confinedRoots(managedRoot string) []*agentv1.FileRoot {
	return []*agentv1.FileRoot{
		fileRoot("volumes", managedRoot, true),
		fileRoot("compose", "/var/lib/gotham-agent/compose", false),
	}
}

func listedContainer(name, state string, labels map[string]string, mounts ...*agentv1.ContainerMount) *agentv1.ContainerInfo {
	return &agentv1.ContainerInfo{
		Id:     name + "-id",
		Name:   name,
		Image:  "app:1",
		State:  state,
		Status: state,
		Labels: labels,
		Mounts: mounts,
	}
}

func appMount(kind, source, name, device, destination string, readOnly bool) *agentv1.ContainerMount {
	return &agentv1.ContainerMount{
		Type:         kind,
		Source:       source,
		VolumeName:   name,
		VolumeDevice: device,
		Destination:  destination,
		ReadOnly:     readOnly,
	}
}

func TestListVolumesSingleContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	bindPath := filepath.Join(root, app.ID.String(), "data")
	volumeName := appNamedVolumePrefix + app.ID.String() + "-cache"
	cacheDevice := filepath.Join(root, app.ID.String(), "cache")
	mountpoint := "/var/lib/docker/volumes/" + volumeName + "/_data"
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "data", HostPath: bindPath, ContainerPath: "/data"},
		{ApplicationID: app.ID, Name: "cache", HostPath: volumeName, ContainerPath: "/cache"},
	}}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()},
			appMount("bind", bindPath, "", "", "/data", false),
			appMount("volume", mountpoint, volumeName, cacheDevice, "/cache", true),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if listing.Runtime != VolumeRuntimeAvailable || len(listing.Volumes) != 2 {
		t.Fatalf("listing = %+v, want available with 2 volumes", listing)
	}
	bind := listing.Volumes[0]
	if bind.Kind != VolumeKindManaged || bind.State != VolumeStateRunning {
		t.Errorf("bind = %+v, want managed/running", bind)
	}
	if bind.FileManager == nil || bind.FileManager.ServerID != app.ServerID {
		t.Fatalf("bind file manager = %+v, want the application's own server", bind.FileManager)
	}
	if bind.FileManager.Root != "volumes" || bind.FileManager.Path != filepath.ToSlash(filepath.Join(app.ID.String(), "data")) {
		t.Errorf("bind file manager = %+v, want volumes/<appID>/data", bind.FileManager)
	}
	cache := listing.Volumes[1]
	if cache.Kind != VolumeKindManaged || cache.State != VolumeStateRunning {
		t.Errorf("cache = %+v, want managed/running", cache)
	}
	if cache.HostPath != cacheDevice {
		t.Errorf("cache host = %q, want the resolved device %q", cache.HostPath, cacheDevice)
	}
	if cache.FileManager == nil || cache.FileManager.Root != "volumes" ||
		cache.FileManager.Path != filepath.ToSlash(filepath.Join(app.ID.String(), "cache")) {
		t.Errorf("cache file manager = %+v, want volumes/<appID>/cache", cache.FileManager)
	}
	if len(cache.Mounts) != 1 || !cache.Mounts[0].ReadOnly || cache.Mounts[0].Destination != "/cache" {
		t.Errorf("cache mounts = %+v, want one read-only /cache mount", cache.Mounts)
	}
}

func TestListVolumesFullAccessRoot(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	bindPath := filepath.Join(root, app.ID.String(), "data")
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "data", HostPath: bindPath, ContainerPath: "/data"},
	}}
	node := newMockNode()
	node.fileRoots = fullAccessRoots()
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	entry := listing.Volumes[0]
	if entry.FileManager == nil {
		t.Fatalf("file manager = nil, want a target through the system root")
	}
	if entry.FileManager.Root != "system" {
		t.Errorf("root = %q, want the node's actual system root", entry.FileManager.Root)
	}
	wantPath := strings.TrimPrefix(filepath.ToSlash(bindPath), "/")
	if entry.FileManager.Path != wantPath {
		t.Errorf("path = %q, want %q", entry.FileManager.Path, wantPath)
	}
}

func TestListVolumesComposeSharedVolume(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceCompose
	app.Provider = ""
	app.ComposeContent = `services:
  web:
    image: nginx
    volumes:
      - shared:/var/www/html
  worker:
    image: busybox
    volumes:
      - shared:/work
volumes:
  shared: {}
`
	repo := &fakeRepository{app: app}
	root := managedVolumeTestRoot(t)
	device := filepath.Join(root, app.ID.String(), "shared")
	project := services.ProjectName(app.ID)
	labels := func(service string) map[string]string {
		return map[string]string{composeProjectLabel: project, composeServiceLabel: service}
	}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer(project+"-web-1", "running", labels("web"),
			appMount("volume", "/var/lib/docker/volumes/"+project+"_shared/_data", project+"_shared", device, "/var/www/html", false)),
		listedContainer(project+"-worker-1", "exited", labels("worker"),
			appMount("volume", "/var/lib/docker/volumes/"+project+"_shared/_data", project+"_shared", device, "/work", true)),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if listing.SourceType != SourceCompose {
		t.Errorf("source = %q, want compose", listing.SourceType)
	}
	if len(listing.Volumes) != 1 {
		t.Fatalf("volumes = %+v, want one grouped shared volume", listing.Volumes)
	}
	entry := listing.Volumes[0]
	if entry.Name != "shared" || entry.HostPath != device || entry.Kind != VolumeKindManaged {
		t.Errorf("entry = %+v, want the managed shared volume", entry)
	}
	if entry.State != VolumeStateRunning {
		t.Errorf("state = %q, want running (one consumer runs)", entry.State)
	}
	if len(entry.Mounts) != 2 || entry.Mounts[0].Service != "web" || entry.Mounts[1].Service != "worker" {
		t.Fatalf("mounts = %+v, want both services", entry.Mounts)
	}
	if entry.Mounts[1].State != VolumeStateStopped || !entry.Mounts[1].ReadOnly {
		t.Errorf("worker mount = %+v, want stopped and read-only", entry.Mounts[1])
	}
	if entry.FileManager == nil || entry.FileManager.Root != "volumes" ||
		entry.FileManager.Path != filepath.ToSlash(filepath.Join(app.ID.String(), "shared")) {
		t.Fatalf("file manager = %+v, want volumes/<appID>/shared", entry.FileManager)
	}
}

func TestListVolumesDistinctComposeVolumes(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceCompose
	app.ComposeContent = `services:
  web:
    image: nginx
    volumes:
      - data:/data
      - my_data:/my-data
volumes:
  data: {}
  my_data: {}
`
	repo := &fakeRepository{app: app}
	root := managedVolumeTestRoot(t)
	project := services.ProjectName(app.ID)
	dataDevice := filepath.Join(root, app.ID.String(), "data")
	myDataDevice := filepath.Join(root, app.ID.String(), "my_data")
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer(project+"-web-1", "running", map[string]string{
			composeProjectLabel: project, composeServiceLabel: "web",
		},
			appMount("volume", "/docker/data", project+"_data", dataDevice, "/data", false),
			appMount("volume", "/docker/my_data", project+"_my_data", myDataDevice, "/my-data", false),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 2 {
		t.Fatalf("volumes = %+v, want two distinct rows", listing.Volumes)
	}
	byName := map[string]VolumeEntry{}
	for _, entry := range listing.Volumes {
		byName[entry.Name] = entry
	}
	data, ok := byName["data"]
	if !ok || data.HostPath != dataDevice {
		t.Errorf("data = %+v, want host %q", data, dataDevice)
	}
	if len(data.Mounts) != 1 || data.Mounts[0].Destination != "/data" {
		t.Errorf("data mounts = %+v, want only /data", data.Mounts)
	}
	myData, ok := byName["my_data"]
	if !ok || myData.HostPath != myDataDevice {
		t.Errorf("my_data = %+v, want host %q", myData, myDataDevice)
	}
	if len(myData.Mounts) != 1 || myData.Mounts[0].Destination != "/my-data" {
		t.Errorf("my_data mounts = %+v, want only /my-data", myData.Mounts)
	}
}

func TestListVolumesKeepsConfiguredStorageWithoutRuntime(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	bindPath := filepath.Join(root, app.ID.String(), "data")
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "data", HostPath: bindPath, ContainerPath: "/data"},
	}}

	t.Run("no container", func(t *testing.T) {
		node := newMockNode()
		node.fileRoots = confinedRoots(root)
		svc := volumeTestService(t, repo, node)
		listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		if listing.Runtime != VolumeRuntimeAvailable || len(listing.Volumes) != 1 {
			t.Fatalf("listing = %+v, want the configured row", listing)
		}
		if listing.Volumes[0].State != VolumeStateUnverified {
			t.Errorf("state = %q, want unverified", listing.Volumes[0].State)
		}
		if listing.Volumes[0].FileManager == nil {
			t.Error("file manager = nil; want the retained managed directory to stay openable")
		}
	})

	t.Run("agent unavailable", func(t *testing.T) {
		node := newMockNode()
		node.listErr = errors.New("dial: connection refused")
		svc := volumeTestService(t, repo, node)
		listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		if listing.Runtime != VolumeRuntimeUnavailable || listing.RuntimeReason == "" {
			t.Fatalf("listing = %+v, want an unavailable runtime with a reason", listing)
		}
		if len(listing.Volumes) != 1 || listing.Volumes[0].State != VolumeStateUnverified {
			t.Fatalf("volumes = %+v, want the configured row kept unverified", listing.Volumes)
		}
		if listing.Volumes[0].FileManager != nil || listing.Volumes[0].OpenReason != VolumeOpenRootUnavailable {
			t.Errorf("entry = %+v, want no target and the root_unavailable reason", listing.Volumes[0])
		}
	})

	t.Run("dial failure", func(t *testing.T) {
		svc := NewService(Config{
			Repository: repo,
			Secret:     testSecretKey,
			Logger:     discardLogger(),
			Dial: func(context.Context, uuid.UUID) (Node, error) {
				return nil, errors.New("dial: connection refused")
			},
		})
		t.Cleanup(func() { _ = svc.Close() })
		listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		if listing.Runtime != VolumeRuntimeUnavailable {
			t.Fatalf("runtime = %q, want unavailable", listing.Runtime)
		}
		if len(listing.Volumes) != 1 || listing.Volumes[0].FileManager != nil {
			t.Errorf("volumes = %+v, want the row kept with no target", listing.Volumes)
		}
	})

	t.Run("no node assigned", func(t *testing.T) {
		unassigned := app
		unassigned.ServerID = uuid.Nil
		otherRepo := &fakeRepository{app: unassigned, storages: repo.storages}
		svc := volumeTestService(t, otherRepo, newMockNode())
		listing, err := svc.ListVolumes(context.Background(), userID, unassigned.ID)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		if listing.Runtime != VolumeRuntimeUnavailable || len(listing.Volumes) != 1 {
			t.Fatalf("listing = %+v, want the configured row and an unavailable runtime", listing)
		}
		if listing.Volumes[0].FileManager != nil || listing.Volumes[0].OpenReason != VolumeOpenNoNode {
			t.Errorf("entry = %+v, want no_node and no target", listing.Volumes[0])
		}
	})

	t.Run("file manager disabled on the control plane", func(t *testing.T) {
		t.Setenv(filespkg.FeatureEnv, "false")
		node := newMockNode()
		node.fileRoots = confinedRoots(root)
		svc := volumeTestService(t, repo, node)
		listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		if listing.Volumes[0].FileManager != nil || listing.Volumes[0].OpenReason != VolumeOpenFilesDisabled {
			t.Errorf("entry = %+v, want files_disabled and no target", listing.Volumes[0])
		}
	})
}

func TestListVolumesOlderAgent(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	bindPath := filepath.Join(root, app.ID.String(), "data")
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "data", HostPath: bindPath, ContainerPath: "/data"},
	}}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()},
			&agentv1.ContainerMount{Source: bindPath, Destination: "/data"},
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if listing.Runtime != VolumeRuntimeLimited || listing.RuntimeReason == "" {
		t.Fatalf("runtime = %q reason %q, want limited", listing.Runtime, listing.RuntimeReason)
	}
	if len(listing.Volumes) != 1 {
		t.Fatalf("volumes = %+v, want the configured row only", listing.Volumes)
	}
	if listing.Volumes[0].HostPath != bindPath || listing.Volumes[0].FileManager == nil {
		t.Errorf("entry = %+v, want the configured path and target", listing.Volumes[0])
	}
}

func TestListVolumesNoRootsRPC(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	bindPath := filepath.Join(root, app.ID.String(), "data")
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "data", HostPath: bindPath, ContainerPath: "/data"},
	}}
	node := newMockNode()
	node.fileRootsErr = status.Error(codes.Unimplemented, "unknown service agent.v1.FileService")
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 1 {
		t.Fatalf("volumes = %+v, want the configured row", listing.Volumes)
	}
	if listing.Volumes[0].FileManager != nil || listing.Volumes[0].OpenReason != VolumeOpenFilesDisabled {
		t.Errorf("entry = %+v, want files_disabled and no target", listing.Volumes[0])
	}
}

func TestListVolumesUnverifiedDevice(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	volumeName := "gotham-app-" + app.ID.String() + "-cache"
	repo := &fakeRepository{app: app, storages: []Storage{
		{ApplicationID: app.ID, Name: "cache", HostPath: volumeName, ContainerPath: "/cache"},
	}}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	mount := appMount("volume", "/var/lib/docker/volumes/"+volumeName+"/_data", volumeName, "", "/cache", false)
	mount.VolumeDeviceUnverified = true
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()}, mount),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	entry := listing.Volumes[0]
	if entry.State != VolumeStateUnverified {
		t.Errorf("state = %q, want unverified", entry.State)
	}
	if entry.HostPath != "" {
		t.Errorf("host path = %q, want none instead of the Docker mountpoint", entry.HostPath)
	}
	if entry.FileManager != nil || entry.OpenReason != VolumeOpenDeviceUnverified {
		t.Errorf("entry = %+v, want volume_unverified and no target", entry)
	}
}

func TestListVolumesClassifiesUnmappedMounts(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()},
			appMount("tmpfs", "", "", "", "/tmp", false),
			appMount("bind", "/etc", "", "", "/host-etc", true),
			appMount("tmpfs", "", "", "", "/tmp", false),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 2 {
		t.Fatalf("volumes = %+v, want tmpfs and the outside bind", listing.Volumes)
	}
	tmpfs := listing.Volumes[0]
	if tmpfs.Kind != VolumeKindTmpfs || tmpfs.FileManager != nil || tmpfs.OpenReason != VolumeOpenNotBrowsable {
		t.Errorf("tmpfs = %+v, want not_browsable and no target", tmpfs)
	}
	if len(tmpfs.Mounts) != 1 {
		t.Errorf("tmpfs mounts = %+v, want the duplicate collapsed", tmpfs.Mounts)
	}
	outside := listing.Volumes[1]
	if outside.Kind != VolumeKindBind || outside.HostPath != "" || outside.FileManager != nil || outside.OpenReason != VolumeOpenNotBrowsable {
		t.Errorf("outside bind = %+v, want no host path and no target", outside)
	}
}

func TestListVolumesIgnoresOtherApplications(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("other-web-1", "running", map[string]string{labelAppID: uuid.New().String()},
			appMount("bind", filepath.Join(root, "other", "data"), "", "", "/data", false),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 0 {
		t.Fatalf("volumes = %+v, want no row from another application", listing.Volumes)
	}
}

func TestFileManagerTargetRejectsEscapingPaths(t *testing.T) {
	app := testApplication(uuid.New())
	root := managedVolumeTestRoot(t)
	cases := []struct {
		name string
		path string
	}{
		{"sibling application", filepath.Join(root, uuid.New().String(), "data")},
		{"sibling prefix", filepath.Join(root, app.ID.String()+"x", "data")},
		{"sibling suffix", filepath.Join(root, app.ID.String()+"-copy", "data")},
		{"sibling prefix through the app dir", filepath.Join(root, app.ID.String(), "..", app.ID.String()+"x", "data")},
		{"managed root itself", root},
		{"absolute outside", "/etc"},
		{"traversal", filepath.Join(root, "..", "etc")},
		{"relative", "data"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, reason := fileManagerTarget(app, VolumeEntry{HostPath: tc.path}, fullAccessRoots(), "")
			if target != nil || reason == "" {
				t.Fatalf("target = %+v reason %q, want none and a reason", target, reason)
			}
		})
	}

	inside := filepath.Join(root, app.ID.String(), "data")
	target, reason := fileManagerTarget(app, VolumeEntry{HostPath: inside}, confinedRoots(root), "")
	if target == nil || reason != "" {
		t.Fatalf("inside target = %+v reason %q, want a target", target, reason)
	}
	if target.ServerID != app.ServerID || target.Root != "volumes" || target.Path != app.ID.String()+"/data" {
		t.Errorf("target = %+v, want the app's server and volumes/<appID>/data", target)
	}

	full, reason := fileManagerTarget(app, VolumeEntry{HostPath: inside}, fullAccessRoots(), "")
	if full == nil || reason != "" || full.Root != "system" ||
		full.Path != strings.TrimPrefix(filepath.ToSlash(inside), "/") {
		t.Errorf("full target = %+v reason %q, want system + the managed path without its slash", full, reason)
	}
	sibling := filepath.Join(root, uuid.New().String(), "data")
	if target, reason := fileManagerTarget(app, VolumeEntry{HostPath: sibling}, fullAccessRoots(), ""); target != nil || reason == "" {
		t.Errorf("sibling target = %+v reason %q, want refused even under /", target, reason)
	}
}

func TestFileManagerTargetResolvesSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv(envManagedVolumeRoot, link)
	app := testApplication(uuid.New())
	host := filepath.Join(link, app.ID.String(), "data")
	roots := []*agentv1.FileRoot{fileRoot("volumes", real, true)}
	target, reason := fileManagerTarget(app, VolumeEntry{HostPath: host}, roots, "")
	if target == nil || reason != "" {
		t.Fatalf("target = %+v reason %q, want a target through the resolved root", target, reason)
	}
	if target.Root != "volumes" || target.Path != app.ID.String()+"/data" {
		t.Errorf("target = %+v, want volumes/<appID>/data", target)
	}
}

func TestFileManagerTargetSkipsReadOnlyRoot(t *testing.T) {
	app := testApplication(uuid.New())
	root := managedVolumeTestRoot(t)
	appDir := filepath.Join(root, app.ID.String())
	readOnly := []*agentv1.FileRoot{fileRoot("appdata", appDir, false)}
	target, reason := fileManagerTarget(app, VolumeEntry{HostPath: filepath.Join(appDir, "data")}, readOnly, "")
	if target != nil || reason != VolumeOpenRootUnavailable {
		t.Fatalf("target = %+v reason %q, want root_unavailable", target, reason)
	}
}

func TestListVolumesRuntimeUnverifiedDevice(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	repo := &fakeRepository{app: app}
	mount := appMount("volume", "/var/lib/docker/volumes/extra/_data", "extra", "", "/extra", false)
	mount.VolumeDeviceUnverified = true
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()}, mount),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 1 {
		t.Fatalf("volumes = %+v, want the runtime volume", listing.Volumes)
	}
	entry := listing.Volumes[0]
	if entry.HostPath != "" {
		t.Errorf("host path = %q, want none instead of Docker's mountpoint", entry.HostPath)
	}
	if entry.OpenReason != VolumeOpenDeviceUnverified || entry.FileManager != nil {
		t.Errorf("entry = %+v, want volume_unverified and no target", entry)
	}
	if entry.State != VolumeStateUnverified {
		t.Errorf("state = %q, want unverified", entry.State)
	}
}

func TestListVolumesNameFallbackWithoutDevice(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceCompose
	app.ComposeContent = `services:
  web:
    image: nginx
    volumes:
      - data:/data
      - my_data:/my-data
volumes:
  data: {}
  my_data: {}
`
	repo := &fakeRepository{app: app}
	root := managedVolumeTestRoot(t)
	project := services.ProjectName(app.ID)
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer(project+"-web-1", "running", map[string]string{
			composeProjectLabel: project, composeServiceLabel: "web",
		},
			appMount("volume", "/docker/data", project+"_data", "", "/data", false),
			appMount("volume", "/docker/my_data", project+"_my_data", "", "/my-data", false),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(listing.Volumes) != 2 {
		t.Fatalf("volumes = %+v, want two rows", listing.Volumes)
	}
	byName := map[string]VolumeEntry{}
	for _, entry := range listing.Volumes {
		byName[entry.Name] = entry
	}
	if data := byName["data"]; len(data.Mounts) != 1 || data.Mounts[0].Destination != "/data" {
		t.Errorf("data mounts = %+v, want only /data", data.Mounts)
	}
	if myData := byName["my_data"]; len(myData.Mounts) != 1 || myData.Mounts[0].Destination != "/my-data" {
		t.Errorf("my_data mounts = %+v, want only /my-data", myData.Mounts)
	}
}

func TestListVolumesRuntimeDeviceBeatsSource(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	device := filepath.Join(root, app.ID.String(), "extra")
	repo := &fakeRepository{app: app}
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	node.listed = []*agentv1.ContainerInfo{
		listedContainer("shop-web-1", "running", map[string]string{labelAppID: app.ID.String()},
			appMount("volume", "/var/lib/docker/volumes/extra/_data", "extra", device, "/extra", false),
		),
	}
	svc := volumeTestService(t, repo, node)

	listing, err := svc.ListVolumes(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	entry := listing.Volumes[0]
	if entry.HostPath != device {
		t.Errorf("host path = %q, want the device %q", entry.HostPath, device)
	}
	if entry.FileManager == nil || entry.FileManager.Root != "volumes" ||
		entry.FileManager.Path != app.ID.String()+"/extra" {
		t.Errorf("file manager = %+v, want volumes/<appID>/extra", entry.FileManager)
	}
}

func TestFileManagerTargetChoosesLongestRoot(t *testing.T) {
	app := testApplication(uuid.New())
	root := managedVolumeTestRoot(t)
	inside := filepath.Join(root, app.ID.String(), "data")
	roots := []*agentv1.FileRoot{
		fileRoot("system", "/", true),
		fileRoot("volumes", root, true),
	}
	target, reason := fileManagerTarget(app, VolumeEntry{HostPath: inside}, roots, "")
	if target == nil || reason != "" {
		t.Fatalf("target = %+v reason %q, want a target", target, reason)
	}
	if target.Root != "volumes" || target.Path != app.ID.String()+"/data" {
		t.Errorf("target = %+v, want the nested volumes root, not /", target)
	}
}

func TestListVolumesCancelledContext(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	root := managedVolumeTestRoot(t)
	node := newMockNode()
	node.fileRoots = confinedRoots(root)
	svc := volumeTestService(t, &fakeRepository{app: app}, node)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listing, err := svc.ListVolumes(ctx, userID, app.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListVolumes(cancelled) = (%+v, %v), want context.Canceled", listing, err)
	}
}
