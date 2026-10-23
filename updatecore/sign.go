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

const SignatureSize = ed25519.SignatureSize

const PublicKeyEnv = "GOTHAM_UPDATE_PUBLIC_KEY"

var PublicKey = ""

var NextPublicKey = ""

var ErrNoPublicKey = errors.New("updates: no release public key configured")

var ErrBadSignature = errors.New("updates: invalid signature")

type Signer struct {
	key ed25519.PrivateKey
}

func NewSigner(key ed25519.PrivateKey) (*Signer, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("updates: invalid Ed25519 private key")
	}
	return &Signer{key: key}, nil
}

func (s *Signer) Sign(data []byte) []byte {
	return ed25519.Sign(s.key, data)
}

func (s *Signer) SignBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(s.Sign(data))
}

func (s *Signer) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

type Verifier struct {
	keys []ed25519.PublicKey
}

func NewVerifier(key ed25519.PublicKey) (*Verifier, error) {
	return NewVerifierSet(key)
}

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

func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func MarshalPrivateKeyPEM(key ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("updates: marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func MarshalPublicKeyPEM(key ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("updates: marshal public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

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

func HasEmbeddedKey() bool {
	return strings.TrimSpace(PublicKey) != "" || strings.TrimSpace(NextPublicKey) != ""
}

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

func LoadPublicKey() (ed25519.PublicKey, error) {
	keys, err := LoadPublicKeys()
	if err != nil {
		return nil, err
	}
	return keys[0], nil
}

func EncodePublicKeyBase64(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

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
