package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignerIssueVerifyRoundtrip(t *testing.T) {
	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if !signer.Ephemeral() {
		t.Fatal("NewSigner(nil, nil).Ephemeral() = false, want true")
	}

	userID := uuid.New()
	token, expiresAt, err := signer.IssueAccessToken(userID, "user", uuid.Nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("IssueAccessToken returned an empty token")
	}
	if remaining := time.Until(expiresAt); remaining < 14*time.Minute {
		t.Fatalf("token expires in %s, want ~15m", remaining)
	}

	claims, err := signer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID)
	}
	if claims.Role != "user" {
		t.Errorf("Role = %q, want user", claims.Role)
	}
	if claims.Issuer != tokenIssuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, tokenIssuer)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// Issue the token as if it were created an hour ago, so it is already
	// expired at verification time.
	signer.now = func() time.Time { return time.Now().Add(-time.Hour) }
	token, _, err := signer.IssueAccessToken(uuid.New(), "user", uuid.Nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	signer.now = time.Now

	if _, err := signer.VerifyAccessToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyAccessToken(expired) error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	signerA, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner A: %v", err)
	}
	signerB, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner B: %v", err)
	}

	token, _, err := signerA.IssueAccessToken(uuid.New(), "user", uuid.Nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	if _, err := signerB.VerifyAccessToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyAccessToken with wrong key error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	if _, err := signer.VerifyAccessToken("not-a-jwt"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyAccessToken(garbage) error = %v, want ErrInvalidToken", err)
	}
}

func TestNewSignerFromPEM(t *testing.T) {
	privatePEM, publicPEM := generateTestKeypair(t)

	signer, err := NewSigner(privatePEM, publicPEM)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if signer.Ephemeral() {
		t.Fatal("Ephemeral() = true for a configured keypair, want false")
	}

	userID := uuid.New()
	token, _, err := signer.IssueAccessToken(userID, "user", uuid.Nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	claims, err := signer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID)
	}
}

func TestNewSignerRejectsInvalidPEM(t *testing.T) {
	if _, err := NewSigner([]byte("not a pem key"), []byte("not a pem key")); err == nil {
		t.Fatal("NewSigner(invalid) = nil error, want error")
	}
}

// generateTestKeypair returns PEM-encoded PKCS#8 and PKIX Ed25519 keys.
func generateTestKeypair(t *testing.T) (privatePEM, publicPEM []byte) {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	derPrivate, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	derPublic, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	privatePEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: derPrivate})
	publicPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derPublic})
	return privatePEM, publicPEM
}
