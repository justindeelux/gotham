package servers

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

// keyNonceSize is the AES-GCM nonce length in bytes. A fresh random nonce is
// prepended to every ciphertext so the same plaintext never encrypts to the
// same stored value.
const keyNonceSize = 12

// ErrEmptySecret is returned when encryption is attempted without a secret.
var ErrEmptySecret = errors.New("servers: encryption secret is empty")

// deriveKey turns the configured secret into a 32-byte AES-256 key.
//
// NOTE: SHA-256 is a deterministic derivation, which is acceptable at this
// stage. Production should replace it with a memory-hard KDF (e.g. Argon2id or
// scrypt) and a per-deployment salt so that a weak GOTHAM_SECRET_KEY cannot be
// brute-forced from the ciphertext alone.
func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// EncryptKey encrypts a PEM private key with AES-256-GCM and returns a
// base64-encoded string of the form nonce||ciphertext. The plaintext never
// leaves this function in the clear.
func EncryptKey(plainPEM, secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	if plainPEM == "" {
		return "", fmt.Errorf("%w: private key is empty", ErrValidation)
	}

	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, keyNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plainPEM), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptKey reverses EncryptKey. A wrong secret or corrupted ciphertext fails
// authentication and returns an error.
func DecryptKey(encoded, secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode key: %w", err)
	}
	if len(raw) < keyNonceSize {
		return "", errors.New("decode key: ciphertext is too short")
	}

	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}

	nonce, ciphertext := raw[:keyNonceSize], raw[keyNonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt key: %w", err)
	}
	return string(plain), nil
}

// newGCM builds the AES-256-GCM AEAD for secret.
func newGCM(secret string) (cipher.AEAD, error) {
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return gcm, nil
}
