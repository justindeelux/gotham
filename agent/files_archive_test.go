package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
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

// zipTestEntry is one entry a test zip carries. method is passed to the zip
// writer verbatim: zip.Store (0, the zero value) writes a real uncompressed
// entry, so a test can exercise the stored-entry CRC/size path.
type zipTestEntry struct {
	name   string
	mode   os.FileMode
	body   []byte
	method uint16
}

// buildZip serialises entries into a zip archive. The method is written as
// given — an unset method (0) is a real Stored entry, never silently Deflated —
// so a Store-entry regression test actually exercises its guard.
func buildZip(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: e.method}
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
		got, err := archiveEntryPath(name, true)
		if err != nil {
			t.Errorf("archiveEntryPath(%q) = %v, want nil", name, err)
			continue
		}
		if got != want {
			t.Errorf("archiveEntryPath(%q) = %q, want %q", name, got, want)
		}
	}
	// Zip normalises a Windows backslash separator into "/".
	if got, err := archiveEntryPath(`a\b`, true); err != nil || got != "a/b" {
		t.Errorf("zip archiveEntryPath(`a\\b`) = %q, %v, want a/b", got, err)
	}
	// A tar keeps backslashes: they are ordinary filename characters on Linux.
	if got, err := archiveEntryPath(`a\b`, false); err != nil || got != `a\b` {
		t.Errorf("tar archiveEntryPath(`a\\b`) = %q, %v, want literal", got, err)
	}
	invalid := []string{"../a", "/abs", "a/../../b", "a\x00b", "a/../b"}
	for _, name := range invalid {
		if _, err := archiveEntryPath(name, true); err == nil {
			t.Errorf("zip archiveEntryPath(%q) = nil, want an error", name)
		}
	}
	// A backslash-escaped traversal is only a traversal for zip; for tar it is
	// a literal name, not an escape.
	if _, err := archiveEntryPath(`\..\a`, true); err == nil {
		t.Errorf("zip archiveEntryPath(`\\..\\a`) = nil, want an error")
	}
	if _, err := archiveEntryPath(`\..\a`, false); err != nil {
		t.Errorf("tar archiveEntryPath(`\\..\\a`) = %v, want a literal name", err)
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

// TestArchiveCounterRejectsInt64Overflow proves a declared size near MaxInt64
// cannot wrap the byte total past the cap (H1).
func TestArchiveCounterRejectsInt64Overflow(t *testing.T) {
	t.Parallel()
	counter := &archiveCounter{maxEntries: 100, maxBytes: 1 << 30}
	if err := counter.addEntry(1); err != nil {
		t.Fatalf("first entry: %v", err)
	}
	if err := counter.addEntry(math.MaxInt64); !errors.Is(err, fsroot.ErrTooLarge) {
		t.Fatalf("addEntry(MaxInt64) = %v, want ErrTooLarge", err)
	}
}

// TestFileServerExtractCountsEveryEntry proves directory entries and names that
// clean to nothing still count against the entry cap (H2).
func TestFileServerExtractCountsEveryEntry(t *testing.T) {
	t.Parallel()

	t.Run("tar-dir-bomb", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		server.archiveMaxEntries = 2
		writeArchiveFile(t, root, "dirs.tar", buildTar(t, []tarEntry{
			{name: "a/", typeflag: tar.TypeDir, mode: 0o755},
			{name: "b/", typeflag: tar.TypeDir, mode: 0o755},
			{name: "c/", typeflag: tar.TypeDir, mode: 0o755},
		}))
		if err := extract(t, server, fileRootVolumes, "dirs.tar", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("tar dir bomb = %v, want ResourceExhausted", err)
		}
	})

	t.Run("zip-dir-bomb", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		server.archiveMaxEntries = 2
		writeArchiveFile(t, root, "dirs.zip", buildZip(t, []zipTestEntry{
			{name: "a/", mode: os.ModeDir | 0o755},
			{name: "b/", mode: os.ModeDir | 0o755},
			{name: "c/", mode: os.ModeDir | 0o755},
		}))
		if err := extract(t, server, fileRootVolumes, "dirs.zip", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("zip dir bomb = %v, want ResourceExhausted", err)
		}
	})

	t.Run("zip-entry-cap-before-iteration", func(t *testing.T) {
		t.Parallel()
		server, root := newTestFileServer(t)
		server.archiveMaxEntries = 2
		writeArchiveFile(t, root, "many.zip", buildZip(t, []zipTestEntry{
			{name: "a", mode: 0o644, body: []byte("a")},
			{name: "b", mode: 0o644, body: []byte("b")},
			{name: "c", mode: 0o644, body: []byte("c")},
		}))
		if err := extract(t, server, fileRootVolumes, "many.zip", ".", false); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("zip over the entry cap = %v, want ResourceExhausted", err)
		}
	})
}

// corruptZipCRC flips a byte of the single entry's central-directory CRC32, so
// the checksum the zip reader verifies at end-of-entry is wrong.
func corruptZipCRC(t *testing.T, body []byte) []byte {
	t.Helper()
	idx := bytes.Index(body, []byte{'P', 'K', 1, 2})
	if idx < 0 {
		t.Fatal("no central directory header in the test zip")
	}
	body[idx+16] ^= 0xFF
	return body
}

// TestFileServerExtractRejectsCorruptZip keeps an existing file intact when a
// corrupt entry fails: the CRC is only checked at EOF, and the extract stages
// the body in a temp file rather than truncating the target in place (M1, H3).
func TestFileServerExtractRejectsCorruptZip(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	body := corruptZipCRC(t, buildZip(t, []zipTestEntry{
		{name: "target.txt", mode: 0o644, method: zip.Store, body: []byte("new")},
	}))
	writeArchiveFile(t, root, "bad.zip", body)
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extract(t, server, fileRootVolumes, "bad.zip", ".", true); err == nil {
		t.Fatal("extract of a corrupt zip succeeded, want a refusal")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "target.txt")); string(got) != "keep" {
		t.Fatalf("a failed overwrite destroyed the existing file: %q", got)
	}
}

// TestFileServerCompressRefusesSymlinkedSourceSelfInclusion proves a destination
// routed through a symlinked parent into a source is refused, so the archive can
// never enumerate its own temp file (OC H1).
func TestFileServerCompressRefusesSymlinkedSourceSelfInclusion(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := os.MkdirAll(filepath.Join(root, "a", "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := compress(t, server, fileRootVolumes, "b/c/out.zip", []string{"a"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("compress through a symlinked parent into its source = %v, want InvalidArgument", err)
	}
}

// TestFileServerExtractAppliesReadOnlyDirMode proves a read-only directory entry
// still receives its children and ends with owner rwx forced (M6/OC N2): the
// archived 0555 keeps its group/other bits but gains owner write, so the tree
// stays usable.
func TestFileServerExtractAppliesReadOnlyDirMode(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "ro.tar", buildTar(t, []tarEntry{
		{name: "ro/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "ro/f.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("hi")},
	}))
	if err := extract(t, server, fileRootVolumes, "ro.tar", ".", false); err != nil {
		t.Fatalf("extract under a read-only dir mode: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "ro", "f.txt")); string(got) != "hi" {
		t.Fatalf("the child under a 0555 dir was not written: %q", got)
	}
	info, err := os.Stat(filepath.Join(root, "ro"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("dir mode = %o, want 755 (owner rwx forced)", info.Mode().Perm())
	}
}

// TestFileServerExtractNormalisesNonExecutableDirMode proves a directory entry
// whose archived mode lacks owner execute (0644) still yields a traversable tree
// with its children readable — not a false success over an unusable directory
// (OC N2).
func TestFileServerExtractNormalisesNonExecutableDirMode(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "noexec.tar", buildTar(t, []tarEntry{
		{name: "dm/", typeflag: tar.TypeDir, mode: 0o644},
		{name: "dm/f.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("hi")},
	}))
	if err := extract(t, server, fileRootVolumes, "noexec.tar", ".", false); err != nil {
		t.Fatalf("extract under a non-executable dir mode: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "dm"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("dir mode = %o, want owner rwx", info.Mode().Perm())
	}
	// The tree is traversable and the child is reachable.
	if got, err := os.ReadFile(filepath.Join(root, "dm", "f.txt")); err != nil || string(got) != "hi" {
		t.Fatalf("child under a 0644 dir = %q, %v", got, err)
	}
}

// TestFileServerExtractPaxGlobalHeader proves a pax global header (as git
// archive and GitHub tarballs emit) is skipped, not refused (M7).
func TestFileServerExtractPaxGlobalHeader(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "pax.tar", buildTar(t, []tarEntry{
		{name: "pax_global_header", typeflag: tar.TypeXGlobalHeader},
		{name: "f.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("hi")},
	}))
	if err := extract(t, server, fileRootVolumes, "pax.tar", ".", false); err != nil {
		t.Fatalf("extract with a pax global header: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "f.txt")); string(got) != "hi" {
		t.Fatalf("the file after the pax header was not extracted: %q", got)
	}
}

// TestFileServerExtractTarKeepsBackslash proves a tar member whose name holds a
// backslash keeps it (only zip rewrites the separator) (M8).
func TestFileServerExtractTarKeepsBackslash(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "bs.tar", buildTar(t, []tarEntry{
		{name: `a\b`, typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")},
	}))
	if err := extract(t, server, fileRootVolumes, "bs.tar", ".", false); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, `a\b`)); err != nil {
		t.Fatalf("the backslashed name was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b")); !os.IsNotExist(err) {
		t.Fatalf("the backslash was rewritten to a separator: %v", err)
	}
}

// TestFileServerCompressRejectsDuplicateSources proves duplicate, nested and
// same-base-name sources are refused (L1).
func TestFileServerCompressRejectsDuplicateSources(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	cases := map[string][]string{
		"duplicate": {"a", "a"},
		"nested":    {"a", "a/b"},
		"same-base": {"x/f", "y/f"},
	}
	for name, sources := range cases {
		if _, err := compress(t, server, fileRootVolumes, "out.zip", sources, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s sources = %v, want InvalidArgument", name, err)
		}
	}
}

// TestFileServerCompressOutputMode proves a fresh archive is readable (0644) and
// a directory destination is refused up front (L2).
func TestFileServerCompressOutputMode(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if _, err := write(t, server, fileRootVolumes, "f.txt", "", false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := compress(t, server, fileRootVolumes, "out.zip", []string{"f.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, false); err != nil {
		t.Fatalf("compress: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "out.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("archive mode = %o, want 644", info.Mode().Perm())
	}
	if err := os.MkdirAll(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := compress(t, server, fileRootVolumes, "adir", []string{"f.txt"}, agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, true); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("compress onto a directory = %v, want InvalidArgument", err)
	}
}

// TestFileServerExtractRefusesSelfOverwrite proves an entry named like the
// archive being read is refused (L3).
func TestFileServerExtractRefusesSelfOverwrite(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	writeArchiveFile(t, root, "self.tar", buildTar(t, []tarEntry{
		{name: "self.tar", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")},
	}))
	if err := extract(t, server, fileRootVolumes, "self.tar", ".", true); err == nil {
		t.Fatal("extract overwrote the archive being read, want a refusal")
	}
}

// TestFileServerExtractRefusesSymlinkedAncestor proves an entry whose parent
// chain holds a pre-existing in-root symlink is refused, so a write never lands
// outside the requested destination (M5).
func TestFileServerExtractRefusesSymlinkedAncestor(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	if err := os.MkdirAll(filepath.Join(root, "d", "real", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "d", "s")); err != nil {
		t.Fatal(err)
	}
	writeArchiveFile(t, root, "e.tar", buildTar(t, []tarEntry{
		{name: "s/x/f.txt", typeflag: tar.TypeReg, mode: 0o644, body: []byte("pwn")},
	}))
	if err := extract(t, server, fileRootVolumes, "e.tar", "d", false); err == nil {
		t.Fatal("extract through a symlinked ancestor succeeded, want a refusal")
	}
	if _, err := os.Stat(filepath.Join(root, "d", "real", "x", "f.txt")); !os.IsNotExist(err) {
		t.Fatalf("extract followed the symlinked ancestor: %v", err)
	}
}

// TestFileServerExtractRefusesDeepName proves an entry with too many path
// segments is refused before any directory walk (M4).
func TestFileServerExtractRefusesDeepName(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	name := strings.Repeat("a/", fileArchiveMaxNameDepth+50) + "f"
	writeArchiveFile(t, root, "deep.tar", buildTar(t, []tarEntry{
		{name: name, typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")},
	}))
	if err := extract(t, server, fileRootVolumes, "deep.tar", ".", false); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("extract of a deep name = %v, want ResourceExhausted", err)
	}
}

// appendZip64EntryTail rewrites a small zip's tail so its zip64 end-of-central-
// directory record declares `entries` members even though the file holds one. It
// proves the pre-parse EOCD check refuses an archive that declares millions of
// entries without zip.NewReader allocating any header (OC M1/OC N1).
func appendZip64EntryTail(t *testing.T, body []byte, entries uint64) []byte {
	t.Helper()
	const eocdLen = 22
	if len(body) < eocdLen {
		t.Fatal("zip body too short")
	}
	base := body[:len(body)-eocdLen]
	recordOffset := uint64(len(base))
	var buf bytes.Buffer
	buf.Write(base)

	// zip64 end of central directory record.
	record := make([]byte, 56)
	binary.LittleEndian.PutUint32(record[0:4], 0x06064b50)
	binary.LittleEndian.PutUint64(record[4:12], 44) // bytes of the record after this field
	binary.LittleEndian.PutUint64(record[24:32], entries)
	binary.LittleEndian.PutUint64(record[32:40], entries)
	buf.Write(record)

	// zip64 end of central directory locator, pointing at the record.
	locator := make([]byte, 20)
	binary.LittleEndian.PutUint32(locator[0:4], 0x07064b50)
	binary.LittleEndian.PutUint64(locator[8:16], recordOffset)
	binary.LittleEndian.PutUint32(locator[16:20], 1)
	buf.Write(locator)

	// EOCD whose 16/32-bit fields are all "see zip64", forcing the zip64 read.
	eocd := make([]byte, eocdLen)
	binary.LittleEndian.PutUint32(eocd[0:4], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:10], 0xFFFF)
	binary.LittleEndian.PutUint16(eocd[10:12], 0xFFFF)
	binary.LittleEndian.PutUint32(eocd[12:16], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(eocd[16:20], 0xFFFFFFFF)
	buf.Write(eocd)
	return buf.Bytes()
}

// TestZipCentralDirectoryBounds proves the tail parser reads a normal zip's
// declared entry count and central-directory size, and is not fooled by a
// comment that embeds the EOCD signature.
func TestZipCentralDirectoryBounds(t *testing.T) {
	t.Parallel()
	plain := buildZip(t, []zipTestEntry{{name: "f", mode: 0o644, body: []byte("x")}})
	entries, dirSize, err := zipCentralDirectoryBounds(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatalf("bounds: %v", err)
	}
	if entries != 1 || dirSize <= 0 {
		t.Fatalf("bounds = %d entries, %d bytes; want 1, >0", entries, dirSize)
	}
	// A comment containing the EOCD signature must not shadow the real record.
	comment := []byte("PK\x05\x06 not the record")
	commented := append([]byte{}, plain...)
	binary.LittleEndian.PutUint16(commented[len(commented)-2:], uint16(len(comment)))
	commented = append(commented, comment...)
	entries, _, err = zipCentralDirectoryBounds(bytes.NewReader(commented), int64(len(commented)))
	if err != nil || entries != 1 {
		t.Fatalf("commented bounds = %d, %v; want 1, nil", entries, err)
	}
}

// TestFileServerExtractRejectsZipDeclaredEntryCount proves the declared entry
// count is refused from the EOCD before zip.NewReader materialises anything
// (OC M1/OC N1).
func TestFileServerExtractRejectsZipDeclaredEntryCount(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	server.archiveMaxEntries = 2
	body := appendZip64EntryTail(t,
		buildZip(t, []zipTestEntry{{name: "f", mode: 0o644, body: []byte("x")}}), 5_000_000)
	writeArchiveFile(t, root, "huge.zip", body)
	if err := extract(t, server, fileRootVolumes, "huge.zip", ".", false); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("extract of a zip declaring 5M entries = %v, want ResourceExhausted", err)
	}
	if _, err := os.Stat(filepath.Join(root, "f")); !os.IsNotExist(err) {
		t.Fatalf("an entry was written despite the refusal: %v", err)
	}
}

// TestFileServerExtractRejectsOversizedInputArchive proves the compressed
// archive's on-disk size is capped for every format before any parser runs
// (OC M1).
func TestFileServerExtractRejectsOversizedInputArchive(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("x"), 4096)
	cases := map[string][]byte{
		"a.tar":    buildTar(t, []tarEntry{{name: "f", typeflag: tar.TypeReg, mode: 0o644, body: body}}),
		"a.zip":    buildZip(t, []zipTestEntry{{name: "f", mode: 0o644, body: body}}),
		"a.tar.gz": buildTarGz(t, []tarEntry{{name: "f", typeflag: tar.TypeReg, mode: 0o644, body: body}}),
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, root := newTestFileServer(t)
			server.archiveMaxInputBytes = 8
			writeArchiveFile(t, root, name, archive)
			if err := extract(t, server, fileRootVolumes, name, ".", false); status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("extract of an over-cap archive = %v, want ResourceExhausted", err)
			}
		})
	}
}

// TestFileServerExtractRejectsTruncatedBody proves a truncated tar/gzip/zip body
// is an invalid archive (InvalidArgument), not an internal error (Claude N3).
func TestFileServerExtractRejectsTruncatedBody(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("x"), 2048)
	cases := map[string][]byte{
		"a.tar":    buildTar(t, []tarEntry{{name: "f", typeflag: tar.TypeReg, mode: 0o644, body: body}}),
		"a.tar.gz": buildTarGz(t, []tarEntry{{name: "f", typeflag: tar.TypeReg, mode: 0o644, body: body}}),
		"a.zip":    buildZip(t, []zipTestEntry{{name: "f", mode: 0o644, body: body}}),
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, root := newTestFileServer(t)
			writeArchiveFile(t, root, name, archive[:len(archive)/2])
			err := extract(t, server, fileRootVolumes, name, ".", false)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("extract of a truncated %s = %v, want InvalidArgument", name, err)
			}
		})
	}
}

// TestExtractStateCachesAncestors proves the per-extract ancestor cache turns
// repeated deep paths into O(depth) work rather than re-walking the chain for
// every entry (Claude N1).
func TestExtractStateCachesAncestors(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	root := server.roots[fileRootVolumes]
	deep := strings.Repeat("d/", fileArchiveMaxNameDepth-1)
	if err := root.MkdirAll(strings.TrimSuffix(deep, "/"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := newExtractState(root)
	for i := 0; i < 2000; i++ {
		if err := state.verifyAncestors(".", deep+"f"+strconv.Itoa(i)); err != nil {
			t.Fatalf("verifyAncestors: %v", err)
		}
	}
	if state.statCalls > fileArchiveMaxNameDepth*2 {
		t.Fatalf("ancestor Stat calls = %d, want ~%d (cache not used?)", state.statCalls, fileArchiveMaxNameDepth)
	}
}

// TestFileServerExtractDeepTreeIsFast proves extracting thousands of entries
// that share one deep directory is linear: without the ancestor cache each
// entry re-walks the whole chain, which is O(entries × depth²) and takes tens of
// seconds (Claude N1). Directory entries are used so the test measures the
// ancestor walk, not per-file fsync cost.
func TestFileServerExtractDeepTreeIsFast(t *testing.T) {
	t.Parallel()
	server, root := newTestFileServer(t)
	const entriesCount = 1000
	prefix := strings.Repeat("d/", fileArchiveMaxNameDepth-2)
	entries := make([]tarEntry, 0, entriesCount)
	for i := 0; i < entriesCount; i++ {
		entries = append(entries, tarEntry{
			name: prefix + "x" + strconv.Itoa(i) + "/", typeflag: tar.TypeDir, mode: 0o755,
		})
	}
	writeArchiveFile(t, root, "deep.tar", buildTar(t, entries))
	start := time.Now()
	if err := extract(t, server, fileRootVolumes, "deep.tar", ".", false); err != nil {
		t.Fatalf("extract deep tree: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("extract of %d entries at depth %d took %s", entriesCount, fileArchiveMaxNameDepth, elapsed)
	}
}
