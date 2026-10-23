package servers

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
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

func testKeyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

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

func dialNoClientCert(t *testing.T, addr string, authority *Authority) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(authority.Pool(), "")))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

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

func issueNodeCert(t *testing.T, authority *Authority, commonName string, key *ecdsa.PrivateKey, usages []x509.ExtKeyUsage, validity time.Duration) []byte {
	t.Helper()
	cert, err := authority.sign(commonName, []string{commonName}, nil, nil, usages, validity, &key.PublicKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func csrFor(t *testing.T, nodeID string, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	return testCSR(t, key, &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID}, DNSNames: []string{nodeID}})
}

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

func enrollNode(t *testing.T, addr string, authority *Authority, nodeID string) (certPEM, keyPEM []byte) {
	t.Helper()
	key := testKey(t)
	return enrollNodeKey(t, addr, authority, nodeID, key), testKeyPEM(t, key)
}

func cleanupNode(t *testing.T, service *ServerService, nodeID string) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, regErr := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, cpCert, cpKey)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: "victim", Csr: csrFor(t, "victim", testKey(t))})
	if status.Code(regErr) != codes.Unauthenticated {
		t.Fatalf("control-plane credential Register = %v, want Unauthenticated", regErr)
	}
}

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

func TestGatewayEnrollmentRefusalsAreUniform(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service, EnrollmentToken: "tok"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, authority))
	wrong := metadata.AppendToOutgoingContext(ctx, enrollmentTokenMetadataKey, "nope")
	valid := metadata.AppendToOutgoingContext(ctx, enrollmentTokenMetadataKey, "tok")
	message := func(err error) string { return status.Convert(err).Message() }
	code := func(err error) codes.Code { return status.Convert(err).Code() }

	unknownID := uniqueNodeID("node-uniform-unknown")
	_, errUnknown := client.Register(wrong, &agentv1.RegisterRequest{NodeId: unknownID, Csr: csrFor(t, unknownID, testKey(t))})

	enrolledID := uniqueNodeID("node-uniform-enrolled")
	if _, err := client.Register(valid, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))}); err != nil {
		t.Fatalf("enroll with the token: %v", err)
	}
	cleanupNode(t, service, enrolledID)
	_, errEnrolled := client.Register(wrong, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))})
	_, errNoToken := client.Register(ctx, &agentv1.RegisterRequest{NodeId: enrolledID, Csr: csrFor(t, enrolledID, testKey(t))})

	if message(errUnknown) != message(errEnrolled) || message(errUnknown) != message(errNoToken) {
		t.Fatalf("enrollment refusal leaks existence: unknown=%q enrolled=%q none=%q",
			message(errUnknown), message(errEnrolled), message(errNoToken))
	}
	if code(errUnknown) != code(errEnrolled) || code(errUnknown) != code(errNoToken) {
		t.Fatalf("enrollment refusal leaks existence by code: unknown=%v enrolled=%v none=%v",
			code(errUnknown), code(errEnrolled), code(errNoToken))
	}
	if code(errUnknown) != codes.PermissionDenied {
		t.Fatalf("wrong-token enrollment = %v, want PermissionDenied", errUnknown)
	}
}

func TestGatewayKeyErrorsAreUniform(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startGatewayConfig(t, GatewayConfig{Authority: authority, Service: service, AllowOpenEnrollment: true})

	nodeID := uniqueNodeID("node-key-uniform")
	enrollNode(t, addr, authority, nodeID)
	cleanupNode(t, service, nodeID)

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

func TestGatewayLegacyLeafSunset(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	deadline := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)

	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	nodeID := uniqueNodeID("node-sunset")
	key := testKey(t)
	keyPEM := testKeyPEM(t, key)
	legacy := issueNodeCert(t, authority, nodeID, key, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Hour)

	bootstrapAddr := startGatewayConfig(t, GatewayConfig{
		Authority: authority, Service: service, AllowOpenEnrollment: true,
		LegacyLeafDeadline: deadline, Now: func() time.Time { return before },
		Logger: logger,
	})
	enrollNodeKey(t, bootstrapAddr, authority, nodeID, key)
	cleanupNode(t, service, nodeID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, bootstrapAddr, authority, legacy, keyPEM)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)}); err != nil {
		t.Fatalf("a legacy leaf before the sunset = %v, want success", err)
	}
	if !strings.Contains(logs.String(), "legacy serverAuth-only node certificate") {
		t.Fatalf("no per-accept warning was logged; logs: %s", logs.String())
	}
	if count, err := service.LegacyLeafNodeCount(ctx); err != nil || count < 1 {
		t.Fatalf("legacy node count = %d (err %v), want >= 1", count, err)
	}

	afterAddr := startGatewayConfig(t, GatewayConfig{
		Authority: authority, Service: service, AllowOpenEnrollment: true,
		LegacyLeafDeadline: deadline, Now: func() time.Time { return after },
	})
	_, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, afterAddr, authority, legacy, keyPEM)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a legacy leaf after the sunset = %v, want Unauthenticated", err)
	}

	modern := issueNodeCert(t, authority, nodeID, key, nodeCertUsages, time.Hour)
	if _, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, afterAddr, authority, modern, keyPEM)).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)}); err != nil {
		t.Fatalf("a dual-EKU leaf after the sunset = %v, want success", err)
	}
	if count, err := service.LegacyLeafNodeCount(ctx); err != nil || count != 0 {
		t.Fatalf("legacy node count after renewal = %d (err %v), want 0", count, err)
	}
}

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

func TestGatewayRejectsExpiredClientCertificate(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	key := testKey(t)
	certPEM := issueNodeCert(t, authority, "node-expired", key, nodeCertUsages, -time.Hour)
	assertHeartbeatRejected(t, addr, authority, certPEM, testKeyPEM(t, key), codes.Unauthenticated)
}

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

func TestGatewayAcceptsServerAuthOnlyLeafUpgrade(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	nodeID := uniqueNodeID("node-upgrade")
	key := testKey(t)
	enrollNodeKey(t, addr, authority, nodeID, key)
	cleanupNode(t, service, nodeID)

	legacy := issueNodeCert(t, authority, nodeID, key, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, authority, legacy, testKeyPEM(t, key))).Register(
		ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)}); err != nil {
		t.Fatalf("Register with a serverAuth-only leaf = %v, want success", err)
	}
}

func TestGatewayRejectsRotatedNodeKey(t *testing.T) {
	service, _, authority := newTestServiceWithAuthority(t)
	addr := startAuthorityGateway(t, service, authority)

	nodeID := uniqueNodeID("node-rotate")
	enrollNode(t, addr, authority, nodeID)
	cleanupNode(t, service, nodeID)

	otherKey := testKey(t)
	otherCert := issueNodeCert(t, authority, nodeID, otherKey, nodeCertUsages, time.Hour)
	assertHeartbeatRejected(t, addr, authority, otherCert, testKeyPEM(t, otherKey), codes.PermissionDenied)
}

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
	if _, err := st.DB.Exec(ctx, "DELETE FROM servers WHERE node_id = $1", nodeID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	_ = stream.Send(&agentv1.HeartbeatRequest{SentAt: timestamppb.Now()})
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("heartbeat after revocation = %v, want PermissionDenied", err)
	}
}

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
	cleanupNode(t, service, nodeID)

	if _, err := client.Register(callCtx, &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("second certificate-less Register = %v, want PermissionDenied", err)
	}
}
