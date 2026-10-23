package server

import (
	"os"
	"testing"
)

func TestEnsureSecretKey(t *testing.T) {
	if got, generated := ensureSecretKey("configured-secret"); got != "configured-secret" || generated {
		t.Fatalf("ensureSecretKey(configured) = (%q, %v), want the value unchanged", got, generated)
	}

	got, generated := ensureSecretKey("")
	if !generated {
		t.Fatal("ensureSecretKey(\"\") did not report a generated key")
	}
	if got == "" {
		t.Fatal("ensureSecretKey(\"\") returned an empty key")
	}

	if got, generated := ensureSecretKey("   "); !generated || got == "" || got == "   " {
		t.Fatalf("ensureSecretKey(whitespace) = (%q, %v), want a fresh generated key", got, generated)
	}

	other, _ := ensureSecretKey("")
	if other == got {
		t.Fatal("ensureSecretKey(\"\") returned the same ephemeral key twice")
	}
}

func TestResolveSecretKeyPersists(t *testing.T) {
	dir := t.TempDir()
	first, generated, persisted := ResolveSecretKey("", dir)
	if generated || !persisted || first == "" {
		t.Fatalf("first = (%q, %v, %v), want a persisted key", first, generated, persisted)
	}
	if second, _, _ := ResolveSecretKey("", dir); second != first {
		t.Fatalf("second key %q != first %q after restart", second, first)
	}
	if got, _, persisted := ResolveSecretKey("configured", dir); got != "configured" || persisted {
		t.Fatalf("configured key not preferred: (%q, %v)", got, persisted)
	}
	if _, generated, _ := ResolveSecretKey("", ""); !generated {
		t.Fatal("no directory should fall back to an ephemeral key")
	}
}

func TestResolveSecretKeyEphemeralIsShared(t *testing.T) {
	a, genA, _ := ResolveSecretKey("", "")
	b, genB, _ := ResolveSecretKey("", "")
	if !genA || !genB || a != b {
		t.Fatalf("ephemeral keys differ: %q vs %q", a, b)
	}
}

func TestResolveSecretKeyIgnoresCWDFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("secret.key", []byte("stray\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, generated, _ := ResolveSecretKey("", ""); got == "stray" || !generated {
		t.Fatalf("adopted the working-directory file: (%q, %v)", got, generated)
	}
}
