package files

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	fileTrashDir       = ".gotham-trash"
	fileTrashMeta      = "meta.json"
	fileTrashData      = "data"
	fileTrashDirMode   = 0o700
	fileTrashMetaMode  = 0o600
	fileTrashMetaLimit = 1 << 20
	fileTrashIDBytes   = 16
)

// fileTrashRestoreDirMode creates a missing restore parent that predates the
// record's parent list (or was never recorded).
const fileTrashRestoreDirMode = 0o755

const fileTrashSweepInterval = time.Hour

var fileTrashMaxList = 5000

type trashParent struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

type trashRecord struct {
	OriginalPath string        `json:"original_path"`
	DeletedAt    time.Time     `json:"deleted_at"`
	Size         int64         `json:"size"`
	Kind         string        `json:"kind"`
	Parents      []trashParent `json:"parents,omitempty"`
}

var errDirStopWalk = errors.New("stop walk")

func trashItemDir(id string) string { return fileTrashDir + "/" + id }

func trashItemData(id string) string { return trashItemDir(id) + "/" + fileTrashData }

func trashItemMeta(id string) string { return trashItemDir(id) + "/" + fileTrashMeta }

func validTrashID(id string) bool {
	if len(id) != fileTrashIDBytes*2 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func newTrashID() string {
	var raw [fileTrashIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("files: generate trash id: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

func firstComponent(rel string) string {
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

func guardTrash(root *fsroot.Root, action, rel string) error {
	if rel == "" || rel == "." {
		return nil
	}
	if firstComponent(rel) == fileTrashDir {
		return status.Errorf(codes.InvalidArgument, "%s: %q is reserved for the recycle bin", action, rel)
	}
	for p := rel; ; {
		resolved, err := root.Resolve(p)
		if err == nil {
			if firstComponent(resolved) == fileTrashDir {
				return status.Errorf(codes.InvalidArgument, "%s: %q resolves into the recycle bin", action, rel)
			}
			return nil
		}
		parent := path.Dir(p)
		if parent == "." || parent == p {
			return nil
		}
		p = parent
	}
}

func guardTrashRaw(root *fsroot.Root, action, raw string) error {
	rel, err := fsroot.Clean(raw)
	if err != nil {
		return nil
	}
	return guardTrash(root, action, rel)
}

func hideTrashEntries(rel string, entries []fsroot.Entry) []fsroot.Entry {
	if rel != "." {
		return entries
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Name == fileTrashDir {
			continue
		}
		out = append(out, e)
	}
	return out
}

func trashKindString(kind fsroot.Kind) string {
	switch kind {
	case fsroot.KindRegular:
		return "file"
	case fsroot.KindDirectory:
		return "directory"
	case fsroot.KindSymlink:
		return "symlink"
	default:
		return "special"
	}
}

func trashKind(kind string) agentv1.FileKind {
	switch kind {
	case "file":
		return agentv1.FileKind_FILE_KIND_FILE
	case "directory":
		return agentv1.FileKind_FILE_KIND_DIRECTORY
	case "symlink":
		return agentv1.FileKind_FILE_KIND_SYMLINK
	default:
		return agentv1.FileKind_FILE_KIND_SPECIAL
	}
}

func ensureTrashDir(root *fsroot.Root) error {
	entry, err := root.Stat(fileTrashDir)
	switch {
	case err == nil:
		if entry.Kind != fsroot.KindDirectory {
			return fmt.Errorf("%w: %s is not a directory", fsroot.ErrInvalid, fileTrashDir)
		}
		return nil
	case errors.Is(err, fsroot.ErrNotFound):
		if _, err := root.Mkdir(fileTrashDir, fileTrashDirMode); err != nil && !errors.Is(err, fsroot.ErrExists) {
			return err
		}
		return nil
	default:
		return err
	}
}

func (s *FileServer) writeTrashRecord(root *fsroot.Root, id string, rec trashRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	file, err := root.CreateFile(trashItemMeta(id), fileTrashMetaMode, false)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s *FileServer) readTrashRecord(root *fsroot.Root, id string) (trashRecord, error) {
	file, _, err := root.OpenRegular(trashItemMeta(id))
	if err != nil {
		return trashRecord{}, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, fileTrashMetaLimit))
	if err != nil {
		return trashRecord{}, err
	}
	var rec trashRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return trashRecord{}, fmt.Errorf("%w: trash metadata for %s: %v", fsroot.ErrInvalid, id, err)
	}
	return rec, nil
}

func dirHasEntries(root *fsroot.Root, rel string) (bool, error) {
	found := false
	err := root.Names(rel, func(string) error {
		found = true
		return errDirStopWalk
	})
	if err != nil && !errors.Is(err, errDirStopWalk) {
		return false, err
	}
	return found, nil
}

// trashParentModes records the modes of rel's existing ancestor directories so
// a later restore can recreate a parent that was deleted after rel.
func trashParentModes(root *fsroot.Root, rel string) []trashParent {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	parents := make([]trashParent, 0, len(parts))
	prefix := ""
	for _, part := range parts {
		if prefix == "" {
			prefix = part
		} else {
			prefix += "/" + part
		}
		entry, err := root.Stat(prefix)
		if err != nil || entry.Kind != fsroot.KindDirectory {
			continue
		}
		parents = append(parents, trashParent{Path: prefix, Mode: uint32(entry.Mode.Perm())})
	}
	return parents
}

func (s *FileServer) trashEntry(ctx context.Context, root *fsroot.Root, rel string, recursive bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := root.Stat(rel)
	if err != nil {
		return err
	}
	if entry.Kind == fsroot.KindDirectory && !recursive {
		nonEmpty, err := dirHasEntries(root, rel)
		if err != nil {
			return err
		}
		if nonEmpty {
			return fmt.Errorf("%w: %s", fsroot.ErrNotEmpty, rel)
		}
	}
	if err := ensureTrashDir(root); err != nil {
		return err
	}
	id := newTrashID()
	if _, err := root.Mkdir(trashItemDir(id), fileTrashDirMode); err != nil {
		return err
	}
	rec := trashRecord{
		OriginalPath: rel,
		DeletedAt:    time.Now().UTC(),
		Size:         entry.Size,
		Kind:         trashKindString(entry.Kind),
		Parents:      trashParentModes(root, rel),
	}
	if err := s.writeTrashRecord(root, id, rec); err != nil {
		_ = root.RemoveTree(context.Background(), trashItemDir(id), nil)
		return err
	}
	if err := s.moveEntry(ctx, root, rel, trashItemData(id), false); err != nil {
		_ = root.RemoveTree(context.Background(), trashItemDir(id), nil)
		return err
	}
	return nil
}

func (s *FileServer) ListTrash(_ context.Context, req *agentv1.ListTrashRequest) (*agentv1.ListTrashResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	selection, err := s.newestTrashIDs(root, fileTrashMaxList)
	if err != nil {
		return nil, fileError("list trash", err)
	}
	items := make([]trashListed, 0, len(selection.ids))
	for _, id := range selection.ids {
		rec, err := s.readTrashRecord(root, id)
		if err != nil {
			continue
		}
		if _, err := root.Stat(trashItemData(id)); err != nil {
			continue
		}
		items = append(items, trashListed{id: id, rec: rec})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].rec.DeletedAt.After(items[j].rec.DeletedAt)
	})
	entries := make([]*agentv1.TrashEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, trashEntryProto(item.id, item.rec))
	}
	return &agentv1.ListTrashResponse{Entries: entries, Truncated: selection.truncated}, nil
}

type trashListed struct {
	id  string
	rec trashRecord
}

type trashSelection struct {
	ids       []string
	truncated bool
}

func (s *FileServer) newestTrashIDs(root *fsroot.Root, limit int) (trashSelection, error) {
	if limit <= 0 {
		limit = 1
	}
	kept := &trashIDHeap{}
	capacity := limit + 1
	err := root.Names(fileTrashDir, func(name string) error {
		if !validTrashID(name) {
			return nil
		}
		entry, statErr := root.Stat(trashItemMeta(name))
		if statErr != nil {
			return nil
		}
		candidate := trashIDHeapEntry{id: name, mod: entry.Modified}
		switch {
		case kept.Len() < capacity:
			heap.Push(kept, candidate)
		case candidate.mod.After((*kept)[0].mod):
			(*kept)[0] = candidate
			heap.Fix(kept, 0)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fsroot.ErrNotFound) {
		return trashSelection{}, err
	}
	truncated := kept.Len() > limit
	ordered := make([]trashIDHeapEntry, kept.Len())
	copy(ordered, *kept)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].mod.After(ordered[j].mod) })
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	ids := make([]string, 0, len(ordered))
	for _, candidate := range ordered {
		ids = append(ids, candidate.id)
	}
	return trashSelection{ids: ids, truncated: truncated}, nil
}

type trashIDHeapEntry struct {
	id  string
	mod time.Time
}

type trashIDHeap []trashIDHeapEntry

func (h trashIDHeap) Len() int           { return len(h) }
func (h trashIDHeap) Less(i, j int) bool { return h[i].mod.Before(h[j].mod) }
func (h trashIDHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *trashIDHeap) Push(x any)        { *h = append(*h, x.(trashIDHeapEntry)) }

func (h *trashIDHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func (s *FileServer) trashIDs(root *fsroot.Root) ([]string, error) {
	var ids []string
	err := root.Names(fileTrashDir, func(name string) error {
		if validTrashID(name) {
			ids = append(ids, name)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, fsroot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return ids, nil
}

func trashEntryProto(id string, rec trashRecord) *agentv1.TrashEntry {
	out := &agentv1.TrashEntry{
		Id:           id,
		OriginalPath: rec.OriginalPath,
		Name:         path.Base(rec.OriginalPath),
		Size:         rec.Size,
		Kind:         trashKind(rec.Kind),
	}
	if !rec.DeletedAt.IsZero() {
		out.DeletedAt = timestamppb.New(rec.DeletedAt)
	}
	return out
}

// restoreParents recreates dst's missing ancestor directories, using the mode
// recorded when the entry was trashed and a default for anything unrecorded.
func restoreParents(root *fsroot.Root, dst string, recorded []trashParent) error {
	dir := path.Dir(dst)
	if dir == "." {
		return nil
	}
	modes := make(map[string]os.FileMode, len(recorded))
	for _, parent := range recorded {
		modes[parent.Path] = os.FileMode(parent.Mode).Perm()
	}
	parts := strings.Split(dir, "/")
	prefix := ""
	for _, part := range parts {
		if prefix == "" {
			prefix = part
		} else {
			prefix += "/" + part
		}
		entry, err := root.Stat(prefix)
		switch {
		case err == nil:
			if entry.Kind == fsroot.KindDirectory || entry.Kind == fsroot.KindSymlink {
				continue
			}
			return fmt.Errorf("%w: restore parent %s is not a directory", fsroot.ErrInvalid, prefix)
		case !errors.Is(err, fsroot.ErrNotFound):
			return err
		}
		mode, ok := modes[prefix]
		if !ok {
			mode = fileTrashRestoreDirMode
		}
		if _, err := root.Mkdir(prefix, mode); err != nil && !errors.Is(err, fsroot.ErrExists) {
			return err
		}
	}
	return nil
}

func (s *FileServer) Restore(ctx context.Context, req *agentv1.RestoreRequest) (*agentv1.RestoreResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "restore: root %q is read-only", req.GetRoot())
	}
	id := req.GetId()
	if !validTrashID(id) {
		return nil, status.Error(codes.InvalidArgument, "restore: invalid trash id")
	}
	rec, err := s.readTrashRecord(root, id)
	if err != nil {
		return nil, fileError("restore", err)
	}
	target := strings.TrimSpace(req.GetDestination())
	if target == "" {
		target = rec.OriginalPath
	}
	dst, err := fsroot.Clean(target)
	if err != nil || dst == "." {
		return nil, status.Errorf(codes.InvalidArgument, "restore: invalid destination %q", target)
	}
	if err := guardTrash(root, "restore", dst); err != nil {
		return nil, err
	}
	if s.protectsAppDir(root, dst) {
		return nil, status.Error(codes.PermissionDenied, "restore: an application directory cannot be replaced")
	}
	unlock := s.lockPaths(root.Name(), dst, trashItemData(id))
	if err := restoreParents(root, dst, rec.Parents); err != nil {
		unlock()
		return nil, fileError("restore", err)
	}
	moveErr := s.moveEntry(ctx, root, trashItemData(id), dst, req.GetOverwrite())
	if moveErr != nil {
		unlock()
		return nil, fileError("restore", moveErr)
	}
	_ = root.RemoveTree(context.Background(), trashItemDir(id), nil)
	unlock()
	entry, err := root.Stat(dst)
	if err != nil {
		return nil, fileError("restore", err)
	}
	return &agentv1.RestoreResponse{Entry: fileEntry(entry), Path: dst}, nil
}

func (s *FileServer) Purge(ctx context.Context, req *agentv1.PurgeRequest) (*agentv1.PurgeResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "purge: root %q is read-only", req.GetRoot())
	}
	id := req.GetId()
	if !validTrashID(id) {
		return nil, status.Error(codes.InvalidArgument, "purge: invalid trash id")
	}
	unlock := s.lockPaths(root.Name(), trashItemData(id))
	defer unlock()
	if _, err := root.Stat(trashItemDir(id)); err != nil {
		return nil, fileError("purge", err)
	}
	if err := root.RemoveTree(ctx, trashItemDir(id), nil); err != nil {
		return nil, fileError("purge", err)
	}
	return &agentv1.PurgeResponse{}, nil
}

func (s *FileServer) EmptyTrash(ctx context.Context, req *agentv1.EmptyTrashRequest) (*agentv1.EmptyTrashResponse, error) {
	root, err := s.root(req.GetRoot())
	if err != nil {
		return nil, err
	}
	if !root.Writable() {
		return nil, status.Errorf(codes.PermissionDenied, "empty trash: root %q is read-only", req.GetRoot())
	}
	ids, err := s.trashIDs(root)
	if err != nil {
		return nil, fileError("empty trash", err)
	}
	var removed uint64
	for _, id := range ids {
		unlock := s.lockPaths(root.Name(), trashItemData(id))
		err := root.RemoveTree(ctx, trashItemDir(id), nil)
		unlock()
		if err != nil {
			if errors.Is(err, fsroot.ErrNotFound) {
				continue
			}
			return nil, fileError("empty trash", err)
		}
		removed++
	}
	return &agentv1.EmptyTrashResponse{Removed: removed}, nil
}

func (s *FileServer) sweepTrashLoop() {
	s.sweepTrash(time.Now())
	ticker := time.NewTicker(fileTrashSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.sweepTrash(now)
		}
	}
}

// sweepTrash expires each writable root's recycle bin independently, so a slow
// root never delays or locks another.
func (s *FileServer) sweepTrash(now time.Time) {
	for _, name := range s.order {
		root := s.roots[name]
		if !root.Writable() {
			continue
		}
		removed, freed, err := s.expireTrash(root, now)
		if err != nil {
			s.log.Warn("file service trash retention", "root", name, "error", err)
			continue
		}
		if removed > 0 {
			s.log.Info("file service trash retention", "root", name, "removed", removed, "bytes", freed)
		}
	}
}

type trashExpiry struct {
	id     string
	rec    trashRecord
	bytes  int64
	reason string
}

// expireTrash purges old or over-ceiling items oldest first. It only ever
// removes valid trash ids below .gotham-trash, one item at a time, so the path
// lock is released between items. Unreadable metadata or data is left alone.
func (s *FileServer) expireTrash(root *fsroot.Root, now time.Time) (int, int64, error) {
	ids, err := s.trashIDs(root)
	if err != nil {
		return 0, 0, err
	}
	items := make([]trashExpiry, 0, len(ids))
	for _, id := range ids {
		rec, err := s.readTrashRecord(root, id)
		if err != nil {
			continue
		}
		if _, err := root.Stat(trashItemData(id)); err != nil {
			continue
		}
		size := rec.Size
		if s.trashMaxBytes > 0 {
			size = trashDataBytes(root, trashItemData(id))
		}
		items = append(items, trashExpiry{id: id, rec: rec, bytes: size})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].rec.DeletedAt.Before(items[j].rec.DeletedAt)
	})

	total := int64(0)
	for _, item := range items {
		total += item.bytes
	}
	if s.trashMaxAgeDays > 0 {
		cutoff := now.Add(-time.Duration(s.trashMaxAgeDays) * 24 * time.Hour)
		for i := range items {
			if items[i].rec.DeletedAt.IsZero() || !items[i].rec.DeletedAt.Before(cutoff) {
				continue
			}
			items[i].reason = "age"
			total -= items[i].bytes
		}
	}
	if s.trashMaxBytes > 0 {
		for i := range items {
			if total <= s.trashMaxBytes {
				break
			}
			if items[i].reason != "" {
				continue
			}
			items[i].reason = "size"
			total -= items[i].bytes
		}
	}

	removed := 0
	var freed int64
	for _, item := range items {
		if item.reason == "" {
			continue
		}
		unlock := s.lockPaths(root.Name(), trashItemData(item.id))
		err := root.RemoveTree(context.Background(), trashItemDir(item.id), nil)
		unlock()
		if err != nil {
			if errors.Is(err, fsroot.ErrNotFound) {
				continue
			}
			s.log.Warn("file service trash retention: purge failed", "root", root.Name(), "id", item.id, "error", err)
			continue
		}
		removed++
		freed += item.bytes
		s.log.Info("file service trash retention: purged", "root", root.Name(), "id", item.id, "path", item.rec.OriginalPath, "bytes", item.bytes, "reason", item.reason)
	}
	return removed, freed, nil
}

// trashDataBytes sums one trashed tree without following symlinks. A subtree
// the agent cannot read contributes only its parent directory's own size.
func trashDataBytes(root *fsroot.Root, rel string) int64 {
	entry, err := root.Stat(rel)
	if err != nil {
		return 0
	}
	if entry.Kind != fsroot.KindDirectory {
		return entry.Size
	}
	total := entry.Size
	_ = root.Names(rel, func(name string) error {
		total += trashDataBytes(root, path.Join(rel, name))
		return nil
	})
	return total
}
