package docker

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// volumeArchiveImage is the pinned helper image. It provides sh, find, du,
// stat, tar, gzip and sha256sum through BusyBox; the agent pulls it on first
// use only.
const volumeArchiveImage = "alpine:3.20"

const (
	volumeArchiveCapability = "volume_archive.v1"
	volumeArchiveMountRoot  = "/gotham-archive"
	volumeArchiveOutDir     = volumeArchiveMountRoot + "/out"

	archiveChunkSize      = 64 << 10
	archiveMaxSources     = 32
	archiveManifestLimit  = 64 << 10
	archiveTrailerLimit   = 4 << 10
	archiveStderrLimit    = 4 << 10
	archiveMaxEntries     = 1_000_000
	archiveMaxRestoreSize = 64 << 30
	archiveHelperTimeout  = 30 * time.Second
	archiveExitPoll       = 100 * time.Millisecond
)

var archiveSourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var archiveVolumeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)

var archiveDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// archiveManifest is the first tar entry of every archive. Keep formatVersion
// in step with the restore validator; an unknown version is refused rather
// than guessed.
type archiveManifest struct {
	FormatVersion int                     `json:"format_version"`
	Sources       []archiveManifestSource `json:"sources"`
}

type archiveManifestSource struct {
	Name      string `json:"name"`
	SizeBytes uint64 `json:"size_bytes"`
	MtimeUnix int64  `json:"mtime_unix"`
}

// archiveTrailer is written after the archive and reports exactly what the
// helper produced. The agent hashes the bytes it streams and compares; a
// mismatch fails the call.
type archiveTrailer struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type archiveSource struct {
	name   string
	volume string
	path   string
}

// archiveScript writes the manifest and the archive to a file, then records
// its exact size and SHA-256. The archive never travels through the container
// log driver, whose rotation can truncate large streams.
const archiveScript = `set -eu
cd /gotham-archive
mkdir -p out
printf '{"format_version":1,"sources":[' > manifest.json
first=1
for name in $GOTHAM_ARCHIVE_SOURCES; do
  if [ "$first" -eq 1 ]; then first=0; else printf ',' >> manifest.json; fi
  size=$(du -sb "sources/$name" | cut -f1)
  mtime=$(stat -c %Y "sources/$name")
  printf '{"name":"%s","size_bytes":%s,"mtime_unix":%s}' "$name" "$size" "$mtime" >> manifest.json
done
printf ']}' >> manifest.json
tar -czf out/archive.tar.gz manifest.json sources
size=$(wc -c < out/archive.tar.gz)
sum=$(sha256sum out/archive.tar.gz | cut -d' ' -f1)
printf '{"bytes":%s,"sha256":"%s"}' "$size" "$sum" > out/trailer.json
`

// restoreClearScript empties each mounted target before the archive is
// imported. The mount points themselves must survive.
const restoreClearScript = `set -eu
cd /gotham-archive
for name in $GOTHAM_ARCHIVE_SOURCES; do
  find "sources/$name" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
done
`

func (s *DockerServer) VolumeArchiveCapabilities(context.Context, *agentv1.VolumeArchiveCapabilitiesRequest) (*agentv1.VolumeArchiveCapabilitiesResponse, error) {
	return &agentv1.VolumeArchiveCapabilitiesResponse{
		Available:    true,
		Capabilities: []string{volumeArchiveCapability},
	}, nil
}

func (s *DockerServer) ArchiveVolumes(req *agentv1.ArchiveVolumesRequest, stream grpc.ServerStreamingServer[agentv1.ArchiveChunk]) error {
	ctx := stream.Context()
	sources, err := s.resolveArchiveSources(ctx, req.GetSources())
	if err != nil {
		return err
	}

	runID := uuid.NewString()
	id, err := s.createArchiveHelper(ctx, sources, runID, false, archiveScript)
	if err != nil {
		return err
	}
	defer s.removeArchiveHelper(ctx, id, runID)
	if err := s.docker.Start(ctx, id); err != nil {
		return dockerError("archive volumes", err)
	}
	details, err := s.inspectUntilExited(ctx, id)
	if err != nil {
		return dockerError("archive volumes", err)
	}
	if details.ExitCode != 0 {
		return s.sendArchiveFailure(stream, ctx, id, details.ExitCode, "")
	}

	trailer, err := s.readArchiveTrailer(ctx, id)
	if err != nil {
		return err
	}
	sent, digest, err := s.streamContainerFile(ctx, id, volumeArchiveOutDir+"/archive.tar.gz", stream)
	if err != nil {
		return err
	}
	if sent != trailer.Bytes || digest != trailer.SHA256 {
		return s.sendArchiveFailure(stream, ctx, id, details.ExitCode, fmt.Sprintf(
			"archive integrity mismatch: helper produced %d bytes (sha256 %s), agent streamed %d bytes (sha256 %s)",
			trailer.Bytes, trailer.SHA256, sent, digest))
	}
	return stream.Send(&agentv1.ArchiveChunk{Part: &agentv1.ArchiveChunk_Result{Result: &agentv1.ArchiveResult{
		Ok:         true,
		ExitCode:   details.ExitCode,
		Bytes:      uint64(sent),
		Sha256:     digest,
		StderrTail: s.archiveStderrTail(ctx, id),
	}}})
}

func (s *DockerServer) sendArchiveFailure(stream grpc.ServerStreamingServer[agentv1.ArchiveChunk], ctx context.Context, id string, exitCode int32, message string) error {
	if message == "" {
		message = fmt.Sprintf("archive helper exited with code %d", exitCode)
	}
	return stream.Send(&agentv1.ArchiveChunk{Part: &agentv1.ArchiveChunk_Result{Result: &agentv1.ArchiveResult{
		Ok:         false,
		ExitCode:   exitCode,
		Error:      message,
		StderrTail: s.archiveStderrTail(ctx, id),
	}}})
}

func (s *DockerServer) archiveStderrTail(ctx context.Context, id string) string {
	chunks, err := s.docker.Logs(ctx, id, false, 20)
	if err != nil {
		return ""
	}
	var tail limitedTail
	for message := range chunks {
		if message.Err != nil {
			break
		}
		if message.Stderr {
			tail.Write(message.Data)
		}
	}
	return tail.String()
}

func (s *DockerServer) readArchiveTrailer(ctx context.Context, id string) (archiveTrailer, error) {
	raw, err := s.readContainerFile(ctx, id, volumeArchiveOutDir+"/trailer.json", archiveTrailerLimit)
	if err != nil {
		return archiveTrailer{}, err
	}
	var trailer archiveTrailer
	if err := json.Unmarshal(raw, &trailer); err != nil {
		return archiveTrailer{}, status.Error(codes.Internal, "archive volumes: the helper trailer is not valid JSON")
	}
	if trailer.Bytes <= 0 || !archiveDigestPattern.MatchString(strings.ToLower(trailer.SHA256)) {
		return archiveTrailer{}, status.Error(codes.Internal, "archive volumes: the helper trailer is incomplete")
	}
	trailer.SHA256 = strings.ToLower(trailer.SHA256)
	return trailer, nil
}

func (s *DockerServer) readContainerFile(ctx context.Context, containerID, source string, limit int64) ([]byte, error) {
	body, err := s.docker.ExportArchive(ctx, containerID, source)
	if err != nil {
		return nil, dockerError("read "+source, err)
	}
	defer func() { _ = body.Close() }()
	reader, err := archiveTarEntry(body, source)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "archive volumes: read %s: %v", source, err)
	}
	if int64(len(data)) > limit {
		return nil, status.Errorf(codes.Internal, "archive volumes: %s is larger than %d bytes", source, limit)
	}
	return data, nil
}

// streamContainerFile copies one container file into the gRPC stream in bounded
// chunks while hashing exactly the bytes it forwards. The wrapper tar's size is
// checked against the helper's count before anything is sent.
func (s *DockerServer) streamContainerFile(ctx context.Context, containerID, source string, stream grpc.ServerStreamingServer[agentv1.ArchiveChunk]) (int64, string, error) {
	body, err := s.docker.ExportArchive(ctx, containerID, source)
	if err != nil {
		return 0, "", dockerError("archive volumes", err)
	}
	defer func() { _ = body.Close() }()
	reader, err := archiveTarEntry(body, source)
	if err != nil {
		return 0, "", err
	}
	hasher := sha256.New()
	buffer := make([]byte, archiveChunkSize)
	var sent int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			hasher.Write(buffer[:n])
			chunk := buffer[:n]
			for len(chunk) > 0 {
				size := min(len(chunk), archiveChunkSize)
				if sendErr := stream.Send(&agentv1.ArchiveChunk{
					Part: &agentv1.ArchiveChunk_Data{Data: chunk[:size]},
				}); sendErr != nil {
					return sent, "", sendErr
				}
				chunk = chunk[size:]
			}
			sent += int64(n)
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return sent, hex.EncodeToString(hasher.Sum(nil)), nil
		case readErr != nil:
			return sent, "", status.Errorf(codes.Internal, "archive volumes: read archive: %v", readErr)
		}
	}
}

func archiveTarEntry(body io.Reader, source string) (io.Reader, error) {
	reader := tar.NewReader(body)
	header, err := reader.Next()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "archive volumes: read %s: %v", path.Base(source), err)
	}
	if header.Name != path.Base(source) || header.Typeflag != tar.TypeReg {
		return nil, status.Errorf(codes.Internal, "archive volumes: unexpected archive entry %q", header.Name)
	}
	return reader, nil
}

func (s *DockerServer) RestoreVolumes(stream grpc.ClientStreamingServer[agentv1.RestoreVolumesRequest, agentv1.RestoreVolumesResponse]) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return streamRecvError("restore volumes", err)
	}
	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "restore volumes: the first message must be a header")
	}
	targets, err := s.resolveArchiveSources(ctx, header.GetTargets())
	if err != nil {
		return err
	}
	replace := false
	switch header.GetMode() {
	case agentv1.RestoreMode_RESTORE_MODE_REPLACE:
		replace = true
	case agentv1.RestoreMode_RESTORE_MODE_OVERLAY:
	case agentv1.RestoreMode_RESTORE_MODE_UNSPECIFIED:
		return status.Error(codes.InvalidArgument, "restore volumes: a restore mode is required")
	default:
		return status.Errorf(codes.InvalidArgument, "restore volumes: unsupported mode %s", header.GetMode())
	}

	archive, err := os.CreateTemp("", "gotham-restore-*.tar.gz")
	if err != nil {
		return status.Errorf(codes.Internal, "restore volumes: stage archive: %v", err)
	}
	defer func() {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
	}()

	var received int64
	for {
		message, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return streamRecvError("restore volumes", recvErr)
		}
		data := message.GetData()
		if len(data) == 0 {
			continue
		}
		received += int64(len(data))
		if received > archiveMaxRestoreSize {
			return status.Errorf(codes.ResourceExhausted, "restore volumes: archive exceeds the %d byte cap", int64(archiveMaxRestoreSize))
		}
		if _, writeErr := archive.Write(data); writeErr != nil {
			return status.Errorf(codes.Internal, "restore volumes: stage archive: %v", writeErr)
		}
	}
	if received == 0 {
		return status.Error(codes.InvalidArgument, "restore volumes: the stream carried no archive data")
	}
	if err := validateArchive(archive, targets); err != nil {
		return err
	}

	runID := uuid.NewString()
	script := "true"
	if replace {
		script = restoreClearScript
	}
	id, err := s.createArchiveHelper(ctx, targets, runID, true, script)
	if err != nil {
		return err
	}
	defer s.removeArchiveHelper(ctx, id, runID)

	if err := s.docker.Start(ctx, id); err != nil {
		return dockerError("restore volumes", err)
	}
	if err := s.waitArchiveHelper(ctx, id, "restore volumes"); err != nil {
		return err
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return status.Errorf(codes.Internal, "restore volumes: rewind archive: %v", err)
	}
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return status.Error(codes.InvalidArgument, "restore volumes: the archive is not valid gzip")
	}
	defer func() { _ = compressed.Close() }()
	if err := s.docker.ImportArchive(ctx, id, volumeArchiveMountRoot, compressed); err != nil {
		return dockerError("restore volumes", err)
	}
	return stream.SendAndClose(&agentv1.RestoreVolumesResponse{RestoredSources: int32(len(targets))})
}

func (s *DockerServer) inspectUntilExited(ctx context.Context, id string) (ContainerDetails, error) {
	for {
		details, err := s.docker.InspectContainer(ctx, id)
		if err != nil {
			return ContainerDetails{}, err
		}
		if details.State == "exited" {
			return details, nil
		}
		select {
		case <-ctx.Done():
			return ContainerDetails{}, ctx.Err()
		case <-time.After(archiveExitPoll):
		}
	}
}

func (s *DockerServer) waitArchiveHelper(ctx context.Context, id, action string) error {
	details, err := s.inspectUntilExited(ctx, id)
	if err != nil {
		return status.FromContextError(err).Err()
	}
	if details.ExitCode != 0 {
		return status.Errorf(codes.Internal, "%s: helper exited with code %d", action, details.ExitCode)
	}
	return nil
}

func (s *DockerServer) createArchiveHelper(ctx context.Context, sources []archiveSource, runID string, writable bool, script string) (string, error) {
	names := make([]string, 0, len(sources))
	mounts := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.name)
		mode := "ro"
		if writable {
			mode = "rw"
		}
		origin := source.path
		if source.volume != "" {
			origin = source.volume
		}
		mounts = append(mounts, origin+":"+volumeArchiveMountRoot+"/sources/"+source.name+":"+mode)
	}
	request := &agentv1.CreateContainerRequest{
		Image:       volumeArchiveImage,
		Name:        "gotham-archive-" + runID,
		Command:     []string{"sh", "-c", script},
		Env:         []string{"GOTHAM_ARCHIVE_SOURCES=" + strings.Join(names, " ")},
		Volumes:     mounts,
		MemoryBytes: 64 << 20,
		// Diagnostics only: the archive travels through out/ and the Docker
		// archive API, so a tiny rotation can never truncate it.
		LogConfig: &agentv1.ContainerLogConfig{
			Driver:  "json-file",
			Options: map[string]string{"max-size": "64k", "max-file": "1"},
		},
		Labels: map[string]string{
			"gotham.managed":           "true",
			"gotham.volume_archive":    runID,
			"gotham.volume_archive_op": archiveOperation(writable),
		},
	}
	id, err := s.docker.CreateContainer(ctx, request)
	if errors.Is(err, ErrDockerImageNotFound) {
		if _, pullErr := s.docker.PullImage(ctx, volumeArchiveImage, "", ""); pullErr != nil {
			return "", dockerError("archive volumes: pull helper image", pullErr)
		}
		id, err = s.docker.CreateContainer(ctx, request)
	}
	if err != nil {
		return "", dockerError("archive volumes: create helper", err)
	}
	return id, nil
}

func archiveOperation(writable bool) string {
	if writable {
		return "restore"
	}
	return "archive"
}

func (s *DockerServer) removeArchiveHelper(ctx context.Context, id, runID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), archiveHelperTimeout)
	defer cancel()
	if err := s.docker.Remove(cleanupCtx, id); err != nil {
		s.log.Warn("docker: could not remove volume archive helper", "container_id", id, "archive", runID, "error", err)
	}
}

// resolveArchiveSources validates the caller's declarations before any
// container starts. Bind paths are resolved through symlinks first, so a link
// cannot smuggle a denied path past the check. Named volumes are resolved to
// their mountpoint so the same deny list (ancestors included) applies.
func (s *DockerServer) resolveArchiveSources(ctx context.Context, sources []*agentv1.ArchiveSource) ([]archiveSource, error) {
	if len(sources) == 0 {
		return nil, status.Error(codes.InvalidArgument, "archive volumes: at least one source is required")
	}
	if len(sources) > archiveMaxSources {
		return nil, status.Errorf(codes.InvalidArgument, "archive volumes: at most %d sources are allowed", archiveMaxSources)
	}
	seen := make(map[string]struct{}, len(sources))
	resolved := make([]archiveSource, 0, len(sources))
	for _, source := range sources {
		name := source.GetName()
		if !archiveSourceNamePattern.MatchString(name) {
			return nil, status.Errorf(codes.InvalidArgument, "archive volumes: invalid source name %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, status.Errorf(codes.InvalidArgument, "archive volumes: duplicate source name %q", name)
		}
		seen[name] = struct{}{}
		item := archiveSource{name: name}
		switch {
		case source.GetVolume() != "":
			volume := source.GetVolume()
			if !archiveVolumeNamePattern.MatchString(volume) {
				return nil, status.Errorf(codes.InvalidArgument, "archive volumes: invalid volume name %q", volume)
			}
			mountpoint, err := s.docker.VolumeMountpoint(ctx, volume)
			if err != nil {
				return nil, dockerError("archive volumes: volume "+volume, err)
			}
			// The mountpoint may live inside the daemon's own VM on a
			// development host, so it is checked, not stat'ed.
			if s.deniedArchivePath(mountpoint) {
				return nil, status.Errorf(codes.PermissionDenied, "archive volumes: volume %q is not allowed", volume)
			}
			item.volume = volume
		case source.GetPath() != "":
			hostPath, err := s.archiveBindPath(source.GetPath())
			if err != nil {
				return nil, err
			}
			item.path = hostPath
		default:
			return nil, status.Errorf(codes.InvalidArgument, "archive volumes: source %q has neither a volume nor a path", name)
		}
		resolved = append(resolved, item)
	}
	return resolved, nil
}

func (s *DockerServer) archiveBindPath(raw string) (string, error) {
	cleaned := filepath.Clean(raw)
	if !filepath.IsAbs(cleaned) {
		return "", status.Errorf(codes.InvalidArgument, "archive volumes: path %q is not absolute", raw)
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", status.Errorf(codes.InvalidArgument, "archive volumes: path %q is not readable: %v", raw, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", status.Errorf(codes.InvalidArgument, "archive volumes: path %q is not readable: %v", raw, err)
	}
	if !info.IsDir() {
		return "", status.Errorf(codes.InvalidArgument, "archive volumes: path %q is not a directory", raw)
	}
	if s.deniedArchivePath(resolved) || s.deniedArchivePath(cleaned) {
		return "", status.Errorf(codes.PermissionDenied, "archive volumes: path %q is not allowed", raw)
	}
	return resolved, nil
}

func (s *DockerServer) deniedArchivePath(candidate string) bool {
	// Check the resolved and lexical forms on both sides: a symlinked system
	// path must not hide an ancestor of a denied path, and a denied path that
	// does not exist yet must still match its real parent.
	candidates := []string{archiveCanonicalPath(candidate), filepath.Clean(candidate)}
	for _, denied := range s.archiveDeniedPaths {
		deniedForms := []string{archiveCanonicalPath(denied), filepath.Clean(denied)}
		for _, c := range candidates {
			for _, d := range deniedForms {
				if c == d {
					return true
				}
				// "/" is denied as the root itself, not as every path below it.
				if d == "/" {
					continue
				}
				// Reject a source inside a denied path and a source that contains
				// one: /var would otherwise expose the agent state directory.
				if withinPath(d, c) || withinPath(c, d) {
					return true
				}
			}
		}
	}
	return false
}

// archiveCanonicalPath resolves symlinks in the longest existing prefix so a
// denied path that does not exist yet still compares correctly with a parent
// that does (for example /var vs /var/lib/gotham-agent on a symlinked /var).
func archiveCanonicalPath(raw string) string {
	cleaned := filepath.Clean(raw)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		return cleaned
	}
	return filepath.Join(archiveCanonicalPath(parent), filepath.Base(cleaned))
}

func validateArchive(file *os.File, targets []archiveSource) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return status.Errorf(codes.Internal, "restore volumes: rewind archive: %v", err)
	}
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return status.Error(codes.InvalidArgument, "restore volumes: the archive is not valid gzip")
	}
	defer func() { _ = compressed.Close() }()
	reader := tar.NewReader(compressed)

	first, err := reader.Next()
	if err != nil {
		return status.Error(codes.InvalidArgument, "restore volumes: the archive is empty")
	}
	if first.Name != "manifest.json" || first.Typeflag != tar.TypeReg {
		return status.Error(codes.InvalidArgument, "restore volumes: the archive does not start with its manifest")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, archiveManifestLimit+1))
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "restore volumes: read manifest: %v", err)
	}
	if len(raw) > archiveManifestLimit {
		return status.Error(codes.InvalidArgument, "restore volumes: the manifest is too large")
	}
	var manifest archiveManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return status.Error(codes.InvalidArgument, "restore volumes: the manifest is not valid JSON")
	}
	if manifest.FormatVersion != 1 {
		return status.Errorf(codes.InvalidArgument, "restore volumes: unsupported archive format version %d", manifest.FormatVersion)
	}
	expected := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		expected[target.name] = struct{}{}
	}
	if len(manifest.Sources) != len(expected) {
		return status.Error(codes.InvalidArgument, "restore volumes: the manifest does not match the requested targets")
	}
	for _, source := range manifest.Sources {
		if _, ok := expected[source.Name]; !ok {
			return status.Errorf(codes.InvalidArgument, "restore volumes: the archive contains undocumented source %q", source.Name)
		}
		delete(expected, source.Name)
	}
	if len(expected) != 0 {
		return status.Error(codes.InvalidArgument, "restore volumes: the manifest does not document every target")
	}

	entries := 0
	seen := make(map[string]struct{}, len(targets))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			for _, target := range targets {
				if _, ok := seen[target.name]; !ok {
					return status.Errorf(codes.InvalidArgument, "restore volumes: the archive has no entries for source %q", target.name)
				}
			}
			return nil
		}
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "restore volumes: read archive: %v", err)
		}
		entries++
		if entries > archiveMaxEntries {
			return status.Errorf(codes.ResourceExhausted, "restore volumes: the archive exceeds the %d entry cap", archiveMaxEntries)
		}
		if !safeArchiveMember(header.Name, targets) {
			return status.Errorf(codes.InvalidArgument, "restore volumes: unsafe archive entry %q", header.Name)
		}
		parts := strings.Split(strings.TrimSuffix(header.Name, "/"), "/")
		if len(parts) > 1 {
			seen[parts[1]] = struct{}{}
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink:
		case tar.TypeLink:
			if !safeArchiveMember(header.Linkname, targets) {
				return status.Errorf(codes.InvalidArgument, "restore volumes: unsafe archive link %q", header.Linkname)
			}
		default:
			return status.Errorf(codes.InvalidArgument, "restore volumes: unsupported archive entry type for %q", header.Name)
		}
	}
}

// safeArchiveMember keeps every entry under sources/<declared name> and refuses
// absolute names, parent traversal and empty components.
func safeArchiveMember(name string, targets []archiveSource) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return false
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	if parts[0] != "sources" {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	for _, target := range targets {
		if parts[1] == target.name {
			return true
		}
	}
	return false
}

// limitedTail keeps the last n bytes written to it.
type limitedTail struct {
	data []byte
}

func (t *limitedTail) Write(data []byte) {
	if len(data) >= archiveStderrLimit {
		t.data = append(t.data[:0], data[len(data)-archiveStderrLimit:]...)
		return
	}
	t.data = append(t.data, data...)
	if len(t.data) > archiveStderrLimit {
		t.data = append(t.data[:0], t.data[len(t.data)-archiveStderrLimit:]...)
	}
}

func (t *limitedTail) String() string {
	return strings.TrimSpace(string(t.data))
}

func streamRecvError(action string, err error) error {
	if errors.Is(err, io.EOF) {
		return status.Errorf(codes.InvalidArgument, "%s: the stream ended before it was complete", action)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return status.Errorf(codes.Internal, "%s: %v", action, err)
}
