package instance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeExec writes an executable shell script at path with the given body.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestProbeRepairViaSudoPolicy is the regression for the round-4 H1 bug: the
// privilege probe must not stat /etc/sudoers.d (root 0750, unreadable by the
// service user) — it asks the sudo policy whether the exact pinned repair
// command is permitted, which works unprivileged, absolute-pathed, with no
// request data. A fake sudo covers rule-present / rule-absent / sudo-missing.
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
	// The helper path does not exist, so Probe reports Missing and then probes
	// the repair policy.
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

	// No sudo on PATH at all: the probe fails closed, never an error.
	t.Setenv("PATH", dir)
	if h := applier.Probe(context.Background()); h.Repair {
		t.Fatalf("missing sudo: %+v, want repair=false", h)
	}

	// Without the payload the probe short-circuits before sudo.
	if err := os.Remove(filepath.Join(payload, "gotham-hostctl")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if h := applier.Probe(context.Background()); h.Repair {
		t.Fatalf("no payload: %+v, want repair=false", h)
	}
}
