package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRealContainerFilesArchiveAndExec exercises the archive and exec paths
// against a live daemon. It is skipped unless GOTHAM_E2E=1.
func TestRealContainerFilesArchiveAndExec(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	server := NewDockerServer(client, nil)
	ctx := context.Background()

	if _, err := server.ChangeContainerFile(ctx, &agentv1.ContainerFilesChangeRequest{
		ContainerId: containerID,
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR,
		Path:        "/data/work",
	}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: containerID,
		Path:        "/data/work/app.conf",
		Size:        int64(len("port=8080\n")),
		Uid:         1234,
		Gid:         2345,
		Mode:        0o640,
	}, []string{"port=", "8080\n"})
	if err := server.WriteContainerFile(stream); err != nil {
		t.Fatalf("write: %v", err)
	}

	stat, err := server.StatContainerFile(ctx, &agentv1.ContainerFilesPathRequest{ContainerId: containerID, Path: "/data/work/app.conf"})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.GetEntry().GetMode() != 0o640 || stat.GetEntry().GetSize() != int64(len("port=8080\n")) {
		t.Fatalf("stat = %+v", stat.GetEntry())
	}

	read := &fakeChunkStream{}
	if err := server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{ContainerId: containerID, Path: "/data/work/app.conf"}, read); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(read.data) != "port=8080\n" {
		t.Fatalf("read = %q", read.data)
	}

	// A rewrite without overrides must keep the owner and mode the first write set.
	rewrite := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: containerID,
		Path:        "/data/work/app.conf",
		Size:        int64(len("port=9090\n")),
		Uid:         -1,
		Gid:         -1,
	}, []string{"port=9090\n"})
	if err := server.WriteContainerFile(rewrite); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	ownerOutput, code, err := client.RunContainerCommand(ctx, containerID, []string{"/bin/sh", "-c", `stat -c '%a %u %g' "$1"`, "gotham", "/data/work/app.conf"})
	if err != nil || code != 0 {
		t.Fatalf("stat in the container: %v (exit %d)", err, code)
	}
	if strings.TrimSpace(ownerOutput) != "640 1234 2345" {
		t.Fatalf("owner and mode = %q, want the preserved 640 1234 2345", strings.TrimSpace(ownerOutput))
	}

	listing, err := server.ListContainerFiles(ctx, &agentv1.ContainerFilesListRequest{ContainerId: containerID, Path: "/data/work"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listing.GetEntries()) != 1 || listing.GetEntries()[0].GetName() != "app.conf" {
		t.Fatalf("entries = %+v", listing.GetEntries())
	}
	if listing.GetEntries()[0].GetUid() != 1234 || listing.GetEntries()[0].GetGid() != 2345 || listing.GetEntries()[0].GetMode() != 0o640 {
		t.Fatalf("listing ownership = %+v", listing.GetEntries()[0])
	}

	for _, step := range []struct {
		action      agentv1.ContainerFileAction
		path        string
		destination string
		mode        uint32
	}{
		{agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY, "/data/work/app.conf", "/data/work/copy.conf", 0},
		{agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_RENAME, "/data/work/copy.conf", "/data/work/renamed.conf", 0},
		{agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_CHMOD, "/data/work/renamed.conf", "", 0o600},
		{agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE, "/data/work/renamed.conf", "", 0},
	} {
		if _, err := server.ChangeContainerFile(ctx, &agentv1.ContainerFilesChangeRequest{
			ContainerId: containerID,
			Action:      step.action,
			Path:        step.path,
			Destination: step.destination,
			Mode:        step.mode,
		}); err != nil {
			t.Fatalf("%s %s: %v", step.action, step.path, err)
		}
	}

	if _, err := server.StatContainerFile(ctx, &agentv1.ContainerFilesPathRequest{ContainerId: containerID, Path: "/proc/self/environ"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stat /proc: %v, want PermissionDenied", err)
	}

	download := &fakeChunkStream{}
	if err := server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{ContainerId: containerID, Path: "/data", Archive: true}, download); err != nil {
		t.Fatalf("download: %v", err)
	}
	reader := tar.NewReader(bytes.NewReader(download.data))
	names := make([]string, 0, 4)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("download archive: %v", err)
		}
		names = append(names, header.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "data/work/app.conf") {
		t.Fatalf("download archive entries = %v", names)
	}

	// A stopped container refuses the listing, while reads still work and the
	// exec actions say why they are unavailable instead of failing obscurely.
	if err := client.Stop(ctx, containerID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	capabilities, err := server.GetContainerFileCapabilities(ctx, &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: containerID})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if capabilities.GetExecAvailable() || capabilities.GetExecBlockedBy() != containerFileExecNotRunning {
		t.Fatalf("stopped capabilities = %+v", capabilities)
	}
	if _, err := server.ListContainerFiles(ctx, &agentv1.ContainerFilesListRequest{ContainerId: containerID, Path: "/data/work"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("list stopped: %v, want FailedPrecondition", err)
	}
	stoppedRead := &fakeChunkStream{}
	if err := server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{ContainerId: containerID, Path: "/data/work/app.conf"}, stoppedRead); err != nil {
		t.Fatalf("read stopped: %v", err)
	}
	if string(stoppedRead.data) != "port=9090\n" {
		t.Fatalf("read stopped = %q", stoppedRead.data)
	}
	if _, err := server.ChangeContainerFile(ctx, &agentv1.ContainerFilesChangeRequest{
		ContainerId: containerID,
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR,
		Path:        "/data/stopped",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mkdir on a stopped container: %v, want FailedPrecondition", err)
	}
}

// TestRealContainerFilesExecListingKeepsOddNames proves the exec script and
// the byte-exact (non-TTY) stream keep every name, including a newline.
func TestRealContainerFilesExecListingKeepsOddNames(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	server := NewDockerServer(client, nil)
	ctx := context.Background()

	const directory = "/data/odd"
	names := []string{"with space", "pipe|name", `quote"name`, "tab\tname", "new\nline", ".hidden"}
	command := []string{
		"/bin/sh", "-c",
		`dir=$1
mkdir -p "$dir"
shift
for name in "$@"; do printf x > "$dir/$name"; done
ln -s -- "/target dir/odd|name" "$dir/link"`,
		"gotham", directory,
	}
	command = append(command, names...)
	if output, code, err := client.RunContainerCommand(ctx, containerID, command); err != nil || code != 0 {
		t.Fatalf("prepare the folder: %v (exit %d): %s", err, code, output)
	}

	listing, err := server.ListContainerFiles(ctx, &agentv1.ContainerFilesListRequest{
		ContainerId: containerID,
		Path:        directory,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := make(map[string]*agentv1.ContainerFileEntry, len(listing.GetEntries()))
	for _, entry := range listing.GetEntries() {
		got[entry.GetName()] = entry
	}
	for _, name := range names {
		entry := got[name]
		if entry == nil {
			t.Errorf("missing %q in the listing", name)
			continue
		}
		if entry.GetKind() != agentv1.FileKind_FILE_KIND_FILE || entry.GetSize() != 1 {
			t.Errorf("%q = %+v", name, entry)
		}
	}
	link := got["link"]
	if link == nil || link.GetKind() != agentv1.FileKind_FILE_KIND_SYMLINK ||
		link.GetSymlinkTarget() != "/target dir/odd|name" {
		t.Errorf("link = %+v", link)
	}
}

// TestRealContainerFilesNoShellFallback keeps the no-shell fallback honest: a
// container whose PATH hides every shell behaves like a distroless image.
func TestRealContainerFilesNoShellFallback(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := runNoShellContainer(t, client)
	server := NewDockerServer(client, nil)
	ctx := context.Background()
	capabilities, err := server.GetContainerFileCapabilities(ctx, &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: containerID})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if capabilities.GetExecAvailable() || capabilities.GetExecBlockedBy() != containerFileExecNoShell {
		t.Fatalf("no-shell capabilities = %+v", capabilities)
	}
	if _, err := server.ChangeContainerFile(ctx, &agentv1.ContainerFilesChangeRequest{
		ContainerId: containerID,
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR,
		Path:        "/data",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mkdir without a shell: %v, want FailedPrecondition", err)
	}
	listing, err := server.ListContainerFiles(ctx, &agentv1.ContainerFilesListRequest{ContainerId: containerID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listing.GetDirectory().GetPath() != "/" {
		t.Fatalf("listing = %+v", listing.GetDirectory())
	}
}

func runNoShellContainer(t *testing.T, client *DockerClient) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := client.PullImage(ctx, "alpine:3", "", ""); err != nil {
		t.Logf("pull alpine:3: %v (using a local image if present)", err)
	}
	id, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:   "alpine:3",
		Name:    "gotham-container-files-noshell",
		Command: []string{"/bin/sleep", "600"},
		Env:     []string{"PATH=/nonexistent"},
	})
	if err != nil {
		t.Skipf("run a no-shell container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Remove(cleanupCtx, id)
	})
	return id
}
