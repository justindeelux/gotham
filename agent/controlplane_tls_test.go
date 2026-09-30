package agent

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// TestControlPlaneDialUsesTLSWithCA proves the agent's control-plane connection
// — the same conn the updater's RequestUpdate client is built on — runs the
// CA-backed TLS credentials when GOTHAM_AGENT_CA is configured: the dial
// completes a TLS handshake against a TLS-only server, while the same dial
// without a CA is refused by that server.
func TestControlPlaneDialUsesTLSWithCA(t *testing.T) {
	caPEM, caCert, caKey := testCA(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	leafPEM, leafKeyPEM := testLeaf(t, caCert, caKey)
	keyPair, err := tls.X509KeyPair(leafPEM, leafKeyPEM)
	if err != nil {
		t.Fatalf("load leaf keypair: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{keyPair},
		MinVersion:   tls.VersionTLS12,
	})))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	// requestUpdate dials the TLS-only server with the agent's real dial options
	// and issues the updater's RPC.
	requestUpdate := func(caPath string) error {
		t.Helper()
		agentInstance := NewAgent(Config{CA: caPath, CPAddr: listener.Addr().String()}, discardLogger(), nil)
		options, err := agentInstance.dialOptionsFor()
		if err != nil {
			return err
		}
		// The test leaf carries the "localhost" SAN; pin the TLS server name so
		// the loopback listener address still verifies.
		options = append(options, grpc.WithAuthority("localhost"))
		conn, err := grpc.NewClient(listener.Addr().String(), options...)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = agentv1.NewUpdateServiceClient(conn).RequestUpdate(ctx, &agentv1.UpdateRequest{})
		return err
	}

	// With the CA configured the handshake completes; no service is registered,
	// so the RPC surfaces as Unimplemented.
	if err := requestUpdate(caPath); status.Code(err) != codes.Unimplemented {
		t.Fatalf("RequestUpdate with a CA = %v (code %v), want Unimplemented after a TLS handshake", err, status.Code(err))
	}
	// Without a CA the agent dials insecure and the TLS-only server must refuse.
	if err := requestUpdate(""); status.Code(err) == codes.Unimplemented {
		t.Fatal("an insecure dial reached a TLS-only control-plane server")
	}
}
