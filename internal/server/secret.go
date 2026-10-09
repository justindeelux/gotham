package server

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// secretKeyFile is the durable fallback key kept beside the CA material.
const secretKeyFile = "secret.key"

// resolveSecretKey is ensureSecretKey with a durable fallback: when no secret
// is configured it reuses (or creates, mode 0600) dir/secret.key, so stored
// credentials survive a restart. persisted reports that file was used; only
// when it cannot be read or written does it fall back to an ephemeral key
// (generated=true).
func resolveSecretKey(configured, dir string) (secret string, generated, persisted bool) {
	if strings.TrimSpace(configured) != "" {
		return configured, false, false
	}
	path := filepath.Join(dir, secretKeyFile)
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), false, true
	}
	secret, _ = ensureSecretKey("")
	if dir == "" || os.MkdirAll(dir, 0o700) != nil || os.WriteFile(path, []byte(secret+"\n"), 0o600) != nil {
		return secret, true, false
	}
	return secret, false, true
}

// ensureSecretKey resolves the credential-encryption secret once at startup.
//
// A configured GOTHAM_SECRET_KEY (non-blank) is returned unchanged. An empty
// or whitespace-only value becomes a fresh ephemeral key and generated=true so
// the caller can warn: every subsystem the server wires directly (deploy,
// webhooks, databases, backups, proxy, and the providers/notifications
// services it constructs) then receives a non-empty key, and none can silently
// seal credentials under the publicly derivable SHA-256(""). The services
// built outside the server keep their own trim-and-generate fallback as
// defense in depth.
//
// The ephemeral fallback matches the documented config contract (config.go:
// "When empty an ephemeral secret is generated at startup and a warning is
// logged"); sealed values do not survive a restart, which is the accepted
// development trade-off — production must configure a durable secret.
func ensureSecretKey(configured string) (secret string, generated bool) {
	if strings.TrimSpace(configured) != "" {
		return configured, false
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		// crypto/rand failure is unrecoverable: fail closed at startup rather
		// than deriving key material from the error string (mirrors the
		// servers/notifications randomSecret implementations).
		panic("server: crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf), true
}
