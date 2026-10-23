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

func TestProbeRepairViaSudoPolicy(t *testing.T) {
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
	if h := applier.Probe(context.Background()); h.Helper != HelperMissing || !h.Repair {
		t.Fatalf("permitted rule: host=%+v, want missing + repair", h)
	}

	writeExec(t, sudo, "#!/bin/sh\n# not permitted\nexit 1\n")
	if h := applier.Probe(context.Background()); h.Repair {
		t.Fatalf("refused rule: %+v, want repair=false", h)
	}

	t.Setenv("PATH", dir)
	if h := applier.Probe(context.Background()); h.Repair {
		t.Fatalf("missing sudo: %+v, want repair=false", h)
	}

	if err := os.Remove(filepath.Join(payload, "gotham-hostctl")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if h := applier.Probe(context.Background()); h.Repair {
		t.Fatalf("no payload: %+v, want repair=false", h)
	}
}
