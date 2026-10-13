package agent

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// listTrash reads a root's recycle bin.
func listTrash(t *testing.T, server *FileServer) []*agentv1.TrashEntry {
	return listTrashResponse(t, server).GetEntries()
}

// listTrashResponse reads a root's recycle bin with its truncation flag.
func listTrashResponse(t *testing.T, server *FileServer) *agentv1.ListTrashResponse {
	t.Helper()
	resp, err := server.ListTrash(context.Background(), &agentv1.ListTrashRequest{Root: fileRootVolumes})
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	return resp
}

// TestFileServerDeleteMovesToTrash pins the core recycle behaviour: a delete
// moves the entry into the root's bin, the bin is hidden from List, and every
// normal file operation refuses the reserved name.
func TestFileServerDeleteMovesToTrash(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("hello")); err != nil {
		t.Fatal(err)
	}

	stream := &collectProgressStream{}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "a.txt"}, stream); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !stream.done() {
		t.Fatal("Delete stream did not end with a done message")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a.txt still exists after delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileTrashDir)); err != nil {
		t.Fatalf("recycle bin missing: %v", err)
	}

	entries := listTrash(t, server)
	if len(entries) != 1 {
		t.Fatalf("trash has %d entries, want 1", len(entries))
	}
	item := entries[0]
	if !validTrashID(item.GetId()) {
		t.Fatalf("trash id %q is not a valid id", item.GetId())
	}
	if item.GetOriginalPath() != "a.txt" || item.GetName() != "a.txt" {
		t.Fatalf("trash entry = %+v, want original a.txt", item)
	}
	if item.GetKind() != agentv1.FileKind_FILE_KIND_FILE || item.GetSize() != 5 {
		t.Fatalf("trash entry = %+v, want file of size 5", item)
	}
	if item.GetDeletedAt() == nil {
		t.Fatal("trash entry has no deleted_at")
	}

	// The bin is not browsable through List.
	listing, err := server.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range listing.GetEntries() {
		if e.GetName() == fileTrashDir {
			t.Fatalf("List exposed the recycle bin: %+v", e)
		}
	}
}

// TestFileServerRefusesReservedTrashPath pins that the reserved tree cannot be
// read, listed, written, renamed or deleted through the normal entry APIs.
func TestFileServerRefusesReservedTrashPath(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	// Delete it so a real bin exists.
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "a.txt", Recursive: true}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	item := listTrash(t, server)[0]
	inside := fileTrashDir + "/" + item.GetId()

	if _, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: fileRootVolumes, Path: fileTrashDir}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Stat on the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes, Path: inside}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("List inside the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: fileRootVolumes, Path: inside + "/" + fileTrashData}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Stat on a bin item = %v, want InvalidArgument", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: fileTrashDir, Recursive: true}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Delete of the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: fileTrashDir, Destination: "moved"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Rename of the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: fileTrashDir + "/evil"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Mkdir inside the bin = %v, want InvalidArgument", err)
	}
	// The painstakingly deleted entry is still there.
	if _, err := os.Stat(filepath.Join(root, fileTrashDir, item.GetId(), fileTrashData)); err != nil {
		t.Fatalf("bin item vanished: %v", err)
	}
}

// TestFileServerTrashRestore pins restore to the original path, the overwrite
// conflict, and restore to an explicit destination.
func TestFileServerTrashRestore(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := listTrash(t, server)[0].GetId()

	resp, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: id})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if resp.GetEntry().GetName() != "a.txt" {
		t.Fatalf("restored entry = %+v, want a.txt", resp.GetEntry())
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(got) != "payload" {
		t.Fatalf("restored body = %q, want payload", got)
	}
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("trash has %d entries after restore, want 0", len(got))
	}

	// A restore over an existing target is a conflict without overwrite.
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id = listTrash(t, server)[0].GetId()
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: id}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("restore over an existing file = %v, want AlreadyExists", err)
	}
	// Overwrite replaces it, and an explicit destination restores elsewhere.
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{
		Root: fileRootVolumes, Id: id, Destination: "b.txt", Overwrite: true,
	}); err != nil {
		t.Fatalf("Restore to b.txt: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(got) != "payload" {
		t.Fatalf("restored body = %q, want payload", got)
	}
}

// TestFileServerTrashPurgeAndEmpty pins permanent delete of one item and of the
// whole bin.
func TestFileServerTrashPurgeAndEmpty(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := write(t, server, fileRootVolumes, name, "", false, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
	}
	entries := listTrash(t, server)
	if len(entries) != 2 {
		t.Fatalf("trash has %d entries, want 2", len(entries))
	}

	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: fileRootVolumes, Id: entries[0].GetId()}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("trash has %d entries after purge, want 1", len(got))
	}
	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: fileRootVolumes, Id: entries[0].GetId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("Purge of a gone item = %v, want NotFound", err)
	}
	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: fileRootVolumes, Id: "../escape"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Purge with a bad id = %v, want InvalidArgument", err)
	}

	resp, err := server.EmptyTrash(context.Background(), &agentv1.EmptyTrashRequest{Root: fileRootVolumes})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if resp.GetRemoved() != 1 {
		t.Fatalf("EmptyTrash removed %d, want 1", resp.GetRemoved())
	}
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("trash has %d entries after empty, want 0", len(got))
	}
	// Emptying an already-empty bin is a no-op success.
	resp, err = server.EmptyTrash(context.Background(), &agentv1.EmptyTrashRequest{Root: fileRootVolumes})
	if err != nil || resp.GetRemoved() != 0 {
		t.Fatalf("EmptyTrash on an empty bin = %+v, %v", resp, err)
	}
}

// TestFileServerTrashNonRecursiveRefuses pins that routing to trash keeps the
// non-recursive delete semantics: a non-empty directory is still refused.
func TestFileServerTrashNonRecursiveRefuses(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "d/f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "d"}, &collectProgressStream{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("non-recursive delete of a non-empty dir = %v, want FailedPrecondition", err)
	}
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("trash has %d entries after a refused delete, want 0", len(got))
	}
}

// TestFileServerTrashPermanentDelete pins that the permanent flag still removes
// an entry outright without touching the bin.
func TestFileServerTrashPermanentDelete(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "d/f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "d", Recursive: true, Permanent: true}, &collectProgressStream{}); err != nil {
		t.Fatalf("permanent Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "d")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("d still exists after a permanent delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileTrashDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("permanent delete used the bin: %v", err)
	}
}

// TestFileServerTrashSymlinkEscape pins confinement: a symlink pointing outside
// the root is moved to the bin as a link (its target is untouched), and a
// restore destination that escapes the root is refused.
func TestFileServerTrashSymlinkEscape(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	// Deleting the link moves the link itself, never its outside target.
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "escape"}, &collectProgressStream{}); err != nil {
		t.Fatalf("Delete of a link: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the symlink target was touched: %v", err)
	}
	id := listTrash(t, server)[0].GetId()

	// A restore destination that escapes the root is refused, lexically...
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: id, Destination: "../escape"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Restore to an escaping destination = %v, want InvalidArgument", err)
	}
	// ...and a destination that resolves back into the bin is refused too.
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: id, Destination: fileTrashDir + "/x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Restore into the bin = %v, want InvalidArgument", err)
	}
	// Clearing the recorded original path is still possible by naming one.
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: id, Destination: "back"}); err != nil {
		t.Fatalf("Restore to a safe destination: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, "back")); err != nil || strings.TrimSpace(target) != outside {
		t.Fatalf("restored link = %q, %v; want %q", target, err, outside)
	}
}

// TestFileServerTrashSymlinkParentBypass pins review finding 1: a create-shaped
// target under a symlink whose parent points into the bin (leaf not existing)
// must still be refused, for every entry-creating operation.
func TestFileServerTrashSymlinkParentBypass(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "src.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "src.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	item := listTrash(t, server)[0]

	// A hostile pre-existing symlink into the bin, at the root and nested, plus
	// one pointing straight at an item directory.
	if err := os.Symlink(fileTrashDir, filepath.Join(root, "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../"+fileTrashDir, filepath.Join(root, "nested", "up2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fileTrashDir+"/"+item.GetId(), filepath.Join(root, "itemup")); err != nil {
		t.Fatal(err)
	}

	if _, err := write(t, server, fileRootVolumes, "up/forged.txt", "", false, []byte("evil")); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Write under a symlinked bin parent = %v, want InvalidArgument", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "nested/up2/dir"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Mkdir under a nested symlinked bin parent = %v, want InvalidArgument", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "nowhere", Destination: "up/moved"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Rename into a symlinked bin = %v, want InvalidArgument", err)
	}
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "nowhere", Destination: "itemup/copied"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Copy into a symlinked item = %v, want InvalidArgument", err)
	}
	if err := server.Compress(&agentv1.CompressRequest{
		Root: fileRootVolumes, Sources: []string{"nested"}, Destination: "up/box.tar", Format: agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR,
	}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Compress into a symlinked bin = %v, want InvalidArgument", err)
	}
	if err := server.Extract(&agentv1.ExtractRequest{Root: fileRootVolumes, Source: "x.tar", Destination: "nested/up2"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Extract into a symlinked bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: fileRootVolumes, Id: item.GetId(), Destination: "up/forged"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Restore into a symlinked bin = %v, want InvalidArgument", err)
	}

	// Nothing was planted in the bin.
	if _, err := os.Stat(filepath.Join(root, fileTrashDir, "forged.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a forged entry landed in the bin: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("the bin holds %d entries, want the original 1", len(got))
	}
}

// TestFileServerExtractTrashMemberRefused pins review finding 2: an archive
// member whose path enters the bin is refused and cannot forge or overwrite a
// bin entry's metadata or payload.
func TestFileServerExtractTrashMemberRefused(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "keep.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "keep.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	item := listTrash(t, server)[0]
	metaPath := filepath.Join(root, fileTrashDir, item.GetId(), fileTrashMeta)
	dataPath := filepath.Join(root, fileTrashDir, item.GetId(), fileTrashData)
	before, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}

	archive := buildTar(t, []tarEntry{
		{name: "ok.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("ok")},
		{name: fileTrashDir + "/" + item.GetId() + "/" + fileTrashMeta, typeflag: tar.TypeReg, mode: 0o644, body: []byte(`{"original_path":"/etc/passwd"}`)},
		{name: fileTrashDir + "/" + item.GetId() + "/" + fileTrashData, typeflag: tar.TypeReg, mode: 0o644, body: []byte("forged")},
	})
	if err := os.WriteFile(filepath.Join(root, "evil.tar"), archive, 0o644); err != nil {
		t.Fatal(err)
	}

	err = server.Extract(&agentv1.ExtractRequest{Root: fileRootVolumes, Source: "evil.tar", Destination: "."}, &collectProgressStream{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Extract with a bin member = %v, want InvalidArgument", err)
	}
	after, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("the bin metadata was removed: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the bin metadata was forged: %q", after)
	}
	if got, err := os.ReadFile(dataPath); err != nil || string(got) != "payload" {
		t.Fatalf("the bin payload was overwritten: %q, %v", got, err)
	}
}

// TestFileServerTrashListTruncatedAndPhantom pins review findings 3/4: a capped
// listing reports truncation instead of silently dropping entries, and a
// metadata-only (half-written) item is not listed as a phantom.
func TestFileServerTrashListTruncatedAndPhantom(t *testing.T) {
	// Not parallel: it lowers the global page cap.
	defer func(old int) { fileTrashMaxList = old }(fileTrashMaxList)
	fileTrashMaxList = 2

	server, root := newTestFileServer(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := write(t, server, fileRootVolumes, name, "", false, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
	}
	resp := listTrashResponse(t, server)
	if !resp.GetTruncated() {
		t.Fatal("a bin over the page cap did not report truncation")
	}
	if len(resp.GetEntries()) != 2 {
		t.Fatalf("capped listing returned %d entries, want 2", len(resp.GetEntries()))
	}

	// A metadata-only item (a crash between the record and the move) is skipped.
	ghostID := "00112233445566778899aabbccddeeff"
	ghostDir := filepath.Join(root, fileTrashDir, ghostID)
	if err := os.MkdirAll(ghostDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghostDir, fileTrashMeta), []byte(`{"original_path":"ghost"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range listTrash(t, server) {
		if entry.GetId() == ghostID {
			t.Fatal("a metadata-only item was listed as a restorable entry")
		}
	}
}

// TestFileServerTrashRestorePurgeRace pins review finding 2 (locks): a restore
// and a purge of the same item must serialise, so the purge can never remove
// the payload mid-restore. Run with -race.
func TestFileServerTrashRestorePurgeRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		server, root := newTestFileServer(t)
		name := fmt.Sprintf("f%d.txt", i)
		if _, err := write(t, server, fileRootVolumes, name, "", false, []byte("payload")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
		item := listTrash(t, server)[0]

		var wg sync.WaitGroup
		var restoreErr, purgeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, restoreErr = server.Restore(ctx, &agentv1.RestoreRequest{Root: fileRootVolumes, Id: item.GetId()})
		}()
		go func() {
			defer wg.Done()
			_, purgeErr = server.Purge(ctx, &agentv1.PurgeRequest{Root: fileRootVolumes, Id: item.GetId()})
		}()
		wg.Wait()

		for _, err := range []error{restoreErr, purgeErr} {
			if err == nil {
				continue
			}
			if status.Code(err) != codes.NotFound {
				t.Fatalf("iteration %d: unexpected error %v", i, err)
			}
		}
		if got := listTrash(t, server); len(got) != 0 {
			t.Fatalf("iteration %d: the bin still holds %d items after the race", i, len(got))
		}
		if restoreErr == nil {
			got, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || string(got) != "payload" {
				t.Fatalf("iteration %d: a successful restore left %q, %v", i, got, err)
			}
		}
	}
}
