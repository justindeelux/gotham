package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// writeSizedFile creates a file of exactly size bytes under dir.
func writeSizedFile(t *testing.T, dir, name string, size int) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestDirectorySizeSumsTreeAndSkipsLinks(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	sub := filepath.Join(rootDir, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSizedFile(t, rootDir, "a.txt", 100)
	writeSizedFile(t, filepath.Join(rootDir, "sub"), "b.txt", 200)
	writeSizedFile(t, sub, "c.txt", 300)

	// A symlink to a file outside the root must be skipped, never followed.
	outside := t.TempDir()
	writeSizedFile(t, outside, "secret.txt", 1<<20)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(rootDir, "link")); err != nil {
		t.Fatal(err)
	}
	// A symlink to a directory inside the root is also not descended.
	if err := os.Symlink(filepath.Join(rootDir, "sub"), filepath.Join(rootDir, "sublink")); err != nil {
		t.Fatal(err)
	}

	resp, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{
		Root: "volumes",
		Path: "",
	})
	if err != nil {
		t.Fatalf("DirectorySize: %v", err)
	}
	if resp.GetBytes() != 600 {
		t.Errorf("bytes = %d, want 600", resp.GetBytes())
	}
	// a.txt, sub, sub/b.txt, sub/deep, sub/deep/c.txt, link, sublink = 7 names.
	if resp.GetEntries() != 7 {
		t.Errorf("entries = %d, want 7", resp.GetEntries())
	}
	if resp.GetSkipped() != 2 {
		t.Errorf("skipped = %d, want 2", resp.GetSkipped())
	}
	if resp.GetTruncated() {
		t.Error("truncated = true, want false")
	}
}

func TestDirectorySizeSizesOneSubfolder(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	target := filepath.Join(rootDir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSizedFile(t, target, "x", 10)
	writeSizedFile(t, target, "y", 20)
	writeSizedFile(t, rootDir, "outside", 9999)

	resp, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{
		Root: "volumes",
		Path: "target",
	})
	if err != nil {
		t.Fatalf("DirectorySize: %v", err)
	}
	if resp.GetBytes() != 30 {
		t.Errorf("bytes = %d, want 30", resp.GetBytes())
	}
}

func TestDirectorySizeCapsEntries(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	for i := 0; i < 6; i++ {
		writeSizedFile(t, rootDir, string(rune('a'+i))+".txt", 10)
	}
	old := fileSizeMaxEntries
	fileSizeMaxEntries = 3
	t.Cleanup(func() { fileSizeMaxEntries = old })

	resp, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{
		Root: "volumes",
	})
	if err != nil {
		t.Fatalf("DirectorySize: %v", err)
	}
	if !resp.GetTruncated() {
		t.Error("truncated = false, want a partial result")
	}
	if resp.GetEntries() != 3 {
		t.Errorf("entries = %d, want 3", resp.GetEntries())
	}
	if resp.GetBytes() != 30 {
		t.Errorf("bytes = %d, want 30 (three sized files)", resp.GetBytes())
	}
}

func TestDirectorySizeRejectsNonDirectory(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	writeSizedFile(t, rootDir, "file.txt", 5)

	_, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{
		Root: "volumes",
		Path: "file.txt",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
	}
}

func TestDirectorySizeCanceled(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	writeSizedFile(t, rootDir, "a.txt", 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := server.DirectorySize(ctx, &agentv1.DirectorySizeRequest{Root: "volumes"})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("code = %v, want Canceled (%v)", status.Code(err), err)
	}
}

func TestDirectorySizePermissionDenied(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	server, rootDir := newTestFileServer(t)
	locked := filepath.Join(rootDir, "locked")
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{Root: "volumes"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied (%v)", status.Code(err), err)
	}
}

func TestDirectorySizeHidesTrash(t *testing.T) {
	t.Parallel()
	server, rootDir := newTestFileServer(t)
	writeSizedFile(t, rootDir, "keep.txt", 7)
	trash := filepath.Join(rootDir, fileTrashDir, "deadbeef")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSizedFile(t, trash, "data", 4096)

	resp, err := server.DirectorySize(context.Background(), &agentv1.DirectorySizeRequest{Root: "volumes"})
	if err != nil {
		t.Fatalf("DirectorySize: %v", err)
	}
	if resp.GetBytes() != 7 {
		t.Errorf("bytes = %d, want 7 (the bin is excluded)", resp.GetBytes())
	}
}
