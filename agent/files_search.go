package agent

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Search bounds. They are variables so a test can lower them, and they are the
// whole reason Search is safe on a node with a very large tree: every bound is
// reported through SearchResponse.truncated, so a capped result set is never
// mistaken for a complete one.
var (
	// searchMaxResults caps the matches one search returns.
	searchMaxResults = 500
	// searchMaxDepth bounds how deep a recursive search descends below the
	// requested directory (the directory itself is depth 1).
	searchMaxDepth = 32
	// searchScanCap bounds how many directory entries one search inspects, so a
	// wide tree cannot pin the agent for an unbounded time.
	searchScanCap = 20_000
	// searchPageSize is the directory page size the walk pulls per call.
	searchPageSize = 1000
)

// Search returns the entries under one directory whose NAME contains a
// case-insensitive substring. It is name-only (never file content) and never
// follows a symbolic link: it only descends into entries the confined List
// reports as a directory, so it cannot leave the requested root. The recycle bin
// is never matched and never descended into.
//
// Every cap that stops the walk early sets truncated=true. A subdirectory that
// cannot be listed (permission, or more entries than one List scans) is skipped
// best-effort rather than failing the whole search; the requested directory
// itself is not: an unreadable or non-directory base is reported to the caller.
// A cancelled or timed-out request stops the walk at the next directory
// boundary: a browser that navigated away or a CP timeout must not leave the
// agent walking a large tree to completion.
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

// searchWalk is one bounded, confined name search.
type searchWalk struct {
	ctx       context.Context
	root      *fsroot.Root
	needle    string
	recursive bool
	results   []*agentv1.SearchResult
	truncated bool
	scanned   int
}

// cancelled reports the request's cancellation as a gRPC status, so the walk
// stops with Canceled/DeadlineExceeded rather than a truncated success.
func (w *searchWalk) cancelled() error {
	if err := w.ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return nil
}

// visit matches one directory's entries, then (when recursive) descends into its
// subdirectories depth-first. base marks the requested directory, whose own
// failure is the caller's to see.
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
			// A deeper directory the walk cannot enumerate is skipped and the
			// caller learns the result set is incomplete from truncated: a
			// too-large or unreadable directory is left out rather than failing
			// the whole search.
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
			// The reserved recycle bin is neither matched nor descended into.
			if dir == "." && entry.Name == fileTrashDir {
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
					// A deeper directory than the depth cap is left unvisited;
					// the caller learns the result set is incomplete.
					w.truncated = true
				}
			}
		}
		// A single directory with more entries than one page is paged through;
		// the loop never revisits a name because the cursor is the last name.
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
