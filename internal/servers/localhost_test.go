package servers

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Localhost reconciliation scans all nodes; unrelated loopback fixtures must be isolated.
func newLocalhostTestService(t *testing.T) (*ServerService, *store.Store) {
	t.Helper()
	_, admin := newTestService(t)
	ctx := context.Background()
	scratch := fmt.Sprintf("localhost_%d", time.Now().UnixNano())
	if _, err := admin.DB.Exec(ctx, "CREATE DATABASE "+scratch); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.DB.Exec(ctx, "DROP DATABASE "+scratch+" WITH (FORCE)"); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	dsn, err := url.Parse(gatewayTestDSN())
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path = "/" + scratch
	t.Setenv("GOTHAM_TEST_DSN", dsn.String())
	return newTestService(t)
}

func TestEnsureLocalhostLegacyNode(t *testing.T) {
	for _, scenario := range []struct {
		name, nodeID, ip     string
		ambiguous, collision bool
	}{
		{name: "custom identity", nodeID: "test-node-1", ip: "127.0.0.1"},
		{name: "hostname identity", nodeID: "original-host-agent", ip: "127.0.0.1"},
		{name: "agent address", nodeID: "test-node-1", ip: "localhost:9443"},
		{name: "ambiguous", nodeID: "test-node-1", ip: "127.0.0.1", ambiguous: true},
		{name: "collision", nodeID: "test-node-1", ip: "127.0.0.1", collision: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, st := newLocalhostTestService(t)
			ctx := context.Background()
			t.Cleanup(func() {
				_, _ = st.DB.Exec(ctx, "DELETE FROM servers WHERE node_id IN ('localhost', 'test-node-1', 'original-host-agent', 'other-local')")
			})
			var originalID string
			if err := st.DB.QueryRow(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id,status)
				VALUES ('old',$1,'',$2,'ready') RETURNING id::text`, scenario.ip, scenario.nodeID).Scan(&originalID); err != nil {
				t.Fatal(err)
			}
			if scenario.ambiguous || scenario.collision {
				nodeID := "other-local"
				if scenario.collision {
					nodeID = "localhost"
				}
				if _, err := st.DB.Exec(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id)
					VALUES ('other','127.0.0.1','',$1)`, nodeID); err != nil {
					t.Fatal(err)
				}
			}
			err := s.EnsureLocalhost(ctx)
			if scenario.ambiguous || scenario.collision {
				if err == nil || !strings.Contains(err.Error(), "reconcile") {
					t.Fatalf("EnsureLocalhost = %v; want actionable reconciliation error", err)
				}
				var unchanged bool
				if err := st.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id::text=$1 AND node_id=$2)`, originalID, scenario.nodeID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatalf("legacy node changed: preserved=%v, err=%v", unchanged, err)
				}
				var count int
				if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM servers WHERE node_id='localhost'`).Scan(&count); err != nil || (!scenario.collision && count != 0) {
					t.Fatalf("created competing localhost: count=%d, err=%v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// A second startup must retain the same UUID and live inventory status.
			if err := s.EnsureLocalhost(ctx); err != nil {
				t.Fatal(err)
			}
			var preserved bool
			if err := st.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id::text=$1
				AND node_id='localhost' AND name='localhost' AND ip='127.0.0.1' AND status='ready')`, originalID).Scan(&preserved); err != nil || !preserved {
				t.Fatalf("local identity lost: preserved=%v, err=%v", preserved, err)
			}
		})
	}
}

func TestEnsureLocalhost(t *testing.T) {
	s, st := newLocalhostTestService(t)
	ctx := context.Background()
	if err := s.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	id := "localhost"
	row, err := st.GetServerByNodeID(ctx, &id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(ctx, "DELETE FROM servers WHERE node_id = 'localhost'") })
	if row.Status != StatusPending {
		t.Fatalf("status = %s; want pending until heartbeat", row.Status)
	}
	if err := s.Delete(ctx, uuidFromPG(row.ID)); !errors.Is(err, ErrConflict) {
		t.Fatalf("Delete = %v; want conflict", err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE servers SET ip = 'wrong', name = 'wrong', status = 'ready' WHERE id = $1", row.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	repaired, err := st.GetServerByNodeID(ctx, &id)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != row.ID || repaired.Ip != "127.0.0.1" || repaired.Name != id || repaired.Status != StatusReady {
		t.Fatalf("repair lost identity or status: %+v", repaired)
	}
	if _, err := st.DB.Exec(ctx, "DELETE FROM servers WHERE id = $1", row.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetServerByNodeID(ctx, &id); err != nil {
		t.Fatal(err)
	}
}

func TestLocalhostRegistration(t *testing.T) {
	s, st, authority := newTestServiceWithAuthority(t)
	g, err := NewGateway(GatewayConfig{Addr: "127.0.0.1:9442", Authority: authority, Service: s, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(context.Background(), "DELETE FROM servers WHERE node_id = 'localhost'") })
	for _, host := range []string{"192.0.2.1", "127.0.0.1", "::1"} {
		ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: 1234}})
		csr := testCSR(t, testKey(t), &x509.CertificateRequest{Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}})
		resp, err := g.Register(ctx, &agentv1.RegisterRequest{NodeId: "localhost", Os: "linux", Csr: csr})
		if host == "192.0.2.1" {
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("remote localhost registration = %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("loopback registration: %v", err)
		}
		cert := parseCertPEM(t, resp.GetCert())
		for _, cpHost := range serverHosts("127.0.0.1:9442", []string{"cp.example.com"}) {
			if _, err := cert.Verify(x509.VerifyOptions{DNSName: cpHost, Roots: authority.Pool()}); err == nil {
				t.Fatalf("agent certificate impersonates CP host %s", cpHost)
			}
		}

		if _, err := cert.Verify(x509.VerifyOptions{DNSName: localhostAgentServerName, Roots: authority.Pool()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNodeID("localhost"); err != nil {
		t.Fatal(err)
	}
	target, name, err := agentTargetFor(&Server{IP: "wrong.example", NodeID: strPtr("localhost")})
	if err != nil || target != "127.0.0.1:9443" || name != localhostAgentServerName {
		t.Fatalf("agent dial = %s, %s, %v", target, name, err)
	}
}

func TestLocalhostUpdateRejected(t *testing.T) {
	s, st := newLocalhostTestService(t)
	ctx := context.Background()
	if err := s.EnsureLocalhost(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(ctx, "DELETE FROM servers WHERE node_id='localhost'") })
	nodeID := "localhost"
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatal(err)
	}
	port := 2222
	for _, params := range []UpdateParams{{Name: strPtr("renamed")}, {IP: strPtr("192.0.2.1")}, {Port: &port}, {SSHUser: strPtr("root")}, {Password: strPtr("password")}, {}} {
		if _, err := s.Update(ctx, uuidFromPG(row.ID), params); !errors.Is(err, ErrConflict) {
			t.Fatalf("Update(%+v) = %v; want conflict", params, err)
		}
	}
	after, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != row.Name || after.Ip != row.Ip || after.Port != row.Port || after.EncryptedPassword != nil {
		t.Fatalf("localhost changed: %+v", after)
	}
}

func TestLocalhostCertificateNameCannotBeCPHost(t *testing.T) {
	s := NewService(Config{Secret: "test", Logger: discardLogger()})
	g, err := NewGateway(GatewayConfig{Addr: "127.0.0.1:9442", Hosts: []string{localhostAgentServerName}, Service: s, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}})
	if _, err := g.Register(ctx, &agentv1.RegisterRequest{NodeId: "localhost"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CP SAN collision allowed: %v", err)
	}
}
