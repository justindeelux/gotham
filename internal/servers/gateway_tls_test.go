package servers

import (
	"context"
	"crypto/tls"
	"testing"
	"time"
)

// TestGatewayServesTLSWhenAuthorityConfigured proves the serve path runs the
// gRPC gateway over TLS once a CA exists: the listener presents a server
// certificate that verifies against the CA pool.
func TestGatewayServesTLSWhenAuthorityConfigured(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	service := NewService(Config{Secret: "tls-test-secret", Logger: discardLogger()})

	g, err := NewGateway(GatewayConfig{
		Addr:      "127.0.0.1:0",
		Authority: authority,
		Service:   service,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if !g.tlsEnabled {
		t.Fatal("gateway tlsEnabled = false with a CA configured")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer g.Stop()

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()
	dialer := &tls.Dialer{Config: &tls.Config{
		RootCAs:    authority.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}}
	conn, err := dialer.DialContext(dialCtx, "tcp", g.listener.Addr().String())
	if err != nil {
		t.Fatalf("TLS dial to the gateway failed: %v", err)
	}
	_ = conn.Close()
}
