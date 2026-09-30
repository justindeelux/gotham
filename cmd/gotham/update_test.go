package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckBinaryOwner covers the CLI ownership guard (M3): a binary owned by
// another uid is refused with a clear message naming the correct invocation,
// while the owning uid and a missing binary are allowed.
func TestCheckBinaryOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gotham")
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if err := checkBinaryOwner(path, os.Geteuid(), "apply"); err != nil {
		t.Fatalf("checkBinaryOwner(same uid) = %v, want nil", err)
	}

	err := checkBinaryOwner(path, os.Geteuid()+1, "apply")
	if err == nil {
		t.Fatal("checkBinaryOwner(mismatched uid) = nil, want an error")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "sudo -u") {
		t.Fatalf("error %q must name the path and the correct invocation", err)
	}

	if err := checkBinaryOwner(filepath.Join(t.TempDir(), "missing"), os.Geteuid()+1, "apply"); err != nil {
		t.Fatalf("checkBinaryOwner(missing binary) = %v, want nil", err)
	}
}
