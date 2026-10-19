package proxy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestControlPlaneHostClashes(t *testing.T) {
	for _, kind := range []string{"primary", "alias", "disabled app", "service", "redirect", "disabled redirect", "invalid", "unroutable service"} {
		t.Run(kind, func(t *testing.T) {
			f := panelFixture(t)
			host := "panel.example.com"
			switch kind {
			case "primary", "disabled app":
				f.source.setApps(ProxiedApplication{Domains: []ProxiedDomain{{Domain: host, Disabled: kind == "disabled app"}}})
			case "alias":
				f.source.setApps(ProxiedApplication{Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 3000}, {Domain: host, Port: 3000}}})
			case "service":
				f.service.services = &fakeServiceSource{services: []ProxiedService{{Domains: []ServiceDomain{{Host: host}}}}}
			case "redirect", "disabled redirect":
				f.service.redirects = &fakeRedirectSource{rules: []RedirectRule{{SourceDomain: host, Enabled: kind == "redirect"}}}
			case "invalid":
				host = "bad_host.example.com"
			case "unroutable service":
				f.service.services = &fakeServiceSource{services: []ProxiedService{{Unroutable: "invalid stored compose"}}}
			}
			if err := f.service.CheckControlPlaneHost(context.Background(), "https://"+strings.ToUpper(host)+":8443/"); err == nil {
				t.Fatal("host accepted")
			}
			if err := f.service.CheckControlPlaneHost(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
		})
	}
	f := panelFixture(t)
	if err := f.service.CheckControlPlaneHost(context.Background(), "https://unused.example.com"); err != nil {
		t.Fatal(err)
	}
	f.service.services = &fakeServiceSource{err: errors.New("database offline")}
	if err := f.service.CheckControlPlaneHost(context.Background(), "https://unused.example.com"); err == nil {
		t.Fatal("ownership read failure ignored")
	}
}

func TestControlPlaneFallbackRoutes(t *testing.T) {
	f := panelFixture(t)
	f.service.controlPlaneURLs = func(ctx context.Context) ([]string, error) {
		current, err := f.service.controlPlaneURL(ctx)
		return []string{current, "https://old.example.com"}, err
	}
	if err := f.service.SyncServer(context.Background(), f.serverID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.service.ListRouters(context.Background(), f.serverID)
	if err != nil || len(rows.Routers) != 2 {
		t.Fatalf("list=%+v err=%v", rows, err)
	}
	for _, row := range rows.Routers {
		if row.Kind != RouterKindControlPlane || row.Target != "http://172.17.0.1:8000" {
			t.Fatalf("fallback row=%+v", row)
		}
	}
	f.service.controlPlaneURL = func(context.Context) (string, error) { return "https://old.example.com", nil }
	routes, _, diagnostics, _ := f.service.gatherRoutes(context.Background(), nil, nil, nil, f.serverID, nil, nil)
	if len(routes) != 1 || len(diagnostics) != 0 {
		t.Fatalf("same-host routes=%+v diagnostics=%+v", routes, diagnostics)
	}
	f.service.controlPlaneURL = func(context.Context) (string, error) { return "", nil }
	routes, _, _, _ = f.service.gatherRoutes(context.Background(), nil, nil, nil, f.serverID, nil, nil)
	if len(routes) != 1 || routes[0].Domain != "old.example.com" {
		t.Fatalf("clear lost fallback: %+v", routes)
	}
}

func TestRedirectCannotClaimPanelHost(t *testing.T) {
	store := newFakeRedirectStore()
	app := redirectApp(uuid.New())
	store.addApp(app)
	svc, _ := newRedirectService(t, store)
	svc.guardDomains = func(_ context.Context, hosts ...string) (func(), error) {
		if hosts[0] == "panel.example.com" {
			return nil, errors.New("reserved for the control plane")
		}
		return func() {}, nil
	}
	ctx := context.Background()
	if _, err := svc.CreateRedirect(ctx, CreateRedirectInput{ApplicationID: app.ID, SourceDomain: "PANEL.example.com", TargetDomain: "app.example.com"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("create=%v", err)
	}
	created, err := svc.CreateRedirect(ctx, CreateRedirectInput{ApplicationID: app.ID, SourceDomain: "www.example.com", TargetDomain: "app.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	host := "panel.example.com"
	if _, err := svc.UpdateRedirect(ctx, created.ID, UpdateRedirectInput{SourceDomain: &host}); !errors.Is(err, ErrConflict) {
		t.Fatalf("update=%v", err)
	}
	if len(store.redirects) != 1 || store.redirects[created.ID].SourceDomain != "www.example.com" {
		t.Fatal("rejected write mutated redirect")
	}
}
