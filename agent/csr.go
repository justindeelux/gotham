package agent

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// csrPEMType is the PEM block type of a PKCS#10 certificate signing request.
const csrPEMType = "CERTIFICATE REQUEST"

// GenerateCSR returns a PEM-encoded PKCS#10 certificate signing request for
// nodeID, signed by key. The subject common name is nodeID; nodeID is always
// included as a DNS SAN and additionally as an IP SAN when it parses as an IP
// address.
func GenerateCSR(nodeID string, key crypto.Signer) ([]byte, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("agent: node id is empty")
	}
	if key == nil {
		return nil, fmt.Errorf("agent: signing key is nil")
	}

	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: nodeID},
		DNSNames: []string{nodeID},
	}
	if ip := net.ParseIP(nodeID); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, fmt.Errorf("agent: create certificate request: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: csrPEMType, Bytes: der}), nil
}

// EnsureKey loads the agent's private key from <certDir>/agent.key, generating
// and persisting a new ECDSA P-256 key (0600) when it does not exist yet. It
// returns the signer and the path it was read from or written to. The same key
// is reused across restarts so the certificate issued for its CSR stays valid.
func EnsureKey(certDir string) (crypto.Signer, string, error) {
	if certDir == "" {
		return nil, "", fmt.Errorf("agent: cert dir is empty")
	}
	return ensureKeyAt(filepath.Join(certDir, keyFileName))
}

// ensureKeyAt loads the private key at path or generates and persists a new
// ECDSA P-256 key there. It backs EnsureKey and the explicit KeyFile override.
func ensureKeyAt(path string) (crypto.Signer, string, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, parseErr := parsePrivateKey(data)
		if parseErr != nil {
			return nil, "", fmt.Errorf("agent: parse key %s: %w", path, parseErr)
		}
		return key, path, nil
	case !os.IsNotExist(err):
		return nil, "", fmt.Errorf("agent: read key %s: %w", path, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("agent: generate key: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, "", fmt.Errorf("agent: marshal key: %w", err)
	}
	if err := savePEM(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		return nil, "", err
	}
	return key, path, nil
}

// parsePrivateKey decodes a PEM private key in SEC 1 (EC PRIVATE KEY) or PKCS#8
// (PRIVATE KEY) form.
func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("not PEM-encoded")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		signer, ok := parsed.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("unsupported private key type %T", parsed)
		}
		return signer, nil
	}
	return nil, fmt.Errorf("unsupported private key encoding %q", block.Type)
}
