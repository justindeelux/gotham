package toolcatalog

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// testEnv is a fixed install environment so plans are host-independent.
func testEnv() Env {
	return Env{Arch: "amd64", Distro: DistroApt, BinDir: "/usr/local/bin", TempDir: "/tmp"}
}

// TestInstallPlanOrdersDependencies proves railpack is preceded by buildkit,
// which is preceded by docker: the install runs in dependency order.
func TestInstallPlanOrdersDependencies(t *testing.T) {
	items, err := InstallPlan(IDRailpack, testEnv())
	if err != nil {
		t.Fatalf("InstallPlan(railpack): %v", err)
	}
	var ids []string
	for _, item := range items {
		ids = append(ids, item.Tool.ID)
	}
	want := []string{IDDocker, IDBuildKit, IDRailpack}
	if !slices.Equal(ids, want) {
		t.Fatalf("InstallPlan(railpack) order = %v; want %v", ids, want)
	}
}

// TestInstallPlanRefusesDetectOnly proves a host unit the platform never
// installs is refused, not silently planned.
func TestInstallPlanRefusesDetectOnly(t *testing.T) {
	for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved, IDNetworkctl, IDSystemctl} {
		if _, err := InstallPlan(id, testEnv()); !errors.Is(err, ErrNotInstallable) {
			t.Errorf("InstallPlan(%s) = %v; want ErrNotInstallable", id, err)
		}
	}
}

// TestUninstallPlanRefusesUndeletable proves docker and the detect-only units
// cannot be removed.
func TestUninstallPlanRefusesUndeletable(t *testing.T) {
	for _, id := range []string{IDDocker, IDSystemdNetworkd, IDNetworkctl, IDSystemctl, IDCurl} {
		if _, err := UninstallPlan(id, testEnv()); !errors.Is(err, ErrNotUninstallable) {
			t.Errorf("UninstallPlan(%s) = %v; want ErrNotUninstallable", id, err)
		}
	}
}

// TestPlansRefuseUnknownTool proves an unknown id is reported as such.
func TestPlansRefuseUnknownTool(t *testing.T) {
	if _, err := InstallPlan("nope", testEnv()); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("InstallPlan(nope) = %v; want ErrUnknownTool", err)
	}
	if _, err := UninstallPlan("nope", testEnv()); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("UninstallPlan(nope) = %v; want ErrUnknownTool", err)
	}
}

// TestPackInstallerIsArchAware proves the release asset matches the
// architecture, including pack's un-suffixed amd64 asset, and an unsupported
// architecture is refused.
func TestPackInstallerIsArchAware(t *testing.T) {
	amd64, err := InstallPlan(IDPack, Env{Arch: "amd64", Distro: DistroApt, BinDir: "/usr/local/bin", TempDir: "/tmp"})
	if err != nil {
		t.Fatalf("InstallPlan(pack amd64): %v", err)
	}
	if !planContains(amd64, "pack-v"+packVersion+"-linux.tgz") {
		t.Errorf("amd64 pack plan did not fetch the linux.tgz asset: %v", planArgvs(amd64))
	}
	if planContains(amd64, "linux-arm64") {
		t.Errorf("amd64 pack plan fetched an arm64 asset: %v", planArgvs(amd64))
	}

	arm64, err := InstallPlan(IDPack, Env{Arch: "arm64", Distro: DistroApt, BinDir: "/usr/local/bin", TempDir: "/tmp"})
	if err != nil {
		t.Fatalf("InstallPlan(pack arm64): %v", err)
	}
	if !planContains(arm64, "pack-v"+packVersion+"-linux-arm64.tgz") {
		t.Errorf("arm64 plan did not fetch the arm64 asset: %v", planArgvs(arm64))
	}

	if _, err := InstallPlan(IDPack, Env{Arch: "riscv64", Distro: DistroApt}); !errors.Is(err, ErrNotInstallable) {
		t.Errorf("InstallPlan(pack riscv64) = %v; want ErrNotInstallable", err)
	}
}

// TestPackageManagerPerDistro proves docker uses the distro's own package
// manager and an unknown distro is refused rather than guessed.
func TestPackageManagerPerDistro(t *testing.T) {
	perDistro := []struct {
		distro Distro
		binary string
	}{
		{DistroApt, "apt-get"},
		{DistroDNF, "dnf"},
		{DistroAPK, "apk"},
	}
	for _, tc := range perDistro {
		items, err := InstallPlan(IDDocker, Env{Arch: "amd64", Distro: tc.distro, BinDir: "/usr/local/bin", TempDir: "/tmp"})
		if err != nil {
			t.Fatalf("InstallPlan(docker %s): %v", tc.distro, err)
		}
		if !planContains(items, tc.binary) {
			t.Errorf("%s plan did not use %s: %v", tc.distro, tc.binary, planArgvs(items))
		}
	}
	if _, err := InstallPlan(IDDocker, Env{Arch: "amd64", Distro: "plan9"}); !errors.Is(err, ErrNotInstallable) {
		t.Errorf("InstallPlan(docker plan9) = %v; want ErrNotInstallable", err)
	}
}

// TestRailpackInstallerIsNonInteractive proves the official installer runs with
// the ask-free flags and the pinned version, so an install never blocks on a
// prompt.
func TestRailpackInstallerIsNonInteractive(t *testing.T) {
	items, err := InstallPlan(IDRailpack, testEnv())
	if err != nil {
		t.Fatalf("InstallPlan(railpack): %v", err)
	}
	var found bool
	for _, item := range items {
		for _, step := range item.Steps {
			if step.Argv[0] != "sh" {
				continue
			}
			found = true
			if !slices.Contains(step.Argv, "-y") || envValue(step, "RAILPACK_VERSION=") != "RAILPACK_VERSION="+railpackVersion {
				t.Errorf("railpack installer is not pinned and non-interactive: %v env=%v", step.Argv, step.Env)
			}
			if !step.Root {
				t.Error("railpack installer step is not marked root")
			}
		}
	}
	if !found {
		t.Fatalf("railpack plan has no installer step: %v", planArgvs(items))
	}
}

// TestStepsUseNoShell proves no step routes through a shell: the argv is a real
// argument vector, so a tool id can never be interpolated into a command.
func TestStepsUseNoShell(t *testing.T) {
	for _, id := range []string{IDRailpack, IDBuildKit, IDPack, IDDocker, IDGit} {
		items, err := InstallPlan(id, testEnv())
		if err != nil {
			continue
		}
		for _, item := range items {
			for _, step := range item.Steps {
				if len(step.Argv) == 0 {
					t.Errorf("%s produced an empty argv", id)
					continue
				}
				switch step.Argv[0] {
				case "sh", "/bin/sh", "bash":
					// Shipping the official installer to sh is deliberate; a
					// shell must never be given an argument string we built.
					if len(step.Argv) > 1 && strings.ContainsAny(step.Argv[1], ";|&$`") {
						t.Errorf("%s sh step carries a command string: %v", id, step.Argv)
					}
				}
				joined := strings.Join(step.Argv, " ")
				if strings.ContainsAny(joined, ";|&`") {
					t.Errorf("%s step looks like a shell string: %v", id, step.Argv)
				}
			}
		}
	}
}

// planContains reports whether any step in the plan has an argument containing
// fragment.
func planContains(items []PlanItem, fragment string) bool {
	for _, item := range items {
		for _, step := range item.Steps {
			for _, arg := range step.Argv {
				if strings.Contains(arg, fragment) {
					return true
				}
			}
		}
	}
	return false
}

// planArgvs flattens a plan's steps to their argv, for failure messages.
func planArgvs(items []PlanItem) [][]string {
	var out [][]string
	for _, item := range items {
		for _, step := range item.Steps {
			out = append(out, step.Argv)
		}
	}
	return out
}

// envValue returns the KEY=VALUE entry of the step's env, so the pinned version
// check reads the same string the runner would pass.
func envValue(step Step, prefix string) string {
	for _, entry := range step.Env {
		if strings.HasPrefix(entry, prefix) {
			return entry
		}
	}
	return ""
}
