package providers

import (
	"errors"
	"testing"
)

// TestSealSecretRejectsEmptyKey pins the guard that keeps the publicly
// derivable SHA-256("") key out of the credential path: sealing or opening
// actual data with no configured secret fails instead of silently using it,
// while empty values still round-trip as empty so "not connected" rows need
// no key.
func TestSealSecretRejectsEmptyKey(t *testing.T) {
	if _, err := SealSecret("", "value"); !errors.Is(err, errEmptySecret) {
		t.Fatalf("SealSecret with an empty secret = %v, want errEmptySecret", err)
	}
	if _, err := OpenSecret("", "ciphertext"); !errors.Is(err, errEmptySecret) {
		t.Fatalf("OpenSecret with an empty secret = %v, want errEmptySecret", err)
	}
	if got, err := SealSecret("", ""); err != nil || got != "" {
		t.Fatalf("SealSecret empty plaintext = (%q, %v), want an empty result", got, err)
	}
	if got, err := OpenSecret("", ""); err != nil || got != "" {
		t.Fatalf("OpenSecret empty ciphertext = (%q, %v), want an empty result", got, err)
	}
}
