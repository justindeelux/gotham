package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TestControlPlaneIdentityLiteral pins the duplicated control-plane identity:
// internal/servers cannot be imported here (cross-scope rule), so this literal
// and internal/servers.ControlPlaneURI must stay equal.
func TestControlPlaneIdentityLiteral(t *testing.T) {
	if controlPlaneURI != "spiffe://gotham/control-plane" {
		t.Errorf("controlPlaneURI = %q, want spiffe://gotham/control-plane", controlPlaneURI)
	}
	if controlPlaneClientName != "gotham-control-plane" {
		t.Errorf("controlPlaneClientName = %q, want gotham-control-plane", controlPlaneClientName)
	}
}

// issueTestLeaf issues a CA-signed leaf with an explicit key usage, URI/DNS SANs
// and validity window, so a test can model the control-plane credential, a node
// leaf, an expired credential or a legacy credential.
func issueTestLeaf(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, ekus []x509.ExtKeyUsage, uris []*url.URL, dns []string, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  ekus,
		DNSNames:     dns,
		URIs:         uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// controlPlaneLeaf returns a CA-signed credential carrying the control-plane URI
// SAN (the shape internal/servers.IssueClientCert produces).
func controlPlaneLeaf(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, keyPEM []byte) {
	t.Helper()
	uri, err := url.Parse(controlPlaneURI)
	if err != nil {
		t.Fatalf("parse control-plane URI: %v", err)
	}
	return issueTestLeaf(t, caCert, caKey, controlPlaneClientName,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, []*url.URL{uri},
		[]string{controlPlaneClientName}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
}

// nodeLeaf returns a CA-signed node certificate (serverAuth + clientAuth, no
// control-plane URI), exactly what the control plane now issues to a node.
func nodeLeaf(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, nodeID string) (certPEM, keyPEM []byte) {
	t.Helper()
	return issueTestLeaf(t, caCert, caKey, nodeID,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, nil,
		[]string{nodeID}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
}

// startTestAgentTLS starts an agent listener that verifies client certificates
// against caPEM, with the mandatory control-plane interceptor (unless an opt-out
// is passed).
func startTestAgentTLS(t *testing.T, caPEM []byte, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, options ...ServerOption) string {
	t.Helper()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	serverCert, serverKey := testLeaf(t, caCert, caKey)
	creds, err := ServerCredentials(serverCert, serverKey, caPath, false)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	return startAgentWithCreds(t, creds, options...)
}

// startAgentWithCreds starts an agent listener from creds and returns its
// address.
func startAgentWithCreds(t *testing.T, creds credentials.TransportCredentials, options ...ServerOption) string {
	t.Helper()
	docker := &fakeDockerClient{version: "24.0.7", containers: []*agentv1.ContainerInfo{{Id: "c1"}}}
	// No control-plane option is passed: NewServer installs the check for any TLS
	// listener (M4), which is what these tests also prove.
	server, err := NewServer("127.0.0.1:0", creds, NewDockerServer(docker, discardLogger()), discardLogger(), options...)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Serve(ctx) }()
	return server.Addr().String()
}

// listOverMTLS dials addr with the given client keypair and lists containers.
func listOverMTLS(t *testing.T, addr string, pool *x509.CertPool, certPEM, keyPEM []byte) (*agentv1.ListContainersResponse, error) {
	t.Helper()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      pool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return agentv1.NewDockerServiceClient(conn).ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
}

func testPool(t *testing.T, caPEM []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append CA")
	}
	return pool
}

// TestAgentListenerRejectsNodeLeaf is the F1 regression: a node leaf chains to
// the same CA and now carries clientAuth, but it must not act as the control
// plane toward another agent. The control-plane credential is accepted.
func TestAgentListenerRejectsNodeLeaf(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	pool := testPool(t, caPEM)
	addr := startTestAgentTLS(t, caPEM, caCert, caKey)

	nodeCert, nodeKey := nodeLeaf(t, caCert, caKey, "node-a")
	if resp, err := listOverMTLS(t, addr, pool, nodeCert, nodeKey); err == nil {
		t.Fatalf("a node leaf was accepted by another agent's listener: %v", resp.GetContainers())
	}

	cpCert, cpKey := controlPlaneLeaf(t, caCert, caKey)
	resp, err := listOverMTLS(t, addr, pool, cpCert, cpKey)
	if err != nil {
		t.Fatalf("the control-plane credential was rejected: %v", err)
	}
	if len(resp.GetContainers()) != 1 || resp.GetContainers()[0].GetId() != "c1" {
		t.Fatalf("containers = %v, want one c1", resp.GetContainers())
	}
}

// TestAgentListenerRejectsExpiredAndCACredentials covers the other credential
// shapes: an expired control-plane leaf and the CA certificate itself.
func TestAgentListenerRejectsExpiredAndCACredentials(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	pool := testPool(t, caPEM)
	addr := startTestAgentTLS(t, caPEM, caCert, caKey)

	uri, err := url.Parse(controlPlaneURI)
	if err != nil {
		t.Fatalf("parse URI: %v", err)
	}
	expiredCert, expiredKey := issueTestLeaf(t, caCert, caKey, controlPlaneClientName,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, []*url.URL{uri}, nil,
		time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	if _, err := listOverMTLS(t, addr, pool, expiredCert, expiredKey); err == nil {
		t.Fatal("an expired control-plane credential was accepted")
	}

	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPKCS8(t, caKey)})
	if _, err := listOverMTLS(t, addr, pool, caPEM, caKeyPEM); err == nil {
		t.Fatal("the CA certificate was accepted as an agent client credential")
	}
}

// TestAgentListenerControlPlaneCheckIsMandatory proves the interceptor is a real
// gate with exactly one opt-out: a TLS listener that does not verify client
// certificates still refuses an uncertified peer unless the caller explicitly
// passes WithoutControlPlanePeer (the dev/test escape production must not use).
// The credential-level check is separate and stays in place.
func TestAgentListenerControlPlaneCheckIsMandatory(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	pool := testPool(t, caPEM)
	serverCert, serverKey := testLeaf(t, caCert, caKey)
	// No CA => the listener does not request a client certificate at all.
	creds, err := ServerCredentials(serverCert, serverKey, "", false)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}

	enforced := startAgentWithCreds(t, creds)
	if _, err := listOverMTLS(t, enforced, pool, caPEM, mustKeyPEM(t, caKey)); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a TLS listener without the control-plane check accepted a peer: %v", err)
	}

	optedOut := startAgentWithCreds(t, creds, WithoutControlPlanePeer())
	if _, err := listOverMTLS(t, optedOut, pool, caPEM, mustKeyPEM(t, caKey)); err != nil {
		t.Fatalf("WithoutControlPlanePeer did not opt out: %v", err)
	}
}

// mustKeyPEM encodes a key as PEM for a client keypair in a test.
func mustKeyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPKCS8(t, key)})
}

// TestAgentAcceptsLegacyControlPlaneCredential is the rollout grace path: a
// control plane upgraded after the agent still presents the pre-change
// credential (clientAuth only, control-plane name), which must keep working. A
// node leaf never matches it (its key usage includes serverAuth).
func TestAgentAcceptsLegacyControlPlaneCredential(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	pool := testPool(t, caPEM)
	addr := startTestAgentTLS(t, caPEM, caCert, caKey)

	legacyCert, legacyKey := issueTestLeaf(t, caCert, caKey, controlPlaneClientName,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil, []string{controlPlaneClientName},
		time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if _, err := listOverMTLS(t, addr, pool, legacyCert, legacyKey); err != nil {
		t.Fatalf("the legacy control-plane credential was rejected: %v", err)
	}
}

// TestRequireControlPlanePeerFromContext proves the gRPC interceptor is a second
// gate: it refuses a peer whose certificate is not the control plane even if the
// transport were misconfigured.
func TestRequireControlPlanePeerFromContext(t *testing.T) {
	_, caCert, caKey := testCA(t)

	cpCert, _ := controlPlaneLeaf(t, caCert, caKey)
	if err := requireControlPlanePeerFromContext(peerWithCert(t, cpCert), discardLogger()); err != nil {
		t.Fatalf("control-plane peer rejected by the interceptor: %v", err)
	}
	nodeCert, _ := nodeLeaf(t, caCert, caKey, "node-a")
	if err := requireControlPlanePeerFromContext(peerWithCert(t, nodeCert), discardLogger()); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("node peer = %v, want PermissionDenied", err)
	}
	if err := requireControlPlanePeerFromContext(context.Background(), discardLogger()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("peerless context = %v, want Unauthenticated", err)
	}
}

// peerWithCert returns a context whose peer carries certPEM as the verified
// client certificate, as the interceptor reads it.
func peerWithCert(t *testing.T, certPEM []byte) context.Context {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return peer.NewContext(context.Background(), &peer.Peer{
		Addr:     &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}},
	})
}

func mustMarshalPKCS8(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return der
}
