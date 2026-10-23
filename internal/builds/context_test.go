package builds

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	for _, want := range []string{"app/app.log", "app/cache/x"} {
		if _, ok := files[want]; !ok {
			t.Errorf("context is missing %s", want)
		}
	}
}

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

func TestBuildContextTarBoundsSize(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "big.bin"), string(make([]byte, 8192)))

	_, err := buildContextTar(contextSpec{root: dir, maxBytes: 1024})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(oversized) error = %v; want ErrValidation", err)
	}
}

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

func TestBuildContextTarDockerIgnoreBOM(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "\ufeff.env\n")
	writeTestFile(t, filepath.Join(dir, ".env"), "SECRET=leaked\n")
	writeTestFile(t, filepath.Join(dir, "keep.txt"), "ok\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files[".env"]; ok {
		t.Error("the first rule after a BOM must still apply (.env excluded)")
	}
	if _, ok := files["keep.txt"]; !ok {
		t.Error("keep.txt is missing")
	}
}

func TestBuildContextTarFollowsSymlinkedDockerIgnore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "real-ignore"), ".env\n")
	if err := os.Symlink("real-ignore", filepath.Join(dir, ".dockerignore")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, ".env"), "SECRET=leaked\n")
	writeTestFile(t, filepath.Join(dir, "keep.txt"), "ok\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files[".env"]; ok {
		t.Error("a symlinked .dockerignore must be followed and its rules applied")
	}
	if _, ok := files["keep.txt"]; !ok {
		t.Error("keep.txt is missing")
	}
}

func TestBuildContextTarRejectsNonRegularDockerIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ignoredir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("ignoredir", filepath.Join(dir, ".dockerignore")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, ".env"), "survives\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	if _, ok := readContextTar(t, data)[".env"]; !ok {
		t.Error("a non-regular .dockerignore target must be treated as absent")
	}
}

func TestBuildContextTarRejectsOversizedDockerIgnore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), strings.Repeat("a", int(maxDockerIgnoreBytes)+1))

	_, err := buildContextTar(contextSpec{root: dir})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(oversized .dockerignore) error = %v; want ErrValidation", err)
	}
}

func TestBuildContextTarDockerIgnoreRepoRelative(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, ".dockerignore"), ".env\npublic/secret.txt\nindex.html\n")
	writeTestFile(t, filepath.Join(repo, "public", "index.html"), "<h1>x</h1>\n")
	writeTestFile(t, filepath.Join(repo, "public", ".env"), "FLAT=survives\n")
	writeTestFile(t, filepath.Join(repo, "public", "secret.txt"), "SECRET=excluded\n")
	writeTestFile(t, filepath.Join(repo, "public", "app.js"), "x\n")

	data, err := buildContextTar(contextSpec{root: filepath.Join(repo, "public"), ignoreDir: repo})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files["secret.txt"]; ok {
		t.Error("public/secret.txt must be excluded by the repository-relative rule")
	}
	if _, ok := files["index.html"]; !ok {
		t.Error("bare index.html is repository-root-relative and must not match public/index.html")
	}
	if _, ok := files[".env"]; !ok {
		t.Error("bare .env is repository-root-relative and must not match public/.env")
	}
	if _, ok := files["app.js"]; !ok {
		t.Error("app.js is missing")
	}
}

func TestBuildContextTarDockerIgnoreNegationReincludes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "docs\n!docs/keep.md\n")
	writeTestFile(t, filepath.Join(dir, "docs", "keep.md"), "keep\n")
	writeTestFile(t, filepath.Join(dir, "docs", "drop.md"), "drop\n")
	writeTestFile(t, filepath.Join(dir, "top.md"), "top\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files["docs/keep.md"]; !ok {
		t.Error("a negated child must be re-included under an excluded directory")
	}
	if _, ok := files["docs/drop.md"]; ok {
		t.Error("the rest of the excluded directory must stay out")
	}
	if _, ok := files["top.md"]; !ok {
		t.Error("an unrelated file must survive")
	}
}

func TestBuildContextTarDockerIgnoreTrailingSlashMatchesFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "cache/\n")
	writeTestFile(t, filepath.Join(dir, "cache"), "regular file\n")
	writeTestFile(t, filepath.Join(dir, "cachedir", "x"), "x\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files["cache"]; ok {
		t.Error("cache/ must also match a regular file named cache")
	}
	if _, ok := files["cachedir/x"]; !ok {
		t.Error("cachedir/x must survive: the pattern is an exact path match")
	}
}

func TestMatchContextPatternBoundedTime(t *testing.T) {
	pattern := strings.Repeat("**/", 25) + "needle"
	name := strings.Repeat("a/", 60) + "b"

	done := make(chan bool, 1)
	go func() { done <- matchContextPattern(pattern, name) }()
	select {
	case got := <-done:
		if got {
			t.Error("pattern must not match a name that does not end in needle")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("matchContextPattern did not finish; ** matching is not memoized")
	}
}

func TestBuildContextTarDockerIgnoreOrder(t *testing.T) {
	t.Run("negation then directory", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "!important/keep.txt\nimportant\n")
		writeTestFile(t, filepath.Join(dir, "important", "keep.txt"), "should be excluded\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		if _, ok := readContextTar(t, data)["important/keep.txt"]; ok {
			t.Error("a later directory exclusion must beat an earlier full-path negation")
		}
	})
	t.Run("negation then glob ancestor", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "!**\nnode_modules\n")
		writeTestFile(t, filepath.Join(dir, "node_modules", "x"), "should be excluded\n")
		writeTestFile(t, filepath.Join(dir, "app.js"), "ok\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		files := readContextTar(t, data)
		if _, ok := files["node_modules/x"]; ok {
			t.Error("a later ancestor exclusion must beat an earlier `!**` negation")
		}
		if _, ok := files["app.js"]; !ok {
			t.Error("app.js is missing")
		}
	})
}

func TestBuildContextTarDockerIgnoreCleansPatterns(t *testing.T) {
	for _, pattern := range []string{"a//b", "./a/b", "/a/./b", "a/b/"} {
		t.Run(pattern, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, ".dockerignore"), pattern+"\n")
			writeTestFile(t, filepath.Join(dir, "a", "b"), "excluded\n")
			writeTestFile(t, filepath.Join(dir, "keep.txt"), "ok\n")

			data, err := buildContextTar(contextSpec{root: dir})
			if err != nil {
				t.Fatalf("buildContextTar: %v", err)
			}
			files := readContextTar(t, data)
			if _, ok := files["a/b"]; ok {
				t.Errorf("pattern %q must match a/b after Clean", pattern)
			}
			if _, ok := files["keep.txt"]; !ok {
				t.Error("keep.txt is missing")
			}
		})
	}
}

func TestBuildContextTarDockerIgnoreChildVersusAncestor(t *testing.T) {
	cases := []struct {
		name   string
		ignore string
	}{
		{name: "exclusion then ancestor negation", ignore: "logs/secret.log\n!logs\n"},
		{name: "ancestor negation then exclusion", ignore: "!logs\nlogs/secret.log\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, ".dockerignore"), tc.ignore)
			writeTestFile(t, filepath.Join(dir, "logs", "secret.log"), "secret\n")
			writeTestFile(t, filepath.Join(dir, "logs", "other.txt"), "other\n")

			data, err := buildContextTar(contextSpec{root: dir})
			if err != nil {
				t.Fatalf("buildContextTar: %v", err)
			}
			files := readContextTar(t, data)
			if _, ok := files["logs/secret.log"]; ok {
				t.Error("logs/secret.log must stay excluded (Docker excludes it)")
			}
			if _, ok := files["logs/other.txt"]; !ok {
				t.Error("logs/other.txt must be included")
			}
		})
	}
}

func TestBuildContextTarDockerIgnoreDirectoryVariant(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), "logs/tmp\n!logs\n")
	writeTestFile(t, filepath.Join(dir, "logs", "tmp", "x"), "x\n")
	writeTestFile(t, filepath.Join(dir, "logs", "other.txt"), "other\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	files := readContextTar(t, data)
	if _, ok := files["logs/tmp/x"]; ok {
		t.Error("logs/tmp/x must stay excluded")
	}
	if _, ok := files["logs/other.txt"]; !ok {
		t.Error("logs/other.txt must be included")
	}
}

func TestBuildContextTarIgnoresEscapingSymlinkedDockerIgnore(t *testing.T) {
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(outside, "host-ignore"), ".env\n")

	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "host-ignore"), filepath.Join(dir, ".dockerignore")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, ".env"), "survives\n")

	data, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	if _, ok := readContextTar(t, data)[".env"]; !ok {
		t.Error("a .dockerignore symlink escaping the repository must be ignored")
	}
}

func TestBuildContextTarDockerIgnoreTrailingDoubleStar(t *testing.T) {
	t.Run("prefix match does not include the dir itself", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "secrets/**\n!secrets\n")
		writeTestFile(t, filepath.Join(dir, "secrets", "token.txt"), "SECRET\n")
		writeTestFile(t, filepath.Join(dir, "secrets", "nested", "deep.txt"), "deep\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		files := readContextTar(t, data)
		if _, ok := files["secrets/token.txt"]; ok {
			t.Error("secrets/** + !secrets must keep secrets/token.txt excluded")
		}
		if _, ok := files["secrets/nested/deep.txt"]; ok {
			t.Error("secrets/** + !secrets must keep the whole subtree excluded")
		}
	})

	t.Run("directory itself is not matched", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "a/**\n")
		writeTestFile(t, filepath.Join(dir, "a"), "regular file named a\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		if _, ok := readContextTar(t, data)["a"]; !ok {
			t.Error("a/** must not match a regular file named a")
		}
	})

	t.Run("subtree is matched", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "a/**\n")
		writeTestFile(t, filepath.Join(dir, "a", "b"), "b\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		if _, ok := readContextTar(t, data)["a/b"]; ok {
			t.Error("a/** must match a/b")
		}
	})

	t.Run("glob prefix does not match the prefix itself", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "*.js/**\n")
		writeTestFile(t, filepath.Join(dir, "a.js"), "file\n")
		writeTestFile(t, filepath.Join(dir, "dir.js", "x"), "x\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		files := readContextTar(t, data)
		if _, ok := files["a.js"]; !ok {
			t.Error("*.js/** must not match a.js itself")
		}
		if _, ok := files["dir.js/x"]; ok {
			t.Error("*.js/** must match dir.js/x")
		}
	})

	t.Run("glob prefix with negation excludes the child", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".dockerignore"), "*.js/**\n!d.js\n")
		writeTestFile(t, filepath.Join(dir, "d.js", "x"), "x\n")

		data, err := buildContextTar(contextSpec{root: dir})
		if err != nil {
			t.Fatalf("buildContextTar: %v", err)
		}
		if _, ok := readContextTar(t, data)["d.js/x"]; ok {
			t.Error("*.js/** + !d.js must keep d.js/x excluded")
		}
	})
}

func TestLiteralTrailingDoubleStarFastPathAllocs(t *testing.T) {
	ignore := parseDockerIgnore("node_modules/**\n")
	if !ignore.excludedOnly("node_modules/pkg/index.js", nil) {
		t.Fatal("literal /** should exclude a path under the prefix")
	}
	if ignore.excludedOnly("node_modules", nil) {
		t.Fatal("literal /** must not exclude the prefix path itself")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		ignore.excludedOnly("node_modules/pkg/index.js", nil)
	})
	if allocs != 0 {
		t.Fatalf("literal /** eval allocated %.1f allocs; want 0", allocs)
	}
}

func TestBuildContextTarWalkerStateBounded(t *testing.T) {
	const (
		dirs     = 200
		patterns = 1000
	)
	dir := t.TempDir()
	for i := 0; i < dirs; i++ {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("d%04d", i), "f.txt"), "x")
	}
	var ignore strings.Builder
	for i := 0; i < patterns; i++ {
		fmt.Fprintf(&ignore, "pattern-%d.txt\n", i)
	}
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), ignore.String())

	_, peak, err := buildContextTarPeak(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTarPeak: %v", err)
	}
	if peak < patterns {
		t.Fatalf("peak retained match state = %d; want >= %d (walker did not record state)", peak, patterns)
	}
	if peak > 2*patterns {
		t.Fatalf("peak retained match state = %d; want <= %d (O(depth), not O(directories))", peak, 2*patterns)
	}
}
