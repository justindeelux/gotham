package agent

import (
	"context"
	"errors"
	"math"
	"path"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fileSizeMaxEntries caps how many entries one DirectorySize walk visits. A
// larger tree stops at the cap and reports a partial result (truncated=true)
// instead of walking unbounded. It is a variable so a test can lower it.
var fileSizeMaxEntries int64 = 100_000

// errSizeLimit stops the walk once the entry cap is reached; DirectorySize
// turns it into a truncated (partial) response.
var errSizeLimit = errors.New("file size: entry cap reached")

// DirectorySize returns one directory's recursive size (JUS-162). It is a
// read-scope, metadata-only walk: every entry is lstat-ed, a symlink or special
// file is counted in skipped and never followed, and the walk never leaves the
// root (fsroot). It stops at fileSizeMaxEntries or fileArchiveDepth and returns
// the accumulated bytes with truncated=true, so a capped walk is a clear
// partial result, never a silent full one.
func (s *FileServer) DirectorySize(ctx context.Context, req *agentv1.DirectorySizeRequest) (*agentv1.DirectorySizeResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "size: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "size", rel); err != nil {
		return nil, err
	}
	entry, err := root.Stat(rel)
	if err != nil {
		return nil, fileError("size", err)
	}
	if entry.Kind != fsroot.KindDirectory {
		return nil, status.Errorf(codes.InvalidArgument, "size: %q is not a directory", req.GetPath())
	}

	walk := &dirSizeWalk{root: root, maxEntries: fileSizeMaxEntries}
	err = walk.walk(ctx, rel, 0)
	if err != nil && !errors.Is(err, errSizeLimit) {
		return nil, fileError("size", err)
	}
	return &agentv1.DirectorySizeResponse{
		Bytes:     walk.bytes,
		Entries:   walk.entries,
		Skipped:   walk.skipped,
		Truncated: walk.truncated,
	}, nil
}

// dirSizeWalk accumulates one DirectorySize walk's totals.
type dirSizeWalk struct {
	root       *fsroot.Root
	maxEntries int64
	bytes      int64
	// entries counts every name visited (files, directories, symlinks and
	// special files); skipped is the symlink/special subset never descended.
	entries   int64
	skipped   uint64
	truncated bool
}

// walk sizes one directory. rel is root-relative and already confined; depth
// bounds the recursion. A symlink or special entry is counted in skipped and
// never descended into, so the walk can never leave the root or double-count a
// shared target. The entry cap and ctx are checked between entries, so a
// cancelled request stops promptly and a large tree returns a partial result.
func (w *dirSizeWalk) walk(ctx context.Context, rel string, depth int) error {
	if w.truncated {
		return nil
	}
	if depth > fileArchiveDepth {
		w.truncated = true
		return nil
	}
	return w.root.Names(rel, func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The reserved recycle bin at the root's top level is not part of the
		// browsable tree, exactly like List (hideTrashEntries).
		if rel == "." && name == fileTrashDir {
			return nil
		}
		if w.entries >= w.maxEntries {
			w.truncated = true
			return errSizeLimit
		}
		child := path.Join(rel, name)
		entry, err := w.root.Stat(child)
		if err != nil {
			// An entry that vanished between the directory read and its stat
			// is skipped, so one transient delete never aborts the walk.
			if errors.Is(err, fsroot.ErrNotFound) {
				return nil
			}
			return err
		}
		w.entries++
		switch entry.Kind {
		case fsroot.KindDirectory:
			return w.walk(ctx, child, depth+1)
		case fsroot.KindRegular:
			if entry.Size > 0 {
				// Saturate rather than wrap: an outlandish declared size must
				// not turn the total negative.
				if entry.Size > math.MaxInt64-w.bytes {
					w.bytes = math.MaxInt64
					return nil
				}
				w.bytes += entry.Size
			}
			return nil
		default:
			// A symlink, socket, device or FIFO is never followed or sized.
			w.skipped++
			return nil
		}
	})
}
