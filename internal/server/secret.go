package server

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// secretKeyFile is the durable fallback key kept beside the CA material.
const secretKeyFile = "secret.key"

// ephemeralKey is the one fallback key per process: the CLI wiring and
// server.New both resolve, and an unwritable directory must not hand them two
// different keys.
var (
	ephemeralMu  sync.Mutex
	ephemeralKey string
)

// ResolveSecretKey is ensureSecretKey with a durable fallback: when no secret
// is configured it reuses (or creates, mode 0600, exclusively) dir/secret.key,
// so stored credentials survive a restart. persisted reports that file was
// used; only when it cannot be read or written does it fall back to an
// ephemeral key (generated=true), shared by every call in this process.
func ResolveSecretKey(configured, dir string) (secret string, generated, persisted bool) {
	if strings.TrimSpace(configured) != "" {
		return configured, false, false
	}
	if strings.TrimSpace(dir) == "" {
		return sharedEphemeralKey(), true, false
	}
	path := filepath.Join(dir, secretKeyFile)
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if key := read(); key != "" {
		return key, false, true
	}
	if os.MkdirAll(dir, 0o700) == nil {
		key, _ := ensureSecretKey("")
		// O_EXCL: of two processes starting together exactly one creates it.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.WriteString(key + "\n")
			if cerr := f.Close(); werr == nil && cerr == nil {
				return key, false, true
			}
			_ = os.Remove(path)
		} else if existing := read(); existing != "" {
			return existing, false, true
		}
	}
	return sharedEphemeralKey(), true, false
}

func sharedEphemeralKey() string {
	ephemeralMu.Lock()
	defer ephemeralMu.Unlock()
	if ephemeralKey == "" {
		ephemeralKey, _ = ensureSecretKey("")
	}
	return ephemeralKey
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
