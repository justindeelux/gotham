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

const secretNonceSize = 12

var errEmptySecret = errors.New("providers: encryption secret is empty")

type secretCipher struct {
	key []byte
}

func newSecretCipher(secret string) *secretCipher {
	sum := sha256.Sum256([]byte(secret))
	return &secretCipher{key: sum[:]}
}

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

func SealSecret(secret, plain string) (string, error) {
	if secret == "" && plain != "" {
		return "", errEmptySecret
	}
	return newSecretCipher(secret).seal(plain)
}

func OpenSecret(secret, encoded string) (string, error) {
	if secret == "" && encoded != "" {
		return "", errEmptySecret
	}
	return newSecretCipher(secret).open(encoded)
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(err.Error()))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
