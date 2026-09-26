package servers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CA file names and validity windows.
const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"

	caValidity         = 10 * 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
	clientCertValidity = 365 * 24 * time.Hour
	agentCertValidity  = 365 * 24 * time.Hour

	caCommonName = "Gotham CA"
)

// Authority is the control-plane certificate authority. It signs the mTLS
// certificates agents present and the certificates the control plane uses for
// its own gRPC listener and for dialing agents.
type Authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

// LoadAuthority loads an existing CA from dir. It returns (nil, nil) when no CA
// has been created yet, which signals the caller to fall back to an insecure
// development listener.
func LoadAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	if !fileExists(certPath) || !fileExists(keyPath) {
		return nil, nil
	}
	return loadAuthority(certPath, keyPath)
}

// LoadOrCreateAuthority loads the CA from dir, generating and persisting a new
// self-signed CA (ECDSA P-256, 10 years) on first use. Files are written with
// 0600 permissions.
func LoadOrCreateAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	if fileExists(certPath) && fileExists(keyPath) {
		return loadAuthority(certPath, keyPath)
	}
	return createAuthority(dir, certPath, keyPath)
}

// CACertPEM returns the PEM-encoded CA certificate.
func (a *Authority) CACertPEM() []byte {
	return a.pem
}

// Pool returns a certificate pool containing the CA certificate, suitable for
// verifying certificates signed by this authority.
func (a *Authority) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

// IssueAgentCert issues a server-auth certificate for the node identified by
// nodeID. When nodeID is an IP address it is added as an IP SAN in addition to
// the DNS SAN.
//
// The returned value is the certificate PEM only: the RegisterResponse contract
// (proto/agent/v1) carries no private key field, so the agent keypair exchange
// must be added to the contract before mTLS can be completed end to end.
func (a *Authority) IssueAgentCert(nodeID string) ([]byte, error) {
	if nodeID == "" {
		return nil, fmt.Errorf("%w: node id is empty", ErrValidation)
	}
	_, cert, err := a.issue(nodeID, []string{nodeID}, ipSANs(nodeID), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, agentCertValidity)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), nil
}

// IssueServerCert issues the control plane's own server certificate for the
// gRPC listener, returning the certificate and private key as PEM.
func (a *Authority) IssueServerCert(hosts []string) (certPEM, keyPEM []byte, err error) {
	if len(hosts) == 0 {
		hosts = []string{"localhost"}
	}
	key, cert, err := a.issue(hosts[0], hosts, ipSANs(hosts...), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, serverCertValidity)
	if err != nil {
		return nil, nil, err
	}
	return leafPEM(cert, key)
}

// IssueClientCert issues a client-auth certificate the control plane presents
// when dialing an agent, returning the certificate and private key as PEM.
func (a *Authority) IssueClientCert(name string) (certPEM, keyPEM []byte, err error) {
	if name == "" {
		name = "gotham-control-plane"
	}
	key, cert, err := a.issue(name, []string{name}, nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, clientCertValidity)
	if err != nil {
		return nil, nil, err
	}
	return leafPEM(cert, key)
}

// issue generates a leaf keypair and certificate signed by the CA.
func (a *Authority) issue(commonName string, dns []string, ips []net.IP, usages []x509.ExtKeyUsage, validity time.Duration) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate leaf key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Gotham"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}
	return key, cert, nil
}

// createAuthority generates and persists a fresh self-signed CA.
func createAuthority(dir, certPath, keyPath string) (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName, Organization: []string{"Gotham"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create ca certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create ca dir: %w", err)
	}
	if err := writeFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal ca key: %w", err)
	}
	if err := writeFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		return nil, err
	}

	return &Authority{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, nil
}

// loadAuthority reads and parses a persisted CA.
func loadAuthority(certPath, keyPath string) (*Authority, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read ca certificate: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("decode ca certificate %s: not PEM", certPath)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ca key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("decode ca key %s: not PEM", keyPath)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca key: %w", err)
	}

	return &Authority{cert: cert, key: key, pem: certPEM}, nil
}

// leafPEM encodes a leaf certificate and its key as PEM.
func leafPEM(cert *x509.Certificate, key *ecdsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal leaf key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// randomSerial returns a random 128-bit certificate serial number.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

// ipSANs returns the subset of hosts that parse as IP addresses.
func ipSANs(hosts ...string) []net.IP {
	var ips []net.IP
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

// writeFile writes data atomically with 0600 permissions.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
