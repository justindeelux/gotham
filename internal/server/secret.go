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

const secretKeyFile = "secret.key"

var (
	ephemeralMu  sync.Mutex
	ephemeralKey string
)

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

func ensureSecretKey(configured string) (secret string, generated bool) {
	if strings.TrimSpace(configured) != "" {
		return configured, false
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic("server: crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf), true
}
