package providers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// PKCE (RFC 7636) parameters for the authorization-code flow. The SPA opens
// the provider's authorize page directly, so the code it receives is bound to
// an S256 challenge whose verifier never leaves the control plane: the
// verifier is minted in Authorize, stored beside the one-time state and
// consumed in Connect.
const (
	// pkceVerifierBytes is the entropy of a generated verifier: 32 bytes
	// encode to 43 base64url characters, inside the 43-128 range RFC 7636
	// requires.
	pkceVerifierBytes = 32
)

// generatePKCEVerifier mints a high-entropy code verifier: base64url (no
// padding) over 32 random bytes, so the value is always 43 characters of the
// RFC 7636 unreserved set.
func generatePKCEVerifier() (string, error) {
	buf := make([]byte, pkceVerifierBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("providers: generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pkceChallenge derives the S256 code challenge for verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
