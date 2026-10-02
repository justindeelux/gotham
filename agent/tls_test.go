package agent

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

// testCA returns a self-signed CA certificate and key.
func testCA(t *testing.T) (caPEM []byte, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return caPEM, cert, key
}

// testLeaf returns a certificate and key signed by the given CA.
func testLeaf(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gotham-agent"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestLoadCertPool(t *testing.T) {
	caPEM, _, _ := testCA(t)
	dir := t.TempDir()

	goodPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(goodPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	if _, err := loadCertPool(goodPath); err != nil {
		t.Errorf("loadCertPool(valid) = %v; want nil", err)
	}

	badPath := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(badPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write bad CA: %v", err)
	}
	if _, err := loadCertPool(badPath); err == nil {
		t.Error("loadCertPool(invalid) = nil; want error")
	}
	if _, err := loadCertPool(filepath.Join(dir, "missing.pem")); err == nil {
		t.Error("loadCertPool(missing) = nil; want error")
	}
}

func TestClientCredentials(t *testing.T) {
	caPEM, _, _ := testCA(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	creds, dev, err := clientCredentials("")
	if err != nil || !dev || creds == nil {
		t.Errorf("clientCredentials(empty) = (%v, %v, %v); want insecure dev creds", creds, dev, err)
	}
	creds, dev, err = clientCredentials(caPath)
	if err != nil || dev || creds == nil {
		t.Errorf("clientCredentials(ca) = (%v, %v, %v); want TLS creds", creds, dev, err)
	}
	if _, _, err := clientCredentials(filepath.Join(dir, "missing.pem")); err == nil {
		t.Error("clientCredentials(missing) = nil error; want error")
	}
}

func TestGenerateSelfSigned(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSigned()
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("generated keypair is unusable: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certificate PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	if !cert.NotAfter.After(time.Now()) {
		t.Error("generated certificate is already expired")
	}
	if len(cert.DNSNames) == 0 {
		t.Error("generated certificate has no DNS names")
	}
}

func TestServerCredentials(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	certPEM, keyPEM := testLeaf(t, caCert, caKey)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	creds, err := ServerCredentials(certPEM, keyPEM, caPath, false)
	if err != nil || creds == nil {
		t.Errorf("ServerCredentials(cert, key, ca) = (%v, %v); want TLS creds", creds, err)
	}

	// No CA and no certificate is plaintext: it must be refused without the
	// explicit insecure opt-in, and allowed only with it.
	if _, err := ServerCredentials(nil, nil, "", false); err == nil {
		t.Error("ServerCredentials(nil, nil, empty, false) = nil error; want refusal")
	}
	creds, err = ServerCredentials(nil, nil, "", true)
	if err != nil || creds != nil {
		t.Errorf("ServerCredentials(nil, nil, empty, true) = (%v, %v); want nil plaintext creds", creds, err)
	}

	// A CA configured with no issued certificate still keeps the listener on
	// TLS with a self-signed certificate.
	creds, err = ServerCredentials(nil, nil, caPath, false)
	if err != nil || creds == nil {
		t.Errorf("ServerCredentials(nil, nil, ca) = (%v, %v); want self-signed TLS creds", creds, err)
	}

	if _, err := ServerCredentials(certPEM, nil, "", false); err == nil {
		t.Error("ServerCredentials(cert, no key) = nil error; want error")
	}
	if _, err := ServerCredentials(nil, nil, filepath.Join(dir, "missing.pem"), false); err == nil {
		t.Error("ServerCredentials(cert, key, missing CA) = nil error; want error")
	}
}

// TestServerCredentialsFromFilesReloads proves the file-backed credentials read
// the certificate on each handshake, so a renewal written to disk takes effect
// without restarting the server.
func TestServerCredentialsFromFilesReloads(t *testing.T) {
	_, caCert, caKey := testCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certA := leafWithKey(t, caCert, caKey, key, big.NewInt(101))
	certB := leafWithKey(t, caCert, caKey, key, big.NewInt(202))

	dir := t.TempDir()
	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)
	if err := os.WriteFile(certPath, certA, 0o600); err != nil {
		t.Fatalf("write cert A: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	creds, err := ServerCredentialsFromFiles(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("ServerCredentialsFromFiles: %v", err)
	}
	addr := startTLSHandshakeServer(t, creds)

	if got := peerSerial(t, addr); got != 101 {
		t.Fatalf("first handshake serial = %d, want 101", got)
	}

	// Rewrite the certificate through the atomic saver; the same credentials
	// must present the new leaf.
	if err := savePEM(certPath, certB); err != nil {
		t.Fatalf("rewrite cert B: %v", err)
	}
	if got := peerSerial(t, addr); got != 202 {
		t.Fatalf("after rewrite handshake serial = %d, want 202", got)
	}
}

// leafWithKey signs a leaf certificate for key with the given serial.
func leafWithKey(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, key *ecdsa.PrivateKey, serial *big.Int) []byte {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "gotham-agent"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// startTLSHandshakeServer accepts connections and completes a TLS handshake
// with creds, returning its address.
func startTLSHandshakeServer(t *testing.T, creds credentials.TransportCredentials) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _, _ = creds.ServerHandshake(c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// peerSerial dials addr and returns the serial of the presented leaf.
func peerSerial(t *testing.T, addr string) int64 {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("no peer certificate presented")
	}
	return certs[0].SerialNumber.Int64()
}

func TestSaveAgentCertAndKeyRoundtrip(t *testing.T) {
	certPEM, _, _ := testCA(t)
	dir := t.TempDir()

	path, err := SaveAgentCert(dir, certPEM)
	if err != nil {
		t.Fatalf("SaveAgentCert: %v", err)
	}
	if filepath.Base(path) != certFileName {
		t.Errorf("cert path = %q; want basename %q", path, certFileName)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved cert: %v", err)
	}
	if !bytes.Equal(saved, certPEM) {
		t.Error("saved certificate does not match input")
	}

	if _, err := SaveAgentCert(dir, nil); err == nil {
		t.Error("SaveAgentCert(empty) = nil error; want error")
	}

	key1, err := LoadOrGenerateKey("", dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFileName)); err != nil {
		t.Errorf("generated key file missing: %v", err)
	}
	key2, err := LoadOrGenerateKey("", dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateKey (second call): %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("LoadOrGenerateKey returned a different key on the second call")
	}
	block, _ := pem.Decode(key1)
	if block == nil {
		t.Fatal("key PEM did not decode")
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		t.Errorf("generated key is not a valid PKCS#8 key: %v", err)
	}
}

func TestLoadOrGenerateKeyExplicitPath(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "nested", "custom.key")

	if _, err := LoadOrGenerateKey(keyPath, dir); err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("explicit key path not written: %v", err)
	}
}
