package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	fileListLimit = 5000
	fileChunkSize = 256 << 10
)

const (
	RootVolumes = "volumes"
	RootCompose = "compose"
	RootSystem  = "system"
)

var dangerousFileTrees = []string{
	"/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/home", "/root",
	"/var/lib/docker",
}

var dockerDaemonConfig = "/etc/docker/daemon.json"

func dockerDataRoot() string {
	const fallback = "/var/lib/docker"
	data, err := os.ReadFile(dockerDaemonConfig)
	if err != nil {
		return fallback
	}
	var cfg struct {
		DataRoot string `json:"data-root"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || !filepath.IsAbs(cfg.DataRoot) {
		return fallback
	}
	return filepath.Clean(cfg.DataRoot)
}

type protectedPath struct {
	path      string
	exclusive bool
}

type fileRootConfig struct {
	name         string
	path         string
	writable     bool
	builtin      bool
	unrestricted bool
}

type FileServer struct {
	agentv1.UnimplementedFileServiceServer

	log                  *slog.Logger
	roots                map[string]*fsroot.Root
	order                []string
	volumeRootPath       string
	volumeRootRel        string
	maxUpload            int64
	archiveMaxBytes      int64
	archiveMaxEntries    int64
	archiveMaxInputBytes int64
	trashMaxAgeDays      int
	trashMaxBytes        int64
	transfers            chan struct{}
	transferRoot         string
	artifacts            map[string]map[string]*transferArtifact
	artifactsMu          sync.Mutex
	sizeWalks            chan struct{}
	sizeMaxEntries       int64
	locks                []sync.Mutex
	stop                 chan struct{}
	closeOnce            sync.Once
}

const fileTempSweepAge = 24 * time.Hour

const fileTempSweepInterval = time.Hour

const fileLockStripes = 64

func NewFileServer(cfg Config, log *slog.Logger) (*FileServer, error) {
	if log == nil {
		log = slog.Default()
	}
	cfg.applyDefaults()
	transferRoot := cfg.FileTransferRoot
	if abs, err := filepath.Abs(transferRoot); err == nil {
		transferRoot = abs
	}
	protected := fileProtectedPaths(cfg)
	server := &FileServer{
		log:                  log,
		roots:                make(map[string]*fsroot.Root),
		maxUpload:            cfg.FileMaxUpload,
		archiveMaxBytes:      cfg.FileArchiveMaxBytes,
		archiveMaxEntries:    cfg.FileArchiveMaxEntries,
		archiveMaxInputBytes: cfg.FileArchiveMaxInputBytes,
		trashMaxAgeDays:      cfg.FileTrashMaxAgeDays,
		trashMaxBytes:        cfg.FileTrashMaxBytes,
		transfers:            make(chan struct{}, cfg.FileMaxTransfers),
		transferRoot:         transferRoot,
		artifacts:            make(map[string]map[string]*transferArtifact),
		sizeWalks:            make(chan struct{}, fileSizeMaxConcurrent),
		sizeMaxEntries:       fileSizeMaxEntries,
		locks:                make([]sync.Mutex, fileLockStripes),
		stop:                 make(chan struct{}),
	}
	var opened []string
	for _, rc := range configuredFileRoots(cfg) {
		if rc.builtin {
			if _, err := os.Stat(rc.path); errors.Is(err, os.ErrNotExist) {
				log.Warn("file service root does not exist yet; skipping", "root", rc.name, "path", rc.path)
				continue
			}
		}
		validate := validateFileRoot
		if rc.unrestricted {
			validate = resolveFileRoot
		}
		real, identity, err := validate(rc.path, protected)
		if err != nil {
			_ = server.Close()
			return nil, err
		}
		if other, ok := overlapRoot(real, opened); ok {
			_ = server.Close()
			return nil, fmt.Errorf("file root %q overlaps root %q", rc.path, other)
		}
		name := uniqueRootName(server.roots, rc.name)
		root, err := fsroot.Open(name, real, identity, rc.writable)
		if err != nil {
			_ = server.Close()
			return nil, err
		}
		if rc.name == RootVolumes {
			server.volumeRootPath = real
		}
		if rc.unrestricted {
			server.volumeRootRel = relativeToRoot(real, cfg.ManagedVolumeRoot)
		}
		server.roots[name] = root
		server.order = append(server.order, name)
		opened = append(opened, real)
	}
	go server.sweepTempFilesLoop()
	if server.trashMaxAgeDays > 0 || server.trashMaxBytes > 0 {
		log.Info("file service trash retention enabled",
			"max_age_days", server.trashMaxAgeDays,
			"max_bytes", server.trashMaxBytes,
			"first_sweep", "now")
		go server.sweepTrashLoop()
	}
	return server, nil
}

func withinPath(base, path string) bool {
	if base == "" || path == "" || !filepath.IsAbs(base) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func overlapRoot(real string, opened []string) (string, bool) {
	for _, other := range opened {
		if withinPath(other, real) || withinPath(real, other) {
			return other, true
		}
	}
	return "", false
}

func (s *FileServer) Close() error {
	s.closeOnce.Do(func() { close(s.stop) })
	var errs []error
	for _, name := range s.order {
		if err := s.roots[name].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *FileServer) sweepTempFilesLoop() {
	now := time.Now()
	s.sweepTransfers(now)
	s.sweepTempFiles(now.Add(-fileTempSweepAge))
	ticker := time.NewTicker(fileTempSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.sweepTransfers(now)
			s.sweepTempFiles(now.Add(-fileTempSweepAge))
		}
	}
}

func (s *FileServer) sweepTempFiles(cutoff time.Time) {
	for _, name := range s.order {
		root := s.roots[name]
		if !root.Writable() {
			continue
		}
		removed, err := root.RemoveStaleTemps(fsroot.TempPrefix, cutoff)
		if err != nil {
			s.log.Warn("file service temp sweep", "root", name, "removed", removed, "error", err)
		}
	}
}

func configuredFileRoots(cfg Config) []fileRootConfig {
	if cfg.FileFullAccess {
		return []fileRootConfig{{name: RootSystem, path: string(filepath.Separator), writable: true, unrestricted: true}}
	}
	roots := []fileRootConfig{
		{name: RootVolumes, path: cfg.ManagedVolumeRoot, writable: true, builtin: true},
		{name: RootCompose, path: cfg.ComposeRoot, writable: false, builtin: true},
	}
	for _, raw := range filepath.SplitList(cfg.FileRoots) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name := filepath.Base(filepath.Clean(raw))
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = raw
		}
		roots = append(roots, fileRootConfig{name: name, path: raw, writable: false})
	}
	return roots
}

func fileProtectedPaths(cfg Config) []protectedPath {
	paths := []protectedPath{
		{path: "/var/lib/gotham", exclusive: false},
		{path: cfg.AgentStateRoot, exclusive: false},
		{path: cfg.ProxyRoot, exclusive: true},
	}
	for _, p := range []struct {
		path      string
		exclusive bool
	}{
		{cfg.CertDir, false},
		{secretFilePath(cfg.CertDir, certFileName), false},
		{secretFilePath(cfg.CertDir, keyFileName), false},
		{cfg.KeyFile, false},
		{cfg.CA, false},
		{cfg.BinaryPath, false},
		{cfg.UpdateScript, false},
		{cfg.UpdateStatusPath, false},
		{cfg.UpdatePendingPath, false},
		{cfg.UpdateLockPath, false},
		{cfg.UpdateRetryPath, false},
		{cfg.UpdateBackoffPath, false},
	} {
		if p.path == "" {
			continue
		}
		abs, err := filepath.Abs(p.path)
		if err != nil {
			continue
		}
		paths = append(paths, protectedPath{path: abs, exclusive: p.exclusive})
	}
	return paths
}

func secretFilePath(certDir, name string) string {
	if certDir == "" {
		return ""
	}
	return filepath.Join(certDir, name)
}

func relativeToRoot(root, dir string) string {
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(dir)); err == nil {
		dir = resolved
	}
	rel, err := filepath.Rel(root, filepath.Clean(dir))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func resolveFileRoot(dir string, _ []protectedPath) (string, os.FileInfo, error) {
	if !filepath.IsAbs(dir) {
		return "", nil, fmt.Errorf("file root %q must be an absolute path", dir)
	}
	real, identity, err := openRootIdentity(dir, filepath.EvalSymlinks)
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	return real, identity, nil
}

// openRootIdentity opens dir and fstats that descriptor, so the identity is the
// opened inode rather than a later stat of the resolved path. fsroot.Open
// re-checks the same identity against its own descriptor, so a component
// swapped during or after validation refuses startup instead of registering the
// swapped directory under the validated path.
func openRootIdentity(dir string, resolve func(string) (string, error)) (string, os.FileInfo, error) {
	opened, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = opened.Close() }()
	identity, err := opened.Stat(".")
	if err != nil {
		return "", nil, err
	}
	real, err := resolve(filepath.Clean(dir))
	if err != nil {
		return "", nil, err
	}
	return real, identity, nil
}

func validateFileRoot(dir string, protected []protectedPath) (string, os.FileInfo, error) {
	if !filepath.IsAbs(dir) {
		return "", nil, fmt.Errorf("file root %q must be an absolute path", dir)
	}
	real, identity, err := openRootIdentity(dir, filepath.EvalSymlinks)
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	if real == string(filepath.Separator) {
		return "", nil, fmt.Errorf("file root %q is the filesystem root", dir)
	}
	trees := append(dangerousFileTrees[:len(dangerousFileTrees):len(dangerousFileTrees)], dockerDataRoot())
	for _, tree := range trees {
		treeReal := canonicalPath(tree)
		if withinPath(tree, real) || withinPath(treeReal, real) ||
			withinPath(real, tree) || withinPath(real, treeReal) {
			return "", nil, fmt.Errorf("file root %q overlaps %s", dir, tree)
		}
	}
	for _, p := range protected {
		pp := canonicalPath(p.path)
		if p.exclusive && withinPath(pp, real) {
			return "", nil, fmt.Errorf("file root %q is inside %s", dir, p.path)
		}
		if pp == real {
			return "", nil, fmt.Errorf("file root %q is %s", dir, p.path)
		}
		if withinPath(real, pp) {
			return "", nil, fmt.Errorf("file root %q contains %s", dir, p.path)
		}
	}
	return real, identity, nil
}

func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func uniqueRootName(existing map[string]*fsroot.Root, base string) string {
	name := base
	for i := 2; ; i++ {
		if _, ok := existing[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

func (s *FileServer) ListRoots(context.Context, *agentv1.ListRootsRequest) (*agentv1.ListRootsResponse, error) {
	response := &agentv1.ListRootsResponse{}
	for _, name := range s.order {
		root := s.roots[name]
		response.Roots = append(response.Roots, &agentv1.FileRoot{
			Name:      root.Name(),
			Path:      root.Path(),
			Writable:  root.Writable(),
			FreeBytes: freeBytes(root.Path()),
		})
	}
	return response, nil
}

func (s *FileServer) List(_ context.Context, req *agentv1.ListRequest) (*agentv1.ListResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.GetCursor())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "list: %v", err)
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "list: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "list", rel); err != nil {
		return nil, err
	}
	entries, truncated, last, err := root.List(rel, cursor, fileListLimit)
	if err != nil {
		return nil, fileError("list", err)
	}
	return &agentv1.ListResponse{
		Entries:   fileEntries(hideTrashEntries(rel, entries)),
		Truncated: truncated,
		Cursor:    encodeCursor(last),
	}, nil
}

func encodeCursor(name string) string {
	if name == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(name))
}

func decodeCursor(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("invalid cursor %q: %w", token, err)
	}
	return string(raw), nil
}

func (s *FileServer) Stat(_ context.Context, req *agentv1.StatRequest) (*agentv1.StatResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if err := guardTrashRaw(root, "stat", req.GetPath()); err != nil {
		return nil, err
	}
	entry, err := root.Stat(req.GetPath())
	if err != nil {
		return nil, fileError("stat", err)
	}
	return &agentv1.StatResponse{Entry: fileEntry(entry)}, nil
}

func (s *FileServer) Read(req *agentv1.ReadRequest, stream grpc.ServerStreamingServer[agentv1.ReadChunk]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	if err := guardTrashRaw(root, "read", req.GetPath()); err != nil {
		return err
	}
	if req.GetOffset() < 0 || req.GetLimit() < 0 {
		return status.Error(codes.InvalidArgument, "read: offset and limit must not be negative")
	}
	file, _, err := root.OpenRegular(req.GetPath())
	if err != nil {
		return fileError("read", err)
	}
	defer func() { _ = file.Close() }()

	if offset := req.GetOffset(); offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return fileError("read", err)
		}
	}
	var source io.Reader = file
	if limit := req.GetLimit(); limit > 0 {
		source = io.LimitReader(file, limit)
	}

	buffer := make([]byte, fileChunkSize)
	for {
		n, readErr := source.Read(buffer)
		if n > 0 {
			if sendErr := stream.Send(&agentv1.ReadChunk{Data: buffer[:n]}); sendErr != nil {
				return sendErr
			}
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return nil
		case readErr != nil:
			return fileError("read", readErr)
		}
	}
}

func (s *FileServer) root(name string) (*fsroot.Root, error) {
	root, ok := s.roots[name]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown file root %q", name)
	}
	return root, nil
}

func fileEntries(entries []fsroot.Entry) []*agentv1.FileEntry {
	if len(entries) == 0 {
		return nil
	}
	names := newNameCache()
	out := make([]*agentv1.FileEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, fileEntryWith(entry, names))
	}
	return out
}

func fileEntry(entry fsroot.Entry) *agentv1.FileEntry {
	return fileEntryWith(entry, newNameCache())
}

type nameCache struct {
	users  map[uint32]string
	groups map[uint32]string
}

func newNameCache() *nameCache {
	return &nameCache{users: map[uint32]string{}, groups: map[uint32]string{}}
}

func (c *nameCache) owner(uid uint32) string {
	if name, ok := c.users[uid]; ok {
		return name
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	c.users[uid] = name
	return name
}

func (c *nameCache) group(gid uint32) string {
	if name, ok := c.groups[gid]; ok {
		return name
	}
	name := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(name); err == nil {
		name = g.Name
	}
	c.groups[gid] = name
	return name
}

func fileEntryWith(entry fsroot.Entry, names *nameCache) *agentv1.FileEntry {
	out := &agentv1.FileEntry{
		Name:          validUTF8(entry.Name),
		Kind:          fileKind(entry.Kind),
		Size:          entry.Size,
		Mode:          uint32(entry.Mode.Perm()),
		SymlinkTarget: validUTF8(entry.SymlinkTarget),
		Etag:          entry.Etag,
		Owner:         validUTF8(names.owner(entry.UID)),
		Group:         validUTF8(names.group(entry.GID)),
	}
	if !entry.Modified.IsZero() {
		out.ModifiedAt = timestamppb.New(entry.Modified)
	}
	return out
}

func validUTF8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

func fileKind(kind fsroot.Kind) agentv1.FileKind {
	switch kind {
	case fsroot.KindRegular:
		return agentv1.FileKind_FILE_KIND_FILE
	case fsroot.KindDirectory:
		return agentv1.FileKind_FILE_KIND_DIRECTORY
	case fsroot.KindSymlink:
		return agentv1.FileKind_FILE_KIND_SYMLINK
	default:
		return agentv1.FileKind_FILE_KIND_SPECIAL
	}
}

func fileError(action string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, fsroot.ErrInvalid):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrEscape):
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrNotFound):
		return status.Errorf(codes.NotFound, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrPermission):
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrSpecial):
		return status.Errorf(codes.FailedPrecondition, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrExists):
		return status.Errorf(codes.AlreadyExists, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrNotEmpty):
		return status.Errorf(codes.FailedPrecondition, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrConflict):
		return status.Errorf(codes.Aborted, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrReadOnly):
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrTooLarge):
		return status.Errorf(codes.ResourceExhausted, "%s: %v", action, err)
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT), errors.Is(err, syscall.EFBIG):
		return status.Errorf(codes.ResourceExhausted, "%s: %v", action, err)
	case errors.Is(err, syscall.ENAMETOOLONG):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ENOTDIR):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, syscall.EROFS):
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, action+": context canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, action+": deadline exceeded")
	default:
		return status.Errorf(codes.Internal, "%s: %v", action, err)
	}
}

func freeBytes(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

func totalBytes(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Blocks) * uint64(stat.Bsize)
}
