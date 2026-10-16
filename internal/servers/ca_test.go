package servers

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// testKey returns a fresh P-256 key for CSR tests.
func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// testCSR builds and PEM-encodes a PKCS#10 CSR from template.
func testCSR(t *testing.T, key crypto.Signer, template *x509.CertificateRequest) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("create certificate request: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestIssueAgentCertFromCSRRoundtrip(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	key := testKey(t)
	csrPEM := testCSR(t, key, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "node-42"},
		DNSNames: []string{"node-42"},
	})

	certPEM, err := authority.IssueAgentCertFromCSR(csrPEM, "node-42")
	if err != nil {
		t.Fatalf("IssueAgentCertFromCSR: %v", err)
	}

	cert := parseCertPEM(t, certPEM)
	if _, err := cert.Verify(x509.VerifyOptions{
		DNSName:   "node-42",
		Roots:     authority.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("issued cert failed verification: %v", err)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node-42" {
		t.Fatalf("DNS SANs = %v, want [node-42]", cert.DNSNames)
	}

	// The certificate must carry the CSR's public key, not a fresh one.
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("certificate public key type = %T, want *ecdsa.PublicKey", cert.PublicKey)
	}
	if !publicKey.Equal(key.Public()) {
		t.Fatal("certificate public key does not match the CSR key")
	}
}

func TestIssueAgentCertFromCSRAddsCommonNameSAN(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}

	// A CSR with a common name but no SANs must still verify by name.
	csrPEM := testCSR(t, testKey(t), &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "node-9"},
	})
	cert := parseCertPEM(t, mustIssueFromCSR(t, authority, csrPEM, "node-9"))
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node-9" {
		t.Fatalf("DNS SANs = %v, want [node-9]", cert.DNSNames)
	}

	// An IP node id must become an IP SAN so address-based verification works.
	ipCSR := testCSR(t, testKey(t), &x509.CertificateRequest{
		Subject:     pkix.Name{CommonName: "10.0.0.9"},
		DNSNames:    []string{"10.0.0.9"},
		IPAddresses: []net.IP{net.ParseIP("10.0.0.9")},
	})
	ipCert := parseCertPEM(t, mustIssueFromCSR(t, authority, ipCSR, "10.0.0.9"))
	if len(ipCert.IPAddresses) != 1 || !ipCert.IPAddresses[0].Equal(net.ParseIP("10.0.0.9")) {
		t.Fatalf("IP SANs = %v, want [10.0.0.9]", ipCert.IPAddresses)
	}
	if err := ipCert.VerifyHostname("10.0.0.9"); err != nil {
		t.Fatalf("VerifyHostname(10.0.0.9): %v", err)
	}
}

func TestIssueAgentCertFromCSRTampered(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	csrPEM := testCSR(t, testKey(t), &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "node-tamper"},
		DNSNames: []string{"node-tamper"},
	})

	// Flip the last byte of the DER. The CSR still parses, but the ECDSA
	// signature no longer verifies.
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("decode CSR: not PEM")
	}
	der := append([]byte(nil), block.Bytes...)
	der[len(der)-1] ^= 0xff
	tampered := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	if _, err := authority.IssueAgentCertFromCSR(tampered, "node-tamper"); !errors.Is(err, ErrValidation) {
		t.Fatalf("IssueAgentCertFromCSR(tampered) = %v, want ErrValidation", err)
	}
}

func TestIssueAgentCertFromCSRRejectsEmpty(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}

	for name, csr := range map[string][]byte{
		"empty":      nil,
		"not pem":    []byte("garbage"),
		"no subject": testCSR(t, testKey(t), &x509.CertificateRequest{}),
	} {
		if _, err := authority.IssueAgentCertFromCSR(csr, "node-x"); !errors.Is(err, ErrValidation) {
			t.Errorf("IssueAgentCertFromCSR(%s) = %v, want ErrValidation", name, err)
		}
	}
}

// TestIssueAgentCertFromCSRRejectsUnboundSANs is the FX-3 item-1 rejection
// matrix: a certificate is only issued for the enrolled identity, and a CSR
// carrying a foreign common name, extra/wildcard SANs or an oversized SAN set
// is refused.
func TestIssueAgentCertFromCSRRejectsUnboundSANs(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	ip := net.ParseIP("10.0.0.9")

	tests := []struct {
		name   string
		nodeID string
		csr    *x509.CertificateRequest
	}{
		{
			name:   "common name mismatch",
			nodeID: "node-1",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-2"}, DNSNames: []string{"node-1"}},
		},
		{
			name:   "missing common name",
			nodeID: "node-1",
			csr:    &x509.CertificateRequest{DNSNames: []string{"node-1"}},
		},
		{
			name:   "extra dns san",
			nodeID: "node-1",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-1"}, DNSNames: []string{"node-1", "victim.example.com"}},
		},
		{
			name:   "wildcard dns san",
			nodeID: "node-1",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-1"}, DNSNames: []string{"*.example.com"}},
		},
		{
			name:   "foreign ip san",
			nodeID: "node-1",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-1"}, DNSNames: []string{"node-1"}, IPAddresses: []net.IP{ip}},
		},
		{
			name:   "wildcard node id",
			nodeID: "*.example.com",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "*.example.com"}, DNSNames: []string{"*.example.com"}},
		},
		{
			name:   "ip node id with mismatched ip san",
			nodeID: "10.0.0.9",
			csr:    &x509.CertificateRequest{Subject: pkix.Name{CommonName: "10.0.0.9"}, DNSNames: []string{"10.0.0.9"}, IPAddresses: []net.IP{net.ParseIP("10.0.0.10")}},
		},
		{
			name:   "oversized san set",
			nodeID: "node-1",
			csr: &x509.CertificateRequest{
				Subject:     pkix.Name{CommonName: "node-1"},
				DNSNames:    []string{"node-1"},
				IPAddresses: []net.IP{ip, ip, ip, ip, ip, ip, ip, ip},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csrPEM := testCSR(t, testKey(t), tt.csr)
			if _, err := authority.IssueAgentCertFromCSR(csrPEM, tt.nodeID); !errors.Is(err, ErrValidation) {
				t.Fatalf("IssueAgentCertFromCSR = %v, want ErrValidation", err)
			}
		})
	}

	// A correctly bound CSR is accepted and carries exactly the node identity.
	valid := testCSR(t, testKey(t), &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "node-1"},
		DNSNames: []string{"node-1"},
	})
	cert := parseCertPEM(t, mustIssueFromCSR(t, authority, valid, "node-1"))
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node-1" || len(cert.IPAddresses) != 0 {
		t.Fatalf("issued SANs = dns %v ip %v, want [node-1] and no IP", cert.DNSNames, cert.IPAddresses)
	}
}

// mustIssueFromCSR issues an agent certificate from csrPEM, failing on error.
func mustIssueFromCSR(t *testing.T, authority *Authority, csrPEM []byte, nodeID string) []byte {
	t.Helper()
	certPEM, err := authority.IssueAgentCertFromCSR(csrPEM, nodeID)
	if err != nil {
		t.Fatalf("IssueAgentCertFromCSR: %v", err)
	}
	return certPEM
}

// TestFileStateSurfacesStatErrors is the FX-3 R5 guard: only an absent file is
// (false, nil); any other stat failure is an error, never silently "absent".
func TestFileStateSurfacesStatErrors(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "present")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ok, err := fileState(existing); err != nil || !ok {
		t.Errorf("fileState(existing) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := fileState(filepath.Join(dir, "missing")); err != nil || ok {
		t.Errorf("fileState(missing) = (%v, %v), want (false, nil)", ok, err)
	}
	asDir := filepath.Join(dir, "adir")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := fileState(asDir); err == nil {
		t.Error("fileState(directory) = nil error, want an error")
	}
	// A path under a non-directory cannot be stat'd: ENOTDIR, not NotExist.
	notdir := filepath.Join(existing, "child")
	if _, err := fileState(notdir); err == nil {
		t.Error("fileState(ENOTDIR) = nil error, want an error")
	}
}

// TestLoadAuthorityFailsClosedOnIncompletePair is the FX-3 item-2 guard: half a
// CA pair must never silently downgrade to a plaintext listener.
func TestLoadAuthorityFailsClosedOnIncompletePair(t *testing.T) {
	dir := t.TempDir()
	authority, err := LoadOrCreateAuthority(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	// Certificate without key.
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, err := LoadAuthority(dir); err == nil {
		t.Error("LoadAuthority(cert only) = nil error, want fail-closed")
	}
	if _, err := LoadOrCreateAuthority(dir); err == nil {
		t.Error("LoadOrCreateAuthority(cert only) = nil error, want fail-closed")
	}

	// A key without certificate.
	if err := os.WriteFile(keyPath, authority.CACertPEM(), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Remove(certPath); err != nil {
		t.Fatalf("remove cert: %v", err)
	}
	if _, err := LoadAuthority(dir); err == nil {
		t.Error("LoadAuthority(key only) = nil error, want fail-closed")
	}

	// A directory where a file belongs is a stat failure, not an absent CA:
	// it must not be reported as "no CA configured".
	odd := t.TempDir()
	if err := os.WriteFile(filepath.Join(odd, caCertFile), authority.CACertPEM(), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.Mkdir(filepath.Join(odd, caKeyFile), 0o700); err != nil {
		t.Fatalf("mkdir key: %v", err)
	}
	if _, err := LoadAuthority(odd); err == nil {
		t.Error("LoadAuthority(key is a directory) = nil error, want failure")
	}
	if _, err := LoadOrCreateAuthority(odd); err == nil {
		t.Error("LoadOrCreateAuthority(key is a directory) = nil error, want failure")
	}

	// A truly empty directory still means "no CA configured".
	if loaded, err := LoadAuthority(t.TempDir()); err != nil || loaded != nil {
		t.Errorf("LoadAuthority(empty) = (%v, %v), want (nil, nil)", loaded, err)
	}
}

// TestUniqueStringsTrims pins N1: environment-provided SAN hosts
// (GOTHAM_GRPC_HOSTS="cp.example.com, 203.0.113.10") can carry surrounding
// spaces; a padded entry must be trimmed, not left as a SAN that matches
// nothing (net.ParseIP fails on " 203.0.113.10").
func TestUniqueStringsTrims(t *testing.T) {
	got := uniqueStrings([]string{"cp.example.com", " 203.0.113.10", "", "cp.example.com", "  ", "\t::1 "})
	want := []string{"cp.example.com", "203.0.113.10", "::1"}
	if len(got) != len(want) {
		t.Fatalf("uniqueStrings = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueStrings = %q, want %q", got, want)
		}
	}
}

// TestIssueClientCertCarriesControlPlaneIdentity pins the two-role model: the
// control-plane client credential carries the ControlPlaneURI URI SAN and is
// refused as a node credential, while a node leaf is not accepted as the CP
// credential (agent-side test).
func TestIssueClientCertCarriesControlPlaneIdentity(t *testing.T) {
	if ControlPlaneURI != "spiffe://gotham/control-plane" {
		t.Fatalf("ControlPlaneURI = %q, want spiffe://gotham/control-plane", ControlPlaneURI)
	}
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	certPEM, _, err := authority.IssueClientCert("")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	cert := parseCertPEM(t, certPEM)
	if !hasControlPlaneURI(cert) {
		t.Fatal("the control-plane credential does not carry the control-plane URI SAN")
	}
	if _, err := authority.VerifyNodeClientCert(cert); err == nil {
		t.Fatal("the control-plane credential was accepted as a node credential")
	}
	if _, err := authority.VerifyNodeClientCert(authority.cert); err == nil {
		t.Fatal("the CA certificate was accepted as a node credential")
	}
}

// TestVerifyNodeClientCertReportsLegacy pins the sunset input: a pre-mTLS
// (serverAuth-only) leaf is reported legacy, a dual-EKU leaf is not.
func TestVerifyNodeClientCertReportsLegacy(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	key := testKey(t)
	legacyPEM, err := authority.IssueAgentCertFromCSR(testCSR(t, key, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "legacy"}, DNSNames: []string{"legacy"},
	}), "legacy")
	if err != nil {
		t.Fatalf("IssueAgentCertFromCSR: %v", err)
	}
	if legacy, err := authority.VerifyNodeClientCert(parseCertPEM(t, legacyPEM)); err != nil || legacy {
		t.Fatalf("dual-EKU leaf: legacy=%v err=%v, want false/nil", legacy, err)
	}
	// A pre-mTLS leaf (serverAuth only) is what the sunset is about.
	legacyLeaf, err := authority.sign("legacy", []string{"legacy"}, nil, nil,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Hour, &key.PublicKey)
	if err != nil {
		t.Fatalf("sign legacy leaf: %v", err)
	}
	if legacy, err := authority.VerifyNodeClientCert(legacyLeaf); err != nil || !legacy {
		t.Fatalf("serverAuth-only leaf: legacy=%v err=%v, want true/nil", legacy, err)
	}
}

// TestParseLegacyLeafDeadline pins the sunset configuration: a default, an
// explicit date, a disabled sunset and a malformed value that must not default.
func TestParseLegacyLeafDeadline(t *testing.T) {
	deflt, err := ParseLegacyLeafDeadline("")
	if err != nil || deflt.IsZero() {
		t.Fatalf("ParseLegacyLeafDeadline(\"\") = (%v, %v), want the built-in default", deflt, err)
	}
	want, _ := time.Parse(time.RFC3339, "2030-06-01T12:00:00Z")
	if got, err := ParseLegacyLeafDeadline("2030-06-01T12:00:00Z"); err != nil || !got.Equal(want) {
		t.Fatalf("explicit deadline = (%v, %v), want %v", got, err, want)
	}
	if got, err := ParseLegacyLeafDeadline("none"); err != nil || !got.IsZero() {
		t.Fatalf("ParseLegacyLeafDeadline(none) = (%v, %v), want the zero time", got, err)
	}
	if _, err := ParseLegacyLeafDeadline("soon"); err == nil {
		t.Fatal("a malformed deadline must be an error, not a silent default")
	}
}

// TestLoadOrCreateEnrollmentToken proves the fail-closed default: a token is
// generated, persisted 0600, and reused unchanged on the next call.
func TestLoadOrCreateEnrollmentToken(t *testing.T) {
	dir := t.TempDir()
	token, path, err := LoadOrCreateEnrollmentToken(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateEnrollmentToken: %v", err)
	}
	if len(token) < 43 {
		t.Fatalf("generated token %q is too short", token)
	}
	if want := filepath.Join(dir, enrollmentTokenFile); path != want {
		t.Fatalf("token path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file mode = %04o, want 0600", perm)
	}
	again, _, err := LoadOrCreateEnrollmentToken(dir)
	if err != nil || again != token {
		t.Fatalf("second call = (%q, %v), want the persisted token", again, err)
	}
	// An empty file is treated as absent and regenerated.
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write empty token: %v", err)
	}
	regenerated, _, err := LoadOrCreateEnrollmentToken(dir)
	if err != nil || regenerated == "" || regenerated == token {
		t.Fatalf("regenerated = (%q, %v), want a fresh token", regenerated, err)
	}
	// A token readable beyond its owner is refused rather than trusted.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod token: %v", err)
	}
	if _, _, err := LoadOrCreateEnrollmentToken(dir); err == nil {
		t.Fatal("a group/world-readable enrollment token was accepted")
	}
	// A symlinked token file is refused.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod token: %v", err)
	}
	link := filepath.Join(dir, "token-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := readEnrollmentToken(link); err == nil {
		t.Fatal("a symlinked enrollment token was accepted")
	}
}
