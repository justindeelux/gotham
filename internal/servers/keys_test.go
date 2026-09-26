package servers

import (
	"errors"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	const secret = "unit-test-secret"
	const plain = "-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZS1rZXk=\n-----END OPENSSH PRIVATE KEY-----\n"

	encrypted, err := EncryptKey(plain, secret)
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	if encrypted == plain {
		t.Fatal("EncryptKey returned the plaintext unchanged")
	}

	decrypted, err := DecryptKey(encrypted, secret)
	if err != nil {
		t.Fatalf("DecryptKey: %v", err)
	}
	if decrypted != plain {
		t.Fatalf("roundtrip mismatch: got %q, want %q", decrypted, plain)
	}
}

func TestEncryptKeyUsesFreshNonce(t *testing.T) {
	const secret = "unit-test-secret"
	const plain = "same-plaintext"

	first, err := EncryptKey(plain, secret)
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	second, err := EncryptKey(plain, secret)
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	if first == second {
		t.Fatal("EncryptKey produced identical ciphertext for two calls; nonce was reused")
	}
}

func TestDecryptKeyWithWrongSecretFails(t *testing.T) {
	encrypted, err := EncryptKey("sensitive-pem", "secret-one")
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	if _, err := DecryptKey(encrypted, "secret-two"); err == nil {
		t.Fatal("DecryptKey with the wrong secret = nil error, want authentication failure")
	}
}

func TestEncryptKeyValidation(t *testing.T) {
	if _, err := EncryptKey("pem", ""); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("EncryptKey with empty secret = %v, want ErrEmptySecret", err)
	}
	if _, err := EncryptKey("", "secret"); !errors.Is(err, ErrValidation) {
		t.Fatalf("EncryptKey with empty plaintext = %v, want ErrValidation", err)
	}
}

func TestDecryptKeyRejectsMalformedCiphertext(t *testing.T) {
	if _, err := DecryptKey("not-base64!!", "secret"); err == nil {
		t.Fatal("DecryptKey with invalid base64 = nil error, want error")
	}
	if _, err := DecryptKey("AAAA", "secret"); err == nil {
		t.Fatal("DecryptKey with a short ciphertext = nil error, want error")
	}
}
