package files

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

var (
	searchMaxResults = 500
	searchMaxDepth   = 32
	searchScanCap    = 20_000
	searchPageSize   = 1000
)

func (s *FileServer) Search(ctx context.Context, req *agentv1.SearchRequest) (*agentv1.SearchResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.GetQuery())
	if query == "" {
		return nil, status.Error(codes.InvalidArgument, "search: query is required")
	}
	rel, err := fsroot.Clean(req.GetPath())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "search: invalid path %q", req.GetPath())
	}
	if err := guardTrash(root, "search", rel); err != nil {
		return nil, err
	}

	walk := &searchWalk{
		ctx:       ctx,
		root:      root,
		needle:    strings.ToLower(query),
		recursive: req.GetRecursive(),
	}
	if err := walk.visit(rel, 1, true); err != nil {
		return nil, fileError("search", err)
	}
	return &agentv1.SearchResponse{Results: walk.results, Truncated: walk.truncated}, nil
}

type searchWalk struct {
	ctx       context.Context
	root      *fsroot.Root
	needle    string
	recursive bool
	results   []*agentv1.SearchResult
	truncated bool
	scanned   int
}

func (w *searchWalk) cancelled() error {
	if err := w.ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return nil
}

func (w *searchWalk) visit(dir string, level int, base bool) error {
	if w.truncated {
		return nil
	}
	if err := w.cancelled(); err != nil {
		return err
	}
	var subdirs []string
	cursor := ""
	for {
		if err := w.cancelled(); err != nil {
			return err
		}
		entries, truncated, next, err := w.root.List(dir, cursor, searchPageSize)
		if err != nil {
			if base {
				return err
			}
			w.truncated = true
			return nil
		}
		for _, entry := range entries {
			if err := w.cancelled(); err != nil {
				return err
			}
			w.scanned++
			if w.scanned > searchScanCap {
				w.truncated = true
				return nil
			}
			if dir == "." && (entry.Name == fileTrashDir || entry.Name == fileTransferDir) {
				continue
			}
			path := entry.Name
			if dir != "." {
				path = dir + "/" + entry.Name
			}
			if strings.Contains(strings.ToLower(entry.Name), w.needle) {
				w.results = append(w.results, &agentv1.SearchResult{
					Path:  validUTF8(path),
					Entry: fileEntry(entry),
				})
				if len(w.results) >= searchMaxResults {
					w.truncated = true
					return nil
				}
			}
			if w.recursive && entry.Kind == fsroot.KindDirectory {
				if level < searchMaxDepth {
					subdirs = append(subdirs, path)
				} else {
					w.truncated = true
				}
			}
		}
		if w.truncated || !truncated || next == "" {
			break
		}
		cursor = next
	}
	for _, sub := range subdirs {
		if err := w.visit(sub, level+1, false); err != nil {
			return err
		}
		if w.truncated {
			return nil
		}
	}
	return nil
}
