package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	// fileListLimit caps one List page. A larger directory is paginated with
	// the cursor so one call cannot make the agent build an unbounded
	// response.
	fileListLimit = 5000
	// fileChunkSize bounds a Read chunk; it must stay under the agent's 2 MiB
	// receive cap so a client never has to raise its buffer.
	fileChunkSize = 256 << 10
)

// rootNames are the fixed identifiers the two built-in roots are addressed by.
const (
	fileRootVolumes = "volumes"
	fileRootCompose = "compose"
	// fileRootSystem is the whole-filesystem root of the full-access mode.
	fileRootSystem = "system"
)

// dangerousFileTrees are whole trees the file service must never expose: the
// system configuration and runtime directories, the package and home trees and
// Docker's data root. A browsable root at or under any of them is refused.
var dangerousFileTrees = []string{
	"/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/home", "/root",
	"/var/lib/docker",
}

// dockerDaemonConfig is Docker's daemon configuration file, read to learn a
// non-default data root. It is a variable so a test can point it at a fixture.
var dockerDaemonConfig = "/etc/docker/daemon.json"

// dockerDataRoot returns the data root Docker is configured with: the
// data-root from daemon.json when it declares an absolute one, else the
// default /var/lib/docker.
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

// protectedPath is a location a browsable root must not collide with. A root
// that is, or is an ancestor of, path is always refused (so a parent that
// contains the location cannot be browsed around it). exclusive paths also
// refuse a root nested inside them, which protects a directory whose children
// are all sensitive (the certificate and proxy directories).
type protectedPath struct {
	path      string
	exclusive bool
}

// fileRootConfig is one allow-listed root before validation.
type fileRootConfig struct {
	name     string
	path     string
	writable bool
	// builtin marks the two managed roots, whose absence is normal (Docker and
	// the compose service create them lazily) and must not abort startup.
	builtin bool
	// unrestricted skips the dangerous-tree and protected-path checks: the
	// operator opted into whole-filesystem access (root "/").
	unrestricted bool
}

// FileServer implements agentv1.FileServiceServer against the node's
// allow-listed roots. It is constructed only when the file service is enabled;
// a disabled agent never registers it.
type FileServer struct {
	agentv1.UnimplementedFileServiceServer

	log   *slog.Logger
	roots map[string]*fsroot.Root
	order []string
	// volumeRootPath is the canonical path of the writable managed-volumes
	// root, whose top-level application directories are protected from rename
	// and delete because running containers bind-mount them.
	volumeRootPath string
	// volumeRootRel is the managed-volumes root relative to the whole-filesystem
	// "system" root (full-access mode), or "" when that mode is off. It keeps the
	// application-directory guard working when the volumes root is no longer a
	// root of its own.
	volumeRootRel string
	// maxUpload bounds one upload in bytes; uploads over it are refused.
	maxUpload int64
	// transfers is a counting semaphore bounding concurrent file transfers.
	transfers chan struct{}
	// locks are striped mutexes serialising the commit of a write to one
	// (root, path), so a concurrent writer sees the previous commit's etag.
	locks []sync.Mutex
	// stop closes to end the background staging-file sweeper; closeOnce makes
	// Close idempotent.
	stop      chan struct{}
	closeOnce sync.Once
}

// fileTempSweepAge is how old a leaked staging file must be before the sweeper
// removes it: old enough that no in-flight upload could still own it.
const fileTempSweepAge = 24 * time.Hour

// fileTempSweepInterval is how often the background sweeper runs.
const fileTempSweepInterval = time.Hour

// fileLockStripes is the number of striped path locks. A stripe is shared by
// unrelated paths, which only adds contention; the map-free design keeps the
// lock set bounded no matter how many paths a node serves.
const fileLockStripes = 64

// NewFileServer validates and opens the configured roots. It fails closed: a
// root that is not absolute, resolves into a dangerous tree or collides with a
// protected path aborts startup rather than being dropped. A built-in root
// that does not exist yet is skipped with a warning, because Docker and the
// compose service create those directories lazily.
func NewFileServer(cfg Config, log *slog.Logger) (*FileServer, error) {
	if log == nil {
		log = slog.Default()
	}
	protected := fileProtectedPaths(cfg)
	maxUpload := cfg.FileMaxUpload
	if maxUpload <= 0 {
		maxUpload = defaultFileMaxUpload
	}
	maxTransfers := cfg.FileMaxTransfers
	if maxTransfers <= 0 {
		maxTransfers = defaultFileMaxTransfers
	}
	server := &FileServer{
		log:       log,
		roots:     make(map[string]*fsroot.Root),
		maxUpload: maxUpload,
		transfers: make(chan struct{}, maxTransfers),
		locks:     make([]sync.Mutex, fileLockStripes),
		stop:      make(chan struct{}),
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
		if rc.name == fileRootVolumes {
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
	return server, nil
}

// overlapRoot reports the first already-opened root that is equal to, contains
// or is contained by real, so two browsable roots can never shadow each other.
func overlapRoot(real string, opened []string) (string, bool) {
	for _, other := range opened {
		if withinPath(other, real) || withinPath(real, other) {
			return other, true
		}
	}
	return "", false
}

// Close releases every root descriptor and stops the staging-file sweeper.
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

// sweepTempFilesLoop sweeps once at startup and then hourly until Close. A
// crashed upload could otherwise leave a ".gotham-tmp-*" file behind forever.
func (s *FileServer) sweepTempFilesLoop() {
	s.sweepTempFiles(time.Now().Add(-fileTempSweepAge))
	ticker := time.NewTicker(fileTempSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.sweepTempFiles(now.Add(-fileTempSweepAge))
		}
	}
}

// sweepTempFiles removes staging files older than cutoff from every writable
// root. It never follows a symlink, so a link planted in the tree cannot make
// the sweep reach outside the root.
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

// configuredFileRoots returns the allow-list in a stable order: the managed
// volumes root (writable) first, then the read-only compose root, then every
// operator-added root (read-only, because writes have no owner-configured
// intent yet).
func configuredFileRoots(cfg Config) []fileRootConfig {
	if cfg.FileFullAccess {
		return []fileRootConfig{{name: fileRootSystem, path: string(filepath.Separator), writable: true, unrestricted: true}}
	}
	roots := []fileRootConfig{
		{name: fileRootVolumes, path: cfg.ManagedVolumeRoot, writable: true, builtin: true},
		{name: fileRootCompose, path: cfg.ComposeRoot, writable: false, builtin: true},
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

// fileProtectedPaths lists the locations a root must not overlap: the agent's
// own state, certificate, binary and proxy directories, the control plane's
// data root and its config directory.
func fileProtectedPaths(cfg Config) []protectedPath {
	paths := []protectedPath{
		// The control plane's data root: browsing it directly is refused, but
		// its managed-volume child is exactly the writable root the node
		// exposes, so a descendant is allowed.
		{path: "/var/lib/gotham", exclusive: false},
		// The agent's own state directory: a root that is or contains it is
		// refused, while the compose child stays browsable read-only.
		{path: defaultAgentStateRoot, exclusive: false},
		// The proxy directory holds the ACME private keys, so nothing under it
		// may be browsed.
		{path: defaultTraefikDir, exclusive: true},
	}
	// The agent's secret and state locations come from config, so a non-default
	// install is protected too. The cert dir is protected non-exclusively, like
	// the state root above: a root that is or contains it is refused, but the
	// default compose root, a child of it, stays browsable (the shipped unit
	// sets GOTHAM_AGENT_CERT_DIR=/var/lib/gotham-agent, the very dir the
	// default compose root lives in). The certificate and key files directly
	// under it are protected individually, so a root that would otherwise
	// contain one is still refused. Entries are relative-safe: one that cannot
	// be made absolute is skipped rather than widened.
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

// secretFilePath returns the path of one of the agent's secret files directly
// under certDir, or "" when certDir is unset. The guard matters: filepath.Join
// would turn an empty cert dir into a relative "agent.key" and, once made
// absolute, silently protect an unrelated path under the working directory.
func secretFilePath(certDir, name string) string {
	if certDir == "" {
		return ""
	}
	return filepath.Join(certDir, name)
}

// relativeToRoot returns dir (resolved through symlinks when it exists) as a
// slash path relative to root, or "" when it is not inside root.
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

// resolveFileRoot is validateFileRoot without the safety checks, used only for
// the operator-approved whole-filesystem root: it still requires an absolute
// path that resolves to a directory.
func resolveFileRoot(dir string, _ []protectedPath) (string, os.FileInfo, error) {
	if !filepath.IsAbs(dir) {
		return "", nil, fmt.Errorf("file root %q must be an absolute path", dir)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	identity, err := os.Stat(real)
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	return real, identity, nil
}

// validateFileRoot returns the canonical absolute path of a browsable root and
// the directory identity (both captured from the same stat), or an error when
// it must not be browsed. The path is resolved through symlinks first (so a
// root that is a symlink into /etc is judged by where it really points, and the
// legitimate /var → /private/var link on macOS does not block an operator
// root), then checked against the dangerous trees and the protected paths. The
// identity travels to fsroot.Open so the descriptor actually opened is proven
// to be the directory that was validated.
func validateFileRoot(dir string, protected []protectedPath) (string, os.FileInfo, error) {
	if !filepath.IsAbs(dir) {
		return "", nil, fmt.Errorf("file root %q must be an absolute path", dir)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	if real == string(filepath.Separator) {
		return "", nil, fmt.Errorf("file root %q is the filesystem root", dir)
	}
	identity, err := os.Stat(real)
	if err != nil {
		return "", nil, fmt.Errorf("file root %q: %w", dir, err)
	}
	// The default Docker data root plus whatever daemon.json declares; the
	// three-index slice keeps the shared slice from being mutated by append.
	trees := append(dangerousFileTrees[:len(dangerousFileTrees):len(dangerousFileTrees)], dockerDataRoot())
	for _, tree := range trees {
		treeReal := canonicalPath(tree)
		// A root inside a dangerous tree, or an ancestor of one (such as "/"),
		// is refused; the ancestor direction keeps a broad parent from
		// exposing a system tree it contains.
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

// canonicalPath resolves p to its real absolute path, falling back to the
// cleaned absolute path when it does not exist yet (a path that is only
// protected in the future still needs a stable comparison value).
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

// uniqueRootName returns base, suffixed when an earlier root already claimed
// it, so every root keeps a stable, unique identifier.
func uniqueRootName(existing map[string]*fsroot.Root, base string) string {
	name := base
	for i := 2; ; i++ {
		if _, ok := existing[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// ListRoots returns the allow-listed roots with their free space.
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

// List returns one page of a directory under a root. The cursor is an opaque
// base64 token so a boundary name that is not valid UTF-8 round-trips: proto3
// strings must be valid UTF-8, so a raw name (legal on Linux) could not be
// echoed back. A malformed cursor is rejected, never treated as "start over".
func (s *FileServer) List(_ context.Context, req *agentv1.ListRequest) (*agentv1.ListResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(req.GetCursor())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "list: %v", err)
	}
	entries, truncated, last, err := root.List(req.GetPath(), cursor, fileListLimit)
	if err != nil {
		return nil, fileError("list", err)
	}
	return &agentv1.ListResponse{
		Entries:   fileEntries(entries),
		Truncated: truncated,
		Cursor:    encodeCursor(last),
	}, nil
}

// encodeCursor encodes the raw boundary name as base64 so arbitrary bytes
// survive the proto3 UTF-8 requirement. An empty name (no more pages) stays
// empty so the wire contract "truncated implies a non-empty cursor" holds.
func encodeCursor(name string) string {
	if name == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(name))
}

// decodeCursor reverses encodeCursor. An empty token means the first page; any
// other token must be valid base64 or the request is malformed.
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

// Stat returns one entry under a root.
func (s *FileServer) Stat(_ context.Context, req *agentv1.StatRequest) (*agentv1.StatResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	entry, err := root.Stat(req.GetPath())
	if err != nil {
		return nil, fileError("stat", err)
	}
	return &agentv1.StatResponse{Entry: fileEntry(entry)}, nil
}

// Read streams one regular file's bytes. A truncated or failed read ends the
// stream with an error, never with a clean end, so the control plane can tell
// a partial body from a complete one.
func (s *FileServer) Read(req *agentv1.ReadRequest, stream grpc.ServerStreamingServer[agentv1.ReadChunk]) error {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return err
	}
	file, _, err := root.OpenRegular(req.GetPath())
	if err != nil {
		return fileError("read", err)
	}
	defer func() { _ = file.Close() }()

	buffer := make([]byte, fileChunkSize)
	for {
		n, readErr := file.Read(buffer)
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

// root resolves a caller-supplied root name.
func (s *FileServer) root(name string) (*fsroot.Root, error) {
	root, ok := s.roots[name]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown file root %q", name)
	}
	return root, nil
}

// fileEntries converts a listing page.
func fileEntries(entries []fsroot.Entry) []*agentv1.FileEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]*agentv1.FileEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, fileEntry(entry))
	}
	return out
}

// fileEntry converts one entry. Proto3 string fields must be valid UTF-8, so a
// filename or symlink target holding arbitrary bytes (legal on Linux) is
// scrubbed rather than making the whole message fail to marshal. The scrubbed
// name may therefore differ from the bytes on disk and cannot be fed back to
// Stat or Read; the List cursor is base64 precisely so paging still addresses
// the real names.
func fileEntry(entry fsroot.Entry) *agentv1.FileEntry {
	out := &agentv1.FileEntry{
		Name:          validUTF8(entry.Name),
		Kind:          fileKind(entry.Kind),
		Size:          entry.Size,
		Mode:          uint32(entry.Mode.Perm()),
		SymlinkTarget: validUTF8(entry.SymlinkTarget),
		Etag:          entry.Etag,
	}
	if !entry.Modified.IsZero() {
		out.ModifiedAt = timestamppb.New(entry.Modified)
	}
	return out
}

// validUTF8 replaces the invalid byte sequences proto3 string fields reject
// with the Unicode replacement character.
func validUTF8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

// fileKind maps an fsroot kind onto the wire enum.
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

// fileError maps an fsroot error onto a gRPC status error. A path shape
// problem is InvalidArgument, an escape attempt and an unreadable entry are
// PermissionDenied, a special file is FailedPrecondition, and the resource
// errnos (ENOSPC, EDQUOT, EFBIG) are ResourceExhausted. An error that is
// already a gRPC status is passed through unchanged so a handler's own status
// is not demoted to Internal.
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
		// A non-recursive delete of a non-empty directory is a failed
		// precondition, not a name conflict.
		return status.Errorf(codes.FailedPrecondition, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrConflict):
		return status.Errorf(codes.Aborted, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrReadOnly):
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, fsroot.ErrTooLarge):
		return status.Errorf(codes.ResourceExhausted, "%s: %v", action, err)
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT), errors.Is(err, syscall.EFBIG):
		// Out of space, over quota, or over the file-size limit: the caller can
		// free space and retry.
		return status.Errorf(codes.ResourceExhausted, "%s: %v", action, err)
	case errors.Is(err, syscall.ENAMETOOLONG):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ENOTDIR):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, syscall.EROFS):
		// A read-only filesystem follows the read-only-root convention.
		return status.Errorf(codes.PermissionDenied, "%s: %v", action, err)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, action+": context canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, action+": deadline exceeded")
	default:
		return status.Errorf(codes.Internal, "%s: %v", action, err)
	}
}

// freeBytes reports the free space on dir's filesystem, or 0 when it cannot be
// read (the caller treats 0 as unknown).
func freeBytes(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

// totalBytes reports the total size of dir's filesystem, or 0 when it cannot
// be read.
func totalBytes(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Blocks) * uint64(stat.Bsize)
}
