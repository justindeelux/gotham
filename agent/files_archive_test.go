package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// tarEntry is one entry a test tar carries.
type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     []byte
	linkname string
}

// buildTar serialises entries into an uncompressed tar.
func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Size: int64(len(e.body)), Linkname: e.linkname}
		if e.typeflag == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildTarGz serialises entries into a gzip-compressed tar.
func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Size: int64(len(e.body)), Linkname: e.linkname}
		if e.typeflag == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zipTestEntry is one entry a test zip carries.
type zipTestEntry struct {
	name   string
	mode   os.FileMode
	body   []byte
	method uint16
}

// buildZip serialises entries into a zip archive.
func buildZip(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		method := e.method
		if method == 0 {
			method = zip.Deflate
		}
		hdr := &zip.FileHeader{Name: e.name, Method: method}
		hdr.SetMode(e.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeArchiveFile places archive bytes into root under name.
func writeArchiveFile(t *testing.T, root, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// compress runs the Compress RPC and returns the collected progress.
func compress(t *testing.T, server *FileServer, root, dst string, sources []string, format agentv1.ArchiveFormat, overwrite bool) (*collectProgressStream, error) {
	t.Helper()
	stream := &collectProgressStream{}
	err := server.Compress(&agentv1.CompressRequest{
		Root: root, Sources: sources, Destination: dst, Format: format, Overwrite: overwrite,
	}, stream)
	return stream, err
}

// extract runs the Extract RPC and returns its error.
func extract(t *testing.T, server *FileServer, root, src, dst string, overwrite bool) error {
	t.Helper()
	stream := &collectProgressStream{}
	return server.Extract(&agentv1.ExtractRequest{
		Root: root, Source: src, Destination: dst, Overwrite: overwrite,
	}, stream)
}

// TestFileServerArchiveRoundTrip proves each format round-trips a directory
// tree byte for byte and that a symlink inside the tree is skipped and counted.
func TestFileServerArchiveRoundTrip(t *testing.T) {
	t.Parallel()
	formats := map[string]agentv1.ArchiveFormat{
		"a.zip":    agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		"a.tar":    agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		"a.tar.gz": agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ,
	}
	for name, format := range formats {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, root := newTestFileServer(t)
			if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "data"}); err != nil {
				t.Fatal(err)
			}
			if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "data/sub"}); err != nil {
				t.Fatal(err)
			}
			if _, err := write(t, server, fileRootVolumes, "data/top.txt", "", false, []byte("top")); err != nil {
				t.Fatal(err)
			}
			if _, err := write(t, server, fileRootVolumes, "data/sub/deep.bin", "", false, bytes.Repeat([]byte("z"), 3000)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("top.txt", filepath.Join(root, "data", "link")); err != nil {
				t.Fatal(err)
			}

			stream, err := compress(t, server, fileRootVolumes, name, []string{"data"}, format, false)
			if err != nil {
				t.Fatalf("Compress: %v", err)
			}
			if !stream.done() {
				t.Fatal("Compress did not end with a done message")
			}
			if got := stream.sent[len(stream.sent)-1].GetSkipped(); got != 1 {
				t.Fatalf("skipped = %d, want 1 (the symlink)", got)
			}
			if _, err := os.Stat(filepath.Join(root, name)); err != nil {
				t.Fatalf("archive not written: %v", err)
			}

			if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "restored"}); err != nil {
				t.Fatal(err)
			}
			if err := extract(t, server, fileRootVolumes, name, "restored", false); err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(root, "restored", "data", "top.txt")); string(got) != "top" {
				t.Fatalf("restored top.txt = %q", got)
			}
			if got, _ := os.ReadFile(filepath.Join(root, "restored", "data", "sub", "deep.bin")); len(got) != 3000 {
				t.Fatalf("restored deep.bin = %d bytes, want 3000", len(got))
			}
			if _, err := os.Lstat(filepath.Join(root, "restored", "data", "link")); !os.IsNotExist(err) {
				t.Fatalf("the symlink was restored: %v", err)
			}
		})
	}
}

// TestFileServerCompressRefusesOverwriteAndDestinationInsideSource pins the
// conflict policy and the recursion guard.
func TestFileServerCompressRefusesOverwriteAndDestinationInsideSource(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := compress(t, server, fileRootVolumes, "f.txt.zip", []string{"f.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if _, err := compress(t, server, fileRootVolumes, "f.txt.zip", []string{"f.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("compress over an existing archive = %v, want AlreadyExists", err)
	}
	if _, err := compress(t, server, fileRootVolumes, "f.txt.zip", []string{"f.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, true); err != nil {
		t.Fatalf("compress with overwrite: %v", err)
	}
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "dir"}); err != nil {
		t.Fatal(err)
	}
	if _, err := compress(t, server, fileRootVolumes, "dir/inner.zip", []string{"dir"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("compress into its own source = %v, want InvalidArgument", err)
	}
	if _, err := compress(t, server, fileRootVolumes, "out.zip", nil, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("compress with no sources = %v, want InvalidArgument", err)
	}
}

// TestFileServerExtractZipSlip proves an absolute name, a ".." segment and a
// name that escapes through cleaning are all refused and write nothing outside
// the destination.
func TestFileServerExtractZipSlip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body []byte
	}{
		{"../evil.txt", []byte("pwn")},
		{"/etc/pwn.txt", []byte("pwn")},
		{"a/../../evil.txt", []byte("pwn")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, root := newTestFileServer(t)
			writeArchiveFile(t, root, "evil.tar", buildTar(t, []tarEntry{
				{name: tc.name, typeflag: tar.TypeReg, mode: 0o644, body: tc.body},
			}))
			if err := extract(t, server, fileRootVolumes, "evil.tar", ".", false); err == nil {
				t.Fatalf("extract of %q succeeded, want a refusal", tc.name)
			}
			// Nothing landed at the root level beyond the archive itself.
			for _, leaked := range []string{"evil.txt", "pwn.txt"} {
				if _, err := os.Lstat(filepath.Join(root, leaked)); !os.IsNotExist(err) {
					t.Fatalf("extract leaked %q: %v", leaked, err)
				}
			}
		})
	}
}

// TestFileServerExtractRefusesLinkAndSpecialEntries pins that symlink, hardlink,
// device and FIFO entries are refused outright.
func TestFileServerExtractRefusesLinkAndSpecialEntries(t *testing.T) {
	t.Parallel()
	cases := map[string]tarEntry{
		"symlink":  {name: "link", typeflag: tar.TypeSymlink, linkname: "target"},
		"hardlink": {name: "hard", typeflag: tar.TypeLink, linkname: "target"},
		"char":     {name: "dev", typeflag: tar.TypeChar},
		"fifo":     {name: "pipe", typeflag: tar.TypeFifo},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, root := newTestFileServer(t)
			writeArchiveFile(t, root, "bad.tar", buildTar(t, []tarEntry{entry}))
			err := extract(t, server, fileRootVolumes, "bad.tar", ".", false)
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("extract of a %s entry = %v, want FailedPrecondition", name, err)
			}
		})
	}

	// A zip member whose mode marks it a symlink is refused too.
	t.Run("zip-symlink", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		writeArchiveFile(t, root, "bad.zip", buildZip(t, []zipTestEntry{
			{name: "link", mode: os.ModeSymlink | 0o777, body: []byte("target")},
		}))
		if err := extract(t, server, fileRootVolumes, "bad.zip", ".", false); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("extract of a zip symlink = %v, want FailedPrecondition", err)
		}
	})
}

// TestFileServerExtractSymlinkedParentEscape proves a name whose parent is a
// symlink out of the root is refused and writes nothing outside.
func TestFileServerExtractSymlinkedParentEscape(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	writeArchiveFile(t, root, "escape.tar", buildTar(t, []tarEntry{
		{name: "link/pwn.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("pwn")},
	}))
	if err := extract(t, server, fileRootVolumes, "escape.tar", ".", false); err == nil {
		t.Fatal("extract through a symlinked parent succeeded, want a refusal")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn.txt")); !os.IsNotExist(err) {
		t.Fatalf("extract escaped the root: %v", err)
	}
}

// TestFileServerExtractCaps proves the entry-count, size and compression-ratio
// caps all refuse an over-limit archive.
func TestFileServerExtractCaps(t *testing.T) {
	t.Parallel()

	t.Run("entries", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		server.archiveMaxEntries = 2
		writeArchiveFile(t, root, "many.tar", buildTar(t, []tarEntry{
			{name: "a", typeflag: tar.TypeReg, mode: 0o644, body: []byte("a")},
			{name: "b", typeflag: tar.TypeReg, mode: 0o644, body: []byte("b")},
			{name: "c", typeflag: tar.TypeReg, mode: 0o644, body: []byte("c")},
		}))
		if err := extract(t, server, fileRootVolumes, "many.tar", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("extract over the entry cap = %v, want ResourceExhausted", err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		server.archiveMaxBytes = 10
		writeArchiveFile(t, root, "big.tar", buildTar(t, []tarEntry{
			{name: "big", typeflag: tar.TypeReg, mode: 0o644, body: bytes.Repeat([]byte("x"), 100)},
		}))
		if err := extract(t, server, fileRootVolumes, "big.tar", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("extract over the byte cap = %v, want ResourceExhausted", err)
		}
	})

	t.Run("ratio", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		// A 1 MiB run of zeroes gzips to a few hundred bytes: a ratio far above
		// the 200:1 cap, well under the 4 GiB byte cap.
		writeArchiveFile(t, root, "bomb.tar.gz", buildTarGz(t, []tarEntry{
			{name: "bomb", typeflag: tar.TypeReg, mode: 0o644, body: bytes.Repeat([]byte{0}, 1<<20)},
		}))
		if err := extract(t, server, fileRootVolumes, "bomb.tar.gz", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("extract of a zip bomb = %v, want ResourceExhausted", err)
		}
	})
}

// TestFileServerExtractStripsSpecialModeBits proves setuid, setgid and sticky
// bits are stripped while the plain permission bits survive.
func TestFileServerExtractStripsSpecialModeBits(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "modes.tar", buildTar(t, []tarEntry{
		{name: "suid", typeflag: tar.TypeReg, mode: 0o4755, body: []byte("x")},
	}))
	if err := extract(t, server, fileRootVolumes, "modes.tar", ".", false); err != nil {
		t.Fatalf("extract: %v", err)
	}
	info, err := os.Lstat(filepath.Join(root, "suid"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("special mode bits survived: %v", info.Mode())
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
}

// TestFileServerExtractConflictPolicy proves an existing file is refused
// without overwrite and replaced with it, and that a directory is reused.
func TestFileServerExtractConflictPolicy(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "one.tar", buildTar(t, []tarEntry{
		{name: "dir/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "dir/f.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("new")},
	}))
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "f.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extract(t, server, fileRootVolumes, "one.tar", ".", false); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("extract over an existing file = %v, want AlreadyExists", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dir", "f.txt")); string(got) != "old" {
		t.Fatalf("refused extract changed the file: %q", got)
	}
	if err := extract(t, server, fileRootVolumes, "one.tar", ".", true); err != nil {
		t.Fatalf("extract with overwrite: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dir", "f.txt")); string(got) != "new" {
		t.Fatalf("overwrite extract did not replace the file: %q", got)
	}
}

// TestFileServerExtractProtectsAppDir proves an entry that would replace a
// top-level application directory is refused.
func TestFileServerExtractProtectsAppDir(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	const appID = "0f8fad5b-d9cb-469f-a165-70867728950e"
	if err := os.MkdirAll(filepath.Join(root, appID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, appID, "keep"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeArchiveFile(t, root, "clobber.tar", buildTar(t, []tarEntry{
		{name: appID + "/", typeflag: tar.TypeDir, mode: 0o755},
		{name: appID + "/keep", typeflag: tar.TypeReg, mode: 0o644, body: []byte("pwn")},
	}))
	if err := extract(t, server, fileRootVolumes, "clobber.tar", ".", true); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("extract over an app dir = %v, want PermissionDenied", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, appID, "keep")); string(got) != "keep" {
		t.Fatalf("the app dir was modified: %q", got)
	}
}

// TestFileServerArchiveRefusesReadOnlyRoot proves both archive operations are
// refused on a read-only root.
func TestFileServerArchiveRefusesReadOnlyRoot(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	if _, err := compress(t, server, fileRootCompose, "x.zip", []string{"a"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Compress on a read-only root = %v, want PermissionDenied", err)
	}
	if err := extract(t, server, fileRootCompose, "x.zip", ".", false); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Extract on a read-only root = %v, want PermissionDenied", err)
	}
}

// TestFileServerArchiveUnknownFormat proves an extract refuses a source whose
// extension selects no known format.
func TestFileServerArchiveUnknownFormat(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "blob.bin", []byte("not an archive"))
	if err := extract(t, server, fileRootVolumes, "blob.bin", ".", false); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("extract of an unknown extension = %v, want InvalidArgument", err)
	}
	if _, err := compress(t, server, fileRootVolumes, "x.zip", []string{"a"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, false); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("compress without a format = %v, want InvalidArgument", err)
	}
}

// TestFullAccessArchiveRoundTrip proves the archives work through the
// whole-filesystem "system" root as well as the confined roots.
func TestFullAccessArchiveRoundTrip(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := NewFileServer(Config{FileFullAccess: true, ManagedVolumeRoot: filepath.Join(base, "volumes")}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	real, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimPrefix(filepath.ToSlash(real), "/")
	if _, err := compress(t, server, fileRootSystem, rel+"/out.zip", []string{rel + "/hello.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); err != nil {
		t.Fatalf("Compress through /: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extract(t, server, fileRootSystem, rel+"/out.zip", rel+"/out", false); err != nil {
		t.Fatalf("Extract through /: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(base, "out", "hello.txt")); string(got) != "hello" {
		t.Fatalf("restored file = %q", got)
	}
}

// TestFileServerArchiveHonoursCancelledContext proves a cancelled extract stops
// rather than running to completion.
func TestFileServerArchiveHonoursCancelledContext(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "a.tar", buildTar(t, []tarEntry{
		{name: "f", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &collectProgressStream{ctx: ctx}
	if err := server.Extract(&agentv1.ExtractRequest{Root: fileRootVolumes, Source: "a.tar", Destination: "."}, stream); status.Code(err) != codes.Canceled {
		t.Fatalf("Extract with a cancelled context = %v, want Canceled", err)
	}
}

// TestArchiveEntryPath pins the name validation directly.
func TestArchiveEntryPath(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"a.txt":       "a.txt",
		"dir/a.txt":   "dir/a.txt",
		"./dir/a.txt": "dir/a.txt",
		"dir/sub/":    "dir/sub",
		".":           "",
		"./":          "",
	}
	for name, want := range valid {
		got, err := archiveEntryPath(name)
		if err != nil {
			t.Errorf("archiveEntryPath(%q) = %v, want nil", name, err)
			continue
		}
		if got != want {
			t.Errorf("archiveEntryPath(%q) = %q, want %q", name, got, want)
		}
	}
	invalid := []string{"../a", "/abs", "a/../../b", "a\x00b", "\\..\\a", "a/../b"}
	for _, name := range invalid {
		if _, err := archiveEntryPath(name); err == nil {
			t.Errorf("archiveEntryPath(%q) = nil, want an error", name)
		}
	}
}

// TestFileServerCompressSkipsSpecialFiles proves a FIFO in the tree is skipped
// and reported, not stored.
func TestFileServerCompressSkipsSpecialFiles(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("the fifo test needs a non-root uid")
	}
	server, root := newTestFileServer(t)
	if _, err := server.Mkdir(context.Background(), &agentv1.MkdirRequest{Root: fileRootVolumes, Path: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d", "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "d", "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	stream, err := compress(t, server, fileRootVolumes, "d.zip", []string{"d"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if got := stream.sent[len(stream.sent)-1].GetSkipped(); got != 1 {
		t.Fatalf("skipped = %d, want 1 (the fifo)", got)
	}
	// The archive holds only the real file.
	zr, err := zip.OpenReader(filepath.Join(root, "d.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if strings.Contains(f.Name, "pipe") {
			t.Fatalf("the fifo was stored as %q", f.Name)
		}
	}
	var found bool
	for _, f := range zr.File {
		if f.Name == "d/real.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("the real file is missing from the archive")
	}
}
