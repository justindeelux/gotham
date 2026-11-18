package docker

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// containerFileMaxBytes caps one read, download or write. Listings have
	// their own, smaller cap so one page stays cheap.
	containerFileMaxBytes      = 64 << 20
	containerFileChunkSize     = 64 << 10
	containerFileListPageSize  = 1000
	containerFileListByteLimit = 32 << 20
	containerFilePathLimit     = 4096
	containerFileMaxHops       = 16
	containerFileMaxMode       = 0o7777
)

// containerFileDeniedPaths are never reachable through the file manager: they
// are kernel interfaces rather than the container's own files. Docker socket
// mounts are added per container from its mounts.
var containerFileDeniedPaths = []string{
	"/proc", "/sys", "/dev", "/run/docker.sock", "/var/run/docker.sock",
}

// The exec channel's blocked reasons are stable codes the web UI translates.
const (
	containerFileExecNotRunning  = "not_running"
	containerFileExecNoShell     = "no_shell"
	containerFileExecUnsupported = "unsupported"
)

var (
	errContainerFileInvalid  = errors.New("container files: invalid path")
	errContainerFileDenied   = errors.New("container files: path is not allowed")
	errContainerFileExists   = errors.New("container files: the destination already exists")
	errContainerFileConflict = errors.New("container files: the destination is not usable")
)

// containerFileTarget is one resolved container the file manager works on.
type containerFileTarget struct {
	id     string
	state  string
	mounts []ContainerMount
}

func (s *DockerServer) containerFileTarget(ctx context.Context, raw string) (containerFileTarget, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return containerFileTarget{}, fmt.Errorf("%w: a container is required", errContainerFileInvalid)
	}
	details, err := s.docker.InspectContainer(ctx, id)
	if err != nil {
		return containerFileTarget{}, err
	}
	return containerFileTarget{id: id, state: details.State, mounts: details.Mounts}, nil
}

func (s *DockerServer) GetContainerFileCapabilities(ctx context.Context, req *agentv1.ContainerFilesCapabilitiesRequest) (*agentv1.ContainerFilesCapabilitiesResponse, error) {
	target, err := s.containerFileTarget(ctx, req.GetContainerId())
	if err != nil {
		return nil, containerFileError(err)
	}
	response := &agentv1.ContainerFilesCapabilitiesResponse{Readable: true, State: target.state}
	if _, err := s.docker.ContainerShell(ctx, target.id); err != nil {
		switch {
		case errors.Is(err, ErrDockerContainerNotRunning):
			response.ExecBlockedBy = containerFileExecNotRunning
		case errors.Is(err, ErrDockerNoShell):
			response.ExecBlockedBy = containerFileExecNoShell
		case errors.Is(err, ErrDockerNotFound):
			return nil, containerFileError(err)
		default:
			response.ExecBlockedBy = containerFileExecUnsupported
		}
		return response, nil
	}
	response.ExecAvailable = true
	return response, nil
}

func (s *DockerServer) ListContainerFiles(ctx context.Context, req *agentv1.ContainerFilesListRequest) (*agentv1.ContainerFilesListResponse, error) {
	target, err := s.containerFileTarget(ctx, req.GetContainerId())
	if err != nil {
		return nil, containerFileError(err)
	}
	directory, stat, err := s.containerFileResolve(ctx, target, req.GetPath())
	if err != nil {
		return nil, containerFileError(err)
	}
	if !containerFileIsDir(stat.Mode) {
		return nil, containerFileError(fmt.Errorf("%w: %q is not a directory", errContainerFileInvalid, directory))
	}
	entries, truncated, cursor, err := s.containerFileList(ctx, target, directory, strings.TrimSpace(req.GetCursor()))
	if err != nil {
		return nil, containerFileError(err)
	}
	return &agentv1.ContainerFilesListResponse{
		Directory: containerFileEntry(directory, stat),
		Entries:   entries,
		Truncated: truncated,
		Cursor:    cursor,
	}, nil
}

func (s *DockerServer) StatContainerFile(ctx context.Context, req *agentv1.ContainerFilesPathRequest) (*agentv1.ContainerFilesStatResponse, error) {
	target, err := s.containerFileTarget(ctx, req.GetContainerId())
	if err != nil {
		return nil, containerFileError(err)
	}
	cleaned, stat, err := s.containerFileResolve(ctx, target, req.GetPath())
	if err != nil {
		return nil, containerFileError(err)
	}
	return &agentv1.ContainerFilesStatResponse{Entry: containerFileEntry(cleaned, stat)}, nil
}

func (s *DockerServer) ReadContainerFile(req *agentv1.ContainerFilesReadRequest, stream grpc.ServerStreamingServer[agentv1.ContainerFileChunk]) error {
	ctx := stream.Context()
	target, err := s.containerFileTarget(ctx, req.GetContainerId())
	if err != nil {
		return containerFileError(err)
	}
	source, stat, err := s.containerFileResolve(ctx, target, req.GetPath())
	if err != nil {
		return containerFileError(err)
	}
	if req.GetArchive() {
		return s.containerFileStreamArchive(ctx, target, source, stream)
	}
	if containerFileIsDir(stat.Mode) {
		return containerFileError(fmt.Errorf("%w: %q is a directory", errContainerFileInvalid, source))
	}
	if !containerFileIsRegular(stat.Mode) {
		return containerFileError(fmt.Errorf("%w: %q is not a regular file", errContainerFileInvalid, source))
	}
	body, err := s.docker.ExportArchive(ctx, target.id, source)
	if err != nil {
		return containerFileError(err)
	}
	defer func() { _ = body.Close() }()
	reader, err := archiveTarEntry(body, source, "container files")
	if err != nil {
		return err
	}
	if err := containerFileCopy(reader, stream); err != nil {
		return containerFileError(err)
	}
	return nil
}

func (s *DockerServer) WriteContainerFile(stream grpc.ClientStreamingServer[agentv1.ContainerFilesWriteRequest, agentv1.ContainerFilesWriteResponse]) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return containerFileError(containerFileRecvError(err))
	}
	header := first.GetHeader()
	if header == nil {
		return containerFileError(fmt.Errorf("%w: the first message must be a header", errContainerFileInvalid))
	}
	target, err := s.containerFileTarget(ctx, header.GetContainerId())
	if err != nil {
		return containerFileError(err)
	}
	size := header.GetSize()
	if size < 0 || size > containerFileMaxBytes {
		return status.Errorf(codes.ResourceExhausted,
			"container files: a write is limited to %d bytes", int64(containerFileMaxBytes))
	}
	destination, mode, uid, gid, err := s.containerFileWriteTarget(ctx, target, header)
	if err != nil {
		return containerFileError(err)
	}

	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- containerFileSend(writer, stream, destination, size, mode, uid, gid)
	}()
	putErr := s.docker.ImportArchive(writeCtx, target.id, path.Dir(destination), reader)
	_ = reader.CloseWithError(io.ErrClosedPipe)
	// Cancel after a failed import so a sender blocked in Recv returns promptly.
	cancel()
	sendErr := <-done
	// A rejected import closes the pipe under the sender, so its closed-pipe
	// error is a symptom: report the engine's reason instead.
	if sendErr != nil && (putErr == nil || !errors.Is(sendErr, io.ErrClosedPipe)) {
		return containerFileError(sendErr)
	}
	if putErr != nil {
		return containerFileError(putErr)
	}
	return stream.SendAndClose(&agentv1.ContainerFilesWriteResponse{})
}

func (s *DockerServer) ChangeContainerFile(ctx context.Context, req *agentv1.ContainerFilesChangeRequest) (*agentv1.ContainerFilesChangeResponse, error) {
	target, err := s.containerFileTarget(ctx, req.GetContainerId())
	if err != nil {
		return nil, containerFileError(err)
	}
	action := req.GetAction()
	script, ok := containerFileScript(action)
	if !ok {
		return nil, containerFileError(fmt.Errorf("%w: unknown file action", errContainerFileInvalid))
	}
	var source string
	if action == agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR {
		source, err = s.containerFileNewPath(ctx, target, req.GetPath())
	} else {
		source, err = s.containerFileOperand(ctx, target, req.GetPath())
	}
	if err != nil {
		return nil, containerFileError(err)
	}
	if action != agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR {
		if _, statErr := s.docker.StatArchive(ctx, target.id, source); statErr != nil {
			return nil, containerFileError(statErr)
		}
	}
	args := []string{source}
	switch action {
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_CHMOD:
		mode := req.GetMode() & containerFileMaxMode
		if mode == 0 {
			return nil, containerFileError(fmt.Errorf("%w: a mode is required", errContainerFileInvalid))
		}
		args = []string{fmt.Sprintf("%04o", mode), source}
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_RENAME,
		agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MOVE,
		agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY:
		destination, err := s.containerFileOperand(ctx, target, req.GetDestination())
		if err != nil {
			return nil, containerFileError(err)
		}
		if strings.HasPrefix(destination+"/", source+"/") {
			return nil, containerFileError(fmt.Errorf("%w: %q cannot be copied or moved into itself", errContainerFileConflict, source))
		}
		stat, statErr := s.docker.StatArchive(ctx, target.id, destination)
		switch {
		case statErr == nil && containerFileIsDir(stat.Mode):
			return nil, containerFileError(fmt.Errorf("%w: %q is a directory", errContainerFileConflict, destination))
		case statErr == nil && !req.GetOverwrite():
			return nil, containerFileError(errContainerFileExists)
		case statErr != nil && !errors.Is(statErr, ErrDockerNotFound):
			return nil, containerFileError(statErr)
		}
		args = []string{source, destination}
	}
	shell, err := s.docker.ContainerShell(ctx, target.id)
	if err != nil {
		return nil, containerFileError(err)
	}
	command := append([]string{shell, "-c", script, "gotham"}, args...)
	output, code, err := s.docker.RunContainerCommand(ctx, target.id, command)
	if err != nil {
		return nil, containerFileError(err)
	}
	if code != 0 {
		message := strings.TrimSpace(output)
		if message == "" {
			message = fmt.Sprintf("exit code %d", code)
		}
		return nil, status.Errorf(codes.FailedPrecondition, "container files: the change failed: %s", message)
	}
	return &agentv1.ContainerFilesChangeResponse{}, nil
}

// containerFileScript is the shell snippet for one exec action. Every caller
// path travels as a separate argv item, so no path is ever parsed by the shell.
func containerFileScript(action agentv1.ContainerFileAction) (string, bool) {
	switch action {
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR:
		return `mkdir -p -- "$1"`, true
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE:
		return `rm -rf -- "$1"`, true
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_RENAME,
		agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MOVE:
		return `mv -- "$1" "$2"`, true
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY:
		return `cp -a -- "$1" "$2"`, true
	case agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_CHMOD:
		return `chmod -- "$1" "$2"`, true
	default:
		return "", false
	}
}

// containerFileWriteTarget resolves where a write lands and which attributes
// the new file keeps. A symlink is followed, a new file inherits the parent
// directory's resolved path, and an existing file keeps its owner and mode
// unless the caller overrides them.
func (s *DockerServer) containerFileWriteTarget(ctx context.Context, target containerFileTarget, header *agentv1.ContainerFilesWriteHeader) (string, uint32, int32, int32, error) {
	cleaned, err := containerFileCleanPath(header.GetPath())
	if err != nil {
		return "", 0, 0, 0, err
	}
	if cleaned == "/" {
		return "", 0, 0, 0, fmt.Errorf("%w: the container root is not a file", errContainerFileInvalid)
	}
	if containerFileDenied(cleaned, target.mounts) {
		return "", 0, 0, 0, fmt.Errorf("%w: %q", errContainerFileDenied, cleaned)
	}
	existing := &tar.Header{}
	destination, stat, err := s.containerFileResolve(ctx, target, cleaned)
	switch {
	case err == nil:
		if containerFileIsDir(stat.Mode) {
			return "", 0, 0, 0, fmt.Errorf("%w: %q is a directory", errContainerFileInvalid, destination)
		}
		existing, err = s.containerFileHeader(ctx, target, destination)
		if err != nil {
			return "", 0, 0, 0, err
		}
	case errors.Is(err, ErrDockerNotFound):
		destination, err = s.containerFileNewPath(ctx, target, cleaned)
		if err != nil {
			return "", 0, 0, 0, err
		}
	default:
		return "", 0, 0, 0, err
	}

	mode := header.GetMode() & containerFileMaxMode
	if mode == 0 {
		mode = uint32(existing.Mode) & containerFileMaxMode
	}
	if mode == 0 {
		mode = 0o644
	}
	uid, gid := header.GetUid(), header.GetGid()
	if uid < 0 {
		uid = int32(existing.Uid)
	}
	if gid < 0 {
		gid = int32(existing.Gid)
	}
	return destination, mode, uid, gid, nil
}

// containerFileHeader reads only the first tar header of an existing path, so
// the owner and mode survive a rewrite without buffering the file.
func (s *DockerServer) containerFileHeader(ctx context.Context, target containerFileTarget, source string) (*tar.Header, error) {
	body, err := s.docker.ExportArchive(ctx, target.id, source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	header, err := tar.NewReader(body).Next()
	if err != nil {
		return nil, fmt.Errorf("container files: read %q: %w", source, err)
	}
	return header, nil
}

// containerFileSend writes one regular file tar into the pipe the archive
// import reads from. The declared size is enforced, but there is no atomic
// write: an aborted stream leaves the destination partially written.
func containerFileSend(writer *io.PipeWriter, stream grpc.ClientStreamingServer[agentv1.ContainerFilesWriteRequest, agentv1.ContainerFilesWriteResponse], destination string, size int64, mode uint32, uid, gid int32) error {
	fail := func(err error) error {
		_ = writer.CloseWithError(err)
		return err
	}
	archive := tar.NewWriter(writer)
	header := &tar.Header{
		Name:     path.Base(destination),
		Typeflag: tar.TypeReg,
		Mode:     int64(mode),
		Uid:      int(uid),
		Gid:      int(gid),
		Size:     size,
		ModTime:  time.Now(),
		Format:   tar.FormatPAX,
	}
	if err := archive.WriteHeader(header); err != nil {
		return fail(err)
	}
	var written int64
	for written < size {
		message, err := stream.Recv()
		if err != nil {
			return fail(containerFileRecvError(err))
		}
		data := message.GetData()
		if len(data) == 0 {
			continue
		}
		if written+int64(len(data)) > size {
			return fail(fmt.Errorf("%w: the stream carried more than the declared %d bytes", errContainerFileInvalid, size))
		}
		if _, err := archive.Write(data); err != nil {
			return fail(err)
		}
		written += int64(len(data))
	}
	if err := archive.Close(); err != nil {
		return fail(err)
	}
	return writer.Close()
}

// containerFileList reads the archive of one directory and keeps its immediate
// children. The archive walk is lexicographic, so a name cursor resumes a page
// exactly. Bytes are counted, never buffered: each page is capped by entries
// and by the tar bytes it may read.
func (s *DockerServer) containerFileList(ctx context.Context, target containerFileTarget, directory, cursor string) ([]*agentv1.ContainerFileEntry, bool, string, error) {
	body, err := s.docker.ExportArchive(ctx, target.id, directory)
	if err != nil {
		return nil, false, "", err
	}
	defer func() { _ = body.Close() }()
	counted := &countingReader{reader: body}
	reader := tar.NewReader(counted)
	base := ""
	if directory != "/" {
		base = path.Base(directory)
	}
	entries := make([]*agentv1.ContainerFileEntry, 0, 16)
	last := ""
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, "", fmt.Errorf("container files: read listing of %q: %w", directory, err)
		}
		entry, ok := containerFileListEntry(directory, base, header)
		if !ok {
			continue
		}
		if cursor != "" && entry.GetName() <= cursor {
			continue
		}
		entries = append(entries, entry)
		last = entry.GetName()
		if len(entries) >= containerFileListPageSize || counted.read >= containerFileListByteLimit {
			return entries, true, last, nil
		}
	}
	return entries, false, "", nil
}

func containerFileListEntry(directory, base string, header *tar.Header) (*agentv1.ContainerFileEntry, bool) {
	name := strings.TrimPrefix(header.Name, "/")
	if base != "" {
		if name == base || name == base+"/" {
			return nil, false
		}
		if !strings.HasPrefix(name, base+"/") {
			return nil, false
		}
		name = strings.TrimPrefix(name, base+"/")
	}
	name = strings.TrimSuffix(name, "/")
	if name == "" || strings.Contains(name, "/") {
		return nil, false
	}
	entry := &agentv1.ContainerFileEntry{
		Name: name,
		Path: path.Join(directory, name),
		Kind: containerFileKind(header.Typeflag),
		Size: header.Size,
		Mode: uint32(header.Mode) & containerFileMaxMode,
		Uid:  uint32(header.Uid),
		Gid:  uint32(header.Gid),
	}
	if !header.ModTime.IsZero() {
		entry.ModifiedAt = timestamppb.New(header.ModTime)
	}
	if header.Typeflag == tar.TypeSymlink {
		entry.SymlinkTarget = header.Linkname
	}
	return entry, true
}

func (s *DockerServer) containerFileStreamArchive(ctx context.Context, target containerFileTarget, source string, stream grpc.ServerStreamingServer[agentv1.ContainerFileChunk]) error {
	body, err := s.docker.ExportArchive(ctx, target.id, source)
	if err != nil {
		return containerFileError(err)
	}
	defer func() { _ = body.Close() }()
	if err := containerFileCopy(body, stream); err != nil {
		return containerFileError(err)
	}
	return nil
}

func containerFileCopy(reader io.Reader, stream grpc.ServerStreamingServer[agentv1.ContainerFileChunk]) error {
	buffer := make([]byte, containerFileChunkSize)
	var sent int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			sent += int64(n)
			if sent > containerFileMaxBytes {
				return status.Errorf(codes.ResourceExhausted,
					"container files: the stream exceeds %d bytes", int64(containerFileMaxBytes))
			}
			if err := stream.Send(&agentv1.ContainerFileChunk{Data: buffer[:n]}); err != nil {
				return err
			}
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return nil
		case readErr != nil:
			return fmt.Errorf("container files: read stream: %w", readErr)
		}
	}
}

// containerFileResolve normalizes one caller path, refuses the denied paths
// and follows the container's own symlinks segment by segment. Docker resolves
// intermediate symlinks itself, so every prefix has to be resolved here too or
// a link in a middle segment would smuggle /proc or a mounted socket past the
// deny list.
func (s *DockerServer) containerFileResolve(ctx context.Context, target containerFileTarget, raw string) (string, ContainerPathStat, error) {
	cleaned, err := containerFileCleanPath(raw)
	if err != nil {
		return "", ContainerPathStat{}, err
	}
	if containerFileDenied(cleaned, target.mounts) {
		return "", ContainerPathStat{}, fmt.Errorf("%w: %q", errContainerFileDenied, cleaned)
	}
	current := "/"
	stat := ContainerPathStat{Name: "/", Mode: 1 << 31}
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if cleaned == "/" {
		parts = nil
	}
	for _, part := range parts {
		resolved, resolvedStat, resolveErr := s.containerFileFollow(ctx, target, path.Join(current, part), cleaned)
		if resolveErr != nil {
			return "", ContainerPathStat{}, resolveErr
		}
		current, stat = resolved, resolvedStat
	}
	if len(parts) == 0 {
		resolvedStat, statErr := s.docker.StatArchive(ctx, target.id, "/")
		if statErr != nil {
			return "", ContainerPathStat{}, containerFileStatError(cleaned, statErr)
		}
		stat = resolvedStat
	}
	return current, stat, nil
}

// containerFileFollow follows the symlinks at the end of one resolved parent,
// re-checking the deny list after every hop.
func (s *DockerServer) containerFileFollow(ctx context.Context, target containerFileTarget, candidate, requested string) (string, ContainerPathStat, error) {
	current := candidate
	for hop := 0; hop < containerFileMaxHops; hop++ {
		stat, err := s.docker.StatArchive(ctx, target.id, current)
		if err != nil {
			return "", ContainerPathStat{}, containerFileStatError(requested, err)
		}
		if containerFileDenied(current, target.mounts) {
			return "", ContainerPathStat{}, fmt.Errorf("%w: %q", errContainerFileDenied, current)
		}
		if !containerFileIsSymlink(stat.Mode) {
			return current, stat, nil
		}
		next := strings.TrimSpace(stat.LinkTarget)
		if next == "" {
			return "", ContainerPathStat{}, fmt.Errorf("%w: %q is a symlink without a target", errContainerFileInvalid, current)
		}
		if !strings.HasPrefix(next, "/") {
			next = path.Join(path.Dir(current), next)
		}
		current = path.Clean(next)
		if containerFileDenied(current, target.mounts) {
			return "", ContainerPathStat{}, fmt.Errorf("%w: %q", errContainerFileDenied, current)
		}
	}
	return "", ContainerPathStat{}, fmt.Errorf("%w: %q has too many symlink hops", errContainerFileInvalid, requested)
}

func containerFileStatError(requested string, err error) error {
	if errors.Is(err, ErrDockerNotFound) {
		return fmt.Errorf("%w: %q no longer exists", ErrDockerNotFound, requested)
	}
	return err
}

// containerFileOperand resolves one exec operand: its parent must exist and is
// resolved through the container's own symlinks, while the leaf name is kept
// as given, so renaming or deleting a symlink acts on the link itself.
func (s *DockerServer) containerFileOperand(ctx context.Context, target containerFileTarget, raw string) (string, error) {
	cleaned, err := containerFileCleanPath(raw)
	if err != nil {
		return "", err
	}
	if cleaned == "/" {
		return "", fmt.Errorf("%w: %q is the container root", errContainerFileInvalid, cleaned)
	}
	if containerFileDenied(cleaned, target.mounts) {
		return "", fmt.Errorf("%w: %q", errContainerFileDenied, cleaned)
	}
	parent, _, err := s.containerFileResolve(ctx, target, path.Dir(cleaned))
	if err != nil {
		return "", err
	}
	operand := path.Join(parent, path.Base(cleaned))
	if containerFileDenied(operand, target.mounts) {
		return "", fmt.Errorf("%w: %q", errContainerFileDenied, operand)
	}
	if _, _, followErr := s.containerFileFollow(ctx, target, operand, cleaned); followErr != nil && !errors.Is(followErr, ErrDockerNotFound) {
		return "", followErr
	}
	return operand, nil
}

// containerFileNewPath resolves a path that does not exist yet: its deepest
// existing ancestor is resolved (so a symlinked parent cannot smuggle a denied
// path) and the missing tail is kept as given.
func (s *DockerServer) containerFileNewPath(ctx context.Context, target containerFileTarget, raw string) (string, error) {
	cleaned, err := containerFileCleanPath(raw)
	if err != nil {
		return "", err
	}
	if cleaned == "/" {
		return "", fmt.Errorf("%w: %q is the container root", errContainerFileInvalid, cleaned)
	}
	if containerFileDenied(cleaned, target.mounts) {
		return "", fmt.Errorf("%w: %q", errContainerFileDenied, cleaned)
	}
	current := cleaned
	missing := make([]string, 0, 2)
	for {
		resolved, _, resolveErr := s.containerFileResolve(ctx, target, current)
		if resolveErr == nil {
			return path.Join(append([]string{resolved}, missing...)...), nil
		}
		if !errors.Is(resolveErr, ErrDockerNotFound) || current == "/" {
			return "", resolveErr
		}
		missing = append([]string{path.Base(current)}, missing...)
		current = path.Dir(current)
	}
}

// containerFileCleanPath normalizes one absolute container path. Parent
// segments are refused rather than folded, so a request that tries to walk out
// of a directory never silently lands somewhere else.
func containerFileCleanPath(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	if len(raw) > containerFilePathLimit || strings.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("%w: %q is too long", errContainerFileInvalid, raw)
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("%w: %q is not absolute", errContainerFileInvalid, raw)
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: %q contains a parent segment", errContainerFileInvalid, raw)
		}
	}
	return path.Clean(raw), nil
}

func containerFileDenied(cleaned string, mounts []ContainerMount) bool {
	for _, denied := range containerFileDeniedPaths {
		if containerFilePathWithin(cleaned, denied) {
			return true
		}
	}
	for _, mount := range mounts {
		if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(mount.Destination)), "docker.sock") &&
			!strings.HasSuffix(strings.ToLower(strings.TrimSpace(mount.Source)), "docker.sock") {
			continue
		}
		if containerFilePathWithin(cleaned, path.Clean(mount.Destination)) {
			return true
		}
	}
	return false
}

func containerFilePathWithin(candidate, directory string) bool {
	return candidate == directory || strings.HasPrefix(candidate, strings.TrimSuffix(directory, "/")+"/")
}

func containerFileKind(typeflag byte) agentv1.FileKind {
	switch typeflag {
	case tar.TypeDir:
		return agentv1.FileKind_FILE_KIND_DIRECTORY
	case tar.TypeSymlink:
		return agentv1.FileKind_FILE_KIND_SYMLINK
	case tar.TypeReg, tar.TypeLink:
		return agentv1.FileKind_FILE_KIND_FILE
	default:
		return agentv1.FileKind_FILE_KIND_SPECIAL
	}
}

func containerFileEntry(containerPath string, stat ContainerPathStat) *agentv1.ContainerFileEntry {
	entry := &agentv1.ContainerFileEntry{
		Name: path.Base(containerPath),
		Path: containerPath,
		Kind: containerFileStatKind(stat.Mode),
		Size: stat.Size,
		Mode: containerFileStatMode(stat.Mode),
	}
	if containerPath == "/" {
		entry.Name = "/"
	}
	if !stat.Mtime.IsZero() {
		entry.ModifiedAt = timestamppb.New(stat.Mtime)
	}
	if entry.Kind == agentv1.FileKind_FILE_KIND_SYMLINK {
		entry.SymlinkTarget = stat.LinkTarget
	}
	return entry
}

// containerFileStatKind reads Go's os.FileMode type bits, which is what the
// engine's archive stat reports.
func containerFileStatKind(mode uint32) agentv1.FileKind {
	const (
		modeDir       = 1 << 31
		modeSymlink   = 1 << 27
		modeDevice    = 1 << 26
		modeNamedPipe = 1 << 25
		modeSocket    = 1 << 24
		modeChar      = 1 << 21
		modeIrregular = 1 << 19
	)
	switch {
	case mode&modeDir != 0:
		return agentv1.FileKind_FILE_KIND_DIRECTORY
	case mode&modeSymlink != 0:
		return agentv1.FileKind_FILE_KIND_SYMLINK
	case mode&(modeDevice|modeNamedPipe|modeSocket|modeChar|modeIrregular) != 0:
		return agentv1.FileKind_FILE_KIND_SPECIAL
	default:
		return agentv1.FileKind_FILE_KIND_FILE
	}
}

func containerFileStatMode(mode uint32) uint32 {
	const (
		modeSetuid = 1 << 23
		modeSetgid = 1 << 22
		modeSticky = 1 << 20
		permMask   = 0o777
	)
	perm := mode & permMask
	if mode&modeSetuid != 0 {
		perm |= 0o4000
	}
	if mode&modeSetgid != 0 {
		perm |= 0o2000
	}
	if mode&modeSticky != 0 {
		perm |= 0o1000
	}
	return perm
}

func containerFileIsDir(mode uint32) bool {
	return containerFileStatKind(mode) == agentv1.FileKind_FILE_KIND_DIRECTORY
}

func containerFileIsRegular(mode uint32) bool {
	return containerFileStatKind(mode) == agentv1.FileKind_FILE_KIND_FILE
}

func containerFileIsSymlink(mode uint32) bool {
	return containerFileStatKind(mode) == agentv1.FileKind_FILE_KIND_SYMLINK
}

func containerFileRecvError(err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: the stream ended before the file was complete", errContainerFileInvalid)
	}
	return err
}

// containerFileError maps the file manager's own sentinels and the engine's
// errors onto gRPC status codes. An error that is already a status is kept.
func containerFileError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
		return err
	}
	switch {
	case errors.Is(err, errContainerFileInvalid):
		return status.Errorf(codes.InvalidArgument, "%s", err)
	case errors.Is(err, errContainerFileDenied):
		return status.Errorf(codes.PermissionDenied, "%s", err)
	case errors.Is(err, errContainerFileExists):
		return status.Errorf(codes.AlreadyExists, "%s", err)
	case errors.Is(err, errContainerFileConflict):
		return status.Errorf(codes.FailedPrecondition, "%s", err)
	case errors.Is(err, ErrDockerNotFound):
		return status.Errorf(codes.NotFound, "%s", err)
	case errors.Is(err, ErrDockerContainerNotRunning), errors.Is(err, ErrDockerNoShell):
		return status.Errorf(codes.FailedPrecondition, "%s", err)
	case errors.Is(err, ErrDockerUnavailable):
		return status.Errorf(codes.Unavailable, "%s", err)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Errorf(codes.Internal, "%s", err)
	}
}

// countingReader counts the bytes the tar reader consumed, so a listing page
// can stop at a byte budget without buffering the archive.
type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}
