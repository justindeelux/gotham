package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordVerifyRoundtrip(t *testing.T) {
	const password = "correct horse battery staple"

	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("encoded hash %q does not use the expected format", encoded)
	}

	ok, err := VerifyPassword(encoded, password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword = false, want true for the correct password")
	}
}

func TestVerifyPasswordWrongPassword(t *testing.T) {
	encoded, err := HashPassword("the-right-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword(encoded, "the-wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword = true, want false for a wrong password")
	}
}

func TestVerifyPasswordTamperedHash(t *testing.T) {
	encoded, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	tampered := encoded[:len(encoded)-1] + flipBase64(encoded[len(encoded)-1])

	ok, err := VerifyPassword(tampered, "secret-password")
	if err == nil && ok {
		t.Fatal("tampered hash verified successfully")
	}
}

func TestVerifyPasswordInvalidEncoding(t *testing.T) {
	for name, encoded := range map[string]string{
		"empty":      "",
		"garbage":    "not-a-hash",
		"wrong algo": "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		"bad base64": "$argon2id$v=19$m=65536,t=3,p=4$!!!$aGFzaA",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyPassword(encoded, "whatever"); err == nil {
				t.Fatalf("VerifyPassword(%q) = nil error, want error", encoded)
			}
		})
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); !errors.Is(err, ErrValidation) {
		t.Fatalf("HashPassword(\"\") error = %v, want ErrValidation", err)
	}
}

func TestDecodeHashParsesParameters(t *testing.T) {
	encoded, err := HashPassword("another-secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	params, salt, key, err := decodeHash(encoded)
	if err != nil {
		t.Fatalf("decodeHash: %v", err)
	}
	if params.memory != argonMemory || params.time != argonTime || params.threads != argonThreads {
		t.Errorf("params = %+v, want m=%d,t=%d,p=%d", params, argonMemory, argonTime, argonThreads)
	}
	if len(salt) != argonSaltLen {
		t.Errorf("salt length = %d, want %d", len(salt), argonSaltLen)
	}
	if len(key) != int(argonKeyLen) {
		t.Errorf("key length = %d, want %d", len(key), argonKeyLen)
	}
}

// flipBase64 returns a different, still-valid base64 character.
func flipBase64(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
