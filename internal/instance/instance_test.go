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
	live                        HostNetwork
	hasLive                     bool
	applied, confirmed, reverts int
	dnsOnlys                    []bool
	err                         error
}

func (h *fakeHost) Capabilities(context.Context) Capabilities { return h.caps }
func (h *fakeHost) HostNetwork(context.Context) (HostNetwork, bool) {
	return h.live, h.hasLive
}
func (h *fakeHost) ApplyNetwork(_ context.Context, _ Network, _ time.Duration, dnsOnly bool) error {
	h.applied++
	h.dnsOnlys = append(h.dnsOnlys, dnsOnly)
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

	st, err := svc.UpdateNetwork(ctx, uuid.Nil, NetworkInput{Network: goodNet()})
	if err != nil || st.Pending == nil || host.applied != 1 {
		t.Fatalf("apply: %+v %v", st, err)
	}
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, NetworkInput{Network: goodNet()}); !errors.Is(err, ErrPending) {
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
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, NetworkInput{Network: next}); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now.Add(NetworkConfirmWindow + time.Second) }
	st, err = svc.Get(ctx)
	if err != nil || st.Pending != nil || st.Network.IPv4.Address != "10.0.0.5/24" {
		t.Fatalf("auto-revert: %+v %v", st.Network, err)
	}

	// Manual revert.
	svc.now = func() time.Time { return now }
	if _, err := svc.UpdateNetwork(ctx, uuid.Nil, NetworkInput{Network: next}); err != nil {
		t.Fatal(err)
	}
	if st, err = svc.RevertNetwork(ctx, uuid.Nil); err != nil || st.Network.IPv4.Address != "10.0.0.5/24" || host.reverts != 1 {
		t.Fatalf("revert: %+v %v", st.Network, err)
	}
	if len(repo.sections) == 0 {
		t.Fatal("no audit saves")
	}
}

// staticLive is the JUS-100 scene: the host is static but the stored record
// still carries the DHCP defaults, so a DNS-only change must apply minimally
// and a static-to-DHCP switch must need an explicit confirmation.
func staticLive() (HostNetwork, Stored) {
	live := HostNetwork{
		Interface:  "ens160",
		DNSServers: []string{"1.1.1.1"},
		IPv4:       IPConfig{Mode: ModeStatic, Address: "103.176.22.225/24", Gateway: "103.176.22.1"},
		IPv6:       IPv6Config{Enabled: false, Mode: ModeDHCP},
	}
	stored := Stored{Network: Network{
		DNSServers: []string{"1.1.1.1"},
		IPv4:       IPConfig{Mode: ModeDHCP}, IPv6: IPv6Config{Enabled: false, Mode: ModeDHCP},
	}}
	return live, stored
}

func liveNet(live HostNetwork, dns ...string) Network {
	if len(dns) > 0 {
		live.DNSServers = dns
	}
	return Network{DNSServers: live.DNSServers, IPv4: live.IPv4, IPv6: live.IPv6}
}

func TestNetworkDNSOnlyOnStaticHost(t *testing.T) {
	host := &fakeHost{caps: Capabilities{Network: true, System: true}, hasLive: true}
	host.live, _ = staticLive()
	_, stored := staticLive()
	repo := &fakeRepo{rec: stored}
	svc := NewService(repo, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.lookup = func(string) string { return "" }

	// The incident request: only the resolvers differ from the host truth.
	req := liveNet(host.live, "9.9.9.9")
	st, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: req})
	if err != nil || st.Pending == nil {
		t.Fatalf("dns-only apply: %+v %v", st, err)
	}
	if len(host.dnsOnlys) != 1 || !host.dnsOnlys[0] {
		t.Fatalf("dns-only change must take the minimal path: %v", host.dnsOnlys)
	}
	if st.Network.IPv4.Mode != ModeStatic || st.Network.IPv4.Address != "103.176.22.225/24" {
		t.Fatalf("desired state lost the static address: %+v", st.Network.IPv4)
	}
}

func TestNetworkRiskyChangeNeedsConfirmation(t *testing.T) {
	host := &fakeHost{caps: Capabilities{Network: true, System: true}, hasLive: true}
	host.live, _ = staticLive()
	newSvcLive := func() *Service {
		_, stored := staticLive()
		repo := &fakeRepo{rec: stored}
		svc := NewService(repo, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
		svc.lookup = func(string) string { return "" }
		return svc
	}

	// Static-to-DHCP on the active interface without the flag is refused.
	svc := newSvcLive()
	risky := liveNet(host.live)
	risky.IPv4 = IPConfig{Mode: ModeDHCP}
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: risky}); !errors.Is(err, ErrRiskyNetwork) {
		t.Fatalf("static->dhcp without confirmation: %v", err)
	}
	if host.applied != 0 {
		t.Fatal("refused change reached the host")
	}

	// A changed static address is equally risky.
	svc = newSvcLive()
	moved := liveNet(host.live)
	moved.IPv4.Address = "103.176.22.99/24"
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: moved}); !errors.Is(err, ErrRiskyNetwork) {
		t.Fatalf("address change without confirmation: %v", err)
	}

	// With the flag the same change applies through the full path.
	svc = newSvcLive()
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: risky, ConfirmInterfaceChange: true}); err != nil {
		t.Fatalf("confirmed change rejected: %v", err)
	}
	if len(host.dnsOnlys) == 0 || host.dnsOnlys[len(host.dnsOnlys)-1] {
		t.Fatalf("interface change must take the full path: %v", host.dnsOnlys)
	}
}

func TestGetReportsHostTruth(t *testing.T) {
	host := &fakeHost{caps: Capabilities{Network: true, System: true}, hasLive: true}
	host.live, _ = staticLive()
	_, stored := staticLive()
	repo := &fakeRepo{rec: stored}
	svc := NewService(repo, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.lookup = func(string) string { return "" }
	st, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Network.IPv4.Mode != ModeStatic || st.Network.IPv4.Address != "103.176.22.225/24" ||
		st.Network.IPv4.Gateway != "103.176.22.1" {
		t.Fatalf("form would show stale defaults: %+v", st.Network.IPv4)
	}
	// While a change is pending the desired (not live) values are shown.
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: liveNet(host.live, "9.9.9.9")}); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.Get(context.Background())
	if len(st.Network.DNSServers) != 1 || st.Network.DNSServers[0] != "9.9.9.9" {
		t.Fatalf("pending desired state hidden: %+v", st.Network.DNSServers)
	}
}

func TestGetReportsEffectiveLinkDNS(t *testing.T) {
	// JUS-101: the helper reports the link DNS as the effective resolvers
	// (resolved prefers per-link over global), so Get must surface them
	// instead of the stored servers.
	host := &fakeHost{caps: Capabilities{Network: true, System: true}, hasLive: true}
	host.live, _ = staticLive()
	host.live.DNSServers = []string{"8.8.8.8", "8.8.4.4"}
	_, stored := staticLive()
	repo := &fakeRepo{rec: stored}
	svc := NewService(repo, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.lookup = func(string) string { return "" }
	st, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Network.DNSServers) != 2 || st.Network.DNSServers[0] != "8.8.8.8" ||
		st.Network.DNSServers[1] != "8.8.4.4" {
		t.Fatalf("form would show shadowed servers: %+v", st.Network.DNSServers)
	}
}

func TestUnsupportedHostAndHelperFailure(t *testing.T) {
	host := &fakeHost{}
	svc, _ := newSvc(host)
	if _, err := svc.UpdateNetwork(context.Background(), uuid.Nil, NetworkInput{Network: goodNet()}); !errors.Is(err, ErrUnsupported) {
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
