package updatecore

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

// PublicKeyEnv optionally supplies a public key for development builds, where
// no key is embedded. It is ignored whenever a key is embedded at build time:
// the release trust anchor is fixed in the binary and cannot be replaced by the
// service environment.
const PublicKeyEnv = "GOTHAM_UPDATE_PUBLIC_KEY"

// PublicKey is the release-signing Ed25519 public key, base64 (standard)
// encoded, injected at build time:
//
//	-ldflags "-X github.com/justindeelux/gotham/updatecore.PublicKey=<base64>"
//
// The symbol lives in updatecore (the package moved out of internal/updates);
// Go silently ignores -X for a missing symbol, so a release wired from a stale
// path would ship a keyless binary and silently disable updates. INFRA-9.1 must
// target updatecore.PublicKey and assert the embedded key is non-empty at build
// time (`cmd/signer keygen` prints the exact ldflags value).
//
// It is empty in development, which disables applying updates. The private key
// is never embedded, read or logged by this package.
var PublicKey = ""

// NextPublicKey is the pre-positioned release-signing Ed25519 public key for the
// next rotation, base64 (standard) encoded, injected at build time:
//
//	-ldflags "-X github.com/justindeelux/gotham/updatecore.NextPublicKey=<base64>"
//
// It may be empty (no rotation pre-positioned). A binary with an embedded next
// key accepts releases signed by either the current or the next key, so a
// rotation can ship through a release signed by the old key before the new key
// is needed. An old binary cannot learn a key retroactively, which is why the
// next key must ship in a release before the signing key is promoted.
//
// Like PublicKey, it is base64 raw 32 bytes (`cmd/signer keygen` prints the
// value). A malformed embedded key fails closed rather than being ignored.
var NextPublicKey = ""

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

// Verifier verifies detached Ed25519 signatures against a release key set: the
// current key, plus the pre-positioned next key when one is embedded. A release
// verifies when any key in the set signed it.
type Verifier struct {
	keys []ed25519.PublicKey
}

// NewVerifier wraps a single release public key. It fails closed on a
// wrong-sized key and keeps the single-key callers (cmd/signer verify) working.
func NewVerifier(key ed25519.PublicKey) (*Verifier, error) {
	return NewVerifierSet(key)
}

// NewVerifierSet wraps a release key set (the ring). It fails closed on an
// empty set or a wrong-sized key, so a binary that cannot prove a signature
// refuses the update rather than accepting it.
func NewVerifierSet(keys ...ed25519.PublicKey) (*Verifier, error) {
	if len(keys) == 0 {
		return nil, ErrNoPublicKey
	}
	ring := make([]ed25519.PublicKey, 0, len(keys))
	for _, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			return nil, errors.New("updates: invalid Ed25519 public key")
		}
		ring = append(ring, key)
	}
	return &Verifier{keys: ring}, nil
}

// Verify checks that sig (standard base64, or the raw 64 bytes) is a valid
// Ed25519 signature over data by any key in the set. An empty set fails closed
// with ErrNoPublicKey; a signature that no key produced fails with
// ErrBadSignature.
func (v *Verifier) Verify(data, sig []byte) error {
	if v == nil || len(v.keys) == 0 {
		return ErrNoPublicKey
	}
	decoded, err := decodeSignature(sig)
	if err != nil {
		return err
	}
	for _, key := range v.keys {
		if ed25519.Verify(key, data, decoded) {
			return nil
		}
	}
	return ErrBadSignature
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

// LoadPublicKeys resolves the release key ring: the embedded current key
// (PublicKey) plus the optional pre-positioned next key (NextPublicKey), in
// that order. An embedded ring is authoritative and the environment override is
// ignored; only when nothing is embedded does GOTHAM_UPDATE_PUBLIC_KEY apply
// (development builds). With neither set it fails closed.
//
// The embedded current key is required whenever anything is embedded: a binary
// carrying a next key but no current key cannot have been signed by the release
// it trusts, so it fails closed instead of trusting the next key alone. A
// malformed embedded key (current or next) also fails closed.
func LoadPublicKeys() ([]ed25519.PublicKey, error) {
	embedded := strings.TrimSpace(PublicKey)
	next := strings.TrimSpace(NextPublicKey)
	if embedded == "" && next == "" {
		dev, err := ParsePublicKey(strings.TrimSpace(os.Getenv(PublicKeyEnv)))
		if err != nil {
			return nil, err
		}
		return []ed25519.PublicKey{dev}, nil
	}
	if embedded == "" {
		return nil, fmt.Errorf("%w: embedded next release key without a current key", ErrNoPublicKey)
	}
	current, err := ParsePublicKey(embedded)
	if err != nil {
		return nil, fmt.Errorf("updates: embedded current public key: %w", err)
	}
	ring := []ed25519.PublicKey{current}
	if next != "" {
		nextKey, err := ParsePublicKey(next)
		if err != nil {
			return nil, fmt.Errorf("updates: embedded next public key: %w", err)
		}
		if !nextKey.Equal(current) {
			ring = append(ring, nextKey)
		}
	}
	return ring, nil
}

// LoadPublicKey resolves the single release public key (the embedded current
// key, or the development override). It is the compatibility path for
// single-key callers; LoadPublicKeys is the full ring.
func LoadPublicKey() (ed25519.PublicKey, error) {
	keys, err := LoadPublicKeys()
	if err != nil {
		return nil, err
	}
	return keys[0], nil
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
