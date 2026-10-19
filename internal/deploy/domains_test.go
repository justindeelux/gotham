package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// seedDomainApp stores one application with the given primary domain row.
func seedDomainApp(t *testing.T, repo *fakeRepository, userID uuid.UUID, domain string) Application {
	t.Helper()
	app := testApplication(userID)
	app.ID = uuid.New()
	app.ServerID = uuid.New()
	repo.apps = append(repo.apps, app)
	if domain != "" {
		if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
			ApplicationID: app.ID,
			Domain:        domain,
			IsPrimary:     true,
			Port:          3000,
			Protocol:      ProtocolHTTP,
		}); err != nil {
			t.Fatalf("seed primary domain: %v", err)
		}
	}
	return app
}

func domainInput(domain string) DomainInput {
	return DomainInput{Domain: domain, Port: 3000, Protocol: ProtocolHTTP}
}

func primaryDomainOf(t *testing.T, domains []ApplicationDomain) ApplicationDomain {
	t.Helper()
	for _, domain := range domains {
		if domain.IsPrimary {
			return domain
		}
	}
	t.Fatalf("no primary in %#v", domains)
	return ApplicationDomain{}
}

func TestAddDomainAttachesAlias(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "WWW.Example.com", Port: 8080, Service: "", Protocol: "HTTPS",
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if created.Domain != "www.example.com" || created.IsPrimary || created.Port != 8080 || created.Protocol != ProtocolHTTPS {
		t.Fatalf("created = %#v, want the normalized routed alias", created)
	}
	domains, err := svc.ListDomains(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 2 || !domains[0].IsPrimary || domains[0].Domain != "app.example.com" {
		t.Fatalf("domains = %#v, want primary first", domains)
	}
}

func TestAddDomainFirstBecomesPrimary(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "")
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("app.example.com"))
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if !created.IsPrimary {
		t.Fatalf("created = %#v, want the first domain primary", created)
	}
	stored, err := repo.GetApplication(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	if stored.PrimaryDomain == nil || stored.PrimaryDomain.Domain != "app.example.com" {
		t.Fatalf("primary = %#v, want the derived primary", stored.PrimaryDomain)
	}
}

// TestAddDomainProvisionsHTTPSIntent pins the explicit protocol: an https
// domain records the default http-01 intent; an http domain records none.
func TestAddDomainProvisionsHTTPSIntent(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "portfolio.example.com", Port: 3000, Protocol: ProtocolHTTPS,
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	var found *CertificateIntent
	for i := range repo.certificates {
		if repo.certificates[i].ApplicationID == app.ID && repo.certificates[i].Domain == "portfolio.example.com" {
			found = &repo.certificates[i]
		}
	}
	if found == nil {
		t.Fatalf("certificates = %#v, want an intent for the https domain", repo.certificates)
	}
	if !found.Enabled || found.Challenge != string(proxy.ChallengeHTTP01) || found.DNSProviderID != uuid.Nil {
		t.Fatalf("intent = %#v, want enabled http-01 with no provider", *found)
	}

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "plain.example.com", Port: 3000, Protocol: ProtocolHTTP,
	}); err != nil {
		t.Fatalf("AddDomain http: %v", err)
	}
	for _, cert := range repo.certificates {
		if cert.Domain == "plain.example.com" {
			t.Fatalf("http domain got a certificate intent: %#v", repo.certificates)
		}
	}
}

// TestAddDomainReusesExistingIntent pins the transaction safety: a
// pre-existing intent for the host is left untouched and the attach still
// succeeds.
func TestAddDomainReusesExistingIntent(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	repo.certificates = []CertificateIntent{{
		ApplicationID: app.ID, Domain: "kept.example.com", Enabled: false, Challenge: string(proxy.ChallengeHTTP01),
	}}
	svc := newTestService(t, repo)

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "kept.example.com", Port: 3000, Protocol: ProtocolHTTPS,
	}); err != nil {
		t.Fatalf("AddDomain with a pre-existing intent: %v", err)
	}
	if len(repo.certificates) != 1 || repo.certificates[0].Enabled {
		t.Fatalf("certificates = %#v, want the operator's intent untouched", repo.certificates)
	}
}

// TestAddDomainSkipsHTTP01UnderWildcard pins the wildcard guard: an enabled
// wildcard intent already covers the new host.
func TestAddDomainSkipsHTTP01UnderWildcard(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	repo.certificates = []CertificateIntent{{
		ApplicationID: app.ID, Domain: "app.example.com", Enabled: true, Challenge: string(proxy.ChallengeDNS01), Wildcard: true,
	}}
	svc := newTestService(t, repo)

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "www.example.com", Port: 3000, Protocol: ProtocolHTTPS,
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	for _, cert := range repo.certificates {
		if cert.Domain == "www.example.com" {
			t.Fatalf("per-host intent created under a wildcard: %#v", repo.certificates)
		}
	}
}

// TestRemoveDomainDeletesAutoIntent pins the cleanup: removing the host drops
// its auto-created http-01 intent but never an operator's wildcard/dns-01 one.
func TestRemoveDomainDeletesAutoIntent(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "gone.example.com", Port: 3000, Protocol: ProtocolHTTPS,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	repo.certificates = append(repo.certificates, CertificateIntent{
		ApplicationID: app.ID, Domain: "sibling.example.com", Enabled: true, Challenge: string(proxy.ChallengeDNS01), Wildcard: true,
	})

	if err := svc.RemoveDomain(context.Background(), userID, app.ID, created.ID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	if len(repo.certificates) != 1 || repo.certificates[0].Domain != "sibling.example.com" {
		t.Fatalf("certificates = %#v, want only the operator intent kept", repo.certificates)
	}
}

func TestAddDomainRejectsConflicts(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	seedDomainApp(t, repo, userID, "other.example.com")
	svc := newTestService(t, repo)

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("not a domain")); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("APP.example.com")); !errors.Is(err, ErrValidation) {
		t.Fatalf("same-app duplicate err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("other.example.com")); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("cross-app duplicate err = %v, want ErrDomainConflict", err)
	}
}

func TestAddDomainValidatesRoutingTarget(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	cases := []struct {
		name string
		in   DomainInput
	}{
		{"port zero", DomainInput{Domain: "zero.example.com"}},
		{"port too high", DomainInput{Domain: "high.example.com", Port: 70000}},
		{"bad protocol", DomainInput{Domain: "proto.example.com", Port: 3000, Protocol: "tcp"}},
		{"service on a non-compose app", DomainInput{Domain: "svc.example.com", Port: 3000, Service: "web"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.AddDomain(context.Background(), userID, app.ID, tc.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestUpdateDomainRetargetsHostAndRouting(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	alias, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "www.example.com", Port: 3000, Protocol: ProtocolHTTP,
	})
	if err != nil {
		t.Fatalf("add alias: %v", err)
	}
	updated, err := svc.UpdateDomain(context.Background(), userID, app.ID, alias.ID, DomainInput{
		Domain: "www.example.org", Port: 8080, Protocol: ProtocolHTTPS,
	})
	if err != nil {
		t.Fatalf("UpdateDomain: %v", err)
	}
	if updated.Domain != "www.example.org" || updated.Port != 8080 || updated.Protocol != ProtocolHTTPS {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := svc.UpdateDomain(context.Background(), userID, app.ID, uuid.New(), domainInput("x.example.com")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestRemoveDomainPromotesOldest(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	first, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("b.example.com"))
	if err != nil {
		t.Fatalf("add b: %v", err)
	}
	second, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("a.example.com"))
	if err != nil {
		t.Fatalf("add a: %v", err)
	}
	domains, _ := svc.ListDomains(context.Background(), userID, app.ID)
	primaryID := primaryDomainOf(t, domains).ID

	if err := svc.RemoveDomain(context.Background(), userID, app.ID, primaryID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	// b was added before a, so b is promoted deterministically.
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 2 || !domains[0].IsPrimary || domains[0].Domain != "b.example.com" {
		t.Fatalf("domains = %#v, want b promoted", domains)
	}
	_ = first

	if err := svc.RemoveDomain(context.Background(), userID, app.ID, second.ID); err != nil {
		t.Fatalf("remove alias: %v", err)
	}
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 1 || domains[0].Domain != "b.example.com" {
		t.Fatalf("domains = %#v, want only b left", domains)
	}

	if err := svc.RemoveDomain(context.Background(), userID, app.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestRemoveLastDomainClearsPrimary(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "only.example.com")
	svc := newTestService(t, repo)

	domains, err := svc.ListDomains(context.Background(), userID, app.ID)
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains = %#v (%v), want the seeded primary", domains, err)
	}
	if err := svc.RemoveDomain(context.Background(), userID, app.ID, domains[0].ID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	stored, err := repo.GetApplication(context.Background(), app.ID)
	if err != nil || stored.PrimaryDomain != nil {
		t.Fatalf("primary = %#v err=%v, want none", stored.PrimaryDomain, err)
	}
}

func TestSetPrimaryDomainFollowsRows(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	alias, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput("www.example.com"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.SetPrimaryDomain(context.Background(), userID, app.ID, alias.ID); err != nil {
		t.Fatalf("SetPrimaryDomain: %v", err)
	}
	domains, _ := svc.ListDomains(context.Background(), userID, app.ID)
	primaries := 0
	for _, domain := range domains {
		if domain.IsPrimary {
			primaries++
			if domain.Domain != "www.example.com" {
				t.Fatalf("primary = %#v", domain)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("domains = %#v, want exactly one primary", domains)
	}
	if err := svc.SetPrimaryDomain(context.Background(), userID, app.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

// TestDomainServiceValidatesComposeService proves a domain may only name a
// service the pasted compose document declares.
func TestDomainServiceValidatesComposeService(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := testApplication(userID)
	app.SourceType = SourceCompose
	app.ComposeContent = "services:\n  web:\n    image: x\n  api:\n    image: y\n"
	app.ComposeService = "web"
	repo.apps = append(repo.apps, app)
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "api.example.com", Port: 8000, Service: "api", Protocol: ProtocolHTTP,
	})
	if err != nil || created.Service != "api" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "missing.example.com", Port: 8000, Service: "missing",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing service err = %v, want ErrValidation", err)
	}
	// An empty service targets the release web service and is valid.
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, DomainInput{
		Domain: "default.example.com", Port: 3000,
	}); err != nil {
		t.Fatalf("empty service err = %v, want nil", err)
	}
}

// TestUpdateDomainReenablesDisabledRow proves editing the host of a
// conflict-disabled row is the explicit resolution: a free host re-enables
// it, while a port-only edit leaves the conflict flag untouched.
func TestUpdateDomainReenablesDisabledRow(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	disabled, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
		ApplicationID: app.ID, Domain: "conflict.example.com", Disabled: true, Port: 3000, Protocol: ProtocolHTTP,
	})
	if err != nil {
		t.Fatalf("seed disabled row: %v", err)
	}

	// A port-only edit keeps the conflict visible.
	stillDisabled, err := svc.UpdateDomain(context.Background(), userID, app.ID, disabled.ID, DomainInput{
		Domain: "conflict.example.com", Port: 8080, Protocol: ProtocolHTTP,
	})
	if err != nil {
		t.Fatalf("port edit: %v", err)
	}
	if !stillDisabled.Disabled {
		t.Fatalf("row = %#v, want the disabled flag preserved", stillDisabled)
	}

	// A new, unclaimed host resolves the conflict and routes again.
	reenabled, err := svc.UpdateDomain(context.Background(), userID, app.ID, disabled.ID, DomainInput{
		Domain: "fresh.example.com", Port: 8080, Protocol: ProtocolHTTPS,
	})
	if err != nil {
		t.Fatalf("host edit: %v", err)
	}
	if reenabled.Disabled || reenabled.Domain != "fresh.example.com" || reenabled.Protocol != ProtocolHTTPS {
		t.Fatalf("row = %#v, want the re-enabled fresh host", reenabled)
	}
}
