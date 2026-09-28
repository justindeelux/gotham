package servers

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeProxyServer is an in-process ProxyService implementation.
type fakeProxyServer struct {
	agentv1.UnimplementedProxyServiceServer
}

func (fakeProxyServer) WriteProxyConfig(_ context.Context, req *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	written := make([]string, 0, len(req.GetFiles()))
	for _, file := range req.GetFiles() {
		written = append(written, file.GetPath())
	}
	return &agentv1.WriteProxyConfigResponse{Written: written, Reloaded: req.GetVerify()}, nil
}

// startFakeProxyServer serves fakeProxyServer over bufconn and returns the
// dial target plus a context dialer option. serverCreds may be nil for an
// insecure listener.
func startFakeProxyServer(t *testing.T, serverCreds credentials.TransportCredentials) (string, grpc.DialOption) {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	opts := make([]grpc.ServerOption, 0, 1)
	if serverCreds != nil {
		opts = append(opts, grpc.Creds(serverCreds))
	}
	srv := grpc.NewServer(opts...)
	agentv1.RegisterProxyServiceServer(srv, fakeProxyServer{})

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveErr; err != nil {
			t.Logf("fake proxy serve: %v", err)
		}
		_ = lis.Close()
	})

	dialer := grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
	return "passthrough:///bufnet", dialer
}

// exerciseProxyClient calls WriteProxyConfig with both verify values.
func exerciseProxyClient(t *testing.T, ctx context.Context, client agentv1.ProxyServiceClient) {
	t.Helper()

	for _, verify := range []bool{false, true} {
		response, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{
			Files: []*agentv1.ProxyConfigFile{
				{Path: "traefik.yml", Content: []byte("static")},
				{Path: "dynamic/gotham.yml", Content: []byte("dynamic")},
			},
			Verify: verify,
		})
		if err != nil {
			t.Fatalf("WriteProxyConfig(verify=%t): %v", verify, err)
		}
		if len(response.GetWritten()) != 2 || response.GetWritten()[0] != "traefik.yml" {
			t.Fatalf("written = %v", response.GetWritten())
		}
		if response.GetReloaded() != verify {
			t.Fatalf("reloaded = %t, want %t", response.GetReloaded(), verify)
		}
	}
}

func TestDialProxyClientInsecureBufconn(t *testing.T) {
	target, dialer := startFakeProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := DialProxyClient(ctx, target, nil, WithDockerDialOptions(dialer))
	if err != nil {
		t.Fatalf("DialProxyClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	}()
	if client.Conn() == nil {
		t.Fatal("Conn is nil")
	}
	exerciseProxyClient(t, ctx, client)
}

func TestDialProxyClientMTLSBufconn(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	const nodeID = "proxy-test-node"

	serverCertPEM, serverKeyPEM, err := authority.IssueServerCert([]string{nodeID})
	if err != nil {
		t.Fatalf("IssueServerCert: %v", err)
	}
	serverPair, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("load server keypair: %v", err)
	}
	target, dialer := startFakeProxyServer(t, credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    authority.Pool(),
		MinVersion:   tls.VersionTLS12,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := DialProxyClient(ctx, target, authority,
		WithDockerServerName(nodeID),
		WithDockerDialOptions(dialer),
	)
	if err != nil {
		t.Fatalf("DialProxyClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	}()
	exerciseProxyClient(t, ctx, client)
}

func TestDialProxyClientValidation(t *testing.T) {
	if _, err := DialProxyClient(context.Background(), "", nil); err == nil {
		t.Fatal("DialProxyClient(empty target) = nil error, want validation error")
	}
}

func TestProxyClientCloseNilSafe(t *testing.T) {
	var client *ProxyClient
	if err := client.Close(); err != nil {
		t.Fatalf("nil Close = %v, want nil", err)
	}
	if conn := client.Conn(); conn != nil {
		t.Fatalf("nil Conn = %v, want nil", conn)
	}
}
