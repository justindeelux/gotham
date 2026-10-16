package servers

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// testKeyPEM encodes a test private key as PEM so a test can build a client
// keypair for the node certificate it was issued.
func testKeyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// dialWithClientCert dials the gateway presenting certPEM/keyPEM as the client
// certificate (the mutual-TLS agent connection).
func dialWithClientCert(t *testing.T, addr string, authority *Authority, certPEM, keyPEM []byte) *grpc.ClientConn {
	t.Helper()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load client keypair: %v", err)
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      authority.Pool(),
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// dialNoClientCert dials the gateway presenting no client certificate, as the
// one bootstrap registration does.
func dialNoClientCert(t *testing.T, addr string, authority *Authority) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(authority.Pool(), "")))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// peerWithClientCert returns ctx with certPEM attached as the verified client
// certificate, exactly as the gateway reads it from a mutual-TLS peer. It lets
// the direct-call (no listener) tests exercise the certificate path.
func peerWithClientCert(ctx context.Context, certPEM []byte) context.Context {
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic(err)
	}
	p, _ := peer.FromContext(ctx)
	return peer.NewContext(ctx, &peer.Peer{
		Addr:     p.Addr,
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}},
	})
}

// issueNodeCert mints a node certificate signed by authority for a key that may
// or may not be enrolled, so a test can model a forged, rotated, expired or
// legacy (serverAuth-only) credential.
func issueNodeCert(t *testing.T, authority *Authority, commonName string, key *ecdsa.PrivateKey, usages []x509.ExtKeyUsage, validity time.Duration) []byte {
	t.Helper()
	cert, err := authority.sign(commonName, []string{commonName}, nil, nil, usages, validity, &key.PublicKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// csrFor builds a CSR for nodeID signed by key.
func csrFor(t *testing.T, nodeID string, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	return testCSR(t, key, &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID}, DNSNames: []string{nodeID}})
}

// enrollNodeKey bootstraps a fresh node with the given key and returns the node
// certificate PEM, leaving the node bound to the key.
func enrollNodeKey(t *testing.T, addr string, authority *Authority, nodeID string, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority)).Register(ctx, &agentv1.RegisterRequest{
		NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key),
	})
	if err != nil {
		t.Fatalf("bootstrap Register: %v", err)
	}
	return resp.GetCert()
}

// enrollNode bootstraps a fresh node and returns its node certificate and key PEM.
func enrollNode(t *testing.T, addr string, authority *Authority, nodeID string) (certPEM, keyPEM []byte) {
	t.Helper()
	key := testKey(t)
	return enrollNodeKey(t, addr, authority, nodeID, key), testKeyPEM(t, key)
}

// deleteNode removes the node row at test cleanup.
func deleteNode(t *testing.T, service *ServerService, nodeID string) {
	t.Helper()
	store := service.store
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		row, err := store.GetServerByNodeID(ctx, &nodeID)
		if err != nil {
			return
		}
		_ = store.DeleteServer(ctx, row.ID)
	})
}

// assertHeartbeatRejected opens a heartbeat with the given keypair and expects it
// to be refused with want.
func assertHeartbeatRejected(t *testing.T, addr string, authority *Authority, certPEM, keyPEM []byte, want codes.Code) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, certPEM, keyPEM)).Heartbeat(
		metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, "victim"))
	if err == nil {
		_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
		_, err = stream.CloseAndRecv()
	}
	if status.Code(err) != want {
		t.Fatalf("Heartbeat = %v, want %v", err, want)
	}
}

// startGatewayConfig starts a gateway over a real TCP listener with cfg and
// returns its address, for tests that need a specific config.
func startGatewayConfig(t *testing.T, cfg GatewayConfig) string {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	gateway, err := NewGateway(cfg)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := gateway.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}
	t.Cleanup(gateway.Stop)
	return gateway.listener.Addr().String()
}

// heartbeatWithClaim opens a heartbeat with the given keypair claiming nodeID
// and returns the resulting error.
func heartbeatWithClaim(t *testing.T, addr string, authority *Authority, certPEM, keyPEM []byte, claimed string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, certPEM, keyPEM)).Heartbeat(
		metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, claimed))
	if err == nil {
		_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
		_, err = stream.CloseAndRecv()
	}
	return err
}

// TestGatewayRejectsControlPlaneCredentialAsNode: the CP's own client credential
// (the agent-direction identity) is not a node credential, even though it chains
// to the same CA.
func TestGatewayRejectsControlPlaneCredentialAsNode(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service, AllowOpenEnrollment: true})

	cpCert, cpKey, err := authority.IssueClientCert("")
	if err != nil {
		t.Fatalf("IssueClientCert: %v", err)
	}
	if err := heartbeatWithClaim(t, addr, authority, cpCert, cpKey, "victim"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("control-plane credential presented as a node = %v, want Unauthenticated", err)
	}
	// ... and it cannot register either.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, regErr := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, cpCert, cpKey)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: "victim", Csr: csrFor(t, "victim", testKey(t))})
	if status.Code(regErr) != codes.Unauthenticated {
		t.Fatalf("control-plane credential Register = %v, want Unauthenticated", regErr)
	}
}

// TestGatewayEnrollmentFailsClosedWithoutToken: with a CA and neither an
// enrollment token nor the open-enrollment opt-in, a certificate-less first
// registration is refused and creates no row.
func TestGatewayEnrollmentFailsClosedWithoutToken(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nodeID := uniqueNodeID("node-closed")
	_, err := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Csr: csrFor(t, nodeID, testKey(t))})
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "enrollment is closed") {
		t.Fatalf("Register with enrollment closed = %v, want PermissionDenied/enrollment is closed", err)
	}
	if _, lookupErr := st.GetServerByNodeID(ctx, &nodeID); lookupErr == nil {
		t.Fatal("a refused enrollment created a registry row")
	}
}

// TestGatewayEnrollmentRefusalsAreUniform: a caller with no or a wrong token
// gets the same refusal whether or not the node id exists, so enrollment cannot
// be used to enumerate node ids.
func TestGatewayEnrollmentRefusalsAreUniform(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service, EnrollmentToken: "tok"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority))
	wrong := metadata.AppendToOutgoingContext(ctx, enrollmentTokenMetadataKey, "nope")
	valid := metadata.AppendToOutgoingContext(ctx, enrollmentTokenMetadataKey, "tok")
	message := func(err error) string { return status.Convert(err).Message() }

	unknownID := uniqueNodeID("node-uniform-unknown")
	_, errUnknown := client.Register(wrong, &agentv1.RegisterRequest{NodeId: unknownID, Csr: csrFor(t, unknownID, testKey(t))})

	enrolledID := uniqueNodeID("node-uniform-enrolled")
	if _, err := client.Register(valid, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))}); err != nil {
		t.Fatalf("enroll with the token: %v", err)
	}
	deleteNode(t, service, enrolledID)
	_, errEnrolled := client.Register(wrong, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))})
	_, errNoToken := client.Register(ctx, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))})

	if message(errUnknown) != message(errEnrolled) || message(errUnknown) != message(errNoToken) {
		t.Fatalf("enrollment refusal leaks existence: unknown=%q enrolled=%q none=%q",
			message(errUnknown), message(errEnrolled), message(errNoToken))
	}
	if status.Code(errUnknown) != codes.PermissionDenied {
		t.Fatalf("wrong-token enrollment = %v, want PermissionDenied", errUnknown)
	}
}

// TestGatewayKeyErrorsAreUniform: an unknown node and a key mismatch return the
// same refusal, so a CA-valid certificate holder cannot probe the registry.
func TestGatewayKeyErrorsAreUniform(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service, AllowOpenEnrollment: true})

	nodeID := uniqueNodeID("node-key-uniform")
	enrollNode(t, addr, authority, nodeID)
	deleteNode(t, service, nodeID)

	otherKey := testKey(t)
	otherCert := issueNodeCert(t, authority, nodeID, otherKey, nodeCertUsages, time.Hour)
	errMismatch := heartbeatWithClaim(t, addr, authority, otherCert, testKeyPEM(t, otherKey), nodeID)

	unknownID := uniqueNodeID("node-key-unknown")
	unknownKey := testKey(t)
	unknownCert := issueNodeCert(t, authority, unknownID, unknownKey, nodeCertUsages, time.Hour)
	errUnknown := heartbeatWithClaim(t, addr, authority, unknownCert, testKeyPEM(t, unknownKey), unknownID)

	if status.Convert(errMismatch).Message() != status.Convert(errUnknown).Message() {
		t.Fatalf("key refusal leaks state: mismatch=%q unknown=%q",
			status.Convert(errMismatch).Message(), status.Convert(errUnknown).Message())
	}
	if status.Code(errMismatch) != codes.PermissionDenied {
		t.Fatalf("key mismatch = %v, want PermissionDenied", errMismatch)
	}
}

// TestGatewayRejectsUnauthenticatedHeartbeatWhenTLSRequired is the JUS-194 core:
// a client that presents no certificate cannot heartbeat on the TLS listener.
func TestGatewayRejectsUnauthenticatedHeartbeatWhenTLSRequired(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority)).Heartbeat(
		metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, "victim"))
	if err == nil {
		_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
		_, err = stream.CloseAndRecv()
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Heartbeat without a client certificate = %v, want Unauthenticated", err)
	}
}

// TestGatewayRejectsUnauthenticatedRequestUpdate proves every RPC on the TLS
// listener requires a client certificate, not just the agent stream.
func TestGatewayRejectsUnauthenticatedRequestUpdate(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := agentv1.NewUpdateServiceClient(dialNoClientCert(t, addr, authority)).RequestUpdate(ctx, &agentv1.UpdateRequest{AgentVersion: "v1.0.0", Arch: "amd64"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("RequestUpdate without a client certificate = %v, want Unauthenticated", err)
	}
}

// TestGatewayRejectsPlaintextDialWhenTLSRequired proves a plaintext client
// cannot reach the authenticated agent channel.
func TestGatewayRejectsPlaintextDialWhenTLSRequired(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := agentv1.NewAgentServiceClient(conn).Register(ctx, &agentv1.RegisterRequest{NodeId: "node-plaintext", Csr: []byte("x")}); err == nil {
		t.Fatal("a plaintext Register reached the TLS agent channel")
	}
}

// TestGatewayRejectsExpiredClientCertificate: a CA-signed leaf outside its
// validity window is not a credential.
func TestGatewayRejectsExpiredClientCertificate(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	key := testKey(t)
	certPEM := issueNodeCert(t, authority, "node-expired", key, nodeCertUsages, -time.Hour)
	assertHeartbeatRejected(t, addr, authority, certPEM, testKeyPEM(t, key), codes.Unauthenticated)
}

// TestGatewayRejectsForeignCAClientCertificate: a certificate signed by another
// CA does not chain to the control-plane authority.
func TestGatewayRejectsForeignCAClientCertificate(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	foreign, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	key := testKey(t)
	certPEM := issueNodeCert(t, foreign, "node-foreign", key, nodeCertUsages, time.Hour)
	assertHeartbeatRejected(t, addr, authority, certPEM, testKeyPEM(t, key), codes.Unauthenticated)
}

// TestGatewayRejectsCACertificateAsNodeCredential: the CA certificate itself is
// not a node credential.
func TestGatewayRejectsCACertificateAsNodeCredential(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	keyDER, err := x509.MarshalECPrivateKey(authority.key)
	if err != nil {
		t.Fatalf("marshal CA key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	assertHeartbeatRejected(t, addr, authority, authority.CACertPEM(), keyPEM, codes.Unauthenticated)
}

// TestGatewayAcceptsServerAuthOnlyLeafUpgrade is the upgrade path: a leaf issued
// before mutual TLS carries only serverAuth and must keep authenticating until
// the node renews.
func TestGatewayAcceptsServerAuthOnlyLeafUpgrade(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	nodeID := uniqueNodeID("node-upgrade")
	key := testKey(t)
	enrollNodeKey(t, addr, authority, nodeID, key)
	deleteNode(t, service, nodeID)

	legacy := issueNodeCert(t, authority, nodeID, key, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, legacy, testKeyPEM(t, key))).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)}); err != nil {
		t.Fatalf("Register with a serverAuth-only leaf = %v, want success", err)
	}
}

// TestGatewayRejectsRotatedNodeKey: once a node is bound to a key, a certificate
// for a different key cannot act as that node.
func TestGatewayRejectsRotatedNodeKey(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	nodeID := uniqueNodeID("node-rotate")
	enrollNode(t, addr, authority, nodeID)
	deleteNode(t, service, nodeID)

	otherKey := testKey(t)
	otherCert := issueNodeCert(t, authority, nodeID, otherKey, nodeCertUsages, time.Hour)
	assertHeartbeatRejected(t, addr, authority, otherCert, testKeyPEM(t, otherKey), codes.PermissionDenied)
}

// TestGatewayClosesRevokedNodeStream: removing a node invalidates its
// credential and closes its live heartbeat stream.
func TestGatewayClosesRevokedNodeStream(t *testing.T) {
	service, st, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	nodeID := uniqueNodeID("node-revoke")
	certPEM, keyPEM := enrollNode(t, addr, authority, nodeID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, certPEM, keyPEM))
	stream, err := client.Heartbeat(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		row, err := st.GetServerByNodeID(ctx, &nodeID)
		if err == nil && row.CpuUsage != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no heartbeat recorded before revocation")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Revoke: removing the node row invalidates the credential.
	if _, err := st.DB.Exec(ctx, "DELETE FROM servers WHERE node_id = $1", nodeID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("heartbeat after revocation = %v, want PermissionDenied", err)
	}
}

// TestGatewayBootstrapRequiresEnrollmentToken: with an enrollment token
// configured, a certificate-less first registration must present it.
func TestGatewayBootstrapRequiresEnrollmentToken(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway, err := NewGateway(GatewayConfig{
		Addr:            "127.0.0.1:0",
		Authority:       authority,
		Service:         service,
		Logger:          discardLogger(),
		EnrollmentToken: "s3cret-enroll",
	})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if err := gateway.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}
	t.Cleanup(gateway.Stop)
	addr := gateway.listener.Addr().String()

	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()
	key := testKey(t)
	client := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority))

	nodeID := uniqueNodeID("node-token")
	csr := csrFor(t, nodeID, key)
	if _, err := client.Register(callCtx, &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Register without the enrollment token = %v, want PermissionDenied", err)
	}
	if _, err := client.Register(metadata.AppendToOutgoingContext(callCtx, enrollmentTokenMetadataKey, "wrong"), &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Register with a wrong enrollment token = %v, want PermissionDenied", err)
	}
	resp, err := client.Register(metadata.AppendToOutgoingContext(callCtx, enrollmentTokenMetadataKey, "s3cret-enroll"), &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr})
	if err != nil {
		t.Fatalf("Register with the enrollment token: %v", err)
	}
	if len(resp.GetCert()) == 0 {
		t.Fatal("Register with the enrollment token returned no certificate")
	}
	deleteNode(t, service, nodeID)

	// The enrolled node still needs its certificate for a second enrollment.
	if _, err := client.Register(callCtx, &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("second certificate-less Register = %v, want PermissionDenied", err)
	}
}
