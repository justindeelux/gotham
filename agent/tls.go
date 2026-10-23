package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
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

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

const selfSignedValidity = 365 * 24 * time.Hour

const controlPlaneURI = "spiffe://gotham/control-plane"

const controlPlaneClientName = "gotham-control-plane"

func verifyControlPlanePeer(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("agent: no client certificate presented")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("agent: parse client certificate: %w", err)
	}
	_, err = verifyControlPlaneCert(cert)
	return err
}

func verifyControlPlaneCert(cert *x509.Certificate) (legacy bool, err error) {
	if cert.IsCA {
		return false, fmt.Errorf("agent: a CA certificate is not a control-plane credential")
	}
	for _, uri := range cert.URIs {
		if uri != nil && uri.String() == controlPlaneURI {
			return false, nil
		}
	}
	if len(cert.ExtKeyUsage) == 1 && cert.ExtKeyUsage[0] == x509.ExtKeyUsageClientAuth &&
		cert.Subject.CommonName == controlPlaneClientName {
		return true, nil
	}
	return false, fmt.Errorf("agent: client certificate is not the control plane")
}

func clientCredentials(caPath, certPath, keyPath string) (credentials.TransportCredentials, bool, error) {
	if caPath == "" {
		return insecure.NewCredentials(), true, nil
	}
	pool, err := loadCertPool(caPath)
	if err != nil {
		return nil, false, err
	}
	config := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	if certPath != "" && keyPath != "" {
		config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			pair, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return &tls.Certificate{}, nil
				}
				return nil, fmt.Errorf("agent: load client keypair: %w", err)
			}
			return &pair, nil
		}
	}
	return credentials.NewTLS(config), false, nil
}

func ClientCertPath(certDir string) string {
	return filepath.Join(certDir, certFileName)
}

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
		config.VerifyPeerCertificate = verifyControlPlanePeer
	}
	return credentials.NewTLS(config), nil
}

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
		config.VerifyPeerCertificate = verifyControlPlanePeer
	}
	return credentials.NewTLS(config), nil
}

func KeyPath(keyFile, certDir string) string {
	if keyFile != "" {
		return keyFile
	}
	return filepath.Join(certDir, keyFileName)
}

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
