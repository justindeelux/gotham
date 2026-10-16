package builds

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestEngineTools pins the toolchain each build engine shells out to: Railpack
// needs Docker, BuildKit and Railpack itself (deps first); Buildpacks needs
// pack; Dockerfile and static need no external CLI, and EngineAuto names none.
// This is the mapping the JUS-179 deploy gate and the Web picker both read.
func TestEngineTools(t *testing.T) {
	cases := []struct {
		kind EngineKind
		want []string
	}{
		{EngineRailpack, []string{"docker", "buildkit", "railpack"}},
		{EngineBuildpacks, []string{"pack"}},
		{EngineDockerfile, nil},
		{EngineStatic, nil},
		{EngineAuto, nil},
	}
	for _, tc := range cases {
		if got := EngineTools(tc.kind); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("EngineTools(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestDetectAutoSkipsEnginesMissingOnNode checks the JUS-179 auto rule: auto
// only resolves among engines whose tools are installed on the node, so a
// language project never silently picks a Railpack that is not there. A nil
// tool set keeps marker-only detection (the local dev path).
func TestDetectAutoSkipsEnginesMissingOnNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(nil)

	if engine, ok := registry.Detect(dir, EngineAuto); !ok || engine.Kind() != EngineRailpack {
		t.Fatalf("marker-only auto = (%v, %v), want railpack", engine, ok)
	}
	// Railpack (with its deps) missing on the node: auto must not pick it.
	nodeWithoutRailpack := map[string]bool{"docker": true, "buildkit": true, "pack": true}
	if engine, ok := registry.detect(dir, EngineAuto, nodeWithoutRailpack); ok {
		t.Fatalf("auto with railpack missing picked %q, want no engine", engine.Kind())
	}
	// Railpack present: auto picks it again.
	nodeWithRailpack := map[string]bool{"docker": true, "buildkit": true, "railpack": true}
	if engine, ok := registry.detect(dir, EngineAuto, nodeWithRailpack); !ok || engine.Kind() != EngineRailpack {
		t.Fatalf("auto with railpack present = (%v, %v), want railpack", engine, ok)
	}
}
