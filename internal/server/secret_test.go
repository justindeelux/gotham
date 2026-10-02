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

	other, _ := ensureSecretKey("")
	if other == got {
		t.Fatal("ensureSecretKey(\"\") returned the same ephemeral key twice")
	}
}
