package files

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

var fileSizeMaxEntries int64 = 100_000

var fileSizeMaxConcurrent = 4

var errSizeLimit = errors.New("file size: entry cap reached")

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
	release, err := s.beginSizeWalk()
	if err != nil {
		return nil, err
	}
	defer release()

	walk := &dirSizeWalk{root: root, maxEntries: s.sizeMaxEntries}
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

func (s *FileServer) beginSizeWalk() (func(), error) {
	select {
	case s.sizeWalks <- struct{}{}:
		return func() { <-s.sizeWalks }, nil
	default:
		return nil, status.Error(codes.ResourceExhausted, "size: too many concurrent size walks")
	}
}

type dirSizeWalk struct {
	root       *fsroot.Root
	maxEntries int64
	bytes      int64
	entries    int64
	skipped    uint64
	truncated  bool
}

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
		if rel == "." && (name == fileTrashDir || name == fileTransferDir) {
			return nil
		}
		if w.entries >= w.maxEntries {
			w.truncated = true
			return errSizeLimit
		}
		child := path.Join(rel, name)
		entry, err := w.root.Stat(child)
		if err != nil {
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
				if entry.Size > math.MaxInt64-w.bytes {
					w.bytes = math.MaxInt64
					return nil
				}
				w.bytes += entry.Size
			}
			return nil
		default:
			w.skipped++
			return nil
		}
	})
}
