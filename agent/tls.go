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

// File names used inside Config.CertDir.
const (
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

// selfSignedValidity is how long a generated development certificate lasts.
const selfSignedValidity = 365 * 24 * time.Hour

// controlPlaneURI mirrors internal/servers.ControlPlaneURI: the URI SAN of the
// control-plane client credential. The agent cannot import internal/servers
// (cross-scope rule), so the literal is duplicated and pinned by
// TestControlPlaneIdentityLiteral plus the end-to-end CP dial test.
const controlPlaneURI = "spiffe://gotham/control-plane"

// controlPlaneClientName is the legacy control-plane client common name,
// accepted only during the rollout window (see verifyControlPlanePeer).
const controlPlaneClientName = "gotham-control-plane"

// verifyControlPlanePeer is the agent-side half of the two-role identity model
// (JUS-194 fix round 1). Agents run the Docker, Compose, File, Build, Proxy and
// Tool services (RCE-class), so the listener must accept ONLY the control plane:
// a node leaf chains to the same CA and now carries clientAuth, so without this
// check any enrolled node could act as the CP toward every other agent.
func verifyControlPlanePeer(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("agent: no client certificate presented")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("agent: parse client certificate: %w", err)
	}
	return verifyControlPlaneCert(cert)
}

// verifyControlPlaneCert is the certificate-level check behind
// verifyControlPlanePeer and the gRPC interceptor.
func verifyControlPlaneCert(cert *x509.Certificate) error {
	if cert.IsCA {
		return fmt.Errorf("agent: a CA certificate is not a control-plane credential")
	}
	for _, uri := range cert.URIs {
		if uri != nil && uri.String() == controlPlaneURI {
			return nil
		}
	}
	// Rollout grace: a control plane upgraded after this agent still presents the
	// pre-change credential, which has no URI SAN. It is unambiguous because
	// only the control plane can obtain it: its extended key usage is clientAuth
	// ALONE (a node leaf always carries serverAuth too) and its identity is the
	// reserved control-plane name. A node cannot present a matching certificate.
	// shortcut: drop this branch once the CP-side URI SAN is mandatory everywhere.
	if len(cert.ExtKeyUsage) == 1 && cert.ExtKeyUsage[0] == x509.ExtKeyUsageClientAuth &&
		cert.Subject.CommonName == controlPlaneClientName {
		return nil
	}
	return fmt.Errorf("agent: client certificate is not the control plane")
}

// clientCredentials returns transport credentials that verify the CP server
// certificate against the PEM bundle at caPath and present the node's own
// certificate as a mutual-TLS client certificate (JUS-194) when certPath/keyPath
// point at an existing pair. The pair is loaded on every handshake, so a
// certificate renewed at re-registration takes effect without a restart; a
// missing pair presents none, which is the one certificate-less bootstrap
// connection. An empty caPath returns insecure credentials for local
// development; Config.Load only accepts an empty CA when
// GOTHAM_AGENT_INSECURE=true, so this path is reachable only through the
// explicit development opt-in. It reports dev = true in that case.
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
					// No certificate issued yet: present none so the control
					// plane treats this as a bootstrap registration.
					return &tls.Certificate{}, nil
				}
				return nil, fmt.Errorf("agent: load client keypair: %w", err)
			}
			return &pair, nil
		}
	}
	return credentials.NewTLS(config), false, nil
}

// ClientCertPath returns the path of the node certificate the agent presents to
// the control plane and serves its own listeners with: the file SaveAgentCert
// writes.
func ClientCertPath(certDir string) string {
	return filepath.Join(certDir, certFileName)
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
		config.VerifyPeerCertificate = verifyControlPlanePeer
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
		config.VerifyPeerCertificate = verifyControlPlanePeer
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
