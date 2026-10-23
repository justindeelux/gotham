package toolcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeBinary(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

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

func TestDetectOnlyToolsAreNeverManaged(t *testing.T) {
	for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved, IDNetworkctl, IDSystemctl} {
		tool := mustLookup(t, id)
		if tool.Installable || tool.Uninstallable {
			t.Errorf("%s is installable/uninstallable (%v/%v); want detect-only", id, tool.Installable, tool.Uninstallable)
		}
	}
}

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
