package providers

import (
	"testing"
)

func TestSecretCipherRoundtrip(t *testing.T) {
	c := newSecretCipher("a-strong-secret")

	sealed, err := c.seal("gho_plaintext")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed == "gho_plaintext" {
		t.Fatal("seal returned the plaintext unchanged")
	}

	opened, err := c.open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != "gho_plaintext" {
		t.Errorf("open = %q, want gho_plaintext", opened)
	}
}

func TestSecretCipherEmpty(t *testing.T) {
	c := newSecretCipher("secret")

	sealed, err := c.seal("")
	if err != nil || sealed != "" {
		t.Fatalf("seal(empty) = (%q, %v), want (\"\", nil)", sealed, err)
	}
	opened, err := c.open("")
	if err != nil || opened != "" {
		t.Fatalf("open(empty) = (%q, %v), want (\"\", nil)", opened, err)
	}
}

func TestSecretCipherWrongKeyFails(t *testing.T) {
	sealed, err := newSecretCipher("key-one").seal("token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := newSecretCipher("key-two").open(sealed); err == nil {
		t.Fatal("open with the wrong key succeeded, want error")
	}
}

func TestSecretCipherDistinctCiphertexts(t *testing.T) {
	c := newSecretCipher("secret")

	first, err := c.seal("same")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := c.seal("same")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if first == second {
		t.Error("two seals of the same plaintext are identical; nonce is not random")
	}
}

func TestRandomSecretNonEmpty(t *testing.T) {
	if randomSecret() == "" {
		t.Fatal("randomSecret returned an empty string")
	}
}
