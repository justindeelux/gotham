package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"golang.org/x/sys/unix"

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
}

func (s *collectProgressStream) Send(p *agentv1.Progress) error {
	s.sent = append(s.sent, p)
	return nil
}

func (s *collectProgressStream) Context() context.Context { return context.Background() }

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
	if _, err := write(t, server, fileRootVolumes, "x.txt", `"stale"`, false, []byte("b")); status.Code(err) != codes.Aborted {
		t.Fatalf("stale etag = %v, want Aborted", err)
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
		case codes.Aborted:
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

	// A non-recursive delete of a non-empty directory is refused.
	if err := server.Delete(&agentv1.DeleteRequest{Root: fileRootVolumes, Path: "dst"}, &collectProgressStream{}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("non-recursive delete of a non-empty dir = %v, want AlreadyExists", err)
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
