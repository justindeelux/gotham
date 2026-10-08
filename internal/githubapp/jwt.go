package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// jwtLifetime bounds an app JWT: GitHub accepts at most 10 minutes.
const jwtLifetime = 9 * time.Minute

// signAppJWT builds a GitHub App JWT (RS256): header and claims are JSON with
// base64url encoding and no padding, signed with the app private key. iat is
// backdated 60s for clock skew. The PEM must hold PKCS#1 or PKCS#8 RSA; the
// key material never leaves this function and is never logged.
func signAppJWT(appID int64, pemBytes []byte, now time.Time) (string, error) {
	if appID == 0 {
		return "", fmt.Errorf("%w: app id is required", ErrValidation)
	}
	key, err := parseAppKey(pemBytes)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	payload := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("githubapp: sign jwt: %w", err)
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseAppKey decodes an RSA private key from PEM (PKCS#1 or PKCS#8).
func parseAppKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: private key is not PEM", ErrValidation)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: private key is not RSA PKCS#1 or PKCS#8", ErrValidation)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: private key is not RSA", ErrValidation)
	}
	return key, nil
}

// redactedPEM keeps PEM-shaped errors out of logs: it reports only the block
// type, never the contents.
func redactedPEM(pemBytes []byte) string {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "non-PEM"
	}
	return strings.ToLower(block.Type)
}
