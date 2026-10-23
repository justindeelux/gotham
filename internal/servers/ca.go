package servers

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	hostsFile  = "hosts"

	caValidity         = 10 * 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
	clientCertValidity = 365 * 24 * time.Hour
	agentCertValidity  = 90 * 24 * time.Hour

	maxNodeIDLength = 253
	maxCSRSANs      = 8

	caCommonName = "Gotham CA"

	ControlPlaneURI = "spiffe://gotham/control-plane"
)

var controlPlaneURIParsed = func() *url.URL {
	u, err := url.Parse(ControlPlaneURI)
	if err != nil {
		panic(err)
	}
	return u
}()

type Authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func LoadAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	certExists, err := fileState(certPath)
	if err != nil {
		return nil, err
	}
	keyExists, err := fileState(keyPath)
	if err != nil {
		return nil, err
	}
	switch {
	case certExists && keyExists:
		return loadAuthority(certPath, keyPath)
	case !certExists && !keyExists:
		return nil, nil
	default:
		return nil, incompleteCAError(certPath, keyPath, certExists)
	}
}

func incompleteCAError(certPath, keyPath string, missingIsKey bool) error {
	if missingIsKey {
		return fmt.Errorf("servers: incomplete CA: %s exists but %s is missing; refusing to serve", certPath, keyPath)
	}
	return fmt.Errorf("servers: incomplete CA: %s exists but %s is missing; refusing to serve", keyPath, certPath)
}

func LoadOrCreateAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return nil, errors.New("servers: ca dir is empty")
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	certExists, err := fileState(certPath)
	if err != nil {
		return nil, err
	}
	keyExists, err := fileState(keyPath)
	if err != nil {
		return nil, err
	}
	switch {
	case certExists && keyExists:
		return loadAuthority(certPath, keyPath)
	case !certExists && !keyExists:
		return createAuthority(dir, certPath, keyPath)
	default:
		return nil, incompleteCAError(certPath, keyPath, certExists)
	}
}

func (a *Authority) CACertPEM() []byte {
	return a.pem
}

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

const enrollmentTokenFile = "enrollment-token"

func LoadOrCreateEnrollmentToken(dir string) (token, path string, err error) {
	if dir == "" {
		return "", "", errors.New("servers: ca dir is empty")
	}
	path = filepath.Join(dir, enrollmentTokenFile)
	if existing, readErr := readEnrollmentToken(path); readErr != nil {
		return "", "", readErr
	} else if existing != "" {
		return existing, path, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate enrollment token: %w", err)
	}
	token = hex.EncodeToString(raw)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create ca dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if existing, readErr := readEnrollmentToken(path); readErr == nil && existing != "" {
				return existing, path, nil
			}
			if writeErr := os.WriteFile(path, []byte(token+"\n"), 0o600); writeErr != nil {
				return "", "", fmt.Errorf("write enrollment token: %w", writeErr)
			}
			return token, path, nil
		}
		return "", "", fmt.Errorf("write enrollment token: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", "", fmt.Errorf("write enrollment token: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", "", fmt.Errorf("write enrollment token: %w", err)
	}
	return token, path, nil
}

func readEnrollmentToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat enrollment token: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("servers: enrollment token %s is not a regular file", path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("servers: enrollment token %s has permissions %04o; must not be readable by group or others (chmod 0600)", path, perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read enrollment token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (a *Authority) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

var nodeCertUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

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
	if nodeID == "localhost" {
		dns = []string{localhostAgentServerName}
	}
	cert, err := a.sign(nodeID, dns, ips, nil, nodeCertUsages, agentCertValidity, csr.PublicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), nil
}

func (a *Authority) VerifyNodeClientCert(cert *x509.Certificate) (legacy bool, err error) {
	if cert == nil {
		return false, errors.New("servers: no client certificate presented")
	}
	if cert.IsCA {
		return false, errors.New("servers: a CA certificate is not a node credential")
	}
	if hasControlPlaneURI(cert) {
		return false, errors.New("servers: a control-plane client certificate is not a node credential")
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     a.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return false, fmt.Errorf("servers: node certificate: %w", err)
	}
	return !hasClientAuthUsage(cert), nil
}

func hasClientAuthUsage(cert *x509.Certificate) bool {
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

const DefaultLegacyLeafDeadline = "2027-01-15T00:00:00Z"

func ParseLegacyLeafDeadline(raw string) (time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		raw = DefaultLegacyLeafDeadline
	case "none", "off":
		return time.Time{}, nil
	}
	deadline, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse legacy leaf deadline %q: %w", raw, err)
	}
	return deadline, nil
}

func hasControlPlaneURI(cert *x509.Certificate) bool {
	for _, uri := range cert.URIs {
		if uri != nil && uri.String() == ControlPlaneURI {
			return true
		}
	}
	return false
}

func NodeKeyFingerprint(pub crypto.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

func CSRKeyFingerprint(csrPEM []byte) (string, error) {
	csr, err := parseCSR(csrPEM)
	if err != nil {
		return "", err
	}
	return NodeKeyFingerprint(csr.PublicKey)
}

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

func identitySANs(nodeID string) ([]string, []net.IP) {
	dns := []string{nodeID}
	if ip := net.ParseIP(nodeID); ip != nil {
		return dns, []net.IP{ip}
	}
	return dns, nil
}

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

func isWildcardName(name string) bool {
	return name == "*" || strings.HasPrefix(name, "*.")
}

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

func containsString(s []string, item string) bool {
	for _, existing := range s {
		if existing == item {
			return true
		}
	}
	return false
}

func (a *Authority) IssueServerCert(hosts []string) (certPEM, keyPEM []byte, err error) {
	if len(hosts) == 0 {
		hosts = []string{"localhost"}
	}
	key, cert, err := a.issue(hosts[0], hosts, ipSANs(hosts...), nil, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, serverCertValidity)
	if err != nil {
		return nil, nil, err
	}
	return leafPEM(cert, key)
}

func (a *Authority) IssueClientCert(name string) (certPEM, keyPEM []byte, err error) {
	if name == "" {
		name = "gotham-control-plane"
	}
	key, cert, err := a.issue(name, []string{name}, nil, []*url.URL{controlPlaneURIParsed}, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, clientCertValidity)
	if err != nil {
		return nil, nil, err
	}
	return leafPEM(cert, key)
}

func (a *Authority) issue(commonName string, dns []string, ips []net.IP, uris []*url.URL, usages []x509.ExtKeyUsage, validity time.Duration) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate leaf key: %w", err)
	}
	cert, err := a.sign(commonName, dns, ips, uris, usages, validity, &key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

func (a *Authority) sign(commonName string, dns []string, ips []net.IP, uris []*url.URL, usages []x509.ExtKeyUsage, validity time.Duration, pub crypto.PublicKey) (*x509.Certificate, error) {
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
		URIs:                  uris,
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

func loadAuthority(certPath, keyPath string) (*Authority, error) {
	if err := checkCAKeyPermissions(keyPath); err != nil {
		return nil, err
	}
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

	var (
		keyPEM []byte
		block  *pem.Block
	)
	if keyPEM, err = os.ReadFile(keyPath); err != nil {
		return nil, fmt.Errorf("read ca key: %w", err)
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("decode ca key %s: not PEM", keyPath)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca key: %w", err)
	}

	return &Authority{cert: cert, key: key, pem: certPEM}, nil
}

func checkCAKeyPermissions(keyPath string) error {
	info, err := os.Stat(keyPath)
	if err != nil {
		return fmt.Errorf("stat ca key: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("servers: ca key %s has permissions %04o; must not be readable by group or others (chmod 0600)", keyPath, perm)
	}
	return nil
}

func leafPEM(cert *x509.Certificate, key *ecdsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal leaf key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

func ipSANs(hosts ...string) []net.IP {
	var ips []net.IP
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

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

func fileState(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("servers: %s is a directory, not a file", path)
	}
	return true, nil
}
