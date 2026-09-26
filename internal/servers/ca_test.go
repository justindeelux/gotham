package servers

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorityGeneratePersistReload(t *testing.T) {
	dir := t.TempDir()

	created, err := LoadOrCreateAuthority(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	if len(created.CACertPEM()) == 0 {
		t.Fatal("generated CA certificate is empty")
	}

	for _, name := range []string{caCertFile, caKeyFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s permissions = %o, want 600", name, perm)
		}
	}

	reloaded, err := LoadOrCreateAuthority(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority (reload): %v", err)
	}
	if !bytes.Equal(created.CACertPEM(), reloaded.CACertPEM()) {
		t.Fatal("reloaded CA certificate differs from the generated one")
	}
	if !created.cert.Equal(reloaded.cert) {
		t.Fatal("reloaded CA certificate is not the same certificate")
	}

	// LoadAuthority returns nil when no CA exists.
	missing, err := LoadAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAuthority (empty dir): %v", err)
	}
	if missing != nil {
		t.Fatal("LoadAuthority on an empty dir returned a CA, want nil")
	}
}

func TestIssueAgentCertVerifiesAndCarriesSAN(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}

	certPEM, err := authority.IssueAgentCert("node-42")
	if err != nil {
		t.Fatalf("IssueAgentCert: %v", err)
	}

	cert := parseCertPEM(t, certPEM)
	if _, err := cert.Verify(x509.VerifyOptions{
		DNSName:   "node-42",
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("issued cert failed verification: %v", err)
	}

	sans := cert.DNSNames
	if len(sans) != 1 || sans[0] != "node-42" {
		t.Fatalf("DNS SANs = %v, want [node-42]", sans)
	}

	if _, err := authority.IssueAgentCert(""); err == nil {
		t.Fatal("IssueAgentCert with empty node id = nil error, want error")
	}
}

func TestIssueServerAndClientCerts(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}

	serverCertPEM, serverKeyPEM, err := authority.IssueServerCert([]string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueServerCert: %v", err)
	}
	if _, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM); err != nil {
		t.Fatalf("server keypair is unusable: %v", err)
	}
	serverCert := parseCertPEM(t, serverCertPEM)
	if _, err := serverCert.Verify(x509.VerifyOptions{
		DNSName:   "localhost",
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("server cert verification failed: %v", err)
	}

	clientCertPEM, clientKeyPEM, err := authority.IssueClientCert("control-plane")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	if _, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM); err != nil {
		t.Fatalf("client keypair is unusable: %v", err)
	}
}

// parseCertPEM decodes the first certificate block in a PEM blob.
func parseCertPEM(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode certificate: not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}
