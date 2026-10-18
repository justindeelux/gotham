package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/justindeelux/gotham/agent/fsroot"
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
		if _, _, err := validateFileRoot(dir, protected); err != nil {
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
		if _, _, err := validateFileRoot(dir, protected); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
}

func TestValidateFileRootRejectsDangerousTrees(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/home", "/root", "/var/lib/docker", "/"} {
		if _, _, err := validateFileRoot(dir, nil); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
	// A root that is a symlink into a dangerous tree is judged by its target.
	base := t.TempDir()
	link := filepath.Join(base, "evil")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateFileRoot(link, nil); err == nil {
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
	if !roots[0].builtin || !roots[1].builtin {
		t.Fatalf("built-in roots = %+v / %+v, want builtin", roots[0], roots[1])
	}
	for _, extra := range roots[2:] {
		if extra.writable {
			t.Fatalf("extra root %+v is writable, want read-only", extra)
		}
		if extra.builtin {
			t.Fatalf("extra root %+v is builtin, want operator", extra)
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

// TestFileServerListReportsOwner pins that a listing entry carries the owning
// user and group names resolved from the file's uid/gid (JUS-214). A temp file
// and a symlink are owned by this process, so the expectation is resolved the
// same way from the process ids, falling back to the numeric id when a name is
// unavailable.
func TestFileServerListReportsOwner(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	if err := os.MkdirAll(filepath.Join(volumeRoot, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volumeRoot, "app", "owned.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A link keeps its own lstat owner, so its entry must report the same
	// resolved name rather than dropping the owner.
	if err := os.Symlink("owned.txt", filepath.Join(volumeRoot, "app", "link.txt")); err != nil {
		t.Fatal(err)
	}

	wantOwner := strconv.Itoa(os.Geteuid())
	if u, err := user.LookupId(wantOwner); err == nil {
		wantOwner = u.Username
	}
	wantGroup := strconv.Itoa(os.Getegid())
	if g, err := user.LookupGroupId(wantGroup); err == nil {
		wantGroup = g.Name
	}

	list, err := server.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes, Path: "app"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.GetEntries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(list.GetEntries()))
	}
	for _, entry := range list.GetEntries() {
		if got := entry.GetOwner(); got != wantOwner {
			t.Fatalf("%s owner = %q, want %q", entry.GetName(), got, wantOwner)
		}
		if got := entry.GetGroup(); got != wantGroup {
			t.Fatalf("%s group = %q, want %q", entry.GetName(), got, wantGroup)
		}
	}
}

// TestNameCacheFallsBackToNumericID pins that an id with no passwd/group entry
// keeps its numeric string, so a listing never fails over a missing name.
func TestNameCacheFallsBackToNumericID(t *testing.T) {
	t.Parallel()
	names := newNameCache()
	absent := absentID(t)
	want := strconv.FormatUint(uint64(absent), 10)
	if got := names.owner(absent); got != want {
		t.Fatalf("owner = %q, want %q", got, want)
	}
	if got := names.group(absent); got != want {
		t.Fatalf("group = %q, want %q", got, want)
	}
	// A repeated lookup is served from the cache with the same answer.
	if got := names.owner(absent); got != want {
		t.Fatalf("cached owner = %q, want %q", got, want)
	}
}

// absentID returns an id no user and no group resolves to on this host.
func absentID(t *testing.T) uint32 {
	t.Helper()
	for _, id := range []uint32{4294967295, 4294967294, 4000000000} {
		s := strconv.FormatUint(uint64(id), 10)
		_, userErr := user.LookupId(s)
		_, groupErr := user.LookupGroupId(s)
		if userErr != nil && groupErr != nil {
			return id
		}
	}
	t.Skip("every candidate id resolves to a name on this host")
	return 0
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

func TestFileServerReadRange(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	body := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(volumeRoot, "range"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := server.Read(&agentv1.ReadRequest{
		Root: fileRootVolumes, Path: "range", Offset: 4, Limit: 6,
	}, &collectReadStream{sink: &got}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.String() != "456789" {
		t.Fatalf("read %q, want %q", got.String(), "456789")
	}

	// Only an offset: read from there to EOF.
	got.Reset()
	if err := server.Read(&agentv1.ReadRequest{
		Root: fileRootVolumes, Path: "range", Offset: 10,
	}, &collectReadStream{sink: &got}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.String() != "abcdef" {
		t.Fatalf("read %q, want %q", got.String(), "abcdef")
	}

	if err := server.Read(&agentv1.ReadRequest{
		Root: fileRootVolumes, Path: "range", Offset: -1,
	}, &collectReadStream{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("negative offset code = %v, want InvalidArgument", status.Code(err))
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

// TestFileEntryScrubsInvalidUTF8 proves an entry with invalid UTF-8 marshals:
// without the scrub, proto3 rejects the string and the page would fail.
func TestFileEntryScrubsInvalidUTF8(t *testing.T) {
	t.Parallel()
	entry := fileEntry(fsroot.Entry{Name: "bad-\xff\xfe", SymlinkTarget: "target-\x80"})
	if !strings.Contains(entry.GetName(), "\uFFFD") || !strings.Contains(entry.GetSymlinkTarget(), "\uFFFD") {
		t.Fatalf("entry = %+v, want scrubbed name and target", entry)
	}
	if _, err := proto.Marshal(entry); err != nil {
		t.Fatalf("marshal scrubbed entry: %v", err)
	}
}

// TestNewFileServerSkipsMissingBuiltinRoots proves the lazily-created built-in
// roots are skipped with a warning instead of aborting startup.
func TestNewFileServerSkipsMissingBuiltinRoots(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	cfg := Config{
		ManagedVolumeRoot: filepath.Join(base, "volumes"),
		ComposeRoot:       filepath.Join(base, "compose"),
		CertDir:           filepath.Join(base, "certs"),
	}
	server, err := NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if len(server.roots) != 0 {
		t.Fatalf("roots = %d, want 0 (both built-ins missing)", len(server.roots))
	}
}

// TestNewFileServerFailsClosed proves a missing operator root still aborts
// construction, so an explicit misconfiguration is not silently dropped.
func TestNewFileServerFailsClosed(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	cfg := Config{
		ManagedVolumeRoot: t.TempDir(),
		ComposeRoot:       t.TempDir(),
		CertDir:           filepath.Join(base, "certs"),
		FileRoots:         filepath.Join(base, "does-not-exist"),
	}
	if _, err := NewFileServer(cfg, discardLogger()); err == nil {
		t.Fatal("NewFileServer with a missing operator root = nil, want an error")
	}
}

// TestNewFileServerRejectsUnsafeRoot proves a root in a dangerous tree still
// aborts construction.
func TestNewFileServerRejectsUnsafeRoot(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ManagedVolumeRoot: t.TempDir(),
		ComposeRoot:       t.TempDir(),
		FileRoots:         "/etc",
	}
	if _, err := NewFileServer(cfg, discardLogger()); err == nil {
		t.Fatal("NewFileServer with a dangerous root = nil, want an error")
	}
}

// TestRootsOverlap table-tests that equal or nested roots are detected.
func TestRootsOverlap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		other string
		real  string
		want  bool
	}{
		{"/a", "/a", true},
		{"/a", "/a/b", true},
		{"/a/b", "/a", true},
		{"/a", "/ab", false},
		{"/a", "/b", false},
		{"/a/b", "/a/c", false},
	}
	for _, tc := range cases {
		if _, got := overlapRoot(tc.real, []string{tc.other}); got != tc.want {
			t.Errorf("overlapRoot(%q, [%q]) = %v, want %v", tc.real, tc.other, got, tc.want)
		}
	}
}

// TestNewFileServerRejectsOverlappingRoots proves two operator roots that nest
// are refused rather than one shadowing the other.
func TestNewFileServerRejectsOverlappingRoots(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	nested := filepath.Join(base, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ManagedVolumeRoot: t.TempDir(),
		ComposeRoot:       t.TempDir(),
		FileRoots:         strings.Join([]string{base, nested}, string(filepath.ListSeparator)),
	}
	if _, err := NewFileServer(cfg, discardLogger()); err == nil {
		t.Fatal("NewFileServer with overlapping roots = nil, want an error")
	}
}

// TestValidateFileRootRejectsDockerDataRoot proves a non-default data root from
// daemon.json is treated as a dangerous tree.
func TestValidateFileRootRejectsDockerDataRoot(t *testing.T) {
	base := t.TempDir()
	dataRoot := filepath.Join(base, "docker-data")
	inside := filepath.Join(dataRoot, "sub")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(base, "daemon.json")
	if err := os.WriteFile(config, []byte(`{"data-root":"`+dataRoot+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := dockerDaemonConfig
	dockerDaemonConfig = config
	t.Cleanup(func() { dockerDaemonConfig = old })

	for _, dir := range []string{dataRoot, inside} {
		if _, _, err := validateFileRoot(dir, nil); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
}

// TestFileServerListNonUTF8Name proves a filename with invalid UTF-8 is
// scrubbed so the page still marshals, instead of failing the whole List.
func TestFileServerListNonUTF8Name(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows filesystems reject invalid UTF-8 names")
	}
	server, volumeRoot := newTestFileServer(t)
	name := "bad-\xff\xfe-name"
	if err := os.WriteFile(filepath.Join(volumeRoot, name), []byte("x"), 0o600); err != nil {
		t.Skipf("filesystem rejects a non-UTF-8 name: %v", err)
	}

	client := startFileServiceTestServer(t, WithFileService(server))
	resp, err := client.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes})
	if err != nil {
		t.Fatalf("List over a non-UTF-8 name: %v", err)
	}
	entries := resp.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].GetName(), "\uFFFD") {
		t.Fatalf("name %q is not scrubbed", entries[0].GetName())
	}
}

// TestCursorRoundTrip proves the cursor is reversible for names that are not
// valid UTF-8, so a boundary name survives the proto3 string round trip that
// scrubbing it would corrupt.
func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "plain", "a\xff", "a\xffz", "\xfd\xfe\xff", "mixed-\x80-\x81"} {
		token := encodeCursor(name)
		if name == "" && token != "" {
			t.Fatalf("encodeCursor(%q) = %q, want empty", name, token)
		}
		got, err := decodeCursor(token)
		if err != nil {
			t.Fatalf("decodeCursor(%q): %v", token, err)
		}
		if got != name {
			t.Fatalf("round trip %q -> %q -> %q", name, token, got)
		}
	}
}

// TestFileServerListMalformedCursor proves a cursor that is not the base64
// token the agent emitted is rejected, never silently treated as the first
// page (which would restart the listing).
func TestFileServerListMalformedCursor(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	_, err := server.List(context.Background(), &agentv1.ListRequest{Root: fileRootVolumes, Cursor: "not base64!!"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("List(malformed cursor) code = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := decodeCursor("!!!!"); err == nil {
		t.Fatal("decodeCursor accepted a malformed token")
	}
}

// TestFullAccessOpensWholeFilesystem pins the operator-approved mode: one
// writable "system" root at "/" with no dangerous-tree or protected-path
// rejection, and the env defaults (files on, full access on, "false" off).
func TestFullAccessOpensWholeFilesystem(t *testing.T) {
	cfg := Config{FileFullAccess: true}
	roots := configuredFileRoots(cfg)
	if len(roots) != 1 || roots[0].name != "system" || roots[0].path != "/" || !roots[0].writable || !roots[0].unrestricted {
		t.Fatalf("roots = %+v, want one writable unrestricted system root at /", roots)
	}
	server, err := NewFileServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileServer full access: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if len(server.order) != 1 || server.order[0] != "system" {
		t.Fatalf("opened roots = %v, want [system]", server.order)
	}

	t.Setenv(envFiles, "")
	t.Setenv(envFileFullAccess, "")
	if c, err := Load(); err != nil || !c.Files || !c.FileFullAccess {
		t.Fatalf("defaults: Files=%v FileFullAccess=%v, want both on", c.Files, c.FileFullAccess)
	}
	t.Setenv(envFiles, "false")
	t.Setenv(envFileFullAccess, "false")
	if c, err := Load(); err != nil || c.Files || c.FileFullAccess {
		t.Fatalf("explicit false: Files=%v FileFullAccess=%v, want both off", c.Files, c.FileFullAccess)
	}
}
