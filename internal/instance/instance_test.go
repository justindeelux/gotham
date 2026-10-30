package instance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeRepo struct {
	rec      Stored
	sections []string
}

func (f *fakeRepo) Load(context.Context) (Stored, error) { return f.rec, nil }
func (f *fakeRepo) Save(_ context.Context, _ uuid.UUID, section string, rec Stored, _ any) error {
	f.rec = rec
	f.sections = append(f.sections, section)
	return nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestValidation(t *testing.T) {
	_, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "ftp://x", InstanceName: ""})
	for _, k := range []string{"control_plane_url", "instance_name"} {
		if errs[k] == "" {
			t.Errorf("expected error for %s: %v", k, errs)
		}
	}
	in, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "https://g.example.com/", InstanceName: "prod"})
	if errs != nil || in.ControlPlaneURL != "https://g.example.com" {
		t.Fatalf("good general rejected: %v %v", in, errs)
	}
	if _, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "https://u:p@h", InstanceName: "a"}); errs["control_plane_url"] == "" {
		t.Error("credentials in URL accepted")
	}

	bad := goodNet()
	bad.DNSServers = []string{"nope"}
	bad.IPv4.Gateway = "192.168.9.1"
	bad.IPv6 = IPv6Config{Enabled: false, Mode: ModeStatic}
	_, errs = normalizeNetwork(bad)
	for _, k := range []string{"dns_servers", "ipv4.gateway", "ipv6.mode"} {
		if errs[k] == "" {
			t.Errorf("expected network error for %s: %v", k, errs)
		}
	}
	if _, errs := normalizeNetwork(goodNet()); errs != nil {
		t.Fatalf("good network rejected: %v", errs)
	}
	sys, errs := normalizeSystem(System{Hostname: "-bad-", NTPServers: []string{"a b"}, Timezone: "Mars/Base"})
	if errs["hostname"] == "" || errs["ntp_servers"] == "" || errs["timezone"] == "" {
		t.Errorf("system errors missing: %v", errs)
	}
	if _, errs := normalizeSystem(sys); errs == nil {
		t.Fatal("invalid system accepted")
	}
	if _, errs := normalizeSystem(System{Hostname: "gotham-1", NTPEnabled: true, Timezone: "Europe/Berlin"}); errs != nil {
		t.Fatalf("good system rejected: %v", errs)
	}
}

func TestEnvLocksGeneral(t *testing.T) {
	svc := NewService(&fakeRepo{}, quietLogger())
	svc.lookup = func(k string) string {
		if k == EnvInstanceName {
			return "from-env"
		}
		return ""
	}
	_, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{InstanceName: "other"})
	var fe FieldErrors
	if !errors.As(err, &fe) || fe["instance_name"] == "" {
		t.Fatalf("locked field edit not rejected: %v", err)
	}
	st, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{InstanceName: "from-env"})
	if err != nil || !st.General.InstanceName.Locked {
		t.Fatalf("unexpected: %+v %v", st.General, err)
	}
}

func TestTimezoneStoredForControlPlane(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, quietLogger())
	svc.lookup = func(string) string { return "" }

	tz, err := svc.Timezone(context.Background())
	if err != nil || tz != DefaultTimezone {
		t.Fatalf("default timezone: %q %v", tz, err)
	}
	if err := svc.SetTimezone(context.Background(), uuid.Nil, "Europe/Berlin"); err != nil {
		t.Fatal(err)
	}
	if tz, _ := svc.Timezone(context.Background()); tz != "Europe/Berlin" {
		t.Fatalf("stored timezone not kept: %q", tz)
	}

	svc.lookup = func(k string) string {
		if k == EnvTimezone {
			return "Asia/Tokyo"
		}
		return ""
	}
	if tz, _ := svc.Timezone(context.Background()); tz != "Asia/Tokyo" {
		t.Fatalf("env timezone must win: %q", tz)
	}
	if err := svc.SetTimezone(context.Background(), uuid.Nil, "Europe/Berlin"); err == nil {
		t.Fatal("env-locked timezone change must be refused")
	}
}

func TestRoutesStatusCodes(t *testing.T) {
	svc := NewService(&fakeRepo{}, quietLogger())
	svc.lookup = func(string) string { return "" }
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	}
	r := chi.NewRouter()
	Mount(r, deny, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false }, svc)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/instance/settings", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin got %d", rec.Code)
	}

	ok := chi.NewRouter()
	Mount(ok, func(n http.Handler) http.Handler { return n }, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false }, svc)
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/instance/settings/general",
		bytes.NewBufferString(`{"control_plane_url":"nope","instance_name":"x"}`)))
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"control_plane_url"`)) {
		t.Fatalf("invalid input: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/instance/settings/general",
		bytes.NewBufferString(`{"control_plane_url":"https://g.example.com","instance_name":"x","timezone":"UTC"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dropped timezone field accepted: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/instance/settings/network/confirm", nil))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("removed instance network route still answers: %d", rec.Code)
	}
}

// newSvc keeps the general-settings tests focused on the URL flow; the node
// fixture below builds the host-settings service.
func newSvc(*fakeHost) (*Service, *fakeRepo) {
	repo := &fakeRepo{}
	svc := NewService(repo, quietLogger())
	svc.lookup = func(string) string { return "" }
	return svc, repo
}
