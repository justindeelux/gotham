package server

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

// ensureSecretKey resolves the credential-encryption secret once at startup.
//
// A configured GOTHAM_SECRET_KEY is returned unchanged. An empty one becomes a
// fresh ephemeral key and generated=true so the caller can warn: every domain
// service (deploy, webhooks, databases, providers, proxy, servers,
// notifications) is then wired with the same non-empty key, and no subsystem
// can silently seal credentials under the publicly derivable SHA-256("") key.
//
// The ephemeral fallback matches the documented config contract (config.go:
// "When empty an ephemeral secret is generated at startup and a warning is
// logged"); sealed values do not survive a restart, which is the accepted
// development trade-off — production must configure a durable secret.
func ensureSecretKey(configured string) (secret string, generated bool) {
	if configured != "" {
		return configured, false
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		// crypto/rand failure is unrecoverable; still return a non-empty
		// marker instead of "", so the empty-key guards never see it.
		return base64.RawURLEncoding.EncodeToString([]byte(err.Error())), true
	}
	return base64.RawURLEncoding.EncodeToString(buf), true
}
