package templates

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestEmbedCoversEveryTemplateDirectory fails when a template directory exists
// in the repository but is missing from the go:embed list in embed.go: the
// catalog tests only see the embedded files, so without this guard a new
// template could silently miss the binary.
func TestEmbedCoversEveryTemplateDirectory(t *testing.T) {
	onDisk, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	want := []string{}
	for _, entry := range onDisk {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			want = append(want, entry.Name())
		}
	}
	embedded, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read embedded filesystem: %v", err)
	}
	got := []string{}
	for _, entry := range embedded {
		if entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("embedded template directories = %v, on disk = %v; add the new directory to the go:embed list in embed.go", got, want)
	}
}
