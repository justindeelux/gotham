package agent

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// The recycle bin lives at <root>/.gotham-trash, one directory per deleted
// entry:
//
//	<root>/.gotham-trash/<id>/meta.json   original path, deleted-at, size, kind
//	<root>/.gotham-trash/<id>/data        the deleted entry itself
//
// A delete is a rename of the entry into "data" (cross-device falls back to a
// guarded copy), so it is atomic on the entry's own filesystem. The reserved
// name is hidden from List and refused by every normal file operation: the bin
// is only reachable through ListTrash/Restore/Purge/EmptyTrash.
const (
	fileTrashDir      = ".gotham-trash"
	fileTrashMeta     = "meta.json"
	fileTrashData     = "data"
	fileTrashDirMode  = 0o700
	fileTrashMetaMode = 0o600
	// fileTrashMetaLimit bounds the metadata file the agent will parse.
	fileTrashMetaLimit = 1 << 20
	// fileTrashIDBytes is the length of a trash id in bytes; the hex id is
	// twice that.
	fileTrashIDBytes = 16
)

// fileTrashMaxList bounds one ListTrash page; a larger bin reports truncated.
// It is a variable so a test can lower it.
var fileTrashMaxList = 5000

// trashRecord is the metadata stored for one deleted entry.
type trashRecord struct {
	OriginalPath string    `json:"original_path"`
	DeletedAt    time.Time `json:"deleted_at"`
	Size         int64     `json:"size"`
	Kind         string    `json:"kind"`
}

// errDirStopWalk stops a directory scan after the first entry, so checking
// whether a directory is empty does not enumerate the whole tree.
var errDirStopWalk = errors.New("stop walk")

// trashItemDir is the directory holding one bin entry.
func trashItemDir(id string) string { return fileTrashDir + "/" + id }

// trashItemData is the moved entry itself.
func trashItemData(id string) string { return trashItemDir(id) + "/" + fileTrashData }

// trashItemMeta is the entry's metadata file.
func trashItemMeta(id string) string { return trashItemDir(id) + "/" + fileTrashMeta }

// validTrashID reports whether id is one of the service's own hex ids. The id
// is validated before it is turned into a path, so a caller cannot address
// anything outside the bin (or the bin's own children) with it.
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

// newTrashID returns a 128-bit hex id.
func newTrashID() string {
	var raw [fileTrashIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand never fails on a supported platform; a panic is
		// preferable to a collision that would overwrite another entry.
		panic(fmt.Sprintf("files: generate trash id: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

// firstComponent returns the first slash-separated component of a clean
// root-relative path.
func firstComponent(rel string) string {
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

// guardTrash refuses a normal file operation that addresses the reserved trash
// tree, lexically or through a symlink that resolves into it. The trash tree is
// only reachable through the bin RPCs.
//
// The check resolves the deepest existing ancestor, not just the full path:
// root.Resolve fails on a missing component, so for a create-shaped target
// ("link/newfile") whose leaf does not exist yet it would silently pass, and a
// pre-existing symlink "link -> .gotham-trash" would let an ordinary op plant
// or overwrite entries inside the bin. Walking up to the deepest ancestor that
// does resolve closes that bypass.
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

// guardTrashRaw cleans a caller-supplied path and applies guardTrash. A path
// the cleaner rejects is left for the operation itself to reject with its own
// typed error.
func guardTrashRaw(root *fsroot.Root, action, raw string) error {
	rel, err := fsroot.Clean(raw)
	if err != nil {
		return nil
	}
	return guardTrash(root, action, rel)
}

// hideTrashEntries drops the reserved bin from a listing of the root itself, so
// the bin is never browsable through List.
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

// trashKindString maps an fsroot kind onto the metadata's stable token.
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

// trashKind maps the metadata token back onto the wire enum.
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

// ensureTrashDir creates the root's trash directory when missing, refusing a
// non-directory that squats the reserved name.
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

// writeTrashRecord writes one entry's metadata. It is written before the entry
// is moved, so a crash can leave a metadata-only item (harmless and skipped by
// ListTrash) but never an item whose original path was lost.
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

// readTrashRecord reads one entry's metadata.
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

// dirHasEntries reports whether the directory holds any entry.
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

// trashEntry moves one entry into the root's recycle bin, recording its
// original path so it can be restored. A non-recursive delete of a non-empty
// directory is still refused, exactly as a hard delete would be.
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

// ListTrash lists a root's recycle bin, newest first. The scan keeps only the
// newest fileTrashMaxList+1 items by the metadata file's modification time (a
// bounded heap), so parsing cost is O(page) rather than O(bin size); when more
// entries exist the response reports truncated, so the cap is never silent. A
// root with no bin lists as empty. An item whose metadata is unreadable or whose
// payload is missing (a half-written delete a crash left behind) is skipped
// rather than listed as a phantom that can never be restored.
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
			// A metadata-only item is not restorable, so it is not listed.
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

// trashListed pairs an id with its metadata so the bin can be sorted by
// deletion time.
type trashListed struct {
	id  string
	rec trashRecord
}

// trashSelection is the bounded newest-N result of a bin scan.
type trashSelection struct {
	ids       []string
	truncated bool
}

// newestTrashIDs selects at most limit valid bin item ids, newest first by the
// modification time of their metadata file (written at delete time), with a
// bounded heap so the work is O(bin) stats but only O(limit) metadata reads. It
// retains limit+1 candidates so a capped listing can report truncation.
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

// trashIDHeapEntry is one candidate in the bounded newest-N selection.
type trashIDHeapEntry struct {
	id  string
	mod time.Time
}

// trashIDHeap is a min-heap of candidates by modification time, so the oldest is
// at the root and can be evicted when a newer one arrives.
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

// trashIDs lists the valid item ids in a root's bin; a missing bin is empty.
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

// trashEntryProto maps one bin item onto the wire.
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

// Restore moves one bin entry back to its recorded original path (or an
// explicit destination). An existing destination is refused unless overwrite is
// set: the agent answers AlreadyExists, so the caller can offer a rename.
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
	moveErr := s.moveEntry(ctx, root, trashItemData(id), dst, req.GetOverwrite())
	if moveErr != nil {
		unlock()
		return nil, fileError("restore", moveErr)
	}
	// The item directory now holds only metadata; drop it. Best effort: the
	// restore itself already succeeded.
	_ = root.RemoveTree(context.Background(), trashItemDir(id), nil)
	unlock()
	entry, err := root.Stat(dst)
	if err != nil {
		return nil, fileError("restore", err)
	}
	return &agentv1.RestoreResponse{Entry: fileEntry(entry), Path: dst}, nil
}

// Purge permanently removes one bin entry.
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
	// Serialise with a restore (and another purge) of the same item: a purge
	// must never remove the item directory while a cross-device restore is
	// still copying its payload.
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

// EmptyTrash permanently removes every entry in a root's recycle bin. It is
// idempotent: an absent bin is a successful empty.
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
		// Take the same per-item lock a restore uses, so emptying the bin
		// cannot remove an item's payload while it is being restored.
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
