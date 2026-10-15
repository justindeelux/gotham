package agent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// writeSearchFixture lays out a small tree under the volumes root.
func writeSearchFixture(t *testing.T, volumeRoot string) {
	t.Helper()
	files := map[string]string{
		"app/config.yaml":     "port: 1",
		"app/notes.txt":       "the word config appears in the body only",
		"app/sub/config.json": "{}",
		"other/config.txt":    "x",
		"other/readme.md":     "y",
	}
	for name, body := range files {
		full := filepath.Join(volumeRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// searchPaths returns the sorted root-relative paths of a result set.
func searchPaths(resp *agentv1.SearchResponse) []string {
	out := make([]string, 0, len(resp.GetResults()))
	for _, hit := range resp.GetResults() {
		out = append(out, hit.GetPath())
	}
	sort.Strings(out)
	return out
}

func TestFileServerSearchNameOnlyNonRecursive(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	writeSearchFixture(t, volumeRoot)

	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:  fileRootVolumes,
		Path:  "app",
		Query: "config",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Only the name matches; notes.txt holds "config" in its body but must not
	// be returned, and the subdirectory is not descended without recursive.
	if got := searchPaths(resp); len(got) != 1 || got[0] != "app/config.yaml" {
		t.Fatalf("results = %v, want [app/config.yaml]", got)
	}
	if resp.GetTruncated() {
		t.Fatal("truncated = true, want false")
	}
}

func TestFileServerSearchRecursive(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	writeSearchFixture(t, volumeRoot)

	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Query:     "CONFIG", // case-insensitive
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []string{"app/config.yaml", "app/sub/config.json", "other/config.txt"}
	if got := searchPaths(resp); len(got) != len(want) {
		t.Fatalf("results = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("results = %v, want %v", got, want)
			}
		}
	}
}

// TestFileServerSearchResultCapIsTruncated proves the result cap is an explicit
// truncated flag, never a short list presented as complete.
func TestFileServerSearchResultCapIsTruncated(t *testing.T) {
	old := searchMaxResults
	searchMaxResults = 2
	t.Cleanup(func() { searchMaxResults = old })

	server, volumeRoot := newTestFileServer(t)
	for i := range 6 {
		name := filepath.Join(volumeRoot, "match-"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:  fileRootVolumes,
		Query: "match",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.GetResults()) != 2 {
		t.Fatalf("results = %d, want 2", len(resp.GetResults()))
	}
	if !resp.GetTruncated() {
		t.Fatal("truncated = false, want true at the result cap")
	}
}

// TestFileServerSearchScanCapIsTruncated proves the scan cap bounds the work a
// wide tree can force.
func TestFileServerSearchScanCapIsTruncated(t *testing.T) {
	old := searchScanCap
	searchScanCap = 3
	t.Cleanup(func() { searchScanCap = old })

	server, volumeRoot := newTestFileServer(t)
	for i := range 20 {
		name := filepath.Join(volumeRoot, "f"+string(rune('a'+i)))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:  fileRootVolumes,
		Query: "zzz-no-match",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !resp.GetTruncated() {
		t.Fatal("truncated = false, want true at the scan cap")
	}
}

// TestFileServerSearchDepthCapIsTruncated proves the depth cap reports an
// incomplete result set instead of silently stopping.
func TestFileServerSearchDepthCapIsTruncated(t *testing.T) {
	old := searchMaxDepth
	searchMaxDepth = 2
	t.Cleanup(func() { searchMaxDepth = old })

	server, volumeRoot := newTestFileServer(t)
	deep := filepath.Join(volumeRoot, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "match.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Query:     "match",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !resp.GetTruncated() {
		t.Fatal("truncated = false, want true at the depth cap")
	}
}

// TestFileServerSearchNeverFollowsSymlinkOutOfRoot proves a link planted in the
// tree cannot direct the walk outside the root, and a link to an in-root
// directory is not descended through the link path.
func TestFileServerSearchNeverFollowsSymlinkOutOfRoot(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(volumeRoot, "app", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volumeRoot, "app", "real", "secret.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(volumeRoot, "app", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(volumeRoot, "app", "link")); err != nil {
		t.Fatal(err)
	}

	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Path:      "app",
		Query:     "secret",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Exactly the real in-root file, reached by its real path only: the escape
	// link is never followed and the in-root link is not descended through.
	if got := searchPaths(resp); len(got) != 1 || got[0] != "app/real/secret.txt" {
		t.Fatalf("results = %v, want [app/real/secret.txt]", got)
	}
}

// TestFileServerSearchExcludesTrash proves the reserved recycle bin is neither
// matched nor descended into.
func TestFileServerSearchExcludesTrash(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	if err := os.MkdirAll(filepath.Join(volumeRoot, fileTrashDir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volumeRoot, fileTrashDir, "store", "match.bin"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Query:     "match",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := resp.GetResults(); len(got) != 0 {
		t.Fatalf("results = %v, want none (trash excluded)", got)
	}
}

func TestFileServerSearchSkipsUnreadableSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	server, volumeRoot := newTestFileServer(t)
	app := filepath.Join(volumeRoot, "app")
	locked := filepath.Join(app, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "visible.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	resp, err := server.Search(context.Background(), &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Path:      "app",
		Query:     "txt",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := searchPaths(resp); len(got) != 1 || got[0] != "app/visible.txt" {
		t.Fatalf("results = %v, want [app/visible.txt]", got)
	}
	// A skipped, unreadable subdirectory means the result set is incomplete.
	if !resp.GetTruncated() {
		t.Fatal("truncated = false, want true when a subdirectory was skipped")
	}
}

func TestFileServerSearchHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	writeSearchFixture(t, volumeRoot)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := server.Search(ctx, &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Query:     "config",
		Recursive: true,
	})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("Search(cancelled) code = %v, want Canceled", status.Code(err))
	}
}

func TestFileServerSearchHonorsDeadline(t *testing.T) {
	t.Parallel()
	server, volumeRoot := newTestFileServer(t)
	writeSearchFixture(t, volumeRoot)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := server.Search(ctx, &agentv1.SearchRequest{
		Root:      fileRootVolumes,
		Query:     "config",
		Recursive: true,
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Search(expired) code = %v, want DeadlineExceeded", status.Code(err))
	}
}

func TestFileServerSearchRejectsBadRequest(t *testing.T) {
	t.Parallel()
	server, _ := newTestFileServer(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  *agentv1.SearchRequest
	}{
		{"unknown root", &agentv1.SearchRequest{Root: "nope", Query: "x"}},
		{"empty query", &agentv1.SearchRequest{Root: fileRootVolumes, Query: "   "}},
		{"traversal base", &agentv1.SearchRequest{Root: fileRootVolumes, Path: "../etc", Query: "x"}},
	}
	for _, tc := range cases {
		if _, err := server.Search(ctx, tc.req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("Search(%s) code = %v, want InvalidArgument", tc.name, status.Code(err))
		}
	}
}
