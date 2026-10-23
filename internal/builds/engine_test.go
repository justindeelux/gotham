package builds

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEngineTool(t *testing.T) {
	cases := []struct {
		kind EngineKind
		want string
	}{
		{EngineRailpack, "railpack"},
		{EngineBuildpacks, "pack"},
		{EngineDockerfile, ""},
		{EngineStatic, ""},
		{EngineAuto, ""},
	}
	for _, tc := range cases {
		if got := EngineTool(tc.kind); got != tc.want {
			t.Errorf("EngineTool(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

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

func TestDetectAutoSkipsEnginesMissingOnNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(nil)

	if engine, ok := registry.Detect(dir, EngineAuto); !ok || engine.Kind() != EngineRailpack {
		t.Fatalf("marker-only auto = (%v, %v), want railpack", engine, ok)
	}
	nodeWithoutRailpack := map[string]bool{"docker": true, "buildkit": true, "pack": true}
	if engine, ok := registry.detect(dir, EngineAuto, nodeWithoutRailpack); ok {
		t.Fatalf("auto with railpack missing picked %q, want no engine", engine.Kind())
	}
	nodeWithRailpack := map[string]bool{"docker": true, "buildkit": true, "railpack": true}
	if engine, ok := registry.detect(dir, EngineAuto, nodeWithRailpack); !ok || engine.Kind() != EngineRailpack {
		t.Fatalf("auto with railpack present = (%v, %v), want railpack", engine, ok)
	}
}

func TestBuildAutoErrorNamesMissingTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(nil)
	_, err := registry.Build(context.Background(), BuildOptions{
		RepoDir:        dir,
		AppID:          uuid.New(),
		DeployID:       uuid.New(),
		BuildPack:      EngineAuto,
		InstalledTools: map[string]bool{"docker": true, "buildkit": true, "pack": true},
	})
	if err == nil {
		t.Fatal("Build(auto) with railpack missing = nil, want an error")
	}
	if !errors.Is(err, ErrNoEngine) {
		t.Fatalf("Build(auto) error = %v, want ErrNoEngine", err)
	}
	if !strings.Contains(err.Error(), "railpack is not installed on this node") {
		t.Fatalf("Build(auto) error = %q, want it to name railpack", err)
	}
}

func TestBuildAutoGenuinelyNoEngine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(nil)
	_, err := registry.Build(context.Background(), BuildOptions{
		RepoDir:        dir,
		AppID:          uuid.New(),
		DeployID:       uuid.New(),
		BuildPack:      EngineAuto,
		InstalledTools: map[string]bool{"docker": true, "buildkit": true, "railpack": true},
	})
	if !errors.Is(err, ErrNoEngine) || strings.Contains(err.Error(), "is not installed") {
		t.Fatalf("Build(auto) on a bare repo = %v, want a plain ErrNoEngine", err)
	}
}
