package servers

import (
	"crypto"
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
	"strings"
	"time"
)

// CA file names and validity windows.
const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	// hostsFile is the optional SAN host list `gotham ca init --host` persists
	// next to the CA. serve reads it when GOTHAM_GRPC_HOSTS is unset.
	hostsFile = "hosts"

	caValidity         = 10 * 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
	clientCertValidity = 365 * 24 * time.Hour
	// agentCertValidity bounds a node leaf's exposure. There is no CRL yet
	// (docs/TODO.md, LOW-4), so a shorter lifetime is the only revocation
	// control; the agent re-registers well before it lapses.
	agentCertValidity = 90 * 24 * time.Hour

	// maxNodeIDLength caps a registered node identity: the DNS name maximum
	// (253 bytes), which is also ample for an IP literal.
	maxNodeIDLength = 253
	// maxCSRSANs caps how many subject alternative names a CSR may carry, so
	// an unauthenticated caller cannot make the CP sign a certificate with an
	// unbounded SAN set.
	maxCSRSANs = 8

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

// LoadAuthority loads an existing CA from dir. It returns (nil, nil) only when
// no CA has been created yet, which signals the caller to fall back to an
// insecure development listener. A directory holding exactly one of
// ca.crt/ca.key is an incomplete CA and is refused: silently serving without
// TLS because half the authority is missing would downgrade the channel.
func LoadAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	certExists, keyExists := fileExists(certPath), fileExists(keyPath)
	switch {
	case certExists && keyExists:
		return loadAuthority(certPath, keyPath)
	case !certExists && !keyExists:
		return nil, nil
	default:
		return nil, incompleteCAError(certPath, keyPath, certExists)
	}
}

// incompleteCAError describes a CA directory that has exactly one of its two
// files. missingIsKey names whether the key (true) or the certificate (false)
// is the missing half.
func incompleteCAError(certPath, keyPath string, missingIsKey bool) error {
	if missingIsKey {
		return fmt.Errorf("servers: incomplete CA: %s exists but %s is missing; refusing to serve", certPath, keyPath)
	}
	return fmt.Errorf("servers: incomplete CA: %s exists but %s is missing; refusing to serve", keyPath, certPath)
}

// LoadOrCreateAuthority loads the CA from dir, generating and persisting a new
// self-signed CA (ECDSA P-256, 10 years) on first use. Files are written with
// 0600 permissions. An existing-but-incomplete pair is refused rather than
// overwritten, so a missing file cannot silently replace the CA.
func LoadOrCreateAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	certExists, keyExists := fileExists(certPath), fileExists(keyPath)
	switch {
	case certExists && keyExists:
		return loadAuthority(certPath, keyPath)
	case !certExists && !keyExists:
		return createAuthority(dir, certPath, keyPath)
	default:
		return nil, incompleteCAError(certPath, keyPath, certExists)
	}
}

// CACertPEM returns the PEM-encoded CA certificate.
func (a *Authority) CACertPEM() []byte {
	return a.pem
}

// SaveHosts persists the extra gRPC listener SAN hosts under dir, one per line,
// so `gotham serve` keeps presenting the operator-declared names and IPs after
// a restart. The file lives inside the 0700 CA directory.
func SaveHosts(dir string, hosts []string) error {
	if dir == "" {
		return errors.New("servers: ca dir is empty")
	}
	clean := uniqueStrings(hosts)
	if len(clean) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ca dir: %w", err)
	}
	var b strings.Builder
	for _, host := range clean {
		b.WriteString(host)
		b.WriteByte('\n')
	}
	return writeFile(filepath.Join(dir, hostsFile), []byte(b.String()))
}

// LoadHosts reads the SAN host list persisted by SaveHosts. It returns nil when
// the file (or the directory) does not exist.
func LoadHosts(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, hostsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gRPC SAN host list: %w", err)
	}
	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		if host := strings.TrimSpace(line); host != "" {
			hosts = append(hosts, host)
		}
	}
	return uniqueStrings(hosts), nil
}

// Pool returns a certificate pool containing the CA certificate, suitable for
// verifying certificates signed by this authority.
func (a *Authority) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

// IssueAgentCertFromCSR verifies a PEM-encoded PKCS#10 CSR, checks that it is
// bound to the enrolled node identity, and issues a server-auth certificate
// (90 days) for the public key it carries.
//
// The certificate is issued for nodeID — the authenticated identity — and
// never for the CSR's self-asserted SAN set: the CSR supplies only the public
// key. A CSR whose common name, DNS SANs or IP SANs do not exactly match
// nodeID, that carries a wildcard, or that carries an oversized SAN set is
// rejected with an error wrapping ErrValidation.
func (a *Authority) IssueAgentCertFromCSR(csrPEM []byte, nodeID string) ([]byte, error) {
	nodeID = strings.TrimSpace(nodeID)
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}

	csr, err := parseCSR(csrPEM)
	if err != nil {
		return nil, err
	}
	if err := verifyCSRBinding(csr, nodeID); err != nil {
		return nil, err
	}

	dns, ips := identitySANs(nodeID)
	cert, err := a.sign(nodeID, dns, ips, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, agentCertValidity, csr.PublicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), nil
}

// validateNodeID rejects node identities that are unusable or hostile: empty,
// over-long, or carrying a wildcard or path/whitespace characters. It is the
// shared gate for registration and CSR binding.
func validateNodeID(nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("%w: node_id is required", ErrValidation)
	}
	if len(nodeID) > maxNodeIDLength {
		return fmt.Errorf("%w: node_id exceeds %d bytes", ErrValidation, maxNodeIDLength)
	}
	if strings.ContainsAny(nodeID, "*\\/\x00") || strings.ContainsAny(nodeID, " \t\r\n") {
		return fmt.Errorf("%w: node_id contains an invalid character", ErrValidation)
	}
	return nil
}

// identitySANs returns the SANs a node certificate is issued with: the node id
// as a DNS name and, when it parses as an IP address, an IP SAN as well.
func identitySANs(nodeID string) ([]string, []net.IP) {
	dns := []string{nodeID}
	if ip := net.ParseIP(nodeID); ip != nil {
		return dns, []net.IP{ip}
	}
	return dns, nil
}

// verifyCSRBinding rejects a CSR that is not bound to nodeID: a different or
// missing common name, a wildcard, any extra/foreign SAN, or an oversized SAN
// set. A caller that controls one node id therefore cannot obtain a
// certificate valid for a different host.
func verifyCSRBinding(csr *x509.CertificateRequest, nodeID string) error {
	commonName := strings.TrimSpace(csr.Subject.CommonName)
	if commonName == "" {
		return fmt.Errorf("%w: csr subject common name is required", ErrValidation)
	}
	if commonName != nodeID {
		return fmt.Errorf("%w: csr common name %q does not match node id %q", ErrValidation, commonName, nodeID)
	}
	if len(csr.DNSNames)+len(csr.IPAddresses) > maxCSRSANs {
		return fmt.Errorf("%w: csr carries more than %d subject alternative names", ErrValidation, maxCSRSANs)
	}

	wantIP := net.ParseIP(nodeID)
	for _, dns := range csr.DNSNames {
		if isWildcardName(dns) {
			return fmt.Errorf("%w: wildcard SAN %q is not allowed", ErrValidation, dns)
		}
		if wantIP != nil {
			if dns != nodeID {
				return fmt.Errorf("%w: csr dns SAN %q does not match node id %q", ErrValidation, dns, nodeID)
			}
			continue
		}
		if !strings.EqualFold(dns, nodeID) {
			return fmt.Errorf("%w: csr dns SAN %q does not match node id %q", ErrValidation, dns, nodeID)
		}
	}
	for _, ip := range csr.IPAddresses {
		if wantIP == nil || !ip.Equal(wantIP) {
			return fmt.Errorf("%w: csr ip SAN %s does not match node id %q", ErrValidation, ip, nodeID)
		}
	}
	return nil
}

// isWildcardName reports whether a DNS name is a wildcard.
func isWildcardName(name string) bool {
	return name == "*" || strings.HasPrefix(name, "*.")
}

// parseCSR decodes and verifies a PEM-encoded PKCS#10 certificate request.
func parseCSR(csrPEM []byte) (*x509.CertificateRequest, error) {
	if len(csrPEM) == 0 {
		return nil, fmt.Errorf("%w: csr is required", ErrValidation)
	}
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, fmt.Errorf("%w: csr is not PEM-encoded", ErrValidation)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse csr: %v", ErrValidation, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: csr signature is invalid: %v", ErrValidation, err)
	}
	return csr, nil
}

// uniqueStrings returns s with surrounding spaces trimmed and blank entries and
// duplicates removed, order preserved. Trimming matters for operator input such
// as GOTHAM_GRPC_HOSTS="cp.example.com, 203.0.113.10": a padded entry would
// otherwise become a SAN that matches nothing.
func uniqueStrings(s []string) []string {
	var out []string
	for _, item := range s {
		item = strings.TrimSpace(item)
		if item == "" || containsString(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// containsString reports whether s contains item.
func containsString(s []string, item string) bool {
	for _, existing := range s {
		if existing == item {
			return true
		}
	}
	return false
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
	cert, err := a.sign(commonName, dns, ips, usages, validity, &key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

// sign builds a leaf certificate for pub and signs it with the CA key.
func (a *Authority) sign(commonName string, dns []string, ips []net.IP, usages []x509.ExtKeyUsage, validity time.Duration, pub crypto.PublicKey) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
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

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, pub, a.key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
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
