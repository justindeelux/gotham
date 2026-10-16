package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/instance"
	"github.com/justindeelux/gotham/internal/teams"
)

type fixedPanelStatus struct{ observation CertificateStatusObservation }

func (f fixedPanelStatus) CertificateStatuses(_ context.Context, certs []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	out := make(map[uuid.UUID]CertificateStatusObservation)
	for _, cert := range certs {
		out[cert.ID] = f.observation
	}
	return out
}

func TestPanelCertificateReadiness(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation CertificateStatusObservation
		want        string
		redirect    bool
	}{
		{"pending", CertificateStatusObservation{Status: CertificateStatusAbsent}, "pending", false},
		{"issued", CertificateStatusObservation{Status: CertificateStatusPresent, NotAfter: time.Now().Add(time.Hour)}, "issued", true},
		{"expired", CertificateStatusObservation{Status: CertificateStatusPresent, NotAfter: time.Now().Add(-time.Hour)}, "pending", false},
		{"unreadable", CertificateStatusObservation{Status: CertificateStatusUnknown}, "failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := panelFixture(t)
			st := newFakeSSLStore()
			st.certificates = []DomainCertificate{{ID: uuid.New(), Domain: "panel.example.com", Enabled: true, Challenge: ChallengeHTTP01}}
			f.service.panelCertificates = st
			f.service.certificateStatus = fixedPanelStatus{tc.observation}
			routes, _, _, _ := f.service.gatherRoutes(context.Background(), nil, nil, nil, f.serverID, nil, nil)
			cfg := BuildConfig(routes, nil, nil, "", "")
			if _, ok := cfg.Routers["panel-websecure"]; !ok {
				t.Fatal("TLS router missing; issuance cannot start")
			}
			web, ok := cfg.Routers["panel-web"]
			if !ok || (len(web.Middlewares) > 0) != tc.redirect {
				t.Fatalf("HTTP router=%+v", web)
			}
			if routes[0].PanelCertificate.Status != tc.want {
				t.Fatalf("state=%+v", routes[0].PanelCertificate)
			}
			if (routes[0].PanelCertificate.NotAfter != nil) != tc.redirect {
				t.Fatal("expiry does not match issued state")
			}
		})
	}
}

func TestPanelTrialCertificateLifecycle(t *testing.T) {
	for _, action := range []string{"confirm", "revert", "issuance failure", "intent failure"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			f := panelFixture(t)
			st := newFakeSSLStore()
			f.service.panelCertificates = st
			f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusAbsent}}
			old := "https://old.example.com"
			if err := f.service.ReconcilePanelCertificates(ctx, []string{old}); err != nil {
				t.Fatal(err)
			}
			repo := &panelSettingsRepo{rec: instance.Stored{ControlPlaneURL: &old}}
			svc := instance.NewService(repo, nil, nil)
			t.Cleanup(svc.Close)
			svc.SyncPanelCertificates = f.service.ReconcilePanelCertificates
			svc.ResyncProxy = f.service.SyncControlPlane
			f.service.controlPlaneURLs = svc.ControlPlaneURLs
			if action == "intent failure" {
				st.certCreateErr = errors.New("database write failed")
			}
			if action == "issuance failure" {
				f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusUnknown}}
			}
			state, err := svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham", Timezone: "UTC"})
			if err != nil || !state.PendingControlPlaneURL.Synced {
				t.Fatalf("save=%+v err=%v", state, err)
			}
			want := 2
			if action == "intent failure" {
				want = 1
			}
			if len(st.certificates) != want {
				t.Fatalf("trial certificates=%+v", st.certificates)
			}
			rows, err := f.service.ListRouters(ctx, f.serverID)
			if err != nil || len(rows.Routers) != 2 || rows.Nodes[0].SyncStatus != RouterSyncSynced {
				t.Fatalf("trial routers=%+v err=%v", rows, err)
			}
			found := false
			for _, router := range rows.Routers {
				if router.Host == "new.example.com" && router.Kind == RouterKindControlPlane {
					found = true
				}
			}
			if !found {
				t.Fatal("proposed host absent during trial")
			}
			if action == "revert" || action == "intent failure" {
				_, err = svc.RevertControlPlaneURL(ctx, uuid.Nil)
			} else {
				_, err = svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com")
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := "new.example.com"
			if action == "revert" || action == "intent failure" {
				expected = "old.example.com"
			}
			if len(st.certificates) != 1 || st.certificates[0].Domain != expected {
				t.Fatalf("settled certificates=%+v", st.certificates)
			}
		})
	}
}

func TestPanelCertificatesNeverBorrowApplicationIntent(t *testing.T) {
	f := panelFixture(t)
	st := newFakeSSLStore()
	st.certificates = []DomainCertificate{{ID: uuid.New(), ApplicationID: uuid.New(), Domain: "panel.example.com", Enabled: true, Challenge: ChallengeHTTP01}}
	f.service.panelCertificates = st
	if err := f.service.ReconcilePanelCertificates(context.Background(), []string{"https://panel.example.com"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("clash=%v", err)
	}
	apps := []ProxiedApplication{{ID: st.certificates[0].ApplicationID, Certificate: &CertificateIntent{Domain: "panel.example.com", Enabled: true, Challenge: ChallengeHTTP01}}}
	routes, _, _, _ := f.service.gatherRoutes(context.Background(), apps, nil, nil, f.serverID, nil, nil)
	if len(routes) != 1 || routes[0].Certificate != nil {
		t.Fatalf("borrowed app certificate: %+v", routes)
	}
}

func TestPanelCertificateRoutesArePlatformOnly(t *testing.T) {
	st := newFakeSSLStore()
	cert := DomainCertificate{ID: uuid.New(), Domain: "panel.example.com", Enabled: true, Challenge: ChallengeHTTP01}
	st.certificates = []DomainCertificate{cert}
	svc := newSSLService(SSLConfig{Store: st})
	teamCtx := teams.WithScope(context.Background(), teams.Scope{TeamID: uuid.New(), Role: teams.RoleAdmin})
	if _, err := svc.GetCertificate(teamCtx, cert.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("team get=%v", err)
	}
	if _, err := svc.UpdateCertificate(teamCtx, cert.ID, UpdateCertificateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("team patch=%v", err)
	}
	if err := svc.DeleteCertificate(teamCtx, cert.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("team delete=%v", err)
	}
	rows, err := svc.ListCertificates(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("team list=%+v err=%v", rows, err)
	}
	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	platformCalls := 0
	platform := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { platformCalls++; next.ServeHTTP(w, r) })
	}
	Mount(router, auth, platform, newSyncFixture(t, nil, uuid.New()).service, nil, svc, nil, fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusAbsent}})
	for _, path := range []string{"/v1/proxy/panel-certificates", "/v1/proxy/panel-certificates/" + cert.ID.String()} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"owner_kind":"control_plane"`) || !strings.Contains(rec.Body.String(), `"status":"pending"`) {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if platformCalls != 2 {
		t.Fatal("platform middleware bypassed")
	}
	provider := DNSProvider{ID: uuid.New(), Enabled: true, Provider: ProviderCloudflare, Zones: []string{"example.com"}}
	st.providers = []DNSProvider{provider}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/proxy/panel-certificates/"+cert.ID.String(), strings.NewReader(`{"challenge":"dns-01","dns_provider_id":"`+provider.ID.String()+`"}`)))
	if rec.Code != 200 || st.lastCertWrite.Challenge != ChallengeDNS01 {
		t.Fatalf("DNS-01 update=%d %s", rec.Code, rec.Body.String())
	}
}

func TestPanelCertificateRefreshConvergesRedirect(t *testing.T) {
	ctx := context.Background()
	f := panelFixture(t)
	st := newFakeSSLStore()
	f.service.panelCertificates = st
	f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusAbsent}}
	if err := f.service.ReconcilePanelCertificates(ctx, []string{"https://panel.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.SyncControlPlane(ctx); err != nil {
		t.Fatal(err)
	}
	before := len(f.agent.calls)
	if err := f.service.refreshPanelCertificates(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.calls) != before {
		t.Fatal("unchanged pending certificate pushed again")
	}
	f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusPresent, NotAfter: time.Now().Add(time.Hour)}}
	if err := f.service.refreshPanelCertificates(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.calls) != before+1 {
		t.Fatal("issuance did not activate redirect")
	}
	f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusAbsent}}
	if err := f.service.refreshPanelCertificates(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.calls) != before+2 {
		t.Fatal("loss of certificate did not remove redirect")
	}
}

func TestPanelCertificateRetryPreservesDNSConfiguration(t *testing.T) {
	ctx := context.Background()
	f := panelFixture(t)
	st := newFakeSSLStore()
	f.service.panelCertificates = st
	raw := "https://panel.example.com"
	repo := &panelSettingsRepo{rec: instance.Stored{ControlPlaneURL: &raw}}
	svc := instance.NewService(repo, nil, nil)
	t.Cleanup(svc.Close)
	svc.SyncPanelCertificates = f.service.ReconcilePanelCertificates
	st.certCreateErr = errors.New("temporary store failure")
	input := instance.GeneralInput{ControlPlaneURL: raw, InstanceName: "Gotham", Timezone: "UTC"}
	if _, err := svc.UpdateGeneral(ctx, uuid.Nil, input); err != nil {
		t.Fatal(err)
	}
	st.certCreateErr = nil
	if _, err := svc.UpdateGeneral(ctx, uuid.Nil, input); err != nil {
		t.Fatal(err)
	}
	if len(st.certificates) != 1 {
		t.Fatalf("same URL save did not retry: %+v", st.certificates)
	}
	provider := uuid.New()
	st.certificates[0].Challenge = ChallengeDNS01
	st.certificates[0].DNSProviderID = provider
	if _, err := svc.UpdateGeneral(ctx, uuid.Nil, input); err != nil {
		t.Fatal(err)
	}
	if len(st.certificates) != 1 || st.certificates[0].Challenge != ChallengeDNS01 || st.certificates[0].DNSProviderID != provider {
		t.Fatalf("DNS configuration overwritten: %+v", st.certificates)
	}
	if err := f.service.ReconcilePanelCertificates(ctx, []string{"http://panel.example.com"}); err != nil {
		t.Fatal(err)
	}
	if len(st.certificates) != 0 {
		t.Fatal("HTTP URL retained an HTTPS intent")
	}
}
