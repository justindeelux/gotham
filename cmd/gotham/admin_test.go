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

// TestCheckAdminPasswordFlags pins the mutual exclusivity of the three
// password sources: --password, --password-stdin and --generate-password.
// An explicitly passed --password counts even when empty.
func TestCheckAdminPasswordFlags(t *testing.T) {
	if err := checkAdminPasswordFlags(false, false, false); err != nil {
		t.Fatalf("no source: %v", err)
	}
	for name, tc := range map[string]struct {
		password   bool
		stdin, gen bool
	}{
		"password only": {password: true},
		"stdin only":    {stdin: true},
		"generate only": {gen: true},
	} {
		if err := checkAdminPasswordFlags(tc.password, tc.stdin, tc.gen); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		password   bool
		stdin, gen bool
	}{
		"password+stdin":    {password: true, stdin: true},
		"password+generate": {password: true, gen: true},
		"stdin+generate":    {stdin: true, gen: true},
		"all three":         {password: true, stdin: true, gen: true},
	} {
		if err := checkAdminPasswordFlags(tc.password, tc.stdin, tc.gen); err == nil {
			t.Fatalf("%s: expected a mutual-exclusivity error", name)
		}
	}
}

// TestReadPasswordLineStripsOneNewline guards the --password-stdin contract:
// a single line is read, only the trailing newline goes, and every other
// byte (including spaces) is significant.
func TestReadPasswordLineStripsOneNewline(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"s3cret pass\n", "s3cret pass"},
		{"s3cret pass\r\n", "s3cret pass"},
		{"s3cret pass", "s3cret pass"},
		{"first\nsecond\n", "first"},
	} {
		got, err := readPasswordLine(strings.NewReader(tc.in))
		if err != nil {
			t.Fatalf("readPasswordLine(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("readPasswordLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := readPasswordLine(strings.NewReader("")); err == nil {
		t.Fatal("empty stdin: expected an error, not an empty password")
	}
	if _, err := readPasswordLine(strings.NewReader(strings.Repeat("x", maxPasswordStdinBytes+1))); err == nil {
		t.Fatal("oversize stdin: expected a bound error")
	}
	if _, err := readPasswordLine(strings.NewReader(strings.Repeat("y", 128) + "\n")); err != nil {
		t.Fatalf("128-char line must fit the bound: %v", err)
	}
}

// TestPrintGeneratedPasswordOnce pins the generated-password stdout contract:
// exactly one machine-readable line on the given writer (the caller passes
// os.Stdout only after a successful insert; failures never reach it).
func TestPrintGeneratedPasswordOnce(t *testing.T) {
	var buf strings.Builder
	printGeneratedPassword(&buf, "s3cret-value")
	if got, want := buf.String(), "generated-password: s3cret-value\n"; got != want {
		t.Fatalf("printGeneratedPassword = %q, want %q", got, want)
	}
}

// TestGenerateAdminPasswordIsStrongAndValid pins the --generate-password
// contract: 24+ URL-safe characters that pass the shared password policy,
// and a fresh value on every call.
func TestGenerateAdminPasswordIsStrongAndValid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		pw, err := generateAdminPassword()
		if err != nil {
			t.Fatalf("generateAdminPassword: %v", err)
		}
		if len(pw) < 24 {
			t.Fatalf("generated password %q is shorter than 24 characters", pw)
		}
		for _, r := range pw {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", r) {
				t.Fatalf("generated password %q is not base64url", pw)
			}
		}
		if err := auth.ValidatePassword(pw); err != nil {
			t.Fatalf("generated password fails the shared policy: %v", err)
		}
		hash, err := adminHashPlaintext(pw)
		if err != nil {
			t.Fatalf("adminHashPlaintext(generated): %v", err)
		}
		ok, err := auth.VerifyPassword(hash, pw)
		if err != nil || !ok {
			t.Fatalf("generated hash does not verify: ok=%v err=%v", ok, err)
		}
		seen[pw] = true
	}
	if len(seen) != 4 {
		t.Fatal("generateAdminPassword returned a duplicate value")
	}
}

// TestAdminHashPlaintextNeverPrompts pins that a resolved password is only
// validated and hashed: an empty value fails instead of opening a prompt.
func TestAdminHashPlaintextNeverPrompts(t *testing.T) {
	if _, err := adminHashPlaintext(""); err == nil {
		t.Fatal("empty password: expected a validation error")
	}
	if _, err := adminHashPlaintext("short"); err == nil {
		t.Fatal("short password: expected a validation error")
	}
}
