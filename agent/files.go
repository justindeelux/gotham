package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

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
)

// dangerousFileTrees are whole trees the file service must never expose: the
// system configuration and runtime directories, the package and home trees and
// Docker's data root. A browsable root at or under any of them is refused.
var dangerousFileTrees = []string{
	"/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/home", "/root",
	"/var/lib/docker",
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
}

// FileServer implements agentv1.FileServiceServer against the node's
// allow-listed roots. It is constructed only when the file service is enabled;
// a disabled agent never registers it.
type FileServer struct {
	agentv1.UnimplementedFileServiceServer

	log   *slog.Logger
	roots map[string]*fsroot.Root
	order []string
}

// NewFileServer validates and opens the configured roots. It fails closed: a
// root that does not exist, is not absolute, resolves into a dangerous tree or
// collides with a protected path aborts startup rather than being dropped.
func NewFileServer(cfg Config, log *slog.Logger) (*FileServer, error) {
	if log == nil {
		log = slog.Default()
	}
	protected := fileProtectedPaths(cfg)
	server := &FileServer{log: log, roots: make(map[string]*fsroot.Root)}
	for _, rc := range configuredFileRoots(cfg) {
		real, err := validateFileRoot(rc.path, protected)
		if err != nil {
			_ = server.Close()
			return nil, err
		}
		name := uniqueRootName(server.roots, rc.name)
		root, err := fsroot.Open(name, real, rc.writable)
		if err != nil {
			_ = server.Close()
			return nil, err
		}
		server.roots[name] = root
		server.order = append(server.order, name)
	}
	return server, nil
}

// Close releases every root descriptor.
func (s *FileServer) Close() error {
	var errs []error
	for _, name := range s.order {
		if err := s.roots[name].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// configuredFileRoots returns the allow-list in a stable order: the managed
// volumes root (writable) first, then the read-only compose root, then every
// operator-added root (read-only, because writes have no owner-configured
// intent yet).
func configuredFileRoots(cfg Config) []fileRootConfig {
	roots := []fileRootConfig{
		{name: fileRootVolumes, path: cfg.ManagedVolumeRoot, writable: true},
		{name: fileRootCompose, path: cfg.ComposeRoot, writable: false},
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
	// The agent state, certificate and binary locations come from config, so a
	// non-default install is protected too. They are relative-safe: an entry
	// that cannot be made absolute is skipped rather than widened.
	for _, p := range []struct {
		path      string
		exclusive bool
	}{
		{cfg.CertDir, true},
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

// validateFileRoot returns the canonical absolute path of a browsable root, or
// an error when it must not be browsed. The path is resolved through symlinks
// first (so a root that is a symlink into /etc is judged by where it really
// points, and the legitimate /var → /private/var link on macOS does not block
// an operator root), then checked against the dangerous trees and the
// protected paths.
func validateFileRoot(dir string, protected []protectedPath) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("file root %q must be an absolute path", dir)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("file root %q: %w", dir, err)
	}
	if real == string(filepath.Separator) {
		return "", fmt.Errorf("file root %q is the filesystem root", dir)
	}
	for _, tree := range dangerousFileTrees {
		treeReal := canonicalPath(tree)
		// A root inside a dangerous tree, or an ancestor of one (such as "/"),
		// is refused; the ancestor direction keeps a broad parent from
		// exposing a system tree it contains.
		if withinPath(tree, real) || withinPath(treeReal, real) ||
			withinPath(real, tree) || withinPath(real, treeReal) {
			return "", fmt.Errorf("file root %q overlaps %s", dir, tree)
		}
	}
	for _, p := range protected {
		pp := canonicalPath(p.path)
		if p.exclusive && withinPath(pp, real) {
			return "", fmt.Errorf("file root %q is inside %s", dir, p.path)
		}
		if pp == real {
			return "", fmt.Errorf("file root %q is %s", dir, p.path)
		}
		if withinPath(real, pp) {
			return "", fmt.Errorf("file root %q contains %s", dir, p.path)
		}
	}
	return real, nil
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

// List returns one page of a directory under a root.
func (s *FileServer) List(_ context.Context, req *agentv1.ListRequest) (*agentv1.ListResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	entries, truncated, cursor, err := root.List(req.GetPath(), req.GetCursor(), fileListLimit)
	if err != nil {
		return nil, fileError("list", err)
	}
	return &agentv1.ListResponse{
		Entries:   fileEntries(entries),
		Truncated: truncated,
		Cursor:    cursor,
	}, nil
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

// fileEntry converts one entry.
func fileEntry(entry fsroot.Entry) *agentv1.FileEntry {
	out := &agentv1.FileEntry{
		Name:          entry.Name,
		Kind:          fileKind(entry.Kind),
		Size:          entry.Size,
		Mode:          uint32(entry.Mode.Perm()),
		SymlinkTarget: entry.SymlinkTarget,
		Etag:          entry.Etag,
	}
	if !entry.Modified.IsZero() {
		out.ModifiedAt = timestamppb.New(entry.Modified)
	}
	return out
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
// PermissionDenied, and a special file is FailedPrecondition.
func fileError(action string, err error) error {
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
