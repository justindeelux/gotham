package builds

import (
	"archive/tar"
	"bytes"
	"errors"
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

// TestBuildContextTarDockerIgnoreBOM is the U1 regression: a BOM-encoded
// .dockerignore must not turn its first rule into a non-matching pattern.
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

// TestBuildContextTarFollowsSymlinkedDockerIgnore is the round-2 U3 regression:
// the Docker CLI follows a symlinked .dockerignore, so gotham applies its rules
// rather than packing ignored files.
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

// TestBuildContextTarRejectsNonRegularDockerIgnore pins the memory guard: a
// .dockerignore whose target is not a regular file (for example a directory or
// a device) is treated as absent rather than read.
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

// TestBuildContextTarRejectsOversizedDockerIgnore is the U2 memory-bound
// regression: a huge .dockerignore fails with a clear validation error.
func TestBuildContextTarRejectsOversizedDockerIgnore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), strings.Repeat("a", int(maxDockerIgnoreBytes)+1))

	_, err := buildContextTar(contextSpec{root: dir})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("buildContextTar(oversized .dockerignore) error = %v; want ErrValidation", err)
	}
}

// TestBuildContextTarDockerIgnoreRepoRelative is the U3 regression: when the
// ignore file lives above the context root, patterns are matched
// repository-relative, so `public/.env` excludes public/.env while a bare
// `index.html` matches only the repository root and does not break a public/
// site.
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

// TestBuildContextTarDockerIgnoreNegationReincludes is the U6 regression:
// `docs` + `!docs/keep.md` must keep docs/keep.md (the Docker oracle keeps it)
// while dropping the rest of the subtree.
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

// TestBuildContextTarDockerIgnoreTrailingSlashMatchesFile is the U7 regression:
// Docker Cleans `cache/` to `cache`, so it excludes a regular file named cache
// as well as a directory.
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

// TestMatchContextPatternBoundedTime is the round-2 U4 regression: many `**`
// segments must not make matching exponential. The input does NOT match, so a
// recursive implementation must explore every split and runs for hours; the DP
// returns instantly. The timeout fails the test instead of hanging CI.
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

// TestBuildContextTarDockerIgnoreOrder is the round-2 U2 regression: patterns
// are evaluated in file order with ancestor inheritance, so a later ancestor
// exclusion beats an earlier full-path negation (both cases verified against
// the Docker CLI).
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

// TestBuildContextTarDockerIgnoreCleansPatterns is the round-2 U6 regression:
// patterns are path.Clean-ed like Docker, so `a//b`, `./a/b` and `/a/./b` all
// match a/b.
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

// TestBuildContextTarDockerIgnoreChildVersusAncestor is the round-3 U1
// regression: the threaded per-pattern state machine must match the Docker CLI
// for a child exclusion plus a later/earlier ancestor negation. The previous
// "match path or ancestor, last pattern wins" rule let `!logs` re-include
// `logs/secret.log`; Docker keeps it excluded (and vice versa, so ordering does
// not matter here).
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

// TestBuildContextTarDockerIgnoreDirectoryVariant is the round-3 U1 directory
// variant: `logs/tmp` + `!logs` keeps logs/tmp excluded.
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

// TestBuildContextTarIgnoresEscapingSymlinkedDockerIgnore is the round-3 U2
// regression: a .dockerignore symlink that resolves outside the repository must
// be ignored, so a hostile repo cannot read an arbitrary host file as patterns.
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

// TestBuildContextTarDockerIgnoreTrailingDoubleStar is the round-4 U1
// regression: moby compiles a literal trailing `/**` to a prefix match on
// `dir/`, so it matches the subtree but not the directory itself. Without the
// special case, `secrets/**` + `!secrets` re-included the whole subtree.
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
}

// TestDirStateChainBoundedMemory is the round-4 U2 regression: the walk keeps
// only the current ancestor chain, so the retained match state is bounded by
// tree depth, not by the number of directories (which is attacker-controlled
// through the repository layout).
func TestDirStateChainBoundedMemory(t *testing.T) {
	const patterns = 4000
	var chain dirStateChain

	// A wide, shallow tree: 2000 sibling directories at depth 0.
	for i := 0; i < 2000; i++ {
		chain.set(0, make([]bool, patterns))
	}
	if got := chain.retained(); got != patterns {
		t.Fatalf("retained = %d; want %d (one state at depth 0, not one per directory)", got, patterns)
	}

	// A deep chain retains one state per depth.
	const depth = 50
	for d := 1; d < depth; d++ {
		chain.set(d, make([]bool, patterns))
	}
	if got, want := chain.retained(), depth*patterns; got != want {
		t.Fatalf("retained = %d; want %d (bounded by depth)", got, want)
	}
}
