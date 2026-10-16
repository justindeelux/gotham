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

// TestDetectSystemdUnitsProbeViaPath stubs systemctl on PATH: the systemd
// units must be detected through the probe command, never a host absolute
// path, so a test (or a node without the unit) never sees host state.
func TestDetectSystemdUnitsProbeViaPath(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		emptyPath(t)
		fakeBinary(t, "systemctl", "exit 0")
		for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved} {
			if detection := DetectTool(mustLookup(t, id)); !detection.Installed {
				t.Errorf("%s not detected though its probe succeeded", id)
			}
		}
	})
	t.Run("inactive", func(t *testing.T) {
		emptyPath(t)
		fakeBinary(t, "systemctl", "exit 3")
		for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved} {
			if detection := DetectTool(mustLookup(t, id)); detection.Installed {
				t.Errorf("%s detected though its probe failed", id)
			}
		}
	})
}

// TestCatalogLeaksNoHostState is the CI regression: every catalog entry must
// resolve through PATH, so an empty PATH reports the whole catalog as absent
// even on a host that runs systemd and Docker.
func TestCatalogLeaksNoHostState(t *testing.T) {
	emptyPath(t)
	for _, detection := range DetectAll() {
		if detection.Installed {
			t.Errorf("%s reported installed on an empty PATH", detection.ID)
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
		if len(tool.Detect.Binaries) == 0 && len(tool.Detect.Probe) == 0 {
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
		t.Fatalf("DetectAll returned %d entries; want %d", len(detections), len(Catalog))
	}
	for i, detection := range detections {
		if detection.ID != Catalog[i].ID {
			t.Errorf("position %d = %q; want %q", i, detection.ID, Catalog[i].ID)
		}
	}
}

// TestGitMinVersion is the reviewer's JUS-174 finding: the catalog records
// git's 2.37 minimum and detection must compare the observed version against
// it, not just report the raw version.
func TestGitMinVersion(t *testing.T) {
	if got := mustLookup(t, IDGit).MinVersion; got != "2.37" {
		t.Fatalf("git MinVersion = %q; want 2.37", got)
	}
	tests := []struct {
		version string
		wantOK  bool
	}{
		{"2.30.0", false},
		{"2.36.9", false},
		{"2.37", true},
		{"2.37.0", true},
		{"2.43.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			emptyPath(t)
			fakeBinary(t, "git", "echo 'git version "+tt.version+"'")
			detection := DetectTool(mustLookup(t, IDGit))
			if !detection.Installed {
				t.Fatal("git not detected on the fake PATH")
			}
			if detection.VersionOK != tt.wantOK {
				t.Errorf("git %s VersionOK = %v; want %v", tt.version, detection.VersionOK, tt.wantOK)
			}
		})
	}
}

func TestMeetsMinVersion(t *testing.T) {
	tests := []struct {
		observed, minimum string
		want              bool
	}{
		{"2.43.0", "", true},
		{"", "", true},
		{"", "2.37", false},
		{"2.37", "2.37", true},
		{"2.43", "2.37", true},
		{"2.30.0", "2.37", false},
		{"1.99.99", "2.0", false},
		{"3", "2.37", true},
	}
	for _, tt := range tests {
		if got := meetsMinVersion(tt.observed, tt.minimum); got != tt.want {
			t.Errorf("meetsMinVersion(%q, %q) = %v; want %v", tt.observed, tt.minimum, got, tt.want)
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
