package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/justindeelux/gotham/internal/servers"
)

// TestRunCAInitCreatesLoadableCA proves `gotham ca init` provisions a CA the
// serve path (servers.LoadAuthority) can load, writes it 0600, and is
// idempotent (a second run never replaces the CA).
func TestRunCAInitCreatesLoadableCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	t.Setenv("GOTHAM_CA_DIR", dir)

	if code := runCA([]string{"init"}); code != exitOK {
		t.Fatalf("runCA init = %d, want %d", code, exitOK)
	}

	for _, name := range []string{caCertFileName, "ca.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s permissions = %o, want 600", name, perm)
		}
	}

	authority, err := servers.LoadAuthority(dir)
	if err != nil {
		t.Fatalf("LoadAuthority after init: %v", err)
	}
	if authority == nil {
		t.Fatal("LoadAuthority after init returned nil, want a CA")
	}

	// A second init must keep the same CA, not rotate it.
	if code := runCA([]string{"init"}); code != exitOK {
		t.Fatalf("runCA init (second) = %d, want %d", code, exitOK)
	}
	reloaded, err := servers.LoadAuthority(dir)
	if err != nil {
		t.Fatalf("LoadAuthority after second init: %v", err)
	}
	if !bytes.Equal(authority.CACertPEM(), reloaded.CACertPEM()) {
		t.Fatal("second ca init replaced the CA certificate")
	}
}

// TestRunCAUsage covers the argument handling so an unknown verb is a usage
// error rather than a silent no-op.
func TestRunCAUsage(t *testing.T) {
	if code := runCA(nil); code != exitUsage {
		t.Errorf("runCA(nil) = %d, want %d", code, exitUsage)
	}
	if code := runCA([]string{"bogus"}); code != exitUsage {
		t.Errorf("runCA(bogus) = %d, want %d", code, exitUsage)
	}
	if code := runCA([]string{"help"}); code != exitOK {
		t.Errorf("runCA(help) = %d, want %d", code, exitOK)
	}
}
