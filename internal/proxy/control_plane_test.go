package proxy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type panelNodes struct{ nodes []NamedNode }

func (n panelNodes) ListNodes(context.Context) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(n.nodes))
	for _, node := range n.nodes {
		ids = append(ids, node.ID)
	}
	return ids, nil
}
func (n panelNodes) ListNamedNodes(context.Context) ([]NamedNode, error) { return n.nodes, nil }

func panelFixture(t *testing.T) *syncFixture {
	t.Helper()
	id := uuid.New()
	f := newSyncFixture(t, nil, id)
	f.service.nodes = panelNodes{[]NamedNode{{ID: id, Name: "localhost"}}}
	f.service.controlPlaneURL = func(context.Context) (string, error) { return "https://Panel.Example.com:8443/", nil }
	f.service.serverPort = 8000
	return f
}

func TestControlPlaneConfig(t *testing.T) {
	providerID := uuid.New()
	access := map[uuid.UUID]providerAccess{providerID: {provider: DNSProvider{
		ID: providerID, Enabled: true, Provider: ProviderCloudflare, Zones: []string{"example.com"},
	}}}
	for _, tc := range []struct {
		name     string
		intent   *CertificateIntent
		resolver string
	}{
		{name: "http only"},
		{name: "http01", intent: &CertificateIntent{Domain: "panel.example.com", Enabled: true, Challenge: ChallengeHTTP01}, resolver: DefaultResolverName},
		{name: "dns01", intent: &CertificateIntent{Domain: "panel.example.com", Enabled: true, Challenge: ChallengeDNS01, DNSProviderID: providerID}, resolver: DNSResolverPrefix + "cloudflare"},
		{name: "wildcard", intent: &CertificateIntent{Domain: "app.example.com", Enabled: true, Challenge: ChallengeDNS01, Wildcard: true, DNSProviderID: providerID}, resolver: DNSResolverPrefix + "cloudflare"},
		{name: "disabled", intent: &CertificateIntent{Domain: "panel.example.com", Challenge: ChallengeHTTP01}},
		{name: "unrelated", intent: &CertificateIntent{Domain: "app.example.com", Enabled: true, Challenge: ChallengeDNS01, DNSProviderID: providerID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := panelFixture(t)
			apps := []ProxiedApplication{{ID: uuid.New(), Certificate: tc.intent}}
			routes, redirects, diagnostics, count := f.service.gatherRoutes(context.Background(), apps, nil, nil, f.serverID, nil, access)
			if len(routes) != 1 || len(diagnostics) != 0 || count != 0 {
				t.Fatalf("routes=%+v diagnostics=%+v", routes, diagnostics)
			}
			cfg := BuildConfig(routes, redirects, nil, "", "")
			web := cfg.Routers["panel-web"]
			if web.Rule != "Host(`panel.example.com`)" || web.Service != "panel" || cfg.Services["panel"].LoadBalancer.Servers[0].URL != "http://172.17.0.1:8000" {
				t.Fatalf("config: %+v", cfg)
			}
			secure, ok := cfg.Routers["panel-websecure"]
			if ok != (tc.resolver != "") {
				t.Fatalf("HTTPS=%v want resolver %q", ok, tc.resolver)
			}
			if ok && (secure.TLS == nil || secure.TLS.CertResolver != tc.resolver || len(web.Middlewares) != 1 || web.Middlewares[0] != HTTPRedirectMiddleware) {
				t.Fatalf("TLS=%+v web=%+v", secure, web)
			}
			for _, format := range []Format{FormatYAML, FormatTOML} {
				first, err := Generate(cfg, format)
				if err != nil {
					t.Fatal(err)
				}
				againRoutes, againRedirects, _, _ := f.service.gatherRoutes(context.Background(), apps, nil, nil, f.serverID, nil, access)
				again, err := Generate(BuildConfig(againRoutes, againRedirects, nil, "", ""), format)
				if err != nil || !reflect.DeepEqual(first, again) {
					t.Fatalf("non-identical rerun: %v", err)
				}
			}
		})
	}
}

func TestControlPlaneDiagnosticsPreserveOtherRoutes(t *testing.T) {
	for _, tc := range []struct{ name, raw, node, reason string }{
		{"invalid URL", "ftp://panel.example.com", "localhost", "http(s) URL"},
		{"invalid host", "https://bad_host.example.com", "localhost", "invalid control_plane_url host"},
		{"missing host", "https:///path", "localhost", "invalid control_plane_url host"},
		{"credentials", "https://user:pass@panel.example.com", "localhost", "without credentials"},
		{"missing localhost", "https://panel.example.com", "remote", "localhost node is not registered"},
		{"application clash", "https://app.example.com", "localhost", "already claimed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := panelFixture(t)
			f.service.controlPlaneURL = func(context.Context) (string, error) { return tc.raw, nil }
			f.service.nodes = panelNodes{[]NamedNode{{ID: f.serverID, Name: tc.node}}}
			app := runningApp(f.serverID, "app.example.com", "container", appContainerPort, 9000)
			f.source.setApps(app)
			f.containers.list = append(f.containers.list, appContainer("container", 9000))
			err := f.service.SyncServer(context.Background(), f.serverID)
			var partial *PartialError
			if !errors.As(err, &partial) || len(partial.Diagnostics) != 1 || partial.Diagnostics[0].Kind != string(RouterKindControlPlane) || !strings.Contains(partial.Diagnostics[0].Reason, tc.reason) {
				t.Fatalf("sync=%v", err)
			}
			rows, err := f.service.ListRouters(context.Background(), f.serverID)
			if err != nil || len(rows.Routers) != 1 || rows.Routers[0].Kind != RouterKindApplication || len(rows.Nodes[0].Diagnostics) != 1 {
				t.Fatalf("list=%+v err=%v", rows, err)
			}
			if f.history.activeHash() == "" {
				t.Fatal("healthy routes were not pushed")
			}
		})
	}
}

func TestControlPlaneSyncAndList(t *testing.T) {
	f := panelFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := f.service.SyncServer(ctx, f.serverID); err != nil {
			t.Fatal(err)
		}
		rows, err := f.service.ListRouters(ctx, f.serverID)
		if err != nil || len(rows.Routers) != 1 || rows.Routers[0].Kind != RouterKindControlPlane || rows.Routers[0].Service != "panel" || rows.Nodes[0].SyncStatus != RouterSyncSynced {
			t.Fatalf("list=%+v err=%v", rows, err)
		}
	}
	if len(f.agent.calls) != 2 || !reflect.DeepEqual(f.agent.calls[0].Files, f.agent.calls[1].Files) {
		t.Fatal("sync files differ on rerun")
	}
	// The localhost route must never be emitted on a remote node.
	routes, diagnostics := f.service.controlPlaneRoute(ctx, nil, uuid.New(), nil, nil)
	if len(routes) != 0 || len(diagnostics) != 0 {
		t.Fatalf("remote panel=%+v diagnostics=%+v", routes, diagnostics)
	}
	// Clearing the URL removes the router on the next sync.
	f.service.controlPlaneURL = func(context.Context) (string, error) { return "", nil }
	if err := f.service.SyncServer(ctx, f.serverID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.service.ListRouters(ctx, f.serverID)
	if err != nil || len(rows.Routers) != 0 {
		t.Fatalf("cleared panel=%+v err=%v", rows, err)
	}
}

func TestControlPlaneEmptyRegistryDiagnostic(t *testing.T) {
	f := panelFixture(t)
	f.service.nodes = panelNodes{}
	_, err := f.service.SyncAll(context.Background())
	if !errors.Is(err, ErrPartialSync) || !strings.Contains(err.Error(), "control plane (panel.example.com: localhost node is not registered)") {
		t.Fatalf("SyncAll=%v", err)
	}
	_, err = f.service.ListRouters(context.Background(), uuid.Nil)
	if !errors.Is(err, ErrPartialSync) {
		t.Fatalf("ListRouters=%v", err)
	}
}

func TestControlPlaneOtherHostOwnersWin(t *testing.T) {
	for _, kind := range []string{"service", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			f := panelFixture(t)
			var services []ProxiedService
			var rules []RedirectRule
			if kind == "service" {
				services = []ProxiedService{{ID: uuid.New(), ServerID: f.serverID, Domains: []ServiceDomain{{Host: "panel.example.com", Service: "web", Port: 80}}}}
			} else {
				rules = []RedirectRule{{ID: uuid.New(), SourceDomain: "panel.example.com", TargetDomain: "other.example.com", Enabled: true, ServerID: f.serverID, Code: RedirectCodePermanent}}
			}
			routes, redirects, diagnostics, _ := f.service.gatherRoutes(context.Background(), nil, services, rules, f.serverID, nil, nil)
			for _, route := range routes {
				if route.Name == "panel" {
					t.Fatal("panel overrode existing host")
				}
			}
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Kind == string(RouterKindControlPlane) && strings.Contains(diagnostic.Reason, "already claimed") {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics=%+v", diagnostics)
			}
			if kind == "redirect" && len(redirects) != 1 {
				t.Fatalf("redirect lost: %+v", redirects)
			}
		})
	}
}
