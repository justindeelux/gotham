package main

import (
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

// TestAdminPasswordHashEncodesArgon2id guards the P1 where the CLI stored the
// plaintext: adminPasswordHash must return an argon2id hash that verifies, so a
// CLI-provisioned account can actually sign in.
func TestAdminPasswordHashEncodesArgon2id(t *testing.T) {
	const password = "Gotham-E2E-Password1"

	hash, err := adminPasswordHash(password, "", "")
	if err != nil {
		t.Fatalf("adminPasswordHash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash %q is not an argon2id PHC string", hash)
	}
	if hash == password {
		t.Fatal("the plaintext password was returned instead of a hash")
	}
	ok, err := auth.VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("the stored hash does not verify the password")
	}
}
