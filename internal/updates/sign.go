package updates

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// SignatureSize is the fixed size of an Ed25519 signature.
const SignatureSize = ed25519.SignatureSize

// PublicKeyEnv optionally overrides the embedded public key with an inline
// value (PEM or base64-encoded raw key). It exists for local development and
// tests; release builds embed the key instead.
const PublicKeyEnv = "GOTHAM_UPDATE_PUBLIC_KEY"

// PublicKey is the release-signing Ed25519 public key, base64 (standard)
// encoded, injected at build time:
//
//	-ldflags "-X github.com/justindeelux/gotham/internal/updates.PublicKey=<base64>"
//
// It is empty in development, which disables applying updates. The private key
// is never embedded, read or logged by this package.
var PublicKey = ""

// ErrNoPublicKey is returned when no usable release public key is configured.
var ErrNoPublicKey = errors.New("updates: no release public key configured")

// ErrBadSignature is returned when a detached signature does not verify.
var ErrBadSignature = errors.New("updates: invalid signature")

// Signer signs release artifacts with an Ed25519 private key.
type Signer struct {
	key ed25519.PrivateKey
}

// NewSigner wraps a private key. It fails closed on a wrong-sized key.
func NewSigner(key ed25519.PrivateKey) (*Signer, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("updates: invalid Ed25519 private key")
	}
	return &Signer{key: key}, nil
}

// Sign returns the raw Ed25519 signature over data.
func (s *Signer) Sign(data []byte) []byte {
	return ed25519.Sign(s.key, data)
}

// SignBase64 returns the standard-base64 encoding of Sign(data).
func (s *Signer) SignBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(s.Sign(data))
}

// PublicKey returns the matching public key.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

// Verifier verifies detached Ed25519 signatures with a release public key.
type Verifier struct {
	key ed25519.PublicKey
}

// NewVerifier wraps a public key. It fails closed on a wrong-sized key.
func NewVerifier(key ed25519.PublicKey) (*Verifier, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("updates: invalid Ed25519 public key")
	}
	return &Verifier{key: key}, nil
}

// Verify checks that sig (standard base64, or the raw 64 bytes) is a valid
// Ed25519 signature over data.
func (v *Verifier) Verify(data, sig []byte) error {
	decoded, err := decodeSignature(sig)
	if err != nil {
		return err
	}
	if !ed25519.Verify(v.key, data, decoded) {
		return ErrBadSignature
	}
	return nil
}

// GenerateKey creates a fresh Ed25519 keypair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// MarshalPrivateKeyPEM encodes a private key as a PKCS#8 PEM block.
func MarshalPrivateKeyPEM(key ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("updates: marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// MarshalPublicKeyPEM encodes a public key as a PKIX PEM block.
func MarshalPublicKeyPEM(key ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("updates: marshal public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ParsePrivateKeyPEM parses a PKCS#8 PEM Ed25519 private key.
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("updates: no PEM block in private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("updates: parse private key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("updates: private key is not Ed25519")
	}
	return key, nil
}

// ParsePublicKeyPEM parses a PKIX PEM Ed25519 public key.
func ParsePublicKeyPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("updates: no PEM block in public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("updates: parse public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("updates: public key is not Ed25519")
	}
	return key, nil
}

// ParsePublicKey accepts a PKIX PEM block, a base64-encoded raw 32-byte key, a
// base64-encoded PEM block, or a hex-encoded raw key.
func ParsePublicKey(raw string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrNoPublicKey
	}
	if strings.Contains(trimmed, "-----BEGIN") {
		return ParsePublicKeyPEM([]byte(trimmed))
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		if len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
		if key, pemErr := ParsePublicKeyPEM(decoded); pemErr == nil {
			return key, nil
		}
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == ed25519.PublicKeySize {
		return ed25519.PublicKey(decoded), nil
	}
	return nil, errors.New("updates: unrecognised public key format")
}

// LoadPublicKey resolves the embedded release public key, letting the
// GOTHAM_UPDATE_PUBLIC_KEY environment override it for local development.
func LoadPublicKey() (ed25519.PublicKey, error) {
	raw := strings.TrimSpace(os.Getenv(PublicKeyEnv))
	if raw == "" {
		raw = strings.TrimSpace(PublicKey)
	}
	return ParsePublicKey(raw)
}

// EncodePublicKeyBase64 renders the raw public key for ldflags embedding.
func EncodePublicKeyBase64(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

// decodeSignature accepts a standard-base64 signature (the on-disk ".sig"
// format) or the bare 64 raw bytes.
func decodeSignature(sig []byte) ([]byte, error) {
	if len(sig) == SignatureSize {
		return sig, nil
	}
	trimmed := strings.TrimSpace(string(sig))
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil || len(decoded) != SignatureSize {
		return nil, ErrBadSignature
	}
	return decoded, nil
}
