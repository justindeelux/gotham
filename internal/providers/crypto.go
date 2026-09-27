package providers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// secretNonceSize is the AES-GCM nonce length. A fresh random nonce is
// prepended to every ciphertext so identical plaintext never yields identical
// stored bytes.
const secretNonceSize = 12

// errEmptySecret is returned when sealing or opening with no key material.
var errEmptySecret = errors.New("providers: encryption secret is empty")

// secretCipher seals provider credentials (client secret and tokens) at rest.
//
// NOTE: the key is derived with SHA-256, matching the SSH-key encryption in the
// servers package. Production should move to a memory-hard KDF with a
// per-deployment salt; that migration is shared work, not provider-specific.
type secretCipher struct {
	key []byte
}

// newSecretCipher derives a 32-byte AES-256 key from secret.
func newSecretCipher(secret string) *secretCipher {
	sum := sha256.Sum256([]byte(secret))
	return &secretCipher{key: sum[:]}
}

// seal encrypts plain and returns base64(nonce||ciphertext). Empty plaintext
// stays empty so "not connected" rows need no key.
func (c *secretCipher) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := c.gcm()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, secretNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("providers: generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// open reverses seal. Empty input returns empty; a wrong key or a corrupted
// value fails authentication and returns an error.
func (c *secretCipher) open(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	gcm, err := c.gcm()
	if err != nil {
		return "", err
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("providers: decode secret: %w", err)
	}
	if len(raw) < secretNonceSize {
		return "", errors.New("providers: decode secret: ciphertext is too short")
	}

	nonce, ciphertext := raw[:secretNonceSize], raw[secretNonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("providers: decrypt secret: %w", err)
	}
	return string(plain), nil
}

// gcm builds the AES-GCM AEAD, reporting a missing key.
func (c *secretCipher) gcm() (cipher.AEAD, error) {
	if c == nil || len(c.key) == 0 {
		return nil, errEmptySecret
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("providers: new cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// SealSecret encrypts plain with AES-256-GCM using the key derived from secret
// and returns base64(nonce||ciphertext); empty plaintext stays empty.
//
// It is the exported entry point to this package's cipher, so sibling domain
// packages (deploy seals application secrets) reuse the single AES-256-GCM
// implementation instead of growing a second one. The cost of re-deriving the
// key per call is one SHA-256 hash.
func SealSecret(secret, plain string) (string, error) {
	return newSecretCipher(secret).seal(plain)
}

// OpenSecret reverses SealSecret. A wrong secret, a corrupted value or
// truncated ciphertext fails authentication and returns an error.
func OpenSecret(secret, encoded string) (string, error) {
	return newSecretCipher(secret).open(encoded)
}

// randomSecret returns a base64-encoded 32-byte random secret, used when no
// GOTHAM_SECRET_KEY is configured so provider credentials are never stored in
// the clear (at the cost of not surviving a restart).
func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(err.Error()))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
