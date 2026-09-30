package updatecore

import (
	"errors"
	"strings"
	"testing"
)

// TestSignVerifyRoundtrip proves a signature over bytes verifies with the
// matching public key, and that a wrong key or tampered bytes fail.
func TestSignVerifyRoundtrip(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewSigner(privateKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	payload := []byte("gotham release artifact")
	signature := []byte(signer.SignBase64(payload))

	if err := verifier.Verify(payload, signature); err != nil {
		t.Fatalf("Verify valid signature: %v", err)
	}
	if err := verifier.Verify(payload, signer.Sign(payload)); err != nil {
		t.Fatalf("Verify raw signature: %v", err)
	}

	otherPublic, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (other): %v", err)
	}
	otherVerifier, err := NewVerifier(otherPublic)
	if err != nil {
		t.Fatalf("NewVerifier (other): %v", err)
	}
	if err := otherVerifier.Verify(payload, signature); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify with wrong key = %v, want ErrBadSignature", err)
	}

	tampered := []byte("gotham release art!fact")
	if err := verifier.Verify(tampered, signature); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify tampered bytes = %v, want ErrBadSignature", err)
	}

	if err := verifier.Verify(payload, []byte("not-base64-!!")); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify malformed signature = %v, want ErrBadSignature", err)
	}
}

// TestPublicKeyFormats proves the public key round-trips through PEM and the
// base64/hex forms accepted for embedding.
func TestPublicKeyFormats(t *testing.T) {
	publicKey, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	pemBytes, err := MarshalPublicKeyPEM(publicKey)
	if err != nil {
		t.Fatalf("MarshalPublicKeyPEM: %v", err)
	}
	parsed, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("ParsePublicKeyPEM: %v", err)
	}
	if !parsed.Equal(publicKey) {
		t.Fatal("PEM round-trip changed the public key")
	}

	for name, encoded := range map[string]string{
		"pem":    string(pemBytes),
		"base64": EncodePublicKeyBase64(publicKey),
	} {
		got, err := ParsePublicKey(encoded)
		if err != nil {
			t.Fatalf("ParsePublicKey(%s): %v", name, err)
		}
		if !got.Equal(publicKey) {
			t.Fatalf("ParsePublicKey(%s) changed the public key", name)
		}
	}

	if _, err := ParsePublicKey("   "); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("ParsePublicKey(empty) = %v, want ErrNoPublicKey", err)
	}
	if _, err := ParsePublicKey(strings.Repeat("x", 100)); err == nil {
		t.Fatal("ParsePublicKey(garbage) = nil error, want failure")
	}
}

// TestLoadPublicKeyPrecedence proves the embedded key is the trust anchor: the
// environment override is ignored whenever a key is embedded, and is only used
// for development builds without one.
func TestLoadPublicKeyPrecedence(t *testing.T) {
	embedded, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	envKey, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (env): %v", err)
	}
	original := PublicKey
	t.Cleanup(func() { PublicKey = original })
	t.Setenv(PublicKeyEnv, EncodePublicKeyBase64(envKey))

	// Embedded key wins; the environment cannot replace it.
	PublicKey = EncodePublicKeyBase64(embedded)
	got, err := LoadPublicKey()
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}
	if !got.Equal(embedded) {
		t.Fatal("environment override replaced the embedded trust anchor")
	}

	// Dev build with no embedded key: the environment applies.
	PublicKey = ""
	got, err = LoadPublicKey()
	if err != nil {
		t.Fatalf("LoadPublicKey (dev): %v", err)
	}
	if !got.Equal(envKey) {
		t.Fatal("dev environment key was not used")
	}

	// Neither set fails closed.
	t.Setenv(PublicKeyEnv, "")
	if _, err := LoadPublicKey(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("LoadPublicKey(empty) = %v, want ErrNoPublicKey", err)
	}
}

// TestPrivateKeyPEMRoundtrip proves a PKCS#8 private key survives a PEM
// round-trip and still signs verifiably.
func TestPrivateKeyPEMRoundtrip(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pemBytes, err := MarshalPrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatalf("MarshalPrivateKeyPEM: %v", err)
	}
	parsed, err := ParsePrivateKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	signer, err := NewSigner(parsed)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if !signer.PublicKey().Equal(publicKey) {
		t.Fatal("Signer.PublicKey does not match the original key")
	}
	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	payload := []byte("payload")
	if err := verifier.Verify(payload, signer.Sign(payload)); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, err := NewSigner(make([]byte, 3)); err == nil {
		t.Fatal("NewSigner(short key) = nil error, want failure")
	}
	if _, err := NewVerifier(make([]byte, 3)); err == nil {
		t.Fatal("NewVerifier(short key) = nil error, want failure")
	}
	if _, err := ParsePrivateKeyPEM([]byte("not a pem")); err == nil {
		t.Fatal("ParsePrivateKeyPEM(garbage) = nil error, want failure")
	}
	if _, err := ParsePublicKeyPEM([]byte("not a pem")); err == nil {
		t.Fatal("ParsePublicKeyPEM(garbage) = nil error, want failure")
	}
}
