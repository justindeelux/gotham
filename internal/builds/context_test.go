package builds

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
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

	data, err := buildContextTar(contextSpec{root: dir, extra: map[string][]byte{"extra.txt": []byte("extra")}})
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

	if _, err := buildContextTar(contextSpec{root: path}); !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(file) error = %v; want ErrValidation", err)
	}
}

func TestBuildContextTarMissingDir(t *testing.T) {
	if _, err := buildContextTar(contextSpec{root: filepath.Join(t.TempDir(), "missing")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(missing) error = %v; want ErrValidation", err)
	}
}

// TestBuildContextTarHonorsDockerIgnore is the C1-1 regression: the Docker
// daemon does not apply .dockerignore to a tar context, so the control plane
// must. An ignored credential must not reach the built context while an
// ordinary file must.
func TestBuildContextTarHonorsDockerIgnore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "# secrets\n.env\n*.pem\n!keep.pem\nconfig/secrets/\n")
	writeTestFile(t, filepath.Join(dir, ".env"), "TOKEN=topsecret\n")
	writeTestFile(t, filepath.Join(dir, "app.pem"), "private key\n")
	writeTestFile(t, filepath.Join(dir, "keep.pem"), "public cert\n")
	writeTestFile(t, filepath.Join(dir, "config", "secrets", "db.txt"), "password\n")
	writeTestFile(t, filepath.Join(dir, "app.txt"), "hello\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	for _, want := range []string{"app.txt", "keep.pem"} {
		if _, ok := files[want]; !ok {
			t.Errorf("context is missing non-ignored %s", want)
		}
	}
	for _, unwanted := range []string{".env", "app.pem", "config/secrets/db.txt"} {
		if _, ok := files[unwanted]; ok {
			t.Errorf("context must not contain ignored %s", unwanted)
		}
	}
}

// TestBuildContextTarDockerIgnoreNestedPatterns pins the glob semantics: `**`
// crosses directories, `*` does not, and a bare name matches only the root.
func TestBuildContextTarDockerIgnoreNestedPatterns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "**/node_modules\n*.log\ncache\n")
	writeTestFile(t, filepath.Join(dir, "node_modules", "dep.js"), "dep\n")
	writeTestFile(t, filepath.Join(dir, "app", "node_modules", "dep.js"), "dep\n")
	writeTestFile(t, filepath.Join(dir, "app.log"), "log\n")
	writeTestFile(t, filepath.Join(dir, "app", "app.log"), "log\n")
	writeTestFile(t, filepath.Join(dir, "cache", "x"), "x\n")
	writeTestFile(t, filepath.Join(dir, "app", "cache", "x"), "x\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	for _, unwanted := range []string{"node_modules/dep.js", "app/node_modules/dep.js", "app.log", "cache/x"} {
		if _, ok := files[unwanted]; ok {
			t.Errorf("context must not contain ignored %s", unwanted)
		}
	}
	// `*.log` and the bare `cache` match only at the context root.
	for _, want := range []string{"app/app.log", "app/cache/x"} {
		if _, ok := files[want]; !ok {
			t.Errorf("context is missing %s", want)
		}
	}
}

// TestBuildContextTarPreservesSymlink is the C1-7 regression: a tracked symlink
// survives into the context as a symlink entry instead of disappearing.
func TestBuildContextTarPreservesSymlink(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "real.txt"), "real\n")
	writeTestFile(t, filepath.Join(dir, "sub", "other.txt"), "other\n")
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink("../real.txt", filepath.Join(dir, "sub", "up.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	entries := readTarHeaders(t, data)
	link, ok := entries["link.txt"]
	if !ok || link.Typeflag != tar.TypeSymlink {
		t.Fatalf("link.txt entry = %+v, present=%v; want symlink", link, ok)
	}
	if link.Linkname != "real.txt" {
		t.Errorf("link.txt -> %q; want real.txt", link.Linkname)
	}
	if up := entries["sub/up.txt"]; up.Typeflag != tar.TypeSymlink || up.Linkname != "../real.txt" {
		t.Errorf("sub/up.txt entry = %+v; want symlink to ../real.txt", up)
	}
}

// TestBuildContextTarDropsSymlinkToIgnoredTarget proves the ignore rules apply
// to a symlink's target as well as the link itself.
func TestBuildContextTarDropsSymlinkToIgnoredTarget(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "secret.txt\n")
	writeTestFile(t, filepath.Join(dir, "secret.txt"), "secret\n")
	writeTestFile(t, filepath.Join(dir, "public.txt"), "public\n")
	if err := os.Symlink("secret.txt", filepath.Join(dir, "leak.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink("public.txt", filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	entries := readTarHeaders(t, data)
	if _, ok := entries["leak.txt"]; ok {
		t.Error("symlink to an ignored target must not enter the context")
	}
	if entries["ok.txt"].Typeflag != tar.TypeSymlink {
		t.Error("symlink to a kept target must survive")
	}
}

// TestBuildContextTarBoundsSize is the C1-6 regression: an oversized context
// fails with a clear validation error instead of growing without bound.
func TestBuildContextTarBoundsSize(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "big.bin"), string(make([]byte, 8192)))

	_, err := buildContextTar(contextSpec{root: dir, maxBytes: 1024})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(oversized) error = %v; want ErrValidation", err)
	}
}

// TestBuildContextTarKeepsIgnoredDockerfile pins that a repository's own
// Dockerfile stays in the context even when .dockerignore lists it (Docker
// allows that line), because the builder must read it.
func TestBuildContextTarKeepsIgnoredDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "Dockerfile\nsecret.txt\n")
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")
	writeTestFile(t, filepath.Join(dir, "secret.txt"), "secret\n")
	writeTestFile(t, filepath.Join(dir, "app.txt"), "ok\n")

	data, err := buildContextTar(contextSpec{root: dir, keep: "Dockerfile"})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files["Dockerfile"]; !ok {
		t.Error("Dockerfile must stay in the context even when .dockerignore lists it")
	}
	if _, ok := files["secret.txt"]; ok {
		t.Error("secret.txt must still be ignored")
	}
}

// readTarHeaders decodes a context tar into name -> header (the body is not
// read). It lets a test inspect symlink entries that readContextTar would see
// as empty regular files.
func readTarHeaders(t *testing.T, data []byte) map[string]tar.Header {
	t.Helper()
	out := make(map[string]tar.Header)
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read context tar: %v", err)
		}
		out[header.Name] = *header
	}
	return out
}
