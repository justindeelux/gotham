package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"golang.org/x/sys/unix"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// scriptedWriteStream feeds a fixed request sequence to a client-streaming
// Write and captures the response, without a transport.
type scriptedWriteStream struct {
	grpc.ServerStream
	msgs []*agentv1.WriteRequest
	next int
	resp *agentv1.WriteResponse
}

func (s *scriptedWriteStream) Recv() (*agentv1.WriteRequest, error) {
	if s.next >= len(s.msgs) {
		return nil, io.EOF
	}
	msg := s.msgs[s.next]
	s.next++
	return msg, nil
}

func (s *scriptedWriteStream) SendAndClose(resp *agentv1.WriteResponse) error {
	s.resp = resp
	return nil
}

func (s *scriptedWriteStream) Context() context.Context { return context.Background() }

// collectProgressStream implements a server-streaming Copy/Delete.
type collectProgressStream struct {
	grpc.ServerStream
	sent []*agentv1.Progress
	ctx  context.Context
}

func (s *collectProgressStream) Send(p *agentv1.Progress) error {
	s.sent = append(s.sent, p)
	return nil
}

func (s *collectProgressStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *collectProgressStream) done() bool {
	return len(s.sent) > 0 && s.sent[len(s.sent)-1].GetDone()
}

// writeStream builds a Write message sequence for body.
func writeStream(root, path string, size int64, expect string, overwrite bool, mode uint32, body []byte) *scriptedWriteStream {
	msgs := []*agentv1.WriteRequest{{
		Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{
			Root: root, Path: path, Size: size, ExpectEtag: expect, Overwrite: overwrite, Mode: mode,
		}},
	}}
	const chunk = 4096
	for off := 0; off < len(body); off += chunk {
		end := min(off+chunk, len(body))
		msgs = append(msgs, &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Data{Data: body[off:end]}})
	}
	return &scriptedWriteStream{msgs: msgs}
}

// write sends a complete body and returns the response.
func write(t *testing.T, server *FileServer, root, path string, expect string, overwrite bool, body []byte) (*agentv1.WriteResponse, error) {
	t.Helper()
	stream := writeStream(root, path, int64(len(body)), expect, overwrite, 0o644, body)
	err := server.Write(stream)
	return stream.resp, err
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".gotham-tmp-") {
			t.Fatalf("a staging file leaked: %s", filepath.Join(dir, entry.Name()))
		}
	}
}

func TestFileServerWriteCreateAndReplace(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	ctx := context.Background()

	first, err := write(t, server, fileRootVolumes, "note.txt", "", false, []byte("one"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !first.GetCreated() || first.GetWriteMode() != "rename" || first.GetEntry().GetSize() != 3 {
		t.Fatalf("create response = %+v", first)
	}

	// Replacing with the etag we just read succeeds and is not a create.
	second, err := write(t, server, fileRootVolumes, "note.txt", first.GetEntry().GetEtag(), false, []byte("two-two"))
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if second.GetCreated() || second.GetEntry().GetSize() != 7 {
		t.Fatalf("replace response = %+v", second)
	}

	got, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two-two" {
		t.Fatalf("content = %q, want %q", got, "two-two")
	}
	stat, err := server.Stat(ctx, &agentv1.StatRequest{Root: fileRootVolumes, Path: "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if stat.GetEntry().GetEtag() != second.GetEntry().GetEtag() {
		t.Fatalf("etag changed: %q vs %q", stat.GetEntry().GetEtag(), second.GetEntry().GetEtag())
	}
	assertNoTempFiles(t, root)
}

func TestFileServerWriteExclusiveCreate(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("a")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("b")); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("exclusive create over an existing file = %v, want AlreadyExists", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "a" {
		t.Fatalf("content = %q, want the original", got)
	}
	assertNoTempFiles(t, root)
}

func TestFileServerWriteOverwriteRefusalAndForce(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	// An empty expect_etag means exclusive create, so an existing target is a conflict.
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("b")); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("no-overwrite replace = %v, want AlreadyExists", err)
	}
	// overwrite=true forces the replace without an etag.
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", true, []byte("c")); err != nil {
		t.Fatalf("forced replace: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "c" {
		t.Fatalf("content = %q, want %q", got, "c")
	}
}

func TestFileServerWriteEtagMismatch(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "x.txt", `"stale"`, false, []byte("b")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale etag = %v, want FailedPrecondition", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "a" {
		t.Fatalf("content = %q, want the original", got)
	}
	assertNoTempFiles(t, root)
}

func TestFileServerWriteTruncatedLeavesTargetClean(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("original")); err != nil {
		t.Fatal(err)
	}
	// A short stream: the header declares 100 bytes, the body carries 3.
	stream := &scriptedWriteStream{msgs: []*agentv1.WriteRequest{
		{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{Root: fileRootVolumes, Path: "x.txt", Size: 100, Overwrite: true}}},
		{Part: &agentv1.WriteRequest_Data{Data: []byte("bad")}},
	}}
	if err := server.Write(stream); status.Code(err) != codes.Aborted {
		t.Fatalf("truncated stream = %v, want Aborted", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "original" {
		t.Fatalf("content = %q, want the original after a truncated upload", got)
	}
	assertNoTempFiles(t, root)

	// A create that fails partway must leave no file at all.
	stream = &scriptedWriteStream{msgs: []*agentv1.WriteRequest{
		{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{Root: fileRootVolumes, Path: "new.txt", Size: 100}}},
		{Part: &agentv1.WriteRequest_Data{Data: []byte("bad")}},
	}}
	if err := server.Write(stream); status.Code(err) != codes.Aborted {
		t.Fatalf("truncated create = %v, want Aborted", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a truncated create left a target: %v", err)
	}
	assertNoTempFiles(t, root)
}

func TestFileServerWriteCommitDigest(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	bad := &scriptedWriteStream{msgs: []*agentv1.WriteRequest{
		{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{Root: fileRootVolumes, Path: "d.txt", Size: 3}}},
		{Part: &agentv1.WriteRequest_Data{Data: []byte("abc")}},
		{Part: &agentv1.WriteRequest_Commit{Commit: &agentv1.WriteCommit{Sha256: strings.Repeat("0", 64)}}},
	}}
	if err := server.Write(bad); status.Code(err) != codes.Aborted {
		t.Fatalf("bad digest = %v, want Aborted", err)
	}
	// ba7816bf... is sha256("abc").
	good := &scriptedWriteStream{msgs: []*agentv1.WriteRequest{
		{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{Root: fileRootVolumes, Path: "d.txt", Size: 3}}},
		{Part: &agentv1.WriteRequest_Data{Data: []byte("abc")}},
		{Part: &agentv1.WriteRequest_Commit{Commit: &agentv1.WriteCommit{Sha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"}}},
	}}
	if err := server.Write(good); err != nil {
		t.Fatalf("good digest: %v", err)
	}
}

func TestFileServerWriteInPlaceKeepsInode(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	path := filepath.Join(root, "hard.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(root, "hard.link")); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	etag := etagOf(t, server, "hard.txt")

	resp, err := write(t, server, fileRootVolumes, "hard.txt", etag, false, []byte("replaced-in-place"))
	if err != nil {
		t.Fatalf("in-place write: %v", err)
	}
	if resp.GetWriteMode() != "in_place" {
		t.Fatalf("write mode = %q, want in_place", resp.GetWriteMode())
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("in-place write replaced the inode")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "replaced-in-place" {
		t.Fatalf("content = %q", got)
	}
	linked, _ := os.ReadFile(filepath.Join(root, "hard.link"))
	if string(linked) != "replaced-in-place" {
		t.Fatalf("hardlink content = %q; in-place should update every link", linked)
	}
}

// etagOf returns the current etag of a file through the Stat RPC.
func etagOf(t *testing.T, server *FileServer, rel string) string {
	t.Helper()
	resp, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: fileRootVolumes, Path: rel})
	if err != nil {
		t.Fatal(err)
	}
	return resp.GetEntry().GetEtag()
}

func TestFileServerWriteConcurrentWriters(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "race.txt", "", false, []byte("v0")); err != nil {
		t.Fatal(err)
	}
	base := etagOf(t, server, "race.txt")

	const writers = 2
	results := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := write(t, server, fileRootVolumes, "race.txt", base, false, []byte("value-from-writer"))
			results[i] = err
		}(i)
	}
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, err := range results {
		switch status.Code(err) {
		case codes.OK:
			okCount++
		case codes.FailedPrecondition:
			conflictCount++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if okCount != 1 || conflictCount != writers-1 {
		t.Fatalf("ok=%d conflict=%d, want 1/%d", okCount, conflictCount, writers-1)
	}
}

func TestFileServerMutationsRefuseReadOnlyRoot(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	compose := server.roots[fileRootCompose].Path()
	const root = fileRootCompose

	if _, err := write(t, server, root, "x", "", false, []byte("a")); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Write = %v, want PermissionDenied", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: root, Path: "d"}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Mkdir = %v, want PermissionDenied", err)
	}
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{Root: root, Path: "x", Mode: 0o600}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Chmod = %v, want PermissionDenied", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: root, Source: "a", Destination: "b"}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Rename = %v, want PermissionDenied", err)
	}
	if err := server.Copy(&agentv1.CopyRequest{Root: root, Source: "a", Destination: "b"}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Copy = %v, want PermissionDenied", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: root, Path: "a"}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("Delete = %v, want PermissionDenied", err)
	}
	if _, err := os.Stat(filepath.Join(compose, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a read-only root was mutated")
	}
}

func TestFileServerMkdir(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	resp, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "dir", Mode: 0o750})
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if resp.GetEntry().GetKind() != agentv1.FileKind_FILE_KIND_DIRECTORY {
		t.Fatalf("entry = %+v", resp.GetEntry())
	}
	info, err := os.Stat(filepath.Join(root, "dir"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %o, want 750", info.Mode().Perm())
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "dir"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("Mkdir over an existing dir = %v, want AlreadyExists", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "missing/child"}); status.Code(err) != codes.NotFound {
		t.Fatalf("Mkdir with a missing parent = %v, want NotFound", err)
	}
}

func TestFileServerRename(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "a.txt", Destination: "b.txt"}); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	// Refuse overwriting the existing destination without overwrite.
	if _, err := write(t, server, fileRootVolumes, "c.txt", "", false, []byte("c")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "c.txt", Destination: "b.txt"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("Rename over an existing file = %v, want AlreadyExists", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "dir", Destination: "dir/inside"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Rename into its own subtree = %v, want InvalidArgument", err)
	}
}

func TestFileServerCopyAndDelete(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	// Build a tree to copy: dir/{file, link}.
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "src"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "src/file.txt", "", false, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file.txt", filepath.Join(root, "src", "link")); err != nil {
		t.Fatal(err)
	}

	stream := &collectProgressStream{}
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src", Destination: "dst"}, stream); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if !stream.done() {
		t.Fatal("Copy stream did not end with a done message")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dst", "file.txt")); string(got) != "payload" {
		t.Fatalf("copied file = %q", got)
	}
	target, err := os.Readlink(filepath.Join(root, "dst", "link"))
	if err != nil || target != "file.txt" {
		t.Fatalf("copied symlink = %q, %v; want file.txt", target, err)
	}
	// Copy into the source's own subtree is refused.
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src", Destination: "src/inside"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("copy into itself = %v, want InvalidArgument", err)
	}
	// Refuse overwriting an existing destination (a dir or a file).
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src/file.txt", Destination: "dst/file.txt"}, &collectProgressStream{}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("copy over an existing file = %v, want AlreadyExists", err)
	}
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src", Destination: "dst"}, &collectProgressStream{}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("copy over an existing dir = %v, want AlreadyExists", err)
	}

	// A non-recursive delete of a non-empty directory is a failed precondition.
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "dst"}, &collectProgressStream{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("non-recursive delete of a non-empty dir = %v, want FailedPrecondition", err)
	}
	stream = &collectProgressStream{}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "dst", Recursive: true}, stream); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	if !stream.done() {
		t.Fatal("Delete stream did not end with a done message")
	}
	if _, err := os.Stat(filepath.Join(root, "dst")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dst still exists after delete: %v", err)
	}
}

func TestFileServerProtectsAppDirectory(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	const appID = "0f8fad5b-d9cb-469f-a165-70867728950e"
	if err := os.MkdirAll(filepath.Join(root, appID), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{
		Root: fileRootVolumes, Source: appID, Destination: "moved",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("rename of an app dir = %v, want PermissionDenied", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{
		Root: fileRootVolumes, Path: appID, Recursive: true,
	}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("delete of an app dir = %v, want PermissionDenied", err)
	}
	if err := server.Copy(&agentv1.CopyRequest{
		Root: fileRootVolumes, Source: "elsewhere", Destination: appID,
	}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("copy over an app dir = %v, want PermissionDenied", err)
	}
	if _, err := os.Stat(filepath.Join(root, appID)); err != nil {
		t.Fatalf("the app dir was removed: %v", err)
	}
	// Its children are still editable, and a non-UUID directory is not protected.
	if _, err := write(t, server, fileRootVolumes, appID+"/inside.txt", "", false, []byte("ok")); err != nil {
		t.Fatalf("writing inside an app dir: %v", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "plain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "plain", Destination: "plain2"}); err != nil {
		t.Fatalf("rename of an ordinary directory: %v", err)
	}
}

func TestFileServerWriteRejectsSpecialTarget(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can open a FIFO for writing")
	}
	server, root := newTestFileServer(t)
	fifo := filepath.Join(root, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// expect_etag set so it tries to replace the existing special file.
	if _, err := write(t, server, fileRootVolumes, "pipe", `"x"`, true, []byte("data")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("write to a FIFO = %v, want FailedPrecondition", err)
	}
}

// TestFileServerWriteRejectsSymlinkTarget proves a write refuses to replace a
// symlink even when the link points inside the root, keeping the write path
// limited to regular files.
func TestFileServerWriteRejectsSymlinkTarget(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "link.txt", "", true, []byte("data")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("write to a symlink = %v, want FailedPrecondition", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "real.txt"))
	if string(got) != "real" {
		t.Fatalf("the symlink target changed to %q", got)
	}
}

// TestFileServiceWriteOverGRPC exercises the Write RPC over a real transport,
// so the client-streaming wiring, not just the method, is covered.
func TestFileServiceWriteOverGRPC(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	client := startFileServiceTestServer(t, WithFileService(server))

	stream, err := client.Write(context.Background())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	body := bytes.Repeat([]byte("z"), 300_000)
	if err := stream.Send(&agentv1.WriteRequest{Part: &agentv1.WriteRequest_Header{
		Header: &agentv1.WriteHeader{Root: fileRootVolumes, Path: "big.bin", Size: int64(len(body))},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&agentv1.WriteRequest{Part: &agentv1.WriteRequest_Data{Data: body}}); err != nil {
		t.Fatal(err)
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if resp.GetEntry().GetSize() != int64(len(body)) {
		t.Fatalf("size = %d, want %d", resp.GetEntry().GetSize(), len(body))
	}
	got, err := os.ReadFile(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("the streamed body differs from what was written")
	}
}

// blockingWriteStream returns a header, then blocks the next Recv until the
// test releases it, standing in for a peer that stalls mid-upload.
type blockingWriteStream struct {
	grpc.ServerStream
	header  *agentv1.WriteHeader
	sent    bool
	release chan struct{}
}

func (s *blockingWriteStream) Recv() (*agentv1.WriteRequest, error) {
	if !s.sent {
		s.sent = true
		return &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Header{Header: s.header}}, nil
	}
	<-s.release
	return nil, io.EOF
}

func (s *blockingWriteStream) SendAndClose(*agentv1.WriteResponse) error { return nil }

func (s *blockingWriteStream) Context() context.Context { return context.Background() }

// TestFileServerWriteStaleEtagIgnoresOverwrite pins finding 5: a supplied etag
// that no longer matches is a failed precondition even when overwrite is set,
// so a stale save can never silently clobber a newer file.
func TestFileServerWriteStaleEtagIgnoresOverwrite(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "x.txt", "", false, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "x.txt", `"stale"`, true, []byte("clobber")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale etag with overwrite = %v, want FailedPrecondition", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "original" {
		t.Fatalf("content = %q, want the original", got)
	}
	// A matching etag with overwrite still replaces.
	etag := etagOf(t, server, "x.txt")
	if _, err := write(t, server, fileRootVolumes, "x.txt", etag, true, []byte("replaced")); err != nil {
		t.Fatalf("matching etag with overwrite: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "x.txt")); string(got) != "replaced" {
		t.Fatalf("content = %q, want replaced", got)
	}
}

// TestFileServerWriteInPlaceRefusesFullDisk pins finding 4 and the round-2
// hardening: an in-place write refuses before writing when the target's own
// filesystem does not hold the incoming body, so a full disk cannot leave a
// container-owned file half-written.
func TestFileServerWriteInPlaceRefusesFullDisk(t *testing.T) {
	old := fileFreeBytesOn
	fileFreeBytesOn = func(*os.File) uint64 { return 5 }
	defer func() { fileFreeBytesOn = old }()

	server, root := newTestFileServer(t)
	path := filepath.Join(root, "hard.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(root, "hard.link")); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}
	body := []byte("0123456789") // 10 bytes, more than the 5 bytes "free"
	if _, err := write(t, server, fileRootVolumes, "hard.txt", etagOf(t, server, "hard.txt"), true, body); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("in-place write on a full disk = %v, want ResourceExhausted", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatalf("content = %q, want the original after a refused write", got)
	}
	assertNoTempFiles(t, root)
}

// TestFileServerWriteInPlaceUnknownFreeSpace pins the round-2 fix that a statfs
// failure (free == 0) refuses a non-empty in-place body rather than skipping
// the check, while an empty body still commits.
func TestFileServerWriteInPlaceUnknownFreeSpace(t *testing.T) {
	old := fileFreeBytesOn
	fileFreeBytesOn = func(*os.File) uint64 { return 0 }
	defer func() { fileFreeBytesOn = old }()

	server, root := newTestFileServer(t)
	path := filepath.Join(root, "hard.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(root, "hard.link")); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}
	if _, err := write(t, server, fileRootVolumes, "hard.txt", etagOf(t, server, "hard.txt"), true, []byte("data")); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("in-place write with unknown free space = %v, want ResourceExhausted", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Fatalf("content = %q, want the original", got)
	}
	// An empty body needs no space and still commits in place.
	resp, err := write(t, server, fileRootVolumes, "hard.txt", etagOf(t, server, "hard.txt"), true, nil)
	if err != nil {
		t.Fatalf("empty in-place write: %v", err)
	}
	if resp.GetWriteMode() != "in_place" {
		t.Fatalf("write mode = %q, want in_place", resp.GetWriteMode())
	}
	if got, _ := os.ReadFile(path); len(got) != 0 {
		t.Fatalf("content = %q, want empty", got)
	}
}

// TestFileServerWriteIdleTimeout pins finding 9: a Write that receives nothing
// after its header times out and releases its transfer slot instead of holding
// it forever.
func TestFileServerWriteIdleTimeout(t *testing.T) {
	old := fileRecvIdleTimeout
	fileRecvIdleTimeout = 20 * time.Millisecond
	defer func() { fileRecvIdleTimeout = old }()

	server, root := newTestFileServer(t)
	release := make(chan struct{})
	stream := &blockingWriteStream{
		header:  &agentv1.WriteHeader{Root: fileRootVolumes, Path: "stall.txt", Size: 4},
		release: release,
	}
	err := server.Write(stream)
	close(release)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("stalled write = %v, want DeadlineExceeded", err)
	}
	if _, err := os.Stat(filepath.Join(root, "stall.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a stalled write created a target: %v", err)
	}
	assertNoTempFiles(t, root)
	// The slot is free again: a normal write succeeds.
	if _, err := write(t, server, fileRootVolumes, "after.txt", "", false, []byte("ok")); err != nil {
		t.Fatalf("write after a timeout: %v", err)
	}
}

// TestFileServerProtectsAppDirViaSymlinkedParent pins finding 1: a symlinked
// parent that resolves back to the root cannot be used to rename or delete a
// sibling application directory.
func TestFileServerProtectsAppDirViaSymlinkedParent(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	const appA = "0f8fad5b-d9cb-469f-a165-70867728950e"
	const appB = "1f8fad5b-d9cb-469f-a165-70867728950f"
	for _, name := range []string{appA, appB} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// appA/up -> .. so appA/up/appB resolves to the top-level appB.
	if err := os.Symlink("..", filepath.Join(root, appA, "up")); err != nil {
		t.Fatal(err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: appA + "/up/" + appB, Recursive: true}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("delete through a symlinked parent = %v, want PermissionDenied", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: appA + "/up/" + appB, Destination: "moved"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("rename through a symlinked parent = %v, want PermissionDenied", err)
	}
	if _, err := os.Stat(filepath.Join(root, appB)); err != nil {
		t.Fatalf("the app dir was removed through the symlink: %v", err)
	}
}

// TestFileServerCopyRefusesSymlinkedSelfCopy pins finding 2: a destination whose
// resolved parent is the source is refused, so a symlinked destination cannot
// recurse without bound and corrupt the source.
func TestFileServerCopyRefusesSymlinkedSelfCopy(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "src"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "src/f", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src", filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src", Destination: "out/inner"}, &collectProgressStream{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("copy into a symlink to the source = %v, want InvalidArgument", err)
	}
}

// TestFileServerCopyDeleteHonourCancelledContext pins finding 3: a cancelled
// stream aborts Copy and Delete promptly.
func TestFileServerCopyDeleteHonourCancelledContext(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "src"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(t, server, fileRootVolumes, "src/f", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &collectProgressStream{ctx: ctx}
	if err := server.Copy(&agentv1.CopyRequest{Root: fileRootVolumes, Source: "src", Destination: "dst"}, stream); status.Code(err) != codes.Canceled {
		t.Fatalf("Copy with a cancelled context = %v, want Canceled", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "src", Recursive: true}, stream); status.Code(err) != codes.Canceled {
		t.Fatalf("Delete with a cancelled context = %v, want Canceled", err)
	}
}

// TestFileServerRenameNonOverwriteRefusesExistingDir pins finding 7 at the RPC
// level: a non-overwriting rename onto an existing directory is refused and the
// source survives.
func TestFileServerRenameNonOverwriteRefusesExistingDir(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	for _, name := range []string{"src", "dst"} {
		if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{Root: fileRootVolumes, Source: "src", Destination: "dst"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("non-overwrite rename onto an existing dir = %v, want AlreadyExists", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src")); err != nil {
		t.Fatalf("source lost on a refused rename: %v", err)
	}
}

// TestFileErrorMapping pins finding 6: the resource, name and read-only errnos
// map onto the documented gRPC codes, and an existing status passes through.
func TestFileErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"enospc", &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}, codes.ResourceExhausted},
		{"edquot", syscall.EDQUOT, codes.ResourceExhausted},
		{"efbig", syscall.EFBIG, codes.ResourceExhausted},
		{"enametoolong", &os.PathError{Op: "open", Path: "x", Err: syscall.ENAMETOOLONG}, codes.InvalidArgument},
		{"eisdir", &os.PathError{Op: "open", Path: "x", Err: syscall.EISDIR}, codes.InvalidArgument},
		{"enotdir", &os.PathError{Op: "open", Path: "x", Err: syscall.ENOTDIR}, codes.InvalidArgument},
		{"erofs", &os.PathError{Op: "write", Path: "x", Err: syscall.EROFS}, codes.PermissionDenied},
		{"notempty", fsroot.ErrNotEmpty, codes.FailedPrecondition},
		{"exists", fsroot.ErrExists, codes.AlreadyExists},
		{"escape", fsroot.ErrEscape, codes.PermissionDenied},
	}
	for _, tc := range cases {
		if got := status.Code(fileError("test", tc.err)); got != tc.want {
			t.Errorf("%s: fileError = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := status.Code(fileError("test", status.Error(codes.ResourceExhausted, "x"))); got != codes.ResourceExhausted {
		t.Errorf("status passthrough = %v, want ResourceExhausted", got)
	}
}

// TestPathLockIndexInRange pins finding 8: the stripe index is always in range,
// including when the 32-bit hash has its high bit set (the old int(uint32)
// modulo could go negative on a 32-bit build).
func TestPathLockIndexInRange(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	for i := range 5000 {
		idx := pathLockIndex("volumes", fmt.Sprintf("some/deep/path-%d", i), len(server.locks))
		if idx < 0 || idx >= len(server.locks) {
			t.Fatalf("pathLockIndex returned %d, out of range [0,%d)", idx, len(server.locks))
		}
	}
}

// TestFileServerSweepTempFiles pins finding 10: the sweeper removes an old
// staging file and keeps a fresh one.
func TestFileServerSweepTempFiles(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	old := time.Now().Add(-48 * time.Hour)
	stale := filepath.Join(root, fsroot.TempPrefix+"stale")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(root, fsroot.TempPrefix+"fresh")
	if err := os.WriteFile(fresh, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.sweepTempFiles(time.Now().Add(-24 * time.Hour))
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale staging file not swept: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh staging file swept: %v", err)
	}
}

// TestFileServerRenameCrossDeviceFallback pins finding C: when a bind mount
// forces the copy fallback (EXDEV), an overwriting move onto a non-empty
// directory is refused like a same-filesystem rename (ENOTEMPTY) and leaves
// both the source and destination intact; a move onto a free name succeeds.
func TestFileServerRenameCrossDeviceFallback(t *testing.T) {
	server, root := newTestFileServer(t)
	old := renameWithin
	defer func() { renameWithin = old }()
	renameWithin = func(*fsroot.Root, string, string, bool) error {
		return &os.LinkError{Op: "rename", Err: syscall.EXDEV}
	}

	for _, name := range []string{"src", "dst"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dst", "keep"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, moveErr := server.Rename(context.Background(), &agentv1.RenameRequest{
		Root: fileRootVolumes, Source: "src", Destination: "dst", Overwrite: true,
	})
	// Linux refuses a non-empty directory rename with ENOTEMPTY
	// (FailedPrecondition); macOS may report EEXIST (AlreadyExists). Both are
	// the same refusal; what matters is that nothing is destroyed.
	if code := status.Code(moveErr); code != codes.FailedPrecondition && code != codes.AlreadyExists {
		t.Fatalf("cross-device move onto a non-empty dir = %v, want a refusal", moveErr)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "a")); err != nil {
		t.Fatalf("source lost on a refused move: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dst", "keep")); string(got) != "keep" {
		t.Fatalf("destination modified on a refused move: %q", got)
	}

	// A move onto a free name still copies and then removes the source.
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{
		Root: fileRootVolumes, Source: "src", Destination: "moved", Overwrite: true,
	}); err != nil {
		t.Fatalf("cross-device move to a free name: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "moved", "a")); string(got) != "a" {
		t.Fatalf("moved tree missing: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "src")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source survived a successful move: %v", err)
	}
}

// blockingDeleteStream blocks inside Send so a test can observe whether the
// stripe lock is still held while progress is being sent.
type blockingDeleteStream struct {
	grpc.ServerStream
	entered chan struct{}
	release chan struct{}
}

func (s *blockingDeleteStream) Send(*agentv1.Progress) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

func (s *blockingDeleteStream) Context() context.Context { return context.Background() }

// TestFileServerDeleteSendsProgressOutsideLock pins finding E: Delete releases
// its stripe lock before sending progress, so a slow client cannot stall
// unrelated paths that share the stripe.
func TestFileServerDeleteSendsProgressOutsideLock(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "f", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	stream := &blockingDeleteStream{entered: make(chan struct{}, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "f"}, stream)
	}()
	<-stream.entered
	idx := pathLockIndex(fileRootVolumes, "f", len(server.locks))
	if !server.locks[idx].TryLock() {
		t.Fatal("Delete held the stripe lock while sending progress")
	}
	server.locks[idx].Unlock()
	close(stream.release)
	if err := <-done; err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestFullAccessProtectsAppDirectory pins that the whole-filesystem root keeps
// the running-container guard: the application directory of the managed volumes
// root cannot be renamed or deleted through "/".
func TestFullAccessProtectsAppDirectory(t *testing.T) {
	t.Parallel()
	volumes := t.TempDir()
	const appID = "0f8fad5b-d9cb-469f-a165-70867728950e"
	if err := os.MkdirAll(filepath.Join(volumes, appID), 0o755); err != nil {
		t.Fatal(err)
	}
	server, err := NewFileServer(Config{FileFullAccess: true, ManagedVolumeRoot: volumes}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	real, err := filepath.EvalSymlinks(volumes)
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimPrefix(filepath.ToSlash(real), "/")
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{
		Root: fileRootSystem, Source: rel + "/" + appID, Destination: rel + "/moved",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("rename of an app dir through / = %v, want PermissionDenied", err)
	}
	if err := server.Delete(&agentv1.DeleteRequest{
		Root: fileRootSystem, Path: rel + "/" + appID, Recursive: true,
	}, &collectProgressStream{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("delete of an app dir through / = %v, want PermissionDenied", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootSystem, Path: rel + "/plain"}); err != nil {
		t.Fatalf("mkdir beside the app dir: %v", err)
	}
	if _, err := server.Rename(context.Background(), &agentv1.RenameRequest{
		Root: fileRootSystem, Source: rel + "/plain", Destination: rel + "/plain2",
	}); err != nil {
		t.Fatalf("rename of an ordinary directory: %v", err)
	}
}

// chmodMode returns the permission bits of a path under a root.
func chmodMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// TestFileServerChmod pins the happy path: the mode changes on disk and the
// response is the empty ChmodResponse the wired contract returns.
func TestFileServerChmod(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootVolumes, Path: "a.txt", Mode: 0o600,
	}); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if got := chmodMode(t, filepath.Join(root, "a.txt")); got != 0o600 {
		t.Fatalf("mode = %#o, want 0o600", got)
	}
	// The agent re-stats for the control plane, so the new mode must be visible.
	stat, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: fileRootVolumes, Path: "a.txt"})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.GetEntry().GetMode() != 0o600 {
		t.Fatalf("stat mode = %#o, want 0o600", stat.GetEntry().GetMode())
	}
	// A directory can be chmod-ed too and keeps its type.
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootVolumes, Path: ".", Mode: 0o700,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Chmod of the root = %v, want InvalidArgument", err)
	}
}

// TestFileServerChmodRejectsModeBits pins the mask: setuid, setgid, sticky and
// any bit above 0o777 are a single refusal and never touch the file.
func TestFileServerChmodRejectsModeBits(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "a.txt", "", false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []uint32{0o4755, 0o2755, 0o1755, 0o7777, 0o10000, 0o4000} {
		t.Run(fmt.Sprintf("%#o", mode), func(t *testing.T) {
			if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
				Root: fileRootVolumes, Path: "a.txt", Mode: mode,
			}); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Chmod(%#o) = %v, want InvalidArgument", mode, err)
			}
		})
	}
	if got := chmodMode(t, filepath.Join(root, "a.txt")); got&0o777 != 0o644 {
		t.Fatalf("a rejected chmod changed the mode: %#o", got)
	}
}

// TestFileServerChmodRejectsSymlink proves the target's mode is never changed
// through a link entry, and a special file is refused.
func TestFileServerChmodRejectsSymlink(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootVolumes, Path: "link.txt", Mode: 0o600,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Chmod of a symlink = %v, want InvalidArgument", err)
	}
	if got := chmodMode(t, filepath.Join(root, "real.txt")); got != 0o644 {
		t.Fatalf("the symlink target's mode changed to %#o, want 0o644 unchanged", got)
	}
}

// TestFileServerChmodRejectsSpecialFile pins the FIFO refusal.
func TestFileServerChmodRejectsSpecialFile(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootVolumes, Path: "pipe", Mode: 0o644,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Chmod of a FIFO = %v, want FailedPrecondition", err)
	}
}

// TestFileServerChmodAppDirStaysDirectory pins the plan decision: a managed
// application directory may be chmod-ed (it stays a directory), unlike rename
// and delete which refuse it.
func TestFileServerChmodAppDirStaysDirectory(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	const appID = "0f8fad5b-d9cb-469f-a165-70867728950e"
	if err := os.MkdirAll(filepath.Join(root, appID), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootVolumes, Path: appID, Mode: 0o700,
	}); err != nil {
		t.Fatalf("Chmod of an app dir: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, appID))
	if err != nil {
		t.Fatalf("stat app dir: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("app dir = dir:%v mode:%#o, want a directory with mode 0o700", info.IsDir(), info.Mode().Perm())
	}
}

// TestFullAccessChmod proves chmod works through the whole-filesystem "system"
// root as well as a confined root.
func TestFullAccessChmod(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := NewFileServer(Config{FileFullAccess: true, ManagedVolumeRoot: base}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	real, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimPrefix(filepath.ToSlash(real), "/")
	if _, err := server.Chmod(context.Background(), &agentv1.ChmodRequest{
		Root: fileRootSystem, Path: rel + "/f.txt", Mode: 0o640,
	}); err != nil {
		t.Fatalf("Chmod through the system root: %v", err)
	}
	if got := chmodMode(t, filepath.Join(base, "f.txt")); got != 0o640 {
		t.Fatalf("mode = %#o, want 0o640", got)
	}
}
