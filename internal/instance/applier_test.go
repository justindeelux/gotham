package instance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
