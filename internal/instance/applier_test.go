package instance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestStatusRepairViaSudoPolicy(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "gotham-hostctl"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sudo := filepath.Join(bin, "sudo")
	applier := &sudoApplier{
		path:    filepath.Join(dir, "gotham-hostctl"),
		toolctl: filepath.Join(dir, "gotham-toolctl"),
		payload: payload,
	}

	writeExec(t, sudo, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin)
	if h, err := applier.Status(context.Background()); err != nil || h.Capability != CapabilityHelperMissing || !h.Repair {
		t.Fatalf("permitted rule: status=%+v err=%v, want helper_missing + repair", h, err)
	}

	writeExec(t, sudo, "#!/bin/sh\n# not permitted\nexit 1\n")
	if h, _ := applier.Status(context.Background()); h.Repair {
		t.Fatalf("refused rule: %+v, want repair=false", h)
	}

	t.Setenv("PATH", dir)
	if h, _ := applier.Status(context.Background()); h.Repair {
		t.Fatalf("missing sudo: %+v, want repair=false", h)
	}

	if err := os.Remove(filepath.Join(payload, "gotham-hostctl")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if h, _ := applier.Status(context.Background()); h.Repair {
		t.Fatalf("no payload: %+v, want repair=false", h)
	}
}

func TestStatusReportsOutdatedHelper(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "gotham-hostctl")
	writeExec(t, helper, "#!/bin/sh\n")
	writeExec(t, filepath.Join(bin, "sudo"), "#!/bin/sh\n"+
		"printf '%s\\n' '{\"network\":true,\"system\":true,\"host_network\":{\"interface\":\"eth0\"}}'\n")
	t.Setenv("PATH", bin)

	applier := &sudoApplier{
		path:    helper,
		toolctl: filepath.Join(dir, "gotham-toolctl"),
		payload: filepath.Join(dir, "payload"),
	}
	status, err := applier.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Capability != CapabilityHelperOutdated {
		t.Fatalf("old helper must report helper_outdated, got %q", status.Capability)
	}
}

func cleanupApplier(t *testing.T, dir, sudoBody string) *sudoApplier {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "gotham-hostctl")
	writeExec(t, helper, "#!/bin/sh\n")
	writeExec(t, filepath.Join(bin, "sudo"), sudoBody)
	t.Setenv("PATH", bin)
	return &sudoApplier{path: helper}
}

func TestCleanupStatusMapsCategoriesAndOutdatedHelper(t *testing.T) {
	dir := t.TempDir()
	applier := cleanupApplier(t, dir, "#!/bin/sh\n"+
		"case \"$3\" in\n"+
		"  status) printf '%s\\n' '{\"network\":true,\"system\":true,\"timezone\":\"UTC\",\"cleanup\":true}' ;;\n"+
		"  cleanup-status) printf '%s\\n' '{\"journal\":{\"supported\":true,\"bytes\":100},\"package_cache\":{\"supported\":true,\"manager\":\"dnf\",\"bytes\":50}}' ;;\n"+
		"esac\n")
	status, err := applier.CleanupStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Journal.Supported || status.Journal.Bytes != 100 {
		t.Fatalf("journal = %+v", status.Journal)
	}
	if !status.PackageCache.Supported || status.PackageCache.Manager != "dnf" || status.PackageCache.Bytes != 50 {
		t.Fatalf("package cache = %+v", status.PackageCache)
	}

	old := cleanupApplier(t, t.TempDir(), "#!/bin/sh\n"+
		"printf '%s\\n' '{\"network\":true,\"system\":true,\"timezone\":\"UTC\"}'\n")
	if _, err := old.CleanupStatus(context.Background()); !errors.Is(err, ErrHelperOutdated) {
		t.Fatalf("status without cleanup = %v, want helper outdated", err)
	}

	missing := &sudoApplier{path: filepath.Join(dir, "absent")}
	if _, err := missing.CleanupStatus(context.Background()); !errors.Is(err, ErrHelperOutdated) {
		t.Fatalf("missing helper = %v, want helper outdated", err)
	}
}

func TestCleanupHostMapsHelperExitCodes(t *testing.T) {
	dir := t.TempDir()
	stdinLog := filepath.Join(dir, "stdin")
	applier := cleanupApplier(t, dir, "#!/bin/sh\n"+
		"while IFS= read -r line; do printf '%s\\n' \"$line\" >> \"$STDIN_LOG\"; done\n"+
		"printf '%s\\n' '{\"before\":100,\"after\":40}'\n")
	t.Setenv("STDIN_LOG", stdinLog)

	reclaimed, err := applier.CleanupHost(context.Background(), "journal", 90*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 60 {
		t.Fatalf("reclaimed = %d, want 60", reclaimed)
	}
	data, err := os.ReadFile(stdinLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "min_age_seconds=5400" {
		t.Fatalf("cleanup-journal stdin = %q", got)
	}

	unsupported := cleanupApplier(t, t.TempDir(), "#!/bin/sh\nexit 3\n")
	if _, err := unsupported.CleanupHost(context.Background(), "journal", 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("exit 3 = %v, want unsupported", err)
	}
	old := cleanupApplier(t, t.TempDir(), "#!/bin/sh\nexit 2\n")
	if _, err := old.CleanupHost(context.Background(), "package_cache", 0); !errors.Is(err, ErrHelperOutdated) {
		t.Fatalf("exit 2 = %v, want helper outdated", err)
	}
	if _, err := applier.CleanupHost(context.Background(), "unknown", 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unknown category = %v, want unsupported", err)
	}
}
