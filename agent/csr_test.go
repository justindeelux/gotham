package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCSRNodeID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	csrPEM, err := GenerateCSR("node-7", key)
	if err != nil {
		t.Fatalf("GenerateCSR: %v", err)
	}

	csr := parseCSRRequest(t, csrPEM)
	if csr.Subject.CommonName != "node-7" {
		t.Errorf("common name = %q; want node-7", csr.Subject.CommonName)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "node-7" {
		t.Errorf("DNS SANs = %v; want [node-7]", csr.DNSNames)
	}
	if len(csr.IPAddresses) != 0 {
		t.Errorf("IP SANs = %v; want none", csr.IPAddresses)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature is invalid: %v", err)
	}

	if _, err := GenerateCSR("", key); err == nil {
		t.Error("GenerateCSR(empty node id) = nil error; want error")
	}
	if _, err := GenerateCSR("node-7", nil); err == nil {
		t.Error("GenerateCSR(nil key) = nil error; want error")
	}
}

func TestGenerateCSRIPNodeID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	csrPEM, err := GenerateCSR("10.0.0.5", key)
	if err != nil {
		t.Fatalf("GenerateCSR: %v", err)
	}

	csr := parseCSRRequest(t, csrPEM)
	if len(csr.IPAddresses) != 1 || !csr.IPAddresses[0].Equal(net.ParseIP("10.0.0.5")) {
		t.Errorf("IP SANs = %v; want [10.0.0.5]", csr.IPAddresses)
	}
}

func TestEnsureKeyCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()

	first, path, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	if filepath.Base(path) != keyFileName {
		t.Errorf("key path = %q; want basename %q", path, keyFileName)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key permissions = %o; want 600", perm)
	}

	second, _, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey (second call): %v", err)
	}
	if !first.Public().(*ecdsa.PublicKey).Equal(second.Public()) {
		t.Error("EnsureKey returned a different key on the second call")
	}

	// The persisted key is a PEM EC PRIVATE KEY, not a PKCS#8 blob.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		t.Fatalf("key block type = %v; want EC PRIVATE KEY", block)
	}
}

func TestEnsureKeyRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keyFileName), []byte("not a key"), 0o600); err != nil {
		t.Fatalf("write corrupt key: %v", err)
	}
	if _, _, err := EnsureKey(dir); err == nil {
		t.Error("EnsureKey(corrupt) = nil error; want error")
	}
	if _, _, err := EnsureKey(""); err == nil {
		t.Error("EnsureKey(empty dir) = nil error; want error")
	}
}

// TestEnsureKeyProofOfPossession proves the key returned by EnsureKey is the one
// that signs the CSR: the CSR public key must match the persisted private key.
func TestEnsureKeyProofOfPossession(t *testing.T) {
	dir := t.TempDir()
	key, _, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}

	csrPEM, err := GenerateCSR("node-key", key)
	if err != nil {
		t.Fatalf("GenerateCSR: %v", err)
	}
	csr := parseCSRRequest(t, csrPEM)

	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("CSR public key type = %T; want *ecdsa.PublicKey", csr.PublicKey)
	}
	if !publicKey.Equal(key.Public()) {
		t.Error("CSR public key does not match the persisted private key")
	}
}

// TestCertificateRequestUsesKeyFileOverride ensures that when GOTHAM_AGENT_KEY
// points at an explicit path, the CSR is built from the key stored there so it
// matches the key cmd/gotham-agent later loads for the DockerService.
func TestCertificateRequestUsesKeyFileOverride(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "nested", "custom.key")
	a := NewAgent(Config{NodeID: "node-override", CertDir: dir, KeyFile: keyPath}, nil, nil)

	csrPEM, err := a.certificateRequest()
	if err != nil {
		t.Fatalf("certificateRequest: %v", err)
	}

	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("explicit key file not written: %v", err)
	}
	signer, err := parsePrivateKey(raw)
	if err != nil {
		t.Fatalf("parse key file: %v", err)
	}

	csr := parseCSRRequest(t, csrPEM)
	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("CSR public key type = %T; want *ecdsa.PublicKey", csr.PublicKey)
	}
	if !publicKey.Equal(signer.Public()) {
		t.Error("CSR public key does not match the explicit key file")
	}
}
