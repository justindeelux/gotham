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
	"time"

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

type fakeHost struct {
	caps                        Capabilities
	applied, confirmed, reverts int
	err                         error
}

func (h *fakeHost) Capabilities(context.Context) Capabilities { return h.caps }
func (h *fakeHost) ApplyNetwork(context.Context, Network, time.Duration) error {
	h.applied++
	return h.err
}
func (h *fakeHost) ConfirmNetwork(context.Context) error      { h.confirmed++; return nil }
func (h *fakeHost) RevertNetwork(context.Context) error       { h.reverts++; return nil }
func (h *fakeHost) ApplySystem(context.Context, System) error { return h.err }

func newSvc(host *fakeHost) (*Service, *fakeRepo) {
	repo := &fakeRepo{rec: Stored{Network: Network{
		IPv4: IPConfig{Mode: ModeDHCP}, IPv6: IPv6Config{Enabled: true, Mode: ModeDHCP},
	}}}
	svc := NewService(repo, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.lookup = func(string) string { return "" }
	return svc, repo
}

func goodNet() Network {
	return Network{
		DNSServers: []string{"1.1.1.1", "2606:4700:4700::1111"},
		IPv4:       IPConfig{Mode: ModeStatic, Address: "10.0.0.5/24", Gateway: "10.0.0.1"},
		IPv6:       IPv6Config{Enabled: true, Mode: ModeDHCP},
	}
}

func TestValidation(t *testing.T) {
	_, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "ftp://x", InstanceName: "", Timezone: "Mars/Base"})
	for _, k := range []string{"control_plane_url", "instance_name", "timezone"} {
		if errs[k] == "" {
			t.Errorf("expected error for %s: %v", k, errs)
		}
	}
	in, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "https://g.example.com/", InstanceName: "prod", Timezone: "Europe/Berlin"})
	if errs != nil || in.ControlPlaneURL != "https://g.example.com" {
		t.Fatalf("good general rejected: %v %v", in, errs)
	}
	if _, errs := normalizeGeneral(GeneralInput{ControlPlaneURL: "https://u:p@h", InstanceName: "a", Timezone: "UTC"}); errs["control_plane_url"] == "" {
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
	if _, errs := normalizeSystem(System{Hostname: "-bad-", NTPServers: []string{"a b"}}); errs["hostname"] == "" || errs["ntp_servers"] == "" {
		t.Errorf("system errors missing: %v", errs)
	}
}

func TestEnvLocksGeneral(t *testing.T) {
	svc, _ := newSvc(&fakeHost{})
	svc.lookup = func(k string) string {
		if k == EnvInstanceName {
			return "from-env"
		}
		return ""
	}
	_, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{InstanceName: "other", Timezone: "UTC"})
	var fe FieldErrors
	if !errors.As(err, &fe) || fe["instance_name"] == "" {
		t.Fatalf("locked field edit not rejected: %v", err)
	}
	st, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{InstanceName: "from-env", Timezone: "Asia/Tokyo"})
	if err != nil || st.General.Timezone.Value != "Asia/Tokyo" || !st.General.InstanceName.Locked {
		t.Fatalf("unexpected: %+v %v", st.General, err)
	}
}

func TestNetworkConfirmAndRevert(t *testing.T) {
	host := &fakeHost{caps: Capabilities{Network: true, System: true}}
	svc, repo := newSvc(host)
	ctx := context.Background()
	now := time.Now()
	svc.now = func() time.Time { return now }

	st, err := svc.UpdateNetwork(ctx, uuid.Nil, goodNet())
	if err != nil || st.Pending == nil || host.applied != 1 {
		t.Fatalf("apply: %+v %v", st, err)
	}
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, goodNet()); !errors.Is(err, ErrPending) {
		t.Fatalf("second change while pending: %v", err)
	}
	if st, err = svc.ConfirmNetwork(ctx, uuid.Nil); err != nil || st.Pending != nil || host.confirmed != 1 {
		t.Fatalf("confirm: %+v %v", st, err)
	}
	if _, err := svc.ConfirmNetwork(ctx, uuid.Nil); !errors.Is(err, ErrNoPending) {
		t.Fatalf("confirm without pending: %v", err)
	}

	// Unconfirmed change: the deadline passes and the desired state reverts.
	next := goodNet()
	next.IPv4.Address = "10.0.0.6/24"
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, next); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now.Add(NetworkConfirmWindow + time.Second) }
	st, err = svc.Get(ctx)
	if err != nil || st.Pending != nil || st.Network.IPv4.Address != "10.0.0.5/24" {
		t.Fatalf("auto-revert: %+v %v", st.Network, err)
	}

	// Manual revert.
	svc.now = func() time.Time { return now }
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, next); err != nil {
		t.Fatal(err)
	}
	if st, err = svc.RevertNetwork(ctx, uuid.Nil); err != nil || st.Network.IPv4.Address != "10.0.0.5/24" || host.reverts != 1 {
		t.Fatalf("revert: %+v %v", st.Network, err)
	}
	if len(repo.sections) == 0 {
		t.Fatal("no audit saves")
	}
}

func TestUnsupportedHostAndHelperFailure(t *testing.T) {
	host := &fakeHost{}
	svc, _ := newSvc(host)
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, goodNet()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want unsupported: %v", err)
	}
	host.caps, host.err = Capabilities{Network: true, System: true}, ErrHost
	if _, err := svc.UpdateSystem(context.Background(), uuid.Nil, System{Hostname: "gotham-1", NTPEnabled: true}); !errors.Is(err, ErrHost) {
		t.Fatalf("want host error: %v", err)
	}
}

func TestRoutesStatusCodes(t *testing.T) {
	svc, _ := newSvc(&fakeHost{})
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
		bytes.NewBufferString(`{"control_plane_url":"nope","instance_name":"x","timezone":"UTC"}`)))
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"control_plane_url"`)) {
		t.Fatalf("invalid input: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/instance/settings/network/confirm", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm w/o pending: %d", rec.Code)
	}
}
