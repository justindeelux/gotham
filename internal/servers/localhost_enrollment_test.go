package servers

import (
	"context"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestLocalhostPlaceholderEnrollment(t *testing.T) {
	service, st := newLocalhostTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := service.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	nodeID := "localhost"
	original, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatal(err)
	}
	addr := startGatewayConfig(t, GatewayConfig{Service: service, Authority: service.authority, EnrollmentToken: "bootstrap-test"})
	client := agentv1.NewAgentServiceClient(dialNoClientCert(t, addr, service.authority))
	key := testKey(t)
	req := &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux", Csr: csrFor(t, nodeID, key)}
	if _, err := client.Register(ctx, req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing token: %v", err)
	}
	tokenCtx := metadata.AppendToOutgoingContext(ctx, enrollmentTokenMetadataKey, "bootstrap-test")
	response, err := client.Register(tokenCtx, req)
	if err != nil {
		t.Fatalf("fresh localhost placeholder must enroll: %v", err)
	}
	if len(response.GetCert()) == 0 {
		t.Fatal("no client certificate issued")
	}
	// Even the valid token cannot enroll this identity a second time.
	for _, csr := range [][]byte{req.Csr, csrFor(t, nodeID, testKey(t))} {
		if _, err := client.Register(tokenCtx, &agentv1.RegisterRequest{NodeId: nodeID, Csr: csr}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("token reuse: %v", err)
		}
	}
	authClient := agentv1.NewAgentServiceClient(dialWithClientCert(t, addr, service.authority, response.Cert, testKeyPEM(t, key)))
	stream, err := authClient.Heartbeat(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&agentv1.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.CloseAndRecv(); err != nil {
		t.Fatalf("authenticated heartbeat: %v", err)
	}
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != original.ID || row.Status != StatusReady || !row.LastSeen.Valid || row.AgentKeyFingerprint == nil {
		t.Fatalf("identity/heartbeat not preserved: %+v", row)
	}
}

func TestLocalhostLegacyRegistrationCannotBootstrap(t *testing.T) {
	service, st := newLocalhostTestService(t)
	ctx := context.Background()
	if err := service.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE servers SET os='linux' WHERE node_id='localhost'`); err != nil {
		t.Fatal(err)
	}
	_, enrolled, err := service.NodeKeyFingerprint(ctx, "localhost")
	if err != nil || !enrolled {
		t.Fatalf("legacy unbound node must remain enrolled: %v %v", enrolled, err)
	}
}
