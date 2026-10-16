package toolcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeBinary writes an executable shell stub named name and prepends its
// directory to PATH for the test, so detection resolves the stub instead of a
// real tool. The stub is what makes these tests independent of the host.
func fakeBinary(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// emptyPath points PATH at a directory with nothing in it so no catalog tool
// resolves.
func emptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestDetectInstalledBinary(t *testing.T) {
	emptyPath(t)
	fakeBinary(t, "railpack", "echo 'railpack 0.4.1'")

	detection := DetectTool(mustLookup(t, IDRailpack))
	if !detection.Installed {
		t.Fatal("railpack not detected on the fake PATH")
	}
	if detection.Version != "0.4.1" {
		t.Errorf("version = %q; want 0.4.1", detection.Version)
	}
	if detection.Path == "" {
		t.Error("path is empty; want the resolved fake binary")
	}
}

func TestDetectMissingBinary(t *testing.T) {
	emptyPath(t)
	for _, id := range []string{IDRailpack, IDPack, IDGit, IDDocker, IDNetworkctl} {
		detection := DetectTool(mustLookup(t, id))
		if detection.Installed {
			t.Errorf("%s reported installed on an empty PATH", id)
		}
	}
}

// TestDetectToolPresentWithoutVersionProbe checks a tool that is on PATH but
// does not answer its version probe is still installed, with no version.
func TestDetectToolPresentWithoutVersionProbe(t *testing.T) {
	emptyPath(t)
	fakeBinary(t, "git", "exit 1")

	detection := DetectTool(mustLookup(t, IDGit))
	if !detection.Installed {
		t.Fatal("git not detected though it is on PATH")
	}
	if detection.Version != "" {
		t.Errorf("version = %q; want empty when the probe fails", detection.Version)
	}
}

// TestDetectContainerProbe covers the BuildKit entry, which is a container
// image probed through Docker rather than a host binary.
func TestDetectContainerProbe(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		emptyPath(t)
		fakeBinary(t, "docker", "exit 0")
		if detection := DetectTool(mustLookup(t, IDBuildKit)); !detection.Installed {
			t.Fatal("buildkit not detected though the probe succeeded")
		}
	})
	t.Run("absent", func(t *testing.T) {
		emptyPath(t)
		fakeBinary(t, "docker", "exit 1")
		detection := DetectTool(mustLookup(t, IDBuildKit))
		if detection.Installed {
			t.Fatal("buildkit detected though the probe failed")
		}
		if detection.Path != "" {
			t.Errorf("path = %q; want empty for a container tool", detection.Path)
		}
	})
}

// TestDetectPathFallback checks a tool whose executable lives outside PATH
// resolves through its absolute fallback path. It uses a synthetic entry so the
// test never touches a real host path.
func TestDetectPathFallback(t *testing.T) {
	emptyPath(t)
	path := filepath.Join(t.TempDir(), "systemd-networkd")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'systemd 255 (255.4)'\n"), 0o755); err != nil {
		t.Fatalf("write fallback binary: %v", err)
	}
	tool := Tool{ID: "netd", Detect: Detect{Paths: []string{path}, Version: []string{"--version"}}}

	detection := DetectTool(tool)
	if !detection.Installed {
		t.Fatal("tool not detected through its fallback path")
	}
	if detection.Path != path {
		t.Errorf("path = %q; want %q", detection.Path, path)
	}
	if detection.Version != "255.4" {
		t.Errorf("version = %q; want 255.4", detection.Version)
	}
}

// TestCatalogSystemdEntriesUseFixedPaths keeps the systemd units pointed at the
// real executables, which are not on PATH.
func TestCatalogSystemdEntriesUseFixedPaths(t *testing.T) {
	for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved} {
		tool := mustLookup(t, id)
		if len(tool.Detect.Binaries) != 0 || len(tool.Detect.Paths) == 0 {
			t.Errorf("%s should resolve by absolute path, not PATH", id)
		}
	}
}

func TestInstalledHelper(t *testing.T) {
	emptyPath(t)
	if Installed(IDRailpack) {
		t.Error("Installed(railpack) = true on an empty PATH")
	}
	fakeBinary(t, "railpack", "echo 'railpack 0.4.1'")
	if !Installed(IDRailpack) {
		t.Error("Installed(railpack) = false though it is on PATH")
	}
	if Installed("no-such-tool") {
		t.Error("Installed(unknown) = true; want false")
	}
}

// TestCatalogIntegrity keeps the catalog internally consistent: unique ids,
// required fields, declared category, and every dependency an existing entry.
func TestCatalogIntegrity(t *testing.T) {
	seen := make(map[string]bool, len(Catalog))
	for _, tool := range Catalog {
		if tool.ID == "" || tool.Name == "" || tool.Description == "" {
			t.Errorf("entry %q is missing id, name or description", tool.ID)
		}
		if seen[tool.ID] {
			t.Errorf("duplicate catalog id %q", tool.ID)
		}
		seen[tool.ID] = true
		switch tool.Category {
		case CategoryBuild, CategoryRuntime, CategoryNetwork:
		default:
			t.Errorf("%s has unknown category %q", tool.ID, tool.Category)
		}
		if len(tool.Detect.Binaries) == 0 && len(tool.Detect.Paths) == 0 && len(tool.Detect.Probe) == 0 {
			t.Errorf("%s has no detect rule", tool.ID)
		}
	}
	for _, tool := range Catalog {
		for _, dep := range tool.Deps {
			if !seen[dep] {
				t.Errorf("%s depends on unknown tool %q", tool.ID, dep)
			}
		}
	}
}

// TestDetectOnlyToolsAreNeverManaged pins the JUS-174 requirement: the systemd
// network units are detect-only and must never be offered for install or
// removal.
func TestDetectOnlyToolsAreNeverManaged(t *testing.T) {
	for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved, IDNetworkctl, IDSystemctl} {
		tool := mustLookup(t, id)
		if tool.Installable || tool.Uninstallable {
			t.Errorf("%s is installable/uninstallable (%v/%v); want detect-only", id, tool.Installable, tool.Uninstallable)
		}
	}
}

// TestDetectAllReturnsCatalogOrder checks DetectAll covers the whole catalog in
// order, so the agent's ListTools never silently drops an entry.
func TestDetectAllReturnsCatalogOrder(t *testing.T) {
	emptyPath(t)
	detections := DetectAll()
	if len(detections) != len(Catalog) {
		t.Fatalf("Detect returned %d entries; want %d", len(detections), len(Catalog))
	}
	for i, detection := range detections {
		if detection.ID != Catalog[i].ID {
			t.Errorf("position %d = %q; want %q", i, detection.ID, Catalog[i].ID)
		}
	}
}

func mustLookup(t *testing.T, id string) Tool {
	t.Helper()
	tool, ok := Lookup(id)
	if !ok {
		t.Fatalf("catalog has no %q", id)
	}
	return tool
}
