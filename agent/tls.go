package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// File names used inside Config.CertDir.
const (
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

// selfSignedValidity is how long a generated development certificate lasts.
const selfSignedValidity = 365 * 24 * time.Hour

// clientCredentials returns transport credentials that verify the CP server
// certificate against the PEM bundle at caPath. An empty caPath returns
// insecure credentials for local development; Config.Load only accepts an empty
// CA when GOTHAM_AGENT_INSECURE=true, so this path is reachable only through the
// explicit development opt-in. It reports dev = true in that case.
func clientCredentials(caPath string) (credentials.TransportCredentials, bool, error) {
	if caPath == "" {
		return insecure.NewCredentials(), true, nil
	}
	pool, err := loadCertPool(caPath)
	if err != nil {
		return nil, false, err
	}
	return credentials.NewClientTLSFromCert(pool, ""), false, nil
}

// ServerCredentials builds credentials for the agent's DockerService server.
// certPEM is the certificate issued by the CP at registration and keyPEM its
// matching private key. When caPath is non-empty the server requires and
// verifies client certificates; otherwise it does not request one.
//
// With no certificate and no CA the agent would serve plaintext: that is only
// returned when allowInsecure is set (the explicit development opt-in), and the
// caller must additionally bind the listener to loopback. Without the opt-in it
// is an error, so a misconfiguration cannot silently expose a plaintext Docker
// control channel. With a CA configured but no issued certificate it falls back
// to a self-signed certificate (still TLS).
func ServerCredentials(certPEM, keyPEM []byte, caPath string, allowInsecure bool) (credentials.TransportCredentials, error) {
	if len(certPEM) == 0 && caPath == "" {
		if !allowInsecure {
			return nil, fmt.Errorf("agent: refusing to serve plaintext without an explicit insecure opt-in")
		}
		return nil, nil
	}
	if len(certPEM) == 0 {
		generatedCert, generatedKey, err := generateSelfSigned()
		if err != nil {
			return nil, err
		}
		certPEM, keyPEM = generatedCert, generatedKey
	}
	if len(keyPEM) == 0 {
		return nil, fmt.Errorf("agent: private key is required with a server certificate")
	}

	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("agent: load server keypair: %w", err)
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{keyPair},
		MinVersion:   tls.VersionTLS12,
	}
	if caPath != "" {
		pool, err := loadCertPool(caPath)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(config), nil
}

// ServerCredentialsFromFiles is ServerCredentials for a certificate and key
// that live on disk and are rewritten on renewal. The TLS handshake loads them
// on demand, so a re-registration that persists a fresh certificate takes
// effect without restarting the gRPC server. caPath enables the same client
// verification as ServerCredentials.
func ServerCredentialsFromFiles(certPath, keyPath, caPath string) (credentials.TransportCredentials, error) {
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("agent: certificate and key paths are required")
	}
	var pool *x509.CertPool
	if caPath != "" {
		loaded, err := loadCertPool(caPath)
		if err != nil {
			return nil, err
		}
		pool = loaded
	}

	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			keyPair, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				return nil, fmt.Errorf("agent: load server keypair: %w", err)
			}
			return &keyPair, nil
		},
	}
	if pool != nil {
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(config), nil
}

// KeyPath returns the private-key path LoadOrGenerateKey resolves for the given
// explicit override and cert dir, so callers that need the on-disk path (for
// ServerCredentialsFromFiles) agree with the loader.
func KeyPath(keyFile, certDir string) string {
	if keyFile != "" {
		return keyFile
	}
	return filepath.Join(certDir, keyFileName)
}

// SaveAgentCert persists the certificate issued by the CP as
// <certDir>/agent.crt and returns the path written.
func SaveAgentCert(certDir string, certPEM []byte) (string, error) {
	if len(certPEM) == 0 {
		return "", fmt.Errorf("agent: empty certificate from control plane")
	}
	path := filepath.Join(certDir, certFileName)
	if err := savePEM(path, certPEM); err != nil {
		return "", err
	}
	return path, nil
}

// LoadOrGenerateKey returns the agent private key in PEM form. It reads keyFile
// when set, otherwise <certDir>/agent.key, generating and persisting a new
// P-256 key when neither exists.
func LoadOrGenerateKey(keyFile, certDir string) ([]byte, error) {
	path := keyFile
	if path == "" {
		path = filepath.Join(certDir, keyFileName)
	}
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("agent: read key %s: %w", path, err)
	}

	_, keyPEM, err := generateSelfSigned()
	if err != nil {
		return nil, err
	}
	if err := savePEM(path, keyPEM); err != nil {
		return nil, err
	}
	return keyPEM, nil
}

// loadCertPool reads a PEM bundle into a certificate pool.
func loadCertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent: read CA bundle %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("agent: no certificates found in %s", path)
	}
	return pool, nil
}

// generateSelfSigned returns a fresh P-256 self-signed certificate and key in
// PEM form, valid for localhost.
func generateSelfSigned() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("agent: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("agent: generate serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "gotham-agent"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "gotham-agent"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("agent: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("agent: marshal key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// savePEM writes data to path atomically, creating the parent directory with
// 0o700 and the file with 0o600. The temp-then-rename write matters because the
// file-backed server credentials read the certificate on each handshake while a
// renewal may be rewriting it.
func savePEM(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("agent: create cert dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("agent: write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("agent: rename %s: %w", path, err)
	}
	return nil
}
