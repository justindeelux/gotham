package builds

import (
	"os"
	"path/filepath"
	"testing"
)

// buildFixtures writes the repository shapes the detection matrix exercises and
// returns fixture name -> directory.
func buildFixtures(t *testing.T) map[string]string {
	t.Helper()
	root := t.TempDir()
	fixtures := map[string]map[string]string{
		"dockerfile":    {"Dockerfile": "FROM scratch\n", "app.go": "package main\n"},
		"static":        {"index.html": "<h1>hi</h1>\n", "assets/app.css": "body{}\n"},
		"static-public": {"public/index.html": "<h1>hi</h1>\n"},
		"node":          {"package.json": "{}\n"},
		"go":            {"go.mod": "module example.com/app\n", "main.go": "package main\n"},
		"cnb":           {"project.toml": "[build]\n"},
		"empty":         {},
	}
	dirs := make(map[string]string, len(fixtures))
	for name, files := range fixtures {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir fixture %s: %v", name, err)
		}
		for rel, content := range files {
			writeTestFile(t, filepath.Join(dir, rel), content)
		}
		dirs[name] = dir
	}
	return dirs
}

func TestEngineDetectMatrix(t *testing.T) {
	dirs := buildFixtures(t)
	cases := []struct {
		name    string
		engine  BuildEngine
		fixture string
		want    bool
	}{
		{"dockerfile/dockerfile", NewDockerfileEngine(nil), "dockerfile", true},
		{"dockerfile/static", NewDockerfileEngine(nil), "static", false},
		{"dockerfile/node", NewDockerfileEngine(nil), "node", false},
		{"dockerfile/empty", NewDockerfileEngine(nil), "empty", false},

		{"static/static", NewStaticEngine(nil), "static", true},
		{"static/static-public", NewStaticEngine(nil), "static-public", true},
		{"static/dockerfile", NewStaticEngine(nil), "dockerfile", false},
		{"static/empty", NewStaticEngine(nil), "empty", false},

		{"railpack/node", NewRailpackEngine(), "node", true},
		{"railpack/go", NewRailpackEngine(), "go", true},
		{"railpack/cnb", NewRailpackEngine(), "cnb", false},
		{"railpack/static", NewRailpackEngine(), "static", false},
		{"railpack/dockerfile", NewRailpackEngine(), "dockerfile", false},

		{"buildpacks/cnb", NewBuildpacksEngine(), "cnb", true},
		{"buildpacks/node", NewBuildpacksEngine(), "node", false},
		{"buildpacks/static", NewBuildpacksEngine(), "static", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.engine.Detect(dirs[tc.fixture], EngineAuto); got != tc.want {
				t.Errorf("Detect(%s) = %v; want %v", tc.fixture, got, tc.want)
			}
		})
	}
}

func TestEngineDetectExplicitHint(t *testing.T) {
	dirs := buildFixtures(t)
	engines := []BuildEngine{
		NewDockerfileEngine(nil),
		NewRailpackEngine(),
		NewBuildpacksEngine(),
		NewStaticEngine(nil),
	}
	// An explicit hint selects the named engine without inspecting the tree.
	for _, engine := range engines {
		for name, dir := range dirs {
			if got := engine.Detect(dir, engine.Kind()); !got {
				t.Errorf("%s.Detect(%s, hint=%s) = false; want true", engine.Kind(), name, engine.Kind())
			}
		}
	}
	// A different hint is never a match.
	if NewDockerfileEngine(nil).Detect(dirs["static"], EngineStatic) {
		t.Error("dockerfile engine matched a static hint")
	}
	if NewStaticEngine(nil).Detect(dirs["dockerfile"], EngineDockerfile) {
		t.Error("static engine matched a dockerfile hint")
	}
}

func TestRegistryDetectPriority(t *testing.T) {
	dirs := buildFixtures(t)
	registry := NewRegistry(nil)

	// A repository with both a Dockerfile and index.html prefers the Dockerfile.
	mixed := t.TempDir()
	writeTestFile(t, filepath.Join(mixed, "Dockerfile"), "FROM scratch\n")
	writeTestFile(t, filepath.Join(mixed, "index.html"), "<h1>hi</h1>\n")

	cases := []struct {
		name    string
		fixture string
		want    EngineKind
		wantOK  bool
	}{
		{"dockerfile", "dockerfile", EngineDockerfile, true},
		{"static", "static", EngineStatic, true},
		{"static-public", "static-public", EngineStatic, true},
		{"node", "node", EngineRailpack, true},
		{"go", "go", EngineRailpack, true},
		{"cnb", "cnb", EngineBuildpacks, true},
		{"empty", "empty", EngineAuto, false},
		{"mixed", "", EngineDockerfile, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := dirs[tc.fixture]
			if tc.name == "mixed" {
				dir = mixed
			}
			engine, ok := registry.Detect(dir, EngineAuto)
			if ok != tc.wantOK {
				t.Fatalf("Detect ok = %v; want %v", ok, tc.wantOK)
			}
			if ok && engine.Kind() != tc.want {
				t.Errorf("Detect kind = %q; want %q", engine.Kind(), tc.want)
			}
		})
	}
}

func TestRegistryDetectExplicitHint(t *testing.T) {
	dirs := buildFixtures(t)
	registry := NewRegistry(nil)

	engine, ok := registry.Detect(dirs["static"], EngineStatic)
	if !ok || engine.Kind() != EngineStatic {
		t.Fatalf("explicit static hint = (%v, %v); want static", engine, ok)
	}
	// An explicit hint selects the named engine even when the tree does not
	// obviously match it; Build then validates the repository.
	engine, ok = registry.Detect(dirs["static"], EngineDockerfile)
	if !ok || engine.Kind() != EngineDockerfile {
		t.Fatalf("explicit dockerfile hint = (%v, %v); want dockerfile", engine, ok)
	}
	if _, ok := registry.Detect("", EngineStatic); ok {
		t.Error("empty repo directory matched an engine")
	}
}
