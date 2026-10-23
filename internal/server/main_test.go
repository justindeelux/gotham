package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/updates"
)

var updatesSandbox string

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
