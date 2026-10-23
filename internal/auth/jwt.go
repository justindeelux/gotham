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

const (
	accessTokenTTL = 15 * time.Minute
	tokenIssuer    = "gotham"
)

const mfaTokenTTL = 5 * time.Minute

const mfaPurpose = "mfa"

type Claims struct {
	Role              string `json:"role"`
	SessionID         string `json:"sid,omitempty"`
	Purpose           string `json:"purpose,omitempty"`
	CredentialVersion int32  `json:"cv,omitempty"`
	jwt.RegisteredClaims
}

type Signer struct {
	private   ed25519.PrivateKey
	public    ed25519.PublicKey
	ephemeral bool
	now       func() time.Time
}

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

func (s *Signer) Ephemeral() bool {
	return s.ephemeral
}

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
	if claims.Purpose != "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (s *Signer) IssueMFAToken(userID uuid.UUID, credentialVersion int32) (string, error) {
	now := s.now()

	claims := Claims{
		Purpose:           mfaPurpose,
		CredentialVersion: credentialVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(mfaTokenTTL)),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(s.private)
	if err != nil {
		return "", fmt.Errorf("auth: sign mfa token: %w", err)
	}
	return signed, nil
}

func (s *Signer) VerifyMFAToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims,
		func(*jwt.Token) (any, error) { return s.public, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid || claims.Purpose != mfaPurpose {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

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
