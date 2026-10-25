package proxy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/instance"
)

type fixedPanelStatus struct{ observation CertificateStatusObservation }

func (f fixedPanelStatus) CertificateStatuses(_ context.Context, certs []DomainCertificate) map[uuid.UUID]CertificateStatusObservation {
	out := make(map[uuid.UUID]CertificateStatusObservation)
	for _, cert := range certs {
		out[cert.ID] = f.observation
	}
	return out
}

func TestReconcileApplicationCertificatesGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		urls []string
		want int
	}{
		{"https", []string{"https://panel.example.com"}, 1},
		{"http", []string{"http://panel.example.com"}, 0},
		{"empty", []string{""}, 0},
		{"mixed", []string{"http://panel.example.com", "https://other.example.com"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := panelFixture(t)
			st := newFakeSSLStore()
			f.service.panelCertificates = st
			if err := f.service.ReconcilePanelCertificates(context.Background(), tc.urls); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if st.backfillCalls != tc.want {
				t.Fatalf("backfill calls = %d, want %d", st.backfillCalls, tc.want)
			}
		})
	}
}

func TestReconcileApplicationBackfillResyncs(t *testing.T) {
	newBackfillFixture := func(t *testing.T) *syncFixture {
		t.Helper()
		f := panelFixture(t)
		st := newFakeSSLStore()
		st.uncertified = []DomainCertificate{{ApplicationID: uuid.New(), Domain: "app.example.com"}}
		f.service.panelCertificates = st
		f.source.apps = []ProxiedApplication{runningApp(f.serverID, "app.example.com", "c1", 3000, 32768)}
		f.containers.list = append(f.containers.list, appContainer("c1", 32768))
		return f
	}

	t.Run("insert triggers one push", func(t *testing.T) {
		f := newBackfillFixture(t)
		before := len(f.agent.calls)
		if err := f.service.ReconcilePanelCertificates(context.Background(), []string{"https://panel.example.com"}); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if len(f.agent.calls) != before+1 {
			t.Fatalf("config pushes = %d, want %d after the backfill", len(f.agent.calls), before+1)
		}
		if err := f.service.ReconcilePanelCertificates(context.Background(), []string{"https://panel.example.com"}); err != nil {
			t.Fatalf("second reconcile: %v", err)
		}
		if len(f.agent.calls) != before+1 {
			t.Fatalf("idempotent backfill pushed again (%d)", len(f.agent.calls))
		}
	})

	t.Run("http gate pushes nothing", func(t *testing.T) {
		f := newBackfillFixture(t)
		before := len(f.agent.calls)
		if err := f.service.ReconcilePanelCertificates(context.Background(), []string{"http://panel.example.com"}); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if len(f.agent.calls) != before {
			t.Fatal("an http-only instance pushed a TLS backfill")
		}
	})
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
	for _, action := range []string{"confirm", "revert", "issuance failure"} {
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
			if action == "issuance failure" {
				f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusUnknown}}
			}
			state, err := svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham", Timezone: "UTC"})
			if err != nil || !state.PendingControlPlaneURL.Synced {
				t.Fatalf("save=%+v err=%v", state, err)
			}
			if len(st.certificates) != 1 || st.certificates[0].Domain != "old.example.com" {
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
			routes, _, _, _ := f.service.gatherRoutes(ctx, nil, nil, nil, f.serverID, nil, nil)
			for _, route := range routes {
				if route.Domain == "new.example.com" && route.Certificate != nil {
					t.Fatal("unconfirmed host requested a certificate")
				}
			}
			if !found {
				t.Fatal("proposed host absent during trial")
			}
			if action == "revert" {
				_, err = svc.RevertControlPlaneURL(ctx, uuid.Nil)
			} else {
				_, err = svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com")
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := "new.example.com"
			if action == "revert" {
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

func TestPanelCertificateStartupReconciliation(t *testing.T) {
	for _, raw := range []string{"https://panel.example.com", "http://panel.example.com"} {
		t.Run(raw, func(t *testing.T) {
			f := panelFixture(t)
			st := newFakeSSLStore()
			f.service.panelCertificates = st
			f.service.certificateStatus = fixedPanelStatus{CertificateStatusObservation{Status: CertificateStatusAbsent}}
			f.service.reconcilePanel = func(ctx context.Context) error { return f.service.ReconcilePanelCertificates(ctx, []string{raw}) }
			f.service.controlPlaneURL = func(context.Context) (string, error) { return raw, nil }
			var id uuid.UUID
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				f.service.RunPanelCertificates(ctx)
				cancel()
				certs, err := st.ListCertificates(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(raw, "http:") {
					if len(certs) != 0 {
						t.Fatalf("HTTP intents=%+v", certs)
					}
					continue
				}
				if len(certs) != 1 {
					t.Fatalf("HTTPS intents=%+v", certs)
				}
				if id != uuid.Nil && certs[0].ID != id {
					t.Fatal("restart recreated certificate")
				}
				id = certs[0].ID
				if len(f.agent.calls) != 1 {
					t.Fatalf("startup config pushes=%d", len(f.agent.calls))
				}
			}
		})
	}
}

func TestStoredPanelCertificateReconciliationRetries(t *testing.T) {
	f := panelFixture(t)
	st := newFakeSSLStore()
	f.service.panelCertificates = st
	f.service.reconcilePanel = func(context.Context) error { return errors.New("settings unavailable") }
	if err := f.service.reconcileStoredPanelCertificates(context.Background()); err == nil {
		t.Fatal("missing settings error")
	}
	f.service.reconcilePanel = func(ctx context.Context) error {
		return f.service.ReconcilePanelCertificates(ctx, []string{"https://panel.example.com"})
	}
	st.certCreateErr = errors.New("store unavailable")
	if err := f.service.reconcileStoredPanelCertificates(context.Background()); err == nil {
		t.Fatal("missing store error")
	}
	st.certCreateErr = nil
	if err := f.service.reconcileStoredPanelCertificates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.certificates) != 1 {
		t.Fatalf("retry intents=%+v", st.certificates)
	}
	if err := f.service.refreshPanelCertificates(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPanelCertificateStartupDuringTrial(t *testing.T) {
	for _, previous := range []string{"https://old.example.com", "http://old.example.com", ""} {
		t.Run(previous, func(t *testing.T) {
			f := panelFixture(t)
			st := newFakeSSLStore()
			f.service.panelCertificates = st
			st.certificates = []DomainCertificate{{ID: uuid.New(), Domain: "new.example.com", Enabled: true, Challenge: ChallengeHTTP01}}
			proposed := "https://new.example.com"
			repo := &panelSettingsRepo{rec: instance.Stored{
				ControlPlaneURL: &proposed,
				PendingControlPlaneURL: &instance.PendingControlPlaneURL{
					Previous: &previous, Proposed: proposed, Deadline: time.Now().Add(time.Minute), Synced: true,
				},
			}}
			svc := instance.NewService(repo, nil, nil)
			t.Cleanup(svc.Close)
			svc.SyncPanelCertificates = f.service.ReconcilePanelCertificates
			f.service.reconcilePanel = svc.ReconcilePanelCertificates
			f.service.controlPlaneURLs = svc.ControlPlaneURLs
			for range 2 {
				if err := f.service.reconcileStoredPanelCertificates(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			want := 0
			if strings.HasPrefix(previous, "https:") {
				want = 1
			}
			if len(st.certificates) != want {
				t.Fatalf("trial intents=%+v", st.certificates)
			}
			if want == 1 && st.certificates[0].Domain != "old.example.com" {
				t.Fatal("fallback intent missing")
			}
		})
	}
}

func TestPanelCertificateReconciliationLogsErrorChanges(t *testing.T) {
	f := panelFixture(t)
	f.service.panelCertificates = newFakeSSLStore()
	var logs bytes.Buffer
	f.service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failures := []string{"store unavailable", "store unavailable", "different error", "", "store unavailable"}
	ticks := make(chan time.Time, len(failures))
	for range failures {
		ticks <- time.Now()
	}
	calls := 0
	f.service.reconcilePanel = func(context.Context) error {
		if calls == len(failures) {
			cancel()
			return nil
		}
		failure := failures[calls]
		calls++
		if failure == "" {
			return nil
		}
		return errors.New(failure)
	}
	f.service.runPanelCertificates(ctx, ticks)
	if got := strings.Count(logs.String(), "level=WARN"); got != 3 {
		t.Fatalf("warnings=%d logs=%s", got, logs.String())
	}
}
