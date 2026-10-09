package server

import "testing"

// TestEnsureSecretKey pins the startup resolution: a configured secret is used
// verbatim, an empty one yields a fresh non-empty ephemeral key (never the
// public empty string), and two resolutions never repeat.
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

// TestResolveSecretKeyPersists checks an unconfigured key survives a restart:
// the second resolution reads back the file the first one wrote.
func TestResolveSecretKeyPersists(t *testing.T) {
	dir := t.TempDir()
	first, generated, persisted := resolveSecretKey("", dir)
	if generated || !persisted || first == "" {
		t.Fatalf("first = (%q, %v, %v), want a persisted key", first, generated, persisted)
	}
	if second, _, _ := resolveSecretKey("", dir); second != first {
		t.Fatalf("second key %q != first %q after restart", second, first)
	}
	if got, _, persisted := resolveSecretKey("configured", dir); got != "configured" || persisted {
		t.Fatalf("configured key not preferred: (%q, %v)", got, persisted)
	}
	if _, generated, _ := resolveSecretKey("", ""); !generated {
		t.Fatal("no directory should fall back to an ephemeral key")
	}
}
