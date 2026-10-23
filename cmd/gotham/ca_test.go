package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/justindeelux/gotham/internal/servers"
)

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

func TestRunCAInitPersistsHosts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	t.Setenv("GOTHAM_CA_DIR", dir)

	if code := runCA([]string{"init", "--host", "cp.example.com", "--host=192.0.2.10,cp2.example.com"}); code != exitOK {
		t.Fatalf("runCA init --host = %d, want %d", code, exitOK)
	}
	hosts, err := servers.LoadHosts(dir)
	if err != nil {
		t.Fatalf("LoadHosts: %v", err)
	}
	want := []string{"cp.example.com", "192.0.2.10", "cp2.example.com"}
	if len(hosts) != len(want) {
		t.Fatalf("LoadHosts = %v, want %v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Fatalf("LoadHosts = %v, want %v", hosts, want)
		}
	}

	if code := runCA([]string{"init", "--bogus"}); code != exitUsage {
		t.Errorf("runCA init --bogus = %d, want %d", code, exitUsage)
	}
	if code := runCA([]string{"init", "--host"}); code != exitUsage {
		t.Errorf("runCA init --host (no value) = %d, want %d", code, exitUsage)
	}
}
