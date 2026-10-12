package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// newTestFileServer builds a FileServer over two temp roots.
func newTestFileServer(t *testing.T) (*FileServer, string) {
	t.Helper()
	cfg := Config{
		ManagedVolumeRoot: t.TempDir(),
		ComposeRoot:       t.TempDir(),
		CertDir:           filepath.Join(t.TempDir(), "certs"),
	}
	server, err := NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, cfg.ManagedVolumeRoot
}

func TestValidateFileRootTable(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	data := filepath.Join(base, "gotham")
	secrets := filepath.Join(base, "agent", "certs")
	proxy := filepath.Join(base, "agent", "traefik")
	for _, dir := range []string{filepath.Join(data, "volumes"), secrets, proxy} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	protected := []protectedPath{
		{path: data, exclusive: false},
		{path: secrets, exclusive: true},
		{path: proxy, exclusive: true},
	}

	valid := []string{
		filepath.Join(data, "volumes"), // a descendant of a protected path
		filepath.Join(base, "other"),   // unrelated
	}
	if err := os.MkdirAll(valid[1], 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range valid {
		if _, err := validateFileRoot(dir, protected); err != nil {
			t.Errorf("validateFileRoot(%q) = %v, want nil", dir, err)
		}
	}

	invalid := []string{
		"relative/path",
		base,                          // an ancestor of a protected path
		data,                          // the protected path itself
		secrets,                       // an exclusive protected path
		filepath.Join(secrets, "sub"), // inside an exclusive path
		proxy,                         // the proxy directory (ACME keys)
	}
	for _, dir := range invalid {
		if _, err := validateFileRoot(dir, protected); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
}

func TestValidateFileRootRejectsDangerousTrees(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/home", "/root", "/var/lib/docker", "/"} {
		if _, err := validateFileRoot(dir, nil); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
	// A root that is a symlink into a dangerous tree is judged by its target.
	base := t.TempDir()
	link := filepath.Join(base, "evil")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFileRoot(link, nil); err == nil {
		t.Error("validateFileRoot(symlink to /etc) = nil, want an error")
	}
}

func TestFileProtectedPathsIncludesAgentState(t *testing.T) {
	t.Parallel()
	cfg := Config{
		CertDir:           "/srv/agent/certs",
		BinaryPath:        "/srv/agent/bin/gotham-agent",
		UpdateLockPath:    "/srv/agent/update.lock",
		UpdateStatusPath:  "/srv/updater/update.status",
		UpdatePendingPath: "/srv/agent/update.pending",
		UpdateRetryPath:   "/srv/agent/update.retry",
	}
	paths := fileProtectedPaths(cfg)
	for _, want := range []string{cfg.CertDir, cfg.BinaryPath, cfg.UpdateLockPath, cfg.UpdateStatusPath} {
		found := false
		for _, p := range paths {
			if p.path == want {
				found = true
			}
		}
		if !found {
			t.Errorf("protected paths do not include %q", want)
		}
	}
}

func TestConfiguredFileRoots(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ManagedVolumeRoot: "/srv/volumes",
		ComposeRoot:       "/srv/compose",
		FileRoots:         "/srv/data:/srv/other",
	}
	roots := configuredFileRoots(cfg)
	if len(roots) != 4 {
		t.Fatalf("roots = %d, want 4", len(roots))
	}
	if roots[0].name != fileRootVolumes || !roots[0].writable {
		t.Fatalf("volumes root = %+v, want writable", roots[0])
	}
	if roots[1].name != fileRootCompose || roots[1].writable {
		t.Fatalf("compose root = %+v, want read-only", roots[1])
	}
	for _, extra := range roots[2:] {
		if extra.writable {
			t.Fatalf("extra root %+v is writable, want read-only", extra)
		}
	}
}

func TestFileServerListRoots(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	resp, err := server.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(resp.GetRoots()) != 2 {
		t.Fatalf("roots = %d, want 2", len(resp.GetRoots()))
	}
	if got := resp.GetRoots()[0]; got.GetName() != fileRootVolumes || !got.GetWritable() || got.GetPath() == "" {
		t.Fatalf("volumes root = %+v", got)
	}
	if got := resp.GetRoots()[1]; got.GetName() != fileRootCompose || got.GetWritable() {
		t.Fatalf("compose root = %+v", got)
	}
}

func TestFileServerListStatRead(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	if err := os.MkdirAll(filepath.Join(volumeRoot, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("hello, files")
	if err := os.WriteFile(filepath.Join(volumeRoot, "app", "readme.txt"), body, 0o640); err != nil {
		t.Fatal(err)
	}

	list, err := server.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes, Path: "app"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.GetEntries()) != 1 {
		t.Fatalf("entries = %d, want 1", len(list.GetEntries()))
	}
	entry := list.GetEntries()[0]
	if entry.GetName() != "readme.txt" || entry.GetKind() != agentv1.FileKind_FILE_KIND_FILE {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.GetEtag() == "" || !strings.HasPrefix(entry.GetEtag(), `"`) {
		t.Fatalf("etag %q is not a quoted entity tag", entry.GetEtag())
	}

	stat, err := server.Stat(context.Background(), &agentv1.StatRequest{Root: fileRootVolumes, Path: "app/readme.txt"})
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.GetEntry().GetEtag() != entry.GetEtag() {
		t.Fatalf("stat etag %q != list etag %q", stat.GetEntry().GetEtag(), entry.GetEtag())
	}

	var got bytes.Buffer
	if err := server.Read(&agentv1.ReadRequest{Root: fileRootVolumes, Path: "app/readme.txt"}, &collectReadStream{sink: &got}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got.Bytes(), body) {
		t.Fatalf("read %q, want %q", got.Bytes(), body)
	}
}

func TestFileServerErrors(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	ctx := context.Background()

	if _, err := server.List(ctx, &agentv1.ListRequest{Root: "nope"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("List(unknown root) code = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := server.List(ctx, &agentv1.ListRequest{Root: fileRootVolumes, Path: "../etc"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("List(traversal) code = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := server.Stat(ctx, &agentv1.StatRequest{Root: fileRootVolumes, Path: "missing"}); status.Code(err) != codes.NotFound {
		t.Errorf("Stat(missing) code = %v, want NotFound", status.Code(err))
	}
	if err := server.Read(&agentv1.ReadRequest{Root: fileRootVolumes, Path: "missing"}, &collectReadStream{}); status.Code(err) != codes.NotFound {
		t.Errorf("Read(missing) code = %v, want NotFound", status.Code(err))
	}
}

func TestFileServerUnimplementedWrite(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("Mkdir code = %v, want Unimplemented", status.Code(err))
	}
	if err := server.Write(nil); status.Code(err) != codes.Unimplemented {
		t.Fatalf("Write code = %v, want Unimplemented", status.Code(err))
	}
}

func TestFileServerListCap(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	big := filepath.Join(volumeRoot, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	const total = 10_000
	for i := range total {
		file, err := os.Create(filepath.Join(big, fmt.Sprintf("f%05d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	first, err := server.List(ctx, &agentv1.ListRequest{Root: fileRootVolumes, Path: "big"})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(first.GetEntries()) != fileListLimit || !first.GetTruncated() || first.GetCursor() == "" {
		t.Fatalf("page 1 = %d entries, truncated=%v cursor=%q", len(first.GetEntries()), first.GetTruncated(), first.GetCursor())
	}

	second, err := server.List(ctx, &agentv1.ListRequest{Root: fileRootVolumes, Path: "big", Cursor: first.GetCursor()})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(second.GetEntries()) != total-fileListLimit || second.GetTruncated() {
		t.Fatalf("page 2 = %d entries, truncated=%v, want %d/false", len(second.GetEntries()), second.GetTruncated(), total-fileListLimit)
	}
}

func TestFileChunkSizeBelowRecvCap(t *testing.T) {
	t.Parallel()
	if fileChunkSize >= agentMaxRecvMsgSize {
		t.Fatalf("fileChunkSize %d is not below the %d receive cap", fileChunkSize, agentMaxRecvMsgSize)
	}
}

func TestFileServerReadChunks(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	body := bytes.Repeat([]byte("x"), fileChunkSize+1024)
	if err := os.WriteFile(filepath.Join(volumeRoot, "blob"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	stream := &collectReadStream{sink: &got}
	if err := server.Read(&agentv1.ReadRequest{Root: fileRootVolumes, Path: "blob"}, stream); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Len() != len(body) {
		t.Fatalf("read %d bytes, want %d", got.Len(), len(body))
	}
	for i, size := range stream.sizes {
		if size > fileChunkSize {
			t.Fatalf("chunk %d = %d bytes, exceeds %d", i, size, fileChunkSize)
		}
	}
}

// collectReadStream implements the Read server stream without a transport.
type collectReadStream struct {
	grpc.ServerStream
	sink  io.Writer
	sizes []int
}

func (s *collectReadStream) Send(chunk *agentv1.ReadChunk) error {
	s.sizes = append(s.sizes, len(chunk.GetData()))
	if s.sink == nil {
		return nil
	}
	_, err := s.sink.Write(chunk.GetData())
	return err
}

func (s *collectReadStream) Context() context.Context { return context.Background() }

// startFileServiceTestServer serves a real agent gRPC server over loopback and
// returns a FileService client, so the option that registers (or withholds)
// the service is exercised end to end.
func startFileServiceTestServer(t *testing.T, options ...ServerOption) agentv1.FileServiceClient {
	t.Helper()
	server, err := NewServer("127.0.0.1:0", nil, agentv1.UnimplementedDockerServiceServer{}, discardLogger(), options...)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return agentv1.NewFileServiceClient(conn)
}

func TestFileServiceAbsentWhenDisabled(t *testing.T) {
	t.Parallel()
	client := startFileServiceTestServer(t)
	_, err := client.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("ListRoots code = %v, want Unimplemented", status.Code(err))
	}
	if !strings.Contains(status.Convert(err).Message(), "unknown service") {
		t.Fatalf("error %q does not report an unregistered service", err)
	}
}

func TestFileServicePresentWhenEnabled(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	client := startFileServiceTestServer(t, WithFileService(server))
	resp, err := client.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(resp.GetRoots()) != 2 {
		t.Fatalf("roots = %d, want 2", len(resp.GetRoots()))
	}
}

// TestNewFileServerFailsClosed proves a missing root aborts construction.
func TestNewFileServerFailsClosed(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ManagedVolumeRoot: filepath.Join(t.TempDir(), "does-not-exist"),
		ComposeRoot:       t.TempDir(),
		CertDir:           filepath.Join(t.TempDir(), "certs"),
	}
	if _, err := NewFileServer(cfg, discardLogger()); err == nil {
		t.Fatal("NewFileServer with a missing root = nil, want an error")
	}
}
