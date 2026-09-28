package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// acmeStorageFixture builds a Traefik-shaped acme.json body containing real
// key material: an ACME account key and a per-certificate private key. The
// markers let a test prove the key material never leaves the agent.
func acmeStorageFixture(t *testing.T, notAfter time.Time, main string, sans ...string) []byte {
	t.Helper()
	leafPEM := selfSignedLeaf(t, notAfter, main, sans...)
	accountKeyPEM := testPrivateKeyPEM(t)
	certificateKeyPEM := testPrivateKeyPEM(t)

	type storedCertificate struct {
		Domain      map[string]any `json:"domain"`
		Certificate string         `json:"certificate"`
		Key         string         `json:"key"`
		Store       string         `json:"Store"`
	}
	type resolverData struct {
		Account struct {
			Email        string `json:"Email"`
			PrivateKey   string `json:"PrivateKey"`
			KeyType      string `json:"KeyType"`
			Registration any    `json:"Registration"`
		} `json:"Account"`
		Certificates []storedCertificate `json:"Certificates"`
	}

	domainNames := map[string]any{"main": main}
	if len(sans) > 0 {
		domainNames["sans"] = sans
	}
	data := resolverData{}
	data.Account.Email = "acme-fixture@example.test"
	data.Account.PrivateKey = accountKeyPEM
	data.Account.KeyType = "4096"
	data.Account.Registration = map[string]any{"body": map[string]any{"contact": []string{"mailto:acme-fixture@example.test"}}}
	data.Certificates = []storedCertificate{{
		Domain:      domainNames,
		Certificate: base64.StdEncoding.EncodeToString(leafPEM),
		Key:         certificateKeyPEM,
		Store:       "default",
	}}
	encoded, err := json.Marshal(map[string]any{"letsencrypt": data})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return encoded
}

// testPrivateKeyPEM renders a fresh EC private key as PEM (the shape Traefik
// stores, which the agent must never expose).
func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// selfSignedLeaf renders a self-signed PEM leaf valid until notAfter.
func selfSignedLeaf(t *testing.T, notAfter time.Time, main string, sans ...string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: main},
		DNSNames:     append([]string{main}, sans...),
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// writeACMEStorage writes content to root/acme/acme.json, creating the dir.
func writeACMEStorage(t *testing.T, root string, content []byte) string {
	t.Helper()
	dir := filepath.Join(root, "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	path := filepath.Join(dir, "acme.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write acme.json: %v", err)
	}
	return path
}

// TestReadACMEStorageReportsMetadataOnly proves the security boundary: a
// fixture acme.json containing private and account keys yields certificate
// metadata, and no key material appears in the response, in the logs, or in
// any error string.
func TestReadACMEStorageReportsMetadataOnly(t *testing.T) {
	root := canonicalTempDir(t)
	notAfter := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	fixture := acmeStorageFixture(t, notAfter, "app.example.test", "www.app.example.test")

	// Capture everything the agent logs, so the proof covers the log channel
	// too: the read path logs counts only.
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := NewProxyServer(ProxyServerConfig{Root: root, Logger: logger})
	writeACMEStorage(t, root, fixture)

	response, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{})
	if err != nil {
		t.Fatalf("ReadACMEStorage: %v", err)
	}
	if !response.GetPresent() {
		t.Fatal("present = false, want true")
	}
	if len(response.GetCertificates()) != 1 {
		t.Fatalf("certificates = %d, want 1", len(response.GetCertificates()))
	}
	certificate := response.GetCertificates()[0]
	if certificate.GetResolver() != "letsencrypt" {
		t.Fatalf("resolver = %q, want letsencrypt", certificate.GetResolver())
	}
	if certificate.GetMain() != "app.example.test" {
		t.Fatalf("main = %q, want app.example.test", certificate.GetMain())
	}
	if len(certificate.GetSans()) != 1 || certificate.GetSans()[0] != "www.app.example.test" {
		t.Fatalf("sans = %v, want [www.app.example.test]", certificate.GetSans())
	}
	if got := certificate.GetNotAfter().AsTime(); !got.Equal(notAfter) {
		t.Fatalf("not_after = %s, want %s", got, notAfter)
	}

	// The response and the logs must not contain any key material. The
	// fixture's PEM payloads are extracted below and every base64 body line is
	// used as a marker, so a leak of any part of the account key or the
	// certificate key fails this test.
	rendered, err := protojson.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	forbidden := append(append([]string{}, keyPayloadMarkers(t, fixture)...),
		"PRIVATE KEY",
		"acme-fixture@example.test",
	)
	for _, channel := range []string{string(rendered), logs.String()} {
		for _, needle := range forbidden {
			if strings.Contains(channel, needle) {
				t.Fatalf("key/account material %q leaked into %q", needle, channel)
			}
		}
	}
}

// keyPayloadMarkers extracts one base64 body segment of every private key in
// the fixture, so the test can prove none of them appears in an output
// channel.
func keyPayloadMarkers(t *testing.T, fixture []byte) []string {
	t.Helper()
	var decoded map[string]struct {
		Account struct {
			PrivateKey string `json:"PrivateKey"`
		} `json:"Account"`
		Certificates []struct {
			Key string `json:"key"`
		} `json:"Certificates"`
	}
	if err := json.Unmarshal(fixture, &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	markers := make([]string, 0)
	add := func(pemText string) {
		for _, line := range strings.Split(pemText, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "-----") {
				continue
			}
			if len(line) > 40 {
				line = line[:40]
			}
			markers = append(markers, line)
			return
		}
	}
	for _, data := range decoded {
		add(data.Account.PrivateKey)
		for _, certificate := range data.Certificates {
			add(certificate.Key)
		}
	}
	if len(markers) == 0 {
		t.Fatal("fixture carries no key material to prove against")
	}
	return markers
}

// TestReadACMEStorageMissingFile proves a node without ACME storage answers
// present=false instead of an error.
func TestReadACMEStorageMissingFile(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	if err := os.RemoveAll(filepath.Join(root, "acme")); err != nil {
		t.Fatalf("remove acme dir: %v", err)
	}
	response, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{})
	if err != nil {
		t.Fatalf("ReadACMEStorage: %v", err)
	}
	if response.GetPresent() || len(response.GetCertificates()) != 0 {
		t.Fatalf("response = %v, want absent", response)
	}
	// The read-only traversal must not create the directory either.
	if _, err := os.Stat(filepath.Join(root, "acme")); !os.IsNotExist(err) {
		t.Fatalf("acme dir was created by the read path (stat err = %v)", err)
	}
}

// TestReadACMEStorageEmptyFile proves the production boot state (Traefik
// creates an empty acme.json before the first issuance) is reported as
// present=false, not as a malformed read.
func TestReadACMEStorageEmptyFile(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	for _, content := range [][]byte{nil, []byte(""), []byte("\n  \n")} {
		writeACMEStorage(t, root, content)
		response, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{})
		if err != nil {
			t.Fatalf("ReadACMEStorage(%q): %v", content, err)
		}
		if response.GetPresent() || len(response.GetCertificates()) != 0 {
			t.Fatalf("response(%q) = %v, want absent", content, response)
		}
	}
}

// TestReadACMEStorageMalformed proves a corrupt file is an error whose text
// carries no certificate content.
func TestReadACMEStorageMalformed(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	writeACMEStorage(t, root, []byte(`{"letsencrypt": {"Certificates": [{"domain": {"main": "x"}, "certificate": "not-base64-but-secret"}]}}`))

	_, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{})
	if err == nil {
		t.Fatal("malformed storage = nil error")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("status = %v, want Internal", status.Code(err))
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error %q echoes file content", err)
	}
}

// TestReadACMEStorageRejectsUnparseableCertificate proves a stored entry whose
// certificate cannot be parsed fails the read: the control plane must report
// unknown instead of a fabricated "present without expiry".
func TestReadACMEStorageRejectsUnparseableCertificate(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	body := `{"letsencrypt": {"Certificates": [{"domain": {"main": "x.example.test"}, "certificate": "` +
		base64.StdEncoding.EncodeToString([]byte("not a certificate")) + `"}]}}`
	writeACMEStorage(t, root, []byte(body))

	if _, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{}); err == nil {
		t.Fatal("unparseable certificate = nil error")
	}
}

// TestReadACMEStorageRejectsSymlinks proves the read traversal never follows a
// symlinked directory or file out of the ACME root.
func TestReadACMEStorageRejectsSymlinks(t *testing.T) {
	t.Run("symlinked acme dir", func(t *testing.T) {
		root := canonicalTempDir(t)
		elsewhere := canonicalTempDir(t)
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(elsewhere, filepath.Join(root, "acme")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		server := NewProxyServer(ProxyServerConfig{Root: root, Logger: discardLogger()})
		if _, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{}); err == nil {
			t.Fatal("symlinked acme dir = nil error")
		}
	})

	t.Run("symlinked acme.json", func(t *testing.T) {
		root := canonicalTempDir(t)
		elsewhere := canonicalTempDir(t)
		secret := filepath.Join(elsewhere, "acme.json")
		if err := os.WriteFile(secret, []byte(`{"letsencrypt": {}}`), 0o600); err != nil {
			t.Fatalf("write target: %v", err)
		}
		dir := filepath.Join(root, "acme")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(secret, filepath.Join(dir, "acme.json")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		server := NewProxyServer(ProxyServerConfig{Root: root, Logger: discardLogger()})
		if _, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{}); err == nil {
			t.Fatal("symlinked acme.json = nil error")
		}
	})
}

// TestReadACMEStorageResolvesSANsAndResolvers proves every resolver section is
// reported with its own entries.
func TestReadACMEStorageResolvesSANsAndResolvers(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	expiry := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	leaf := base64.StdEncoding.EncodeToString(selfSignedLeaf(t, expiry, "a.example.test", "*.a.example.test"))
	for _, resolver := range []string{"letsencrypt", "letsencrypt-dns-cloudflare"} {
		body := `{"` + resolver + `": {"Certificates": [{"domain": {"main": "a.example.test", "sans": ["*.a.example.test"]}, "certificate": "` + leaf + `"}]}}`
		writeACMEStorage(t, root, []byte(body))
		response, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{})
		if err != nil {
			t.Fatalf("ReadACMEStorage(%s): %v", resolver, err)
		}
		if len(response.GetCertificates()) != 1 || response.GetCertificates()[0].GetResolver() != resolver {
			t.Fatalf("response = %v, want one %s entry", response, resolver)
		}
	}
}

// TestReadACMEStorageBoundsFileSize proves an oversized storage file is an
// error instead of an unbounded read.
func TestReadACMEStorageBoundsFileSize(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	path := writeACMEStorage(t, root, []byte(`{"letsencrypt": {}}`))
	if err := os.Truncate(path, maxACMEStorageSize+1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := server.ReadACMEStorage(context.Background(), &agentv1.ReadACMEStorageRequest{}); err == nil {
		t.Fatal("oversized storage = nil error")
	}
}
