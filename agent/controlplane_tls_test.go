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

	requestUpdate := func(caPath string) error {
		t.Helper()
		agentInstance := NewAgent(Config{CA: caPath, CPAddr: listener.Addr().String()}, discardLogger(), nil)
		options, err := agentInstance.dialOptionsFor()
		if err != nil {
			return err
		}
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

	if err := requestUpdate(caPath); status.Code(err) != codes.Unimplemented {
		t.Fatalf("RequestUpdate with a CA = %v (code %v), want Unimplemented after a TLS handshake", err, status.Code(err))
	}
	if err := requestUpdate(""); status.Code(err) == codes.Unimplemented {
		t.Fatal("an insecure dial reached a TLS-only control-plane server")
	}
}
