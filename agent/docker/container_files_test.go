package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeContainerFile is one path in the fake container's filesystem.
type fakeContainerFile struct {
	kind   agentv1.FileKind
	data   []byte
	mode   uint32
	uid    int32
	gid    int32
	target string
}

// containerFileEngine fakes the slice of the Docker client the file manager
// uses. It keeps the filesystem in a map so every operation is asserted
// without a daemon.
type containerFileEngine struct {
	*fakeDockerClient
	files    map[string]*fakeContainerFile
	state    string
	mounts   []ContainerMount
	commands []commandCall
	// importErr makes ImportArchive fail without reading the body, the way the
	// engine rejects a PUT on a read-only root or a missing parent directory.
	importErr error
}

type commandCall struct {
	argv []string
}

func newContainerFileEngine(files map[string]*fakeContainerFile) *containerFileEngine {
	if files == nil {
		files = map[string]*fakeContainerFile{}
	}
	files["/"] = &fakeContainerFile{kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755}
	return &containerFileEngine{
		fakeDockerClient: &fakeDockerClient{},
		files:            files,
		state:            "running",
	}
}

func (e *containerFileEngine) InspectContainer(context.Context, string) (ContainerDetails, error) {
	if e.err != nil {
		return ContainerDetails{}, e.err
	}
	return ContainerDetails{State: e.state, Mounts: e.mounts}, nil
}

// expand resolves intermediate symlinks the way the engine does: a symlink in
// the middle of the path is followed, the final component is not.
func (e *containerFileEngine) expand(source string) string {
	current := source
	for hop := 0; hop < 16; hop++ {
		parts := strings.Split(strings.TrimPrefix(current, "/"), "/")
		changed := false
		for index := 1; index < len(parts); index++ {
			prefix := "/" + strings.Join(parts[:index], "/")
			file, ok := e.files[prefix]
			if !ok {
				break
			}
			if file.kind != agentv1.FileKind_FILE_KIND_SYMLINK {
				continue
			}
			target := file.target
			if !strings.HasPrefix(target, "/") {
				target = path.Join(path.Dir(prefix), target)
			}
			current = path.Join(target, strings.Join(parts[index:], "/"))
			changed = true
			break
		}
		if !changed {
			return current
		}
	}
	return current
}

func (e *containerFileEngine) StatArchive(_ context.Context, _, source string) (ContainerPathStat, error) {
	source = e.expand(source)
	file, ok := e.files[source]
	if !ok {
		return ContainerPathStat{}, fmt.Errorf("%w: %s", ErrDockerNotFound, source)
	}
	mode := file.mode
	switch file.kind {
	case agentv1.FileKind_FILE_KIND_DIRECTORY:
		mode |= 1 << 31
	case agentv1.FileKind_FILE_KIND_SYMLINK:
		mode |= 1 << 27
	}
	return ContainerPathStat{
		Name:       path.Base(source),
		Size:       int64(len(file.data)),
		Mode:       mode,
		LinkTarget: file.target,
	}, nil
}

func (e *containerFileEngine) ExportArchive(_ context.Context, _, source string) (io.ReadCloser, error) {
	source = e.expand(source)
	file, ok := e.files[source]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDockerNotFound, source)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	prefix := path.Base(source)
	if source == "/" {
		prefix = ""
	}
	e.writeTar(writer, source, prefix, file.kind == agentv1.FileKind_FILE_KIND_DIRECTORY)
	_ = writer.Close()
	return io.NopCloser(&buffer), nil
}

func (e *containerFileEngine) writeTar(writer *tar.Writer, source, prefix string, recursive bool) {
	name := prefix
	if source == "/" {
		name = "/"
	}
	e.writeEntry(writer, name, e.files[source])
	if !recursive {
		return
	}
	for _, child := range e.children(source) {
		childName := path.Join("/", prefix, path.Base(child))
		if prefix == "" && source != "/" {
			childName = path.Join("/", path.Base(source), path.Base(child))
		}
		childFile := e.files[child]
		if childFile.kind == agentv1.FileKind_FILE_KIND_DIRECTORY {
			e.writeTar(writer, child, strings.TrimPrefix(childName, "/"), true)
			continue
		}
		e.writeEntry(writer, strings.TrimPrefix(childName, "/"), childFile)
	}
}

func (e *containerFileEngine) writeEntry(writer *tar.Writer, name string, file *fakeContainerFile) {
	header := &tar.Header{
		Name:     name,
		Mode:     int64(file.mode),
		Uid:      int(file.uid),
		Gid:      int(file.gid),
		Typeflag: tar.TypeReg,
	}
	switch file.kind {
	case agentv1.FileKind_FILE_KIND_DIRECTORY:
		header.Typeflag = tar.TypeDir
		header.Name = strings.TrimSuffix(name, "/") + "/"
	case agentv1.FileKind_FILE_KIND_SYMLINK:
		header.Typeflag = tar.TypeSymlink
		header.Linkname = file.target
	default:
		header.Size = int64(len(file.data))
	}
	_ = writer.WriteHeader(header)
	if header.Typeflag == tar.TypeReg {
		_, _ = writer.Write(file.data)
	}
}

func (e *containerFileEngine) children(directory string) []string {
	children := make([]string, 0, len(e.files))
	for candidate := range e.files {
		if candidate == "/" || candidate == directory {
			continue
		}
		if path.Dir(candidate) != directory {
			continue
		}
		children = append(children, candidate)
	}
	sort.Strings(children)
	return children
}

func (e *containerFileEngine) ImportArchive(_ context.Context, _, destination string, body io.Reader) error {
	if e.importErr != nil {
		return e.importErr
	}
	reader := tar.NewReader(body)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		e.files[path.Join(destination, header.Name)] = &fakeContainerFile{
			kind: agentv1.FileKind_FILE_KIND_FILE,
			data: data,
			mode: uint32(header.Mode),
			uid:  int32(header.Uid),
			gid:  int32(header.Gid),
		}
	}
}

func (e *containerFileEngine) ContainerShell(context.Context, string) (string, error) {
	if e.state != "running" {
		return "", fmt.Errorf("%w: %s", ErrDockerContainerNotRunning, e.state)
	}
	if e.shellErr != nil {
		return "", e.shellErr
	}
	return "/bin/sh", nil
}

func (e *containerFileEngine) RunContainerCommand(ctx context.Context, containerID string, command []string) (string, int32, error) {
	e.commands = append(e.commands, commandCall{argv: command})
	// Apply the action the way the shell script would, so assertions can check
	// both the argv and the effect.
	if len(command) < 5 {
		return "", 1, nil
	}
	switch command[2] {
	case `mkdir -p -- "$1"`:
		e.files[command[4]] = &fakeContainerFile{kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755}
	case `rm -rf -- "$1"`:
		for candidate := range e.files {
			if candidate == command[4] || strings.HasPrefix(candidate, command[4]+"/") {
				delete(e.files, candidate)
			}
		}
	case `mv -- "$1" "$2"`:
		for candidate := range e.files {
			if candidate != command[4] && !strings.HasPrefix(candidate, command[4]+"/") {
				continue
			}
			next := strings.Replace(candidate, command[4], command[5], 1)
			e.files[next] = e.files[candidate]
			delete(e.files, candidate)
		}
	case `cp -a -- "$1" "$2"`:
		for candidate := range e.files {
			if candidate != command[4] && !strings.HasPrefix(candidate, command[4]+"/") {
				continue
			}
			next := strings.Replace(candidate, command[4], command[5], 1)
			copied := *e.files[candidate]
			e.files[next] = &copied
		}
	case `chmod -- "$1" "$2"`:
		if file, ok := e.files[command[5]]; ok {
			var mode uint32
			if _, err := fmt.Sscanf(strings.TrimPrefix(command[4], "0"), "%o", &mode); err == nil {
				file.mode = mode
			}
		}
	}
	return "", 0, nil
}

func containerFileService(engine *containerFileEngine) *DockerServer {
	return NewDockerServer(engine, nil)
}

func sampleContainerFiles() map[string]*fakeContainerFile {
	return map[string]*fakeContainerFile{
		"/etc":                 {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755, uid: 0, gid: 0},
		"/etc/hosts":           {kind: agentv1.FileKind_FILE_KIND_FILE, data: []byte("127.0.0.1\n"), mode: 0o644, uid: 0, gid: 0},
		"/etc/app.conf":        {kind: agentv1.FileKind_FILE_KIND_FILE, data: []byte("port=8080\n"), mode: 0o640, uid: 1000, gid: 1001},
		"/etc/certs":           {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/etc/certs/ca.pem":    {kind: agentv1.FileKind_FILE_KIND_FILE, data: []byte("ca"), mode: 0o600},
		"/bin":                 {kind: agentv1.FileKind_FILE_KIND_SYMLINK, target: "/usr/bin"},
		"/usr":                 {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/usr/bin":             {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/usr/bin/sh":          {kind: agentv1.FileKind_FILE_KIND_FILE, data: []byte("bin"), mode: 0o755},
		"/tmp":                 {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o1777},
		"/var":                 {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/var/run":             {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/var/run/docker.sock": {kind: agentv1.FileKind_FILE_KIND_SPECIAL, mode: 0o600},
		"/tmp/rundir":          {kind: agentv1.FileKind_FILE_KIND_SYMLINK, target: "/var/run"},
		"/tmp/procdir":         {kind: agentv1.FileKind_FILE_KIND_SYMLINK, target: "/proc"},
		"/tmp/proclink":        {kind: agentv1.FileKind_FILE_KIND_SYMLINK, target: "/proc/self/environ"},
		"/run":                 {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
		"/run/docker.sock":     {kind: agentv1.FileKind_FILE_KIND_SPECIAL, mode: 0o600},
	}
}

func TestContainerFileListReportsImmediateChildren(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	response, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{
		ContainerId: "c1",
		Path:        "/etc",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := make([]string, 0, len(response.GetEntries()))
	for _, entry := range response.GetEntries() {
		names = append(names, entry.GetName())
	}
	if strings.Join(names, ",") != "app.conf,certs,hosts" {
		t.Fatalf("entries = %v, want only the immediate children", names)
	}
	if response.GetDirectory().GetKind() != agentv1.FileKind_FILE_KIND_DIRECTORY {
		t.Errorf("directory kind = %v", response.GetDirectory().GetKind())
	}
	conf := response.GetEntries()[0]
	if conf.GetPath() != "/etc/app.conf" || conf.GetMode() != 0o640 || conf.GetUid() != 1000 || conf.GetGid() != 1001 {
		t.Errorf("app.conf = %+v", conf)
	}
	if response.GetEntries()[1].GetKind() != agentv1.FileKind_FILE_KIND_DIRECTORY {
		t.Errorf("certs kind = %v", response.GetEntries()[1].GetKind())
	}
}

func TestContainerFileListRootAndCursor(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	root, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{ContainerId: "c1"})
	if err != nil {
		t.Fatalf("list root: %v", err)
	}
	if root.GetDirectory().GetPath() != "/" {
		t.Errorf("directory path = %q", root.GetDirectory().GetPath())
	}
	names := make([]string, 0, len(root.GetEntries()))
	for _, entry := range root.GetEntries() {
		names = append(names, entry.GetName())
	}
	if strings.Join(names, ",") != "bin,etc,run,tmp,usr,var" {
		t.Fatalf("root entries = %v", names)
	}

	page, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{
		ContainerId: "c1",
		Path:        "/",
		Cursor:      "etc",
	})
	if err != nil {
		t.Fatalf("list with cursor: %v", err)
	}
	after := make([]string, 0, len(page.GetEntries()))
	for _, entry := range page.GetEntries() {
		after = append(after, entry.GetName())
	}
	if strings.Join(after, ",") != "run,tmp,usr,var" {
		t.Fatalf("entries after the cursor = %v", after)
	}
}

func TestContainerFileListTruncatesAndResumes(t *testing.T) {
	files := map[string]*fakeContainerFile{
		"/data": {kind: agentv1.FileKind_FILE_KIND_DIRECTORY, mode: 0o755},
	}
	for index := 0; index < containerFileListPageSize+2; index++ {
		files[fmt.Sprintf("/data/f%05d", index)] = &fakeContainerFile{kind: agentv1.FileKind_FILE_KIND_FILE, data: []byte("x")}
	}
	server := containerFileService(newContainerFileEngine(files))
	first, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{ContainerId: "c1", Path: "/data"})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if !first.GetTruncated() || len(first.GetEntries()) != containerFileListPageSize || first.GetCursor() == "" {
		t.Fatalf("first page: %d entries, truncated %v, cursor %q",
			len(first.GetEntries()), first.GetTruncated(), first.GetCursor())
	}
	second, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{
		ContainerId: "c1",
		Path:        "/data",
		Cursor:      first.GetCursor(),
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.GetEntries()) != 2 || second.GetTruncated() {
		t.Fatalf("second page: %d entries, truncated %v", len(second.GetEntries()), second.GetTruncated())
	}
}

func TestContainerFileRejectsDeniedPaths(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	engine.mounts = []ContainerMount{{Type: "bind", Source: "/var/run/docker.sock", Destination: "/socket/docker.sock"}}
	server := containerFileService(engine)
	for _, candidate := range []string{"/proc", "/proc/self/environ", "/sys/kernel", "/dev/null", "/var/run/docker.sock", "/socket/docker.sock"} {
		_, err := server.StatContainerFile(context.Background(), &agentv1.ContainerFilesPathRequest{ContainerId: "c1", Path: candidate})
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("stat %s: %v, want PermissionDenied", candidate, err)
		}
	}
}

func TestContainerFileRejectsDeniedPathThroughAParentSymlink(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	engine.mounts = []ContainerMount{{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}
	server := containerFileService(engine)
	for _, candidate := range []string{
		"/tmp/rundir/docker.sock",
		"/tmp/procdir/self/environ",
		"/tmp/procdir/1/status",
	} {
		_, err := server.StatContainerFile(context.Background(), &agentv1.ContainerFilesPathRequest{ContainerId: "c1", Path: candidate})
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("stat %s: %v, want PermissionDenied", candidate, err)
		}
	}
	// Deleting through the same parent link must be refused too.
	if _, err := server.ChangeContainerFile(context.Background(), &agentv1.ContainerFilesChangeRequest{
		ContainerId: "c1",
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE,
		Path:        "/tmp/rundir/docker.sock",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("delete through a parent symlink: %v, want PermissionDenied", err)
	}
	// Writing through it is a deny as well.
	write := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/tmp/rundir/docker.sock",
		Size:        1,
	}, []string{"x"})
	if err := server.WriteContainerFile(write); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("write through a parent symlink: %v, want PermissionDenied", err)
	}
}

func TestContainerFileRejectsSymlinkToDeniedPath(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	_, err := server.StatContainerFile(context.Background(), &agentv1.ContainerFilesPathRequest{
		ContainerId: "c1",
		Path:        "/tmp/proclink",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stat symlink to /proc: %v, want PermissionDenied", err)
	}
}

func TestContainerFileFollowsAllowedSymlink(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	response, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{
		ContainerId: "c1",
		Path:        "/bin",
	})
	if err != nil {
		t.Fatalf("list through symlink: %v", err)
	}
	if len(response.GetEntries()) != 1 || response.GetEntries()[0].GetPath() != "/usr/bin/sh" {
		t.Fatalf("entries = %+v", response.GetEntries())
	}
}

func TestContainerFileRejectsParentSegment(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	_, err := server.StatContainerFile(context.Background(), &agentv1.ContainerFilesPathRequest{
		ContainerId: "c1",
		Path:        "/etc/../proc",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("stat with a parent segment: %v, want InvalidArgument", err)
	}
}

func TestContainerFileStatAndRead(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	stat, err := server.StatContainerFile(context.Background(), &agentv1.ContainerFilesPathRequest{ContainerId: "c1", Path: "/etc/hosts"})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.GetEntry().GetSize() != 10 || stat.GetEntry().GetKind() != agentv1.FileKind_FILE_KIND_FILE {
		t.Fatalf("entry = %+v", stat.GetEntry())
	}

	stream := &fakeChunkStream{}
	if err := server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{ContainerId: "c1", Path: "/etc/hosts"}, stream); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(stream.data) != "127.0.0.1\n" {
		t.Fatalf("read = %q", stream.data)
	}

	err = server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{ContainerId: "c1", Path: "/etc"}, stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("read a directory: %v, want InvalidArgument", err)
	}
}

func TestContainerFileDownloadStreamsTar(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	stream := &fakeChunkStream{}
	if err := server.ReadContainerFile(&agentv1.ContainerFilesReadRequest{
		ContainerId: "c1",
		Path:        "/etc",
		Archive:     true,
	}, stream); err != nil {
		t.Fatalf("download: %v", err)
	}
	reader := tar.NewReader(bytes.NewReader(stream.data))
	names := make([]string, 0, 4)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		names = append(names, header.Name)
	}
	if strings.Join(names, ",") != "etc/,etc/app.conf,etc/certs/,etc/certs/ca.pem,etc/hosts" {
		t.Fatalf("archive entries = %v", names)
	}
}

func TestContainerFileWriteKeepsOwnerAndMode(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/etc/app.conf",
		Size:        10,
		Uid:         -1,
		Gid:         -1,
	}, []string{"port=", "9090\n"})
	if err := server.WriteContainerFile(stream); err != nil {
		t.Fatalf("write: %v", err)
	}
	written := engine.files["/etc/app.conf"]
	if written == nil || string(written.data) != "port=9090\n" {
		t.Fatalf("written = %+v", written)
	}
	if written.mode != 0o640 || written.uid != 1000 || written.gid != 1001 {
		t.Fatalf("ownership = %o %d:%d, want the existing 640 1000:1001", written.mode, written.uid, written.gid)
	}
}

func TestContainerFileWriteCreatesFileWithRequestedAttributes(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/etc/new/../new.conf",
		Size:        3,
		Uid:         1000,
		Gid:         1000,
		Mode:        0o600,
	}, []string{"abc"})
	if err := server.WriteContainerFile(stream); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("write with a parent segment: %v, want InvalidArgument", err)
	}

	stream = newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/etc/new.conf",
		Size:        3,
		Uid:         1000,
		Gid:         1000,
		Mode:        0o600,
	}, []string{"abc"})
	if err := server.WriteContainerFile(stream); err != nil {
		t.Fatalf("write: %v", err)
	}
	written := engine.files["/etc/new.conf"]
	if written == nil || string(written.data) != "abc" || written.mode != 0o600 || written.uid != 1000 {
		t.Fatalf("written = %+v", written)
	}
}

func TestContainerFileWriteReportsTheEngineReasonForARejectedImport(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	engine.importErr = fmt.Errorf("%w: /etc/missing", ErrDockerNotFound)
	server := containerFileService(engine)
	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/etc/app.conf",
		Size:        10,
		Uid:         -1,
		Gid:         -1,
	}, []string{"port=", "9090\n"})
	if err := server.WriteContainerFile(stream); status.Code(err) != codes.NotFound {
		t.Fatalf("write: %v, want NotFound from the engine, not an internal closed-pipe error", err)
	}
}

func TestContainerFileWriteRejectsOversize(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/etc/big",
		Size:        containerFileMaxBytes + 1,
	}, nil)
	err := server.WriteContainerFile(stream)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversize write: %v, want ResourceExhausted", err)
	}
}

func TestContainerFileWriteRejectsDeniedPath(t *testing.T) {
	server := containerFileService(newContainerFileEngine(sampleContainerFiles()))
	stream := newWriteStream(&agentv1.ContainerFilesWriteHeader{
		ContainerId: "c1",
		Path:        "/proc/self/environ",
		Size:        1,
	}, []string{"x"})
	if err := server.WriteContainerFile(stream); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("write to /proc: %v, want PermissionDenied", err)
	}
}

func TestContainerFileCapabilities(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	response, err := server.GetContainerFileCapabilities(context.Background(), &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: "c1"})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !response.GetExecAvailable() || response.GetExecBlockedBy() != "" || !response.GetReadable() {
		t.Fatalf("running: %+v", response)
	}

	engine.state = "exited"
	response, err = server.GetContainerFileCapabilities(context.Background(), &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: "c1"})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if response.GetExecAvailable() || response.GetExecBlockedBy() != containerFileExecNotRunning || response.GetState() != "exited" {
		t.Fatalf("stopped: %+v", response)
	}

	engine.state = "running"
	engine.shellErr = fmt.Errorf("%w: no /bin/sh", ErrDockerNoShell)
	response, err = server.GetContainerFileCapabilities(context.Background(), &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: "c1"})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if response.GetExecAvailable() || response.GetExecBlockedBy() != containerFileExecNoShell {
		t.Fatalf("distroless: %+v", response)
	}
}

func TestContainerFileChangePassesPathsAsArguments(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		request *agentv1.ContainerFilesChangeRequest
		want    []string
	}{
		{
			name:    "mkdir",
			request: &agentv1.ContainerFilesChangeRequest{ContainerId: "c1", Action: agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR, Path: "/etc/new dir"},
			want:    []string{"/bin/sh", "-c", `mkdir -p -- "$1"`, "gotham", "/etc/new dir"},
		},
		{
			name:    "delete",
			request: &agentv1.ContainerFilesChangeRequest{ContainerId: "c1", Action: agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE, Path: "/etc/hosts"},
			want:    []string{"/bin/sh", "-c", `rm -rf -- "$1"`, "gotham", "/etc/hosts"},
		},
		{
			name:    "rename",
			request: &agentv1.ContainerFilesChangeRequest{ContainerId: "c1", Action: agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_RENAME, Path: "/etc/hosts", Destination: "/etc/hosts.bak"},
			want:    []string{"/bin/sh", "-c", `mv -- "$1" "$2"`, "gotham", "/etc/hosts", "/etc/hosts.bak"},
		},
		{
			name:    "copy",
			request: &agentv1.ContainerFilesChangeRequest{ContainerId: "c1", Action: agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY, Path: "/etc/hosts", Destination: "/etc/hosts.copy"},
			want:    []string{"/bin/sh", "-c", `cp -a -- "$1" "$2"`, "gotham", "/etc/hosts", "/etc/hosts.copy"},
		},
		{
			name:    "chmod",
			request: &agentv1.ContainerFilesChangeRequest{ContainerId: "c1", Action: agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_CHMOD, Path: "/etc/hosts", Mode: 0o600},
			want:    []string{"/bin/sh", "-c", `chmod -- "$1" "$2"`, "gotham", "0600", "/etc/hosts"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newContainerFileEngine(sampleContainerFiles())
			server := containerFileService(engine)
			if _, err := server.ChangeContainerFile(ctx, testCase.request); err != nil {
				t.Fatalf("%s: %v", testCase.name, err)
			}
			if len(engine.commands) != 1 {
				t.Fatalf("commands = %d", len(engine.commands))
			}
			if strings.Join(engine.commands[0].argv, "\x00") != strings.Join(testCase.want, "\x00") {
				t.Fatalf("argv = %q, want %q", engine.commands[0].argv, testCase.want)
			}
		})
	}
}

func TestContainerFileChangeFallsBackWhenExecIsUnavailable(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	request := &agentv1.ContainerFilesChangeRequest{
		ContainerId: "c1",
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR,
		Path:        "/etc/new",
	}
	engine.state = "exited"
	if _, err := server.ChangeContainerFile(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stopped container: %v, want FailedPrecondition", err)
	}
	engine.state = "running"
	engine.shellErr = fmt.Errorf("%w: no shell", ErrDockerNoShell)
	if _, err := server.ChangeContainerFile(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("distroless container: %v, want FailedPrecondition", err)
	}
	if len(engine.commands) != 0 {
		t.Fatalf("commands ran without a shell: %v", engine.commands)
	}
}

func TestContainerFileChangeRefusesSilentOverwrite(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	request := &agentv1.ContainerFilesChangeRequest{
		ContainerId: "c1",
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY,
		Path:        "/etc/hosts",
		Destination: "/etc/app.conf",
	}
	if _, err := server.ChangeContainerFile(context.Background(), request); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("copy onto an existing file: %v, want AlreadyExists", err)
	}
	request.Overwrite = true
	if _, err := server.ChangeContainerFile(context.Background(), request); err != nil {
		t.Fatalf("copy with overwrite: %v", err)
	}
}

func TestContainerFileChangeActsOnTheSymlinkItself(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	// /bin is a symlink to /usr/bin: deleting it must remove the link, not the
	// directory it points at.
	if _, err := server.ChangeContainerFile(context.Background(), &agentv1.ContainerFilesChangeRequest{
		ContainerId: "c1",
		Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE,
		Path:        "/bin",
	}); err != nil {
		t.Fatalf("delete symlink: %v", err)
	}
	if _, ok := engine.files["/bin"]; ok {
		t.Error("/bin still exists")
	}
	if _, ok := engine.files["/usr/bin/sh"]; !ok {
		t.Error("the symlink target was deleted")
	}
	if len(engine.commands) != 1 || engine.commands[0].argv[4] != "/bin" {
		t.Fatalf("argv = %v", engine.commands)
	}
}

func TestContainerFileChangeRefusesCopyIntoItself(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	server := containerFileService(engine)
	for _, destination := range []string{"/etc", "/etc/certs/inside"} {
		_, err := server.ChangeContainerFile(context.Background(), &agentv1.ContainerFilesChangeRequest{
			ContainerId: "c1",
			Action:      agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY,
			Path:        "/etc",
			Destination: destination,
		})
		if status.Code(err) != codes.FailedPrecondition {
			t.Errorf("copy /etc to %s: %v, want FailedPrecondition", destination, err)
		}
	}
}

func TestContainerFileRejectsForeignContainerErrors(t *testing.T) {
	engine := newContainerFileEngine(sampleContainerFiles())
	engine.err = fmt.Errorf("%w: no such container", ErrDockerNotFound)
	server := containerFileService(engine)
	_, err := server.ListContainerFiles(context.Background(), &agentv1.ContainerFilesListRequest{ContainerId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("list of a missing container: %v, want NotFound", err)
	}
}

// fakeChunkStream collects the chunks ReadContainerFile sends.
type fakeChunkStream struct {
	grpc.ServerStream
	data []byte
}

func (s *fakeChunkStream) Send(chunk *agentv1.ContainerFileChunk) error {
	s.data = append(s.data, chunk.GetData()...)
	return nil
}

func (s *fakeChunkStream) Context() context.Context { return context.Background() }

// fakeWriteStream replays one header and the given data chunks.
type fakeWriteStream struct {
	grpc.ServerStream
	requests []*agentv1.ContainerFilesWriteRequest
	closed   bool
}

func newWriteStream(header *agentv1.ContainerFilesWriteHeader, chunks []string) *fakeWriteStream {
	requests := []*agentv1.ContainerFilesWriteRequest{{Part: &agentv1.ContainerFilesWriteRequest_Header{Header: header}}}
	for _, chunk := range chunks {
		requests = append(requests, &agentv1.ContainerFilesWriteRequest{Part: &agentv1.ContainerFilesWriteRequest_Data{Data: []byte(chunk)}})
	}
	return &fakeWriteStream{requests: requests}
}

func (s *fakeWriteStream) Recv() (*agentv1.ContainerFilesWriteRequest, error) {
	if len(s.requests) == 0 {
		return nil, io.EOF
	}
	next := s.requests[0]
	s.requests = s.requests[1:]
	return next, nil
}

func (s *fakeWriteStream) SendAndClose(*agentv1.ContainerFilesWriteResponse) error {
	s.closed = true
	return nil
}

func (s *fakeWriteStream) Context() context.Context { return context.Background() }
