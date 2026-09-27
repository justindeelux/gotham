package builds

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestBuildContextTar(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dir, "sub", "b.txt"), "b")
	writeTestFile(t, filepath.Join(dir, ".git", "config"), "[core]\n")
	writeTestFile(t, filepath.Join(dir, "sub", ".git", "HEAD"), "ref\n")
	writeTestFile(t, filepath.Join(dir, ".DS_Store"), "junk")

	data, err := buildContextTar(dir, map[string][]byte{"extra.txt": []byte("extra")})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	for _, want := range []string{"a.txt", "sub/b.txt", "extra.txt"} {
		if _, ok := files[want]; !ok {
			t.Errorf("context is missing %s", want)
		}
	}
	for _, unwanted := range []string{".git/config", "sub/.git/HEAD", ".DS_Store"} {
		if _, ok := files[unwanted]; ok {
			t.Errorf("context must not contain %s", unwanted)
		}
	}
	if files["extra.txt"] != "extra" {
		t.Errorf("extra.txt = %q; want extra", files["extra.txt"])
	}
}

func TestBuildContextTarRejectsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeTestFile(t, path, "x")

	if _, err := buildContextTar(path, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(file) error = %v; want ErrValidation", err)
	}
}

func TestBuildContextTarMissingDir(t *testing.T) {
	if _, err := buildContextTar(filepath.Join(t.TempDir(), "missing"), nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(missing) error = %v; want ErrValidation", err)
	}
}
