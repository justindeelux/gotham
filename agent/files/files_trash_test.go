package files

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func listTrash(t *testing.T, server *FileServer) []*agentv1.TrashEntry {
	return listTrashResponse(t, server).GetEntries()
}

func listTrashResponse(t *testing.T, server *FileServer) *agentv1.ListTrashResponse {
	t.Helper()
	resp, err := server.ListTrash(context.Background(), &agentv1.ListTrashRequest{Root: RootVolumes})
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	return resp
}

func TestFileServerDeleteMovesToTrash(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("hello")); err != nil {
		t.Fatal(err)
	}

	stream := &collectProgressStream{}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, stream); err != nil {
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

	listing, err := server.List(context.Background(), &agentv1.ListRequest{Root: RootVolumes})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range listing.GetEntries() {
		if e.GetName() == fileTrashDir {
			t.Fatalf("List exposed the recycle bin: %+v", e)
		}
	}
}

func TestFileServerRefusesReservedTrashPath(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt", Recursive: true}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	item := listTrash(t, server)[0]
	inside := fileTrashDir + "/" + item.GetId()

	if _, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: RootVolumes, Path: fileTrashDir}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Stat on the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.List(context.Background(), &agentv1.ListRequest{Root: RootVolumes, Path: inside}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("List inside the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: RootVolumes, Path: inside + "/" + fileTrashData}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Stat on a bin item = %v, want InvalidArgument", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: fileTrashDir, Recursive: true}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Delete of the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: RootVolumes, Source: fileTrashDir, Destination: "moved"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Rename of the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: fileTrashDir + "/evil"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Mkdir inside the bin = %v, want InvalidArgument", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileTrashDir, item.GetId(), fileTrashData)); err != nil {
		t.Fatalf("bin item vanished: %v", err)
	}
}

func TestFileServerTrashRestore(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := listTrash(t, server)[0].GetId()

	resp, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id})
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

	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id = listTrash(t, server)[0].GetId()
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("restore over an existing file = %v, want AlreadyExists", err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{
		Root: RootVolumes, Id: id, Destination: "b.txt", Overwrite: true,
	}); err != nil {
		t.Fatalf("Restore to b.txt: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(got) != "payload" {
		t.Fatalf("restored body = %q, want payload", got)
	}
}

func TestFileServerTrashPurgeAndEmpty(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := write(t, server, RootVolumes, name, "", false, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
	}
	entries := listTrash(t, server)
	if len(entries) != 2 {
		t.Fatalf("trash has %d entries, want 2", len(entries))
	}

	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: RootVolumes, Id: entries[0].GetId()}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("trash has %d entries after purge, want 1", len(got))
	}
	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: RootVolumes, Id: entries[0].GetId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("Purge of a gone item = %v, want NotFound", err)
	}
	if _, err := server.Purge(context.Background(), &agentv1.PurgeRequest{Root: RootVolumes, Id: "../escape"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Purge with a bad id = %v, want InvalidArgument", err)
	}

	resp, err := server.EmptyTrash(context.Background(), &agentv1.EmptyTrashRequest{Root: RootVolumes})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if resp.GetRemoved() != 1 {
		t.Fatalf("EmptyTrash removed %d, want 1", resp.GetRemoved())
	}
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("trash has %d entries after empty, want 0", len(got))
	}
	resp, err = server.EmptyTrash(context.Background(), &agentv1.EmptyTrashRequest{Root: RootVolumes})
	if err != nil || resp.GetRemoved() != 0 {
		t.Fatalf("EmptyTrash on an empty bin = %+v, %v", resp, err)
	}
}

func TestFileServerTrashNonRecursiveRefuses(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, RootVolumes, "d/f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d"}, &collectProgressStream{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("non-recursive delete of a non-empty dir = %v, want FailedPrecondition", err)
	}
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("trash has %d entries after a refused delete, want 0", len(got))
	}
}

func TestFileServerTrashPermanentDelete(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, RootVolumes, "d/f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d", Recursive: true, Permanent: true}, &collectProgressStream{}); err != nil {
		t.Fatalf("permanent Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "d")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("d still exists after a permanent delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileTrashDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("permanent delete used the bin: %v", err)
	}
}

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
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "escape"}, &collectProgressStream{}); err != nil {
		t.Fatalf("Delete of a link: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the symlink target was touched: %v", err)
	}
	id := listTrash(t, server)[0].GetId()

	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id, Destination: "../escape"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Restore to an escaping destination = %v, want InvalidArgument", err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id, Destination: fileTrashDir + "/x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Restore into the bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id, Destination: "back"}); err != nil {
		t.Fatalf("Restore to a safe destination: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, "back")); err != nil || strings.TrimSpace(target) != outside {
		t.Fatalf("restored link = %q, %v; want %q", target, err, outside)
	}
}

func TestFileServerTrashSymlinkParentBypass(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "src.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "src.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	item := listTrash(t, server)[0]

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

	if _, err := write(t, server, RootVolumes, "up/forged.txt", "", false, []byte("evil")); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Write under a symlinked bin parent = %v, want InvalidArgument", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: "nested/up2/dir"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Mkdir under a nested symlinked bin parent = %v, want InvalidArgument", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: RootVolumes, Source: "nowhere", Destination: "up/moved"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Rename into a symlinked bin = %v, want InvalidArgument", err)
	}
	if err := server.Copy(&agentv1.CopyRequest{Root: RootVolumes, Source: "nowhere", Destination: "itemup/copied"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Copy into a symlinked item = %v, want InvalidArgument", err)
	}
	if err := server.Compress(&agentv1.CompressRequest{
		Root: RootVolumes, Sources: []string{"nested"}, Destination: "up/box.tar", Format: agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR,
	}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Compress into a symlinked bin = %v, want InvalidArgument", err)
	}
	if err := server.Extract(&agentv1.ExtractRequest{Root: RootVolumes, Source: "x.tar", Destination: "nested/up2"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Extract into a symlinked bin = %v, want InvalidArgument", err)
	}
	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: item.GetId(), Destination: "up/forged"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("Restore into a symlinked bin = %v, want InvalidArgument", err)
	}

	if _, err := os.Stat(filepath.Join(root, fileTrashDir, "forged.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a forged entry landed in the bin: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("the bin holds %d entries, want the original 1", len(got))
	}
}

func TestFileServerExtractTrashMemberRefused(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "keep.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "keep.txt"}, &collectProgressStream{}); err != nil {
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

	err = server.Extract(&agentv1.ExtractRequest{Root: RootVolumes, Source: "evil.tar", Destination: "."}, &collectProgressStream{})
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

func TestFileServerTrashListTruncatedAndPhantom(t *testing.T) {
	defer func(old int) { fileTrashMaxList = old }(fileTrashMaxList)
	fileTrashMaxList = 2

	server, root := newTestFileServer(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := write(t, server, RootVolumes, name, "", false, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: name}, &collectProgressStream{}); err != nil {
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

func TestFileServerTrashRestorePurgeRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		server, root := newTestFileServer(t)
		name := fmt.Sprintf("f%d.txt", i)
		if _, err := write(t, server, RootVolumes, name, "", false, []byte("payload")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
		item := listTrash(t, server)[0]

		var wg sync.WaitGroup
		var restoreErr, purgeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, restoreErr = server.Restore(ctx, &agentv1.RestoreRequest{Root: RootVolumes, Id: item.GetId()})
		}()
		go func() {
			defer wg.Done()
			_, purgeErr = server.Purge(ctx, &agentv1.PurgeRequest{Root: RootVolumes, Id: item.GetId()})
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

func setTrashDeletedAt(t *testing.T, root, id string, at time.Time) {
	t.Helper()
	metaPath := filepath.Join(root, fileTrashDir, id, fileTrashMeta)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read trash metadata: %v", err)
	}
	var rec trashRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode trash metadata: %v", err)
	}
	rec.DeletedAt = at.UTC()
	data, err = json.Marshal(rec)
	if err != nil {
		t.Fatalf("encode trash metadata: %v", err)
	}
	if err := os.WriteFile(metaPath, data, fileTrashMetaMode); err != nil {
		t.Fatalf("write trash metadata: %v", err)
	}
}

func trashEntryByPath(t *testing.T, server *FileServer, original string) *agentv1.TrashEntry {
	t.Helper()
	for _, entry := range listTrash(t, server) {
		if entry.GetOriginalPath() == original {
			return entry
		}
	}
	t.Fatalf("trash has no entry for %q", original)
	return nil
}

func TestFileServerTrashRetentionExpiresByAge(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.txt", "new.txt"} {
		if _, err := write(t, server, RootVolumes, name, "", false, []byte(name)); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(victim, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "link"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	old := trashEntryByPath(t, server, "old.txt")
	link := trashEntryByPath(t, server, "link")
	setTrashDeletedAt(t, root, old.GetId(), now.Add(-31*24*time.Hour))
	setTrashDeletedAt(t, root, link.GetId(), now.Add(-31*24*time.Hour))

	server.trashMaxAgeDays = 30
	server.sweepTrash(now)
	server.trashMaxAgeDays = 0

	if _, err := os.Stat(filepath.Join(root, fileTrashDir, old.GetId())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an expired item survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileTrashDir, link.GetId())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an expired symlink item survived: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("retention followed a symlink out of the bin: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Fatalf("retention deleted a file outside the bin: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 || got[0].GetOriginalPath() != "new.txt" {
		t.Fatalf("trash holds %+v, want only new.txt", got)
	}
}

func TestFileServerTrashRetentionExpiresBySize(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, name := range []string{"old.txt", "mid.txt", "new.txt"} {
		if _, err := write(t, server, RootVolumes, name, "", false, []byte("01234567")); err != nil {
			t.Fatal(err)
		}
		if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: name}, &collectProgressStream{}); err != nil {
			t.Fatal(err)
		}
		entry := trashEntryByPath(t, server, name)
		setTrashDeletedAt(t, root, entry.GetId(), now.Add(-time.Duration(4-i)*time.Hour))
	}

	server.trashMaxBytes = 12
	server.sweepTrash(now)
	server.trashMaxBytes = 0

	if got := listTrash(t, server); len(got) != 1 || got[0].GetOriginalPath() != "new.txt" {
		t.Fatalf("trash holds %+v, want only new.txt", got)
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Fatalf("retention deleted a file outside the bin: %v", err)
	}
}

func TestFileServerTrashRestoreRecreatesMissingParent(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, RootVolumes, "d/f.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d/f.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d", Recursive: true}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := trashEntryByPath(t, server, "d/f.txt").GetId()

	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id}); err != nil {
		t.Fatalf("Restore with a missing parent: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "d"))
	if err != nil {
		t.Fatalf("restore did not recreate the parent: %v", err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("recreated parent mode = %o, want 750", info.Mode().Perm())
	}
	if got, err := os.ReadFile(filepath.Join(root, "d", "f.txt")); err != nil || string(got) != "payload" {
		t.Fatalf("restored body = %q, %v", got, err)
	}
}

func TestFileServerTrashRestoreDestinationCreatesParents(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := listTrash(t, server)[0].GetId()

	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{
		Root: RootVolumes, Id: id, Destination: "x/y/a.txt",
	}); err != nil {
		t.Fatalf("Restore to a missing parent: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "x", "y"))
	if err != nil {
		t.Fatalf("restore did not create the destination parents: %v", err)
	}
	if info.Mode().Perm() != fileTrashRestoreDirMode {
		t.Fatalf("unrecorded parent mode = %o, want %o", info.Mode().Perm(), fileTrashRestoreDirMode)
	}
	if got, err := os.ReadFile(filepath.Join(root, "x", "y", "a.txt")); err != nil || string(got) != "payload" {
		t.Fatalf("restored body = %q, %v", got, err)
	}
}

func setTrashParents(t *testing.T, root, id string, parents []trashParent) {
	t.Helper()
	metaPath := filepath.Join(root, fileTrashDir, id, fileTrashMeta)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read trash metadata: %v", err)
	}
	var rec trashRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode trash metadata: %v", err)
	}
	rec.Parents = parents
	data, err = json.Marshal(rec)
	if err != nil {
		t.Fatalf("encode trash metadata: %v", err)
	}
	if err := os.WriteFile(metaPath, data, fileTrashMetaMode); err != nil {
		t.Fatalf("write trash metadata: %v", err)
	}
}

func TestTrashMaxAgeDaysClamped(t *testing.T) {
	for _, days := range []int{106752, 1_000_000} {
		cfg := Config{FileTrashMaxAgeDays: days}
		cfg.applyDefaults()
		if cfg.FileTrashMaxAgeDays != MaxTrashMaxAgeDays {
			t.Errorf("FileTrashMaxAgeDays = %d for %d, want the %d cap", cfg.FileTrashMaxAgeDays, days, MaxTrashMaxAgeDays)
		}
	}
}

func TestFileServerTrashRetentionHugeAgeDoesNotPurge(t *testing.T) {
	now := time.Now()
	for _, days := range []int{106752, 1_000_000} {
		days := days
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			t.Parallel()
			if cutoff := trashAgeCutoff(now, days); !cutoff.Before(now) {
				t.Fatalf("cutoff for %d days = %s, want before %s", days, cutoff, now)
			}
			server, root := newTestFileServer(t)
			if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
				t.Fatal(err)
			}
			id := listTrash(t, server)[0].GetId()
			setTrashDeletedAt(t, root, id, now.AddDate(0, 0, -40))

			server.trashMaxAgeDays = days
			server.sweepTrash(now)
			server.trashMaxAgeDays = 0

			if got := listTrash(t, server); len(got) != 1 {
				t.Fatalf("a %d-day setting purged a 40-day-old item: %+v", days, got)
			}
		})
	}
}

func TestFileServerTrashRetentionSizeGrace(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "big.txt", "", false, []byte("01234567890123456789")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "big.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := listTrash(t, server)[0].GetId()
	now := time.Now()

	server.trashMaxBytes = 10
	server.sweepTrash(now)
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("the size pass purged a just-deleted item: %+v", got)
	}

	setTrashDeletedAt(t, root, id, now.Add(-2*time.Hour))
	server.sweepTrash(now)
	server.trashMaxBytes = 0
	if got := listTrash(t, server); len(got) != 0 {
		t.Fatalf("the size pass kept an item past the grace period: %+v", got)
	}
}

func TestFileServerTrashRestoreParentReadOnlyMode(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: RootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, RootVolumes, "d/f.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d/f.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "d", Recursive: true}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := trashEntryByPath(t, server, "d/f.txt").GetId()
	setTrashParents(t, root, id, []trashParent{{Path: "d", Mode: 0o500}})

	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{Root: RootVolumes, Id: id}); err != nil {
		t.Fatalf("Restore into a recorded read-only parent: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "d"))
	if err != nil {
		t.Fatalf("restore did not recreate the parent: %v", err)
	}
	if got := info.Mode().Perm(); got&0o700 != 0o700 {
		t.Fatalf("recreated parent mode = %o, want owner rwx", got)
	}
	if got, err := os.ReadFile(filepath.Join(root, "d", "f.txt")); err != nil || string(got) != "payload" {
		t.Fatalf("restored body = %q, %v", got, err)
	}
}

func TestFileServerTrashRestoreFailedMoveRemovesCreatedParents(t *testing.T) {
	server, root := newTestFileServer(t)
	if _, err := write(t, server, RootVolumes, "a.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: RootVolumes, Path: "a.txt"}, &collectProgressStream{}); err != nil {
		t.Fatal(err)
	}
	id := listTrash(t, server)[0].GetId()

	old := renameWithin
	renameWithin = func(*fsroot.Root, string, string, bool) error { return errors.New("move failed") }
	defer func() { renameWithin = old }()

	if _, err := server.Restore(context.Background(), &agentv1.RestoreRequest{
		Root: RootVolumes, Id: id, Destination: "x/y/a.txt",
	}); err == nil {
		t.Fatal("Restore with a failing move succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a failed restore left created parents behind: %v", err)
	}
	if got := listTrash(t, server); len(got) != 1 {
		t.Fatalf("a failed restore removed the trash entry: %+v", got)
	}
}
