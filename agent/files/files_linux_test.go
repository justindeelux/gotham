//go:build linux

package files

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/justindeelux/gotham/agent/fsroot"
)

// TestListCursorNonUTF8RoundTrip pages a directory whose names straddle the
// page boundary with invalid UTF-8, through the same encode/decode the List
// RPC uses, and proves every name is visited exactly once and the walk
// terminates. Linux is the only target that accepts non-UTF-8 filenames, so
// this lives behind the linux build tag.
func TestListCursorNonUTF8RoundTrip(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a", "a\xff", "a\xffz", "b", "c\xfd", "d"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := fsroot.Open("t", dir, info, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	sort.Strings(names)
	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > len(names)+1 {
			t.Fatal("paging did not terminate")
		}
		raw, err := decodeCursor(cursor)
		if err != nil {
			t.Fatalf("decodeCursor(%q): %v", cursor, err)
		}
		entries, truncated, last, err := root.List(".", raw, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, entry := range entries {
			got = append(got, entry.Name)
		}
		if !truncated {
			break
		}
		cursor = encodeCursor(last)
		if cursor == "" {
			t.Fatal("a truncated page produced an empty cursor")
		}
	}
	if !reflect.DeepEqual(got, names) {
		t.Fatalf("paged %q, want %q", got, names)
	}
}
