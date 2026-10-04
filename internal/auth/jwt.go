package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Access-token lifetime and issuer claim.
const (
	accessTokenTTL = 15 * time.Minute
	tokenIssuer    = "gotham"
)

// Claims is the payload carried by an access token. SessionID is the refresh
// session the token was minted with (the "sid" claim, PF-2): the sessions
// list marks that row as the caller's current one. Tokens minted before PF-2
// carry no sid; a missing or malformed value means "current unknown", never a
// verification failure.
type Claims struct {
	Role      string `json:"role"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// Signer issues and verifies Ed25519 (EdDSA) access tokens.
type Signer struct {
	private   ed25519.PrivateKey
	public    ed25519.PublicKey
	ephemeral bool
	now       func() time.Time
}

// NewSigner builds a Signer from PEM-encoded Ed25519 keys (PKCS#8 private key,
// PKIX public key). When either input is empty it generates an ephemeral keypair
// instead; Callers should log a warning in that case because tokens do not
// survive a restart.
func NewSigner(privatePEM, publicPEM []byte) (*Signer, error) {
	if len(privatePEM) == 0 || len(publicPEM) == 0 {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("auth: generate ephemeral key: %w", err)
		}
		public, ok := private.Public().(ed25519.PublicKey)
		if !ok {
			return nil, errors.New("auth: ephemeral key is not ed25519")
		}
		return &Signer{private: private, public: public, ephemeral: true, now: time.Now}, nil
	}

	private, err := parsePrivateKey(privatePEM)
	if err != nil {
		return nil, err
	}
	public, err := parsePublicKey(publicPEM)
	if err != nil {
		return nil, err
	}
	return &Signer{private: private, public: public, now: time.Now}, nil
}

// Ephemeral reports whether the signer uses a generated, non-persistent keypair.
func (s *Signer) Ephemeral() bool {
	return s.ephemeral
}

// IssueAccessToken signs an access token for userID with the given role and
// returns it together with its expiry. sessionID is the refresh session the
// token belongs to; it is recorded as the "sid" claim so the sessions list
// can mark the caller's row. A Nil sessionID omits the claim (pre-PF-2 token
// shape), which readers treat as "current unknown".
func (s *Signer) IssueAccessToken(userID uuid.UUID, role string, sessionID uuid.UUID) (string, time.Time, error) {
	now := s.now()
	expiresAt := now.Add(accessTokenTTL)

	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	if sessionID != uuid.Nil {
		claims.SessionID = sessionID.String()
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(s.private)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// VerifyAccessToken parses and validates tokenString, requiring the EdDSA
// algorithm, the Gotham issuer, and a non-expired token. Any failure returns
// ErrInvalidToken.
func (s *Signer) VerifyAccessToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims,
		func(*jwt.Token) (any, error) { return s.public, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// parsePrivateKey decodes a PEM PKCS#8 Ed25519 private key.
func parsePrivateKey(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: invalid PEM private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse private key: %w", err)
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("auth: private key is %T, want ed25519", key)
	}
	return private, nil
}

// parsePublicKey decodes a PEM PKIX Ed25519 public key.
func parsePublicKey(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: invalid PEM public key")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse public key: %w", err)
	}
	public, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("auth: public key is %T, want ed25519", key)
	}
	return public, nil
}
