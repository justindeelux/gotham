package servers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"
)

// dialTLS dials addr with a client that trusts pool and verifies serverName,
// completing the handshake.
func dialTLS(ctx context.Context, pool *x509.CertPool, addr, serverName string) error {
	dialer := &tls.Dialer{Config: &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

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
	if err := dialTLS(dialCtx, authority.Pool(), g.listener.Addr().String(), "localhost"); err != nil {
		t.Fatalf("TLS dial to the gateway failed: %v", err)
	}
}

// TestGatewayServesTLSForConfiguredHosts is F1: the listener certificate must
// carry the operator-configured SANs so a remote agent dialing the control
// plane by its name or IP verifies, while a name that is not in the SANs is
// rejected.
func TestGatewayServesTLSForConfiguredHosts(t *testing.T) {
	authority, err := LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateAuthority: %v", err)
	}
	service := NewService(Config{Secret: "tls-test-secret", Logger: discardLogger()})

	g, err := NewGateway(GatewayConfig{
		Addr:      "127.0.0.1:0",
		Hosts:     []string{"cp.example.com", "192.0.2.10"},
		Authority: authority,
		Service:   service,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer g.Stop()

	addr := g.listener.Addr().String()
	dial := func(serverName string) error {
		dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
		defer dialCancel()
		return dialTLS(dialCtx, authority.Pool(), addr, serverName)
	}
	if err := dial("cp.example.com"); err != nil {
		t.Fatalf("dial with a configured DNS SAN failed: %v", err)
	}
	if err := dial("192.0.2.10"); err != nil {
		t.Fatalf("dial with a configured IP SAN failed: %v", err)
	}
	if err := dial("not-configured.example.com"); err == nil {
		t.Fatal("dial with an unconfigured name succeeded; want a certificate error")
	}
}

// TestServerHostsAlwaysIncludesLoopbackAndHostname proves the SAN defaults: the
// extra hosts are additive and the loopback names and machine hostname are
// always present.
func TestServerHostsAlwaysIncludesLoopbackAndHostname(t *testing.T) {
	hosts := serverHosts(":9442", []string{"cp.example.com"})
	want := []string{"cp.example.com", "localhost", "127.0.0.1", "::1"}
	if name, err := os.Hostname(); err == nil && name != "" {
		want = append(want, name)
	}
	for _, w := range want {
		if !containsString(hosts, w) {
			t.Errorf("serverHosts = %v, missing %q", hosts, w)
		}
	}
}
