package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/updates"
)

// updatesSandbox is the temp directory TestMain points every self-update path
// at. It is package state so TestUpdatePathsSandboxed can pin the wiring.
var updatesSandbox string

// TestMain disables the self-update feature and points every GOTHAM_UPDATE_*
// path at a temp directory before any test runs. Without it, a root run of this
// package constructs the real updates service (FEATURE_UPDATES defaults on) and
// its startup Recover runs against the host's production paths
// (/var/lib/gotham*, /var/lib/gotham-updater), mutating a real install.
//
// Tests that need the surface on (updates_routes_test.go) override these with
// t.Setenv, which restores them afterwards.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gotham-server-updates-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "server tests: create self-update sandbox: %v\n", err)
		os.Exit(1)
	}
	updatesSandbox = dir
	os.Setenv("FEATURE_UPDATES", "false")
	for name, value := range map[string]string{
		"GOTHAM_UPDATE_BINARY":  filepath.Join(dir, "bin", "gotham"),
		"GOTHAM_UPDATE_LOCK":    filepath.Join(dir, "update.lock"),
		"GOTHAM_UPDATE_PENDING": filepath.Join(dir, "update.pending"),
		"GOTHAM_UPDATE_STATUS":  filepath.Join(dir, "update.status"),
		"GOTHAM_UPDATE_SCRIPT":  filepath.Join(dir, "gotham-update"),
	} {
		os.Setenv(name, value)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestUpdatePathsSandboxed pins the TestMain wiring: every resolved self-update
// path must be inside the sandbox. If the wiring is removed (or a default leaks
// through), this fails loudly instead of silently running against a real
// install.
func TestUpdatePathsSandboxed(t *testing.T) {
	if updatesSandbox == "" {
		t.Fatal("TestMain did not set up the self-update sandbox")
	}
	prefix := updatesSandbox + string(os.PathSeparator)
	paths := map[string]string{
		"binary":  updates.BinaryPathFromEnv(),
		"lock":    updates.LockPathFromEnv(),
		"pending": updates.PendingPathFromEnv(),
		"status":  updates.StatusPathFromEnv(),
		"script":  updates.ScriptFromEnv(),
	}
	for name, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			t.Fatalf("self-update %s path %q is outside the test sandbox %q", name, path, updatesSandbox)
		}
	}
}
