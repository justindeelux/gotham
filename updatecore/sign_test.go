package updatecore

import (
	"crypto/ed25519"
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

// TestVerifierSet proves a key-set verifier accepts a signature from either the
// current or the pre-positioned next key, refuses an unknown key and tampered
// bytes, and fails closed on an empty set.
func TestVerifierSet(t *testing.T) {
	current, currentPrivate, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (current): %v", err)
	}
	next, nextPrivate, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (next): %v", err)
	}
	_, unknownPrivate, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (unknown): %v", err)
	}

	verifier, err := NewVerifierSet(current, next)
	if err != nil {
		t.Fatalf("NewVerifierSet: %v", err)
	}
	payload := []byte("gotham release manifest")

	for name, private := range map[string]ed25519.PrivateKey{"current": currentPrivate, "next": nextPrivate} {
		signer, err := NewSigner(private)
		if err != nil {
			t.Fatalf("NewSigner (%s): %v", name, err)
		}
		if err := verifier.Verify(payload, []byte(signer.SignBase64(payload))); err != nil {
			t.Fatalf("Verify signed by the %s key: %v", name, err)
		}
	}

	unknownSigner, err := NewSigner(unknownPrivate)
	if err != nil {
		t.Fatalf("NewSigner (unknown): %v", err)
	}
	if err := verifier.Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify with an unknown key = %v, want ErrBadSignature", err)
	}
	if err := verifier.Verify([]byte("tampered manifest"), unknownSigner.Sign(payload)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify tampered bytes = %v, want ErrBadSignature", err)
	}

	if _, err := NewVerifierSet(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("NewVerifierSet(empty) = %v, want ErrNoPublicKey", err)
	}
	if _, err := NewVerifierSet(current, make([]byte, 3)); err == nil {
		t.Fatal("NewVerifierSet(short key) = nil error, want failure")
	}
	var empty *Verifier
	if err := empty.Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("(*Verifier)(nil).Verify = %v, want ErrNoPublicKey", err)
	}
	if err := (&Verifier{}).Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Verifier{}.Verify = %v, want ErrNoPublicKey", err)
	}
}

// TestLoadPublicKeysRing proves the embedded current + next ring is loaded in
// order, that a next key without a current key and a malformed embedded key
// both fail closed, and that the development override applies only when nothing
// is embedded.
func TestLoadPublicKeysRing(t *testing.T) {
	originalCurrent, originalNext := PublicKey, NextPublicKey
	t.Cleanup(func() { PublicKey, NextPublicKey = originalCurrent, originalNext })

	current, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (current): %v", err)
	}
	next, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (next): %v", err)
	}
	envKey, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (env): %v", err)
	}
	t.Setenv(PublicKeyEnv, EncodePublicKeyBase64(envKey))

	// A next key without a current key fails closed: the ring cannot trust the
	// release that carried it.
	PublicKey = ""
	NextPublicKey = EncodePublicKeyBase64(next)
	if _, err := LoadPublicKeys(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("LoadPublicKeys(next only) = %v, want ErrNoPublicKey", err)
	}
	if _, err := LoadPublicKey(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("LoadPublicKey(next only) = %v, want ErrNoPublicKey", err)
	}

	// Both embedded: the ring is current then next, and the environment
	// override is ignored.
	PublicKey = EncodePublicKeyBase64(current)
	keys, err := LoadPublicKeys()
	if err != nil {
		t.Fatalf("LoadPublicKeys: %v", err)
	}
	if len(keys) != 2 || !keys[0].Equal(current) || !keys[1].Equal(next) {
		t.Fatalf("LoadPublicKeys = %d keys, want the embedded current + next ring", len(keys))
	}
	single, err := LoadPublicKey()
	if err != nil {
		t.Fatalf("LoadPublicKey: %v", err)
	}
	if !single.Equal(current) {
		t.Fatal("LoadPublicKey did not return the embedded current key")
	}

	// A malformed embedded key fails closed instead of being ignored.
	NextPublicKey = "not-a-key"
	if _, err := LoadPublicKeys(); err == nil {
		t.Fatal("LoadPublicKeys(malformed next) = nil error, want failure")
	}
	PublicKey = "not-a-key"
	NextPublicKey = ""
	if _, err := LoadPublicKeys(); err == nil {
		t.Fatal("LoadPublicKeys(malformed current) = nil error, want failure")
	}

	// Nothing embedded: the development override supplies a single-key ring.
	PublicKey = ""
	if keys, err = LoadPublicKeys(); err != nil || len(keys) != 1 || !keys[0].Equal(envKey) {
		t.Fatalf("LoadPublicKeys(dev) = (%d keys, %v), want the env key", len(keys), err)
	}

	// Neither embedded nor override fails closed.
	t.Setenv(PublicKeyEnv, "")
	if _, err := LoadPublicKeys(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("LoadPublicKeys(empty) = %v, want ErrNoPublicKey", err)
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
