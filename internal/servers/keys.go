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

const keyNonceSize = 12

var ErrEmptySecret = errors.New("servers: encryption secret is empty")

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

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
