package proxy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRedirectStore is an in-memory RedirectStore for the service tests.
type fakeRedirectStore struct {
	apps      map[uuid.UUID]ApplicationInfo
	redirects map[uuid.UUID]DomainRedirect
}

func newFakeRedirectStore() *fakeRedirectStore {
	return &fakeRedirectStore{
		apps:      map[uuid.UUID]ApplicationInfo{},
		redirects: map[uuid.UUID]DomainRedirect{},
	}
}

func (s *fakeRedirectStore) addApp(info ApplicationInfo) { s.apps[info.ID] = info }

func (s *fakeRedirectStore) GetApplication(_ context.Context, id uuid.UUID) (ApplicationInfo, error) {
	info, ok := s.apps[id]
	if !ok {
		return ApplicationInfo{}, ErrNotFound
	}
	return info, nil
}

func (s *fakeRedirectStore) CreateRedirect(_ context.Context, in RedirectWrite) (DomainRedirect, error) {
	for _, existing := range s.redirects {
		if existing.SourceDomain == in.SourceDomain {
			return DomainRedirect{}, mapRedirectWriteError(nil)
		}
	}
	redirect := DomainRedirect{
		ID:            uuid.New(),
		ApplicationID: in.ApplicationID,
		SourceDomain:  in.SourceDomain,
		TargetDomain:  in.TargetDomain,
		Code:          in.Code,
		PreservePath:  in.PreservePath,
		Enabled:       in.Enabled,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	s.redirects[redirect.ID] = redirect
	return redirect, nil
}

func (s *fakeRedirectStore) GetRedirect(_ context.Context, id uuid.UUID) (DomainRedirect, error) {
	redirect, ok := s.redirects[id]
	if !ok {
		return DomainRedirect{}, ErrNotFound
	}
	return redirect, nil
}

func (s *fakeRedirectStore) GetRedirectBySource(_ context.Context, source string) (DomainRedirect, error) {
	for _, redirect := range s.redirects {
		if redirect.SourceDomain == source {
			return redirect, nil
		}
	}
	return DomainRedirect{}, ErrNotFound
}

func (s *fakeRedirectStore) ListRedirects(_ context.Context) ([]DomainRedirect, error) {
	out := make([]DomainRedirect, 0, len(s.redirects))
	for _, redirect := range s.redirects {
		out = append(out, redirect)
	}
	return out, nil
}

func (s *fakeRedirectStore) ListRedirectsByApplication(_ context.Context, applicationID uuid.UUID) ([]DomainRedirect, error) {
	out := make([]DomainRedirect, 0)
	for _, redirect := range s.redirects {
		if redirect.ApplicationID == applicationID {
			out = append(out, redirect)
		}
	}
	return out, nil
}

func (s *fakeRedirectStore) UpdateRedirect(_ context.Context, id uuid.UUID, in RedirectWrite) (DomainRedirect, error) {
	redirect, ok := s.redirects[id]
	if !ok {
		return DomainRedirect{}, ErrNotFound
	}
	for _, existing := range s.redirects {
		if existing.ID != id && existing.SourceDomain == in.SourceDomain {
			return DomainRedirect{}, mapRedirectWriteError(nil)
		}
	}
	redirect.SourceDomain = in.SourceDomain
	redirect.TargetDomain = in.TargetDomain
	redirect.Code = in.Code
	redirect.PreservePath = in.PreservePath
	redirect.Enabled = in.Enabled
	redirect.UpdatedAt = time.Now()
	s.redirects[id] = redirect
	return redirect, nil
}

func (s *fakeRedirectStore) DeleteRedirect(_ context.Context, id uuid.UUID) error {
	if _, ok := s.redirects[id]; !ok {
		return ErrNotFound
	}
	delete(s.redirects, id)
	return nil
}

func (s *fakeRedirectStore) ListApplicationBaseDomains(_ context.Context) ([]ApplicationDomain, error) {
	out := make([]ApplicationDomain, 0, len(s.apps))
	for _, app := range s.apps {
		if app.BaseDomain != "" {
			out = append(out, ApplicationDomain{ID: app.ID, Domain: app.BaseDomain})
		}
	}
	return out, nil
}

func (s *fakeRedirectStore) ListEnabledRedirectSources(_ context.Context) ([]RedirectSourceRef, error) {
	out := make([]RedirectSourceRef, 0)
	for _, redirect := range s.redirects {
		if redirect.Enabled {
			out = append(out, RedirectSourceRef{ID: redirect.ID, Source: redirect.SourceDomain})
		}
	}
	return out, nil
}

// newRedirectService wires the service over a fake store and counts resyncs.
func newRedirectService(t *testing.T, store *fakeRedirectStore) (*redirectService, *int) {
	t.Helper()
	resyncs := 0
	svc := NewDefaultRedirectService(RedirectConfig{
		Store:  store,
		Logger: discardLogger(),
		Resync: func(context.Context) error {
			resyncs++
			return nil
		},
	}).(*redirectService)
	return svc, &resyncs
}

// redirectApp is a healthy application fixture.
func redirectApp(serverID uuid.UUID) ApplicationInfo {
	return ApplicationInfo{ID: uuid.New(), BaseDomain: "app.example.com", ServerID: serverID}
}

func TestCreateRedirectDefaultsAndNormalization(t *testing.T) {
	store := newFakeRedirectStore()
	app := redirectApp(uuid.New())
	store.addApp(app)
	svc, resyncs := newRedirectService(t, store)

	created, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
		ApplicationID: app.ID,
		SourceDomain:  "  WWW.Example.COM ",
		TargetDomain:  "App.Example.com",
	})
	if err != nil {
		t.Fatalf("CreateRedirect: %v", err)
	}
	if created.SourceDomain != "www.example.com" || created.TargetDomain != "app.example.com" {
		t.Fatalf("domains = %q -> %q, want normalized", created.SourceDomain, created.TargetDomain)
	}
	if created.Code != RedirectCodePermanent || !created.PreservePath || !created.Enabled {
		t.Fatalf("defaults = %#v, want 301/preserve/enabled", created)
	}
	if *resyncs != 1 {
		t.Fatalf("resyncs = %d, want 1", *resyncs)
	}
}

func TestCreateRedirectValidation(t *testing.T) {
	server := uuid.New()
	owner := redirectApp(server)
	cases := []struct {
		name string
		in   CreateRedirectInput
	}{
		{"missing application", CreateRedirectInput{SourceDomain: "a.example.com", TargetDomain: "b.example.com"}},
		{"invalid source", CreateRedirectInput{ApplicationID: owner.ID, SourceDomain: "not a host", TargetDomain: "b.example.com"}},
		{"invalid target", CreateRedirectInput{ApplicationID: owner.ID, SourceDomain: "a.example.com", TargetDomain: "*.b.example.com"}},
		{"self redirect", CreateRedirectInput{ApplicationID: owner.ID, SourceDomain: "a.example.com", TargetDomain: "A.EXAMPLE.COM"}},
		{"unsupported code", CreateRedirectInput{ApplicationID: owner.ID, SourceDomain: "a.example.com", TargetDomain: "b.example.com", Code: 307}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeRedirectStore()
			store.addApp(owner)
			svc, resyncs := newRedirectService(t, store)
			if _, err := svc.CreateRedirect(context.Background(), tc.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if *resyncs != 0 {
				t.Fatalf("resyncs = %d, want none on a rejected write", *resyncs)
			}
		})
	}
}

func TestCreateRedirectRejectsUnservableApplications(t *testing.T) {
	cases := []struct {
		name string
		app  ApplicationInfo
		add  bool
	}{
		{"unknown application", ApplicationInfo{ID: uuid.New()}, false},
		{"domain disabled", ApplicationInfo{ID: uuid.New(), BaseDomain: "app.example.com", DomainDisabled: true, ServerID: uuid.New()}, true},
		{"unassigned node", ApplicationInfo{ID: uuid.New(), BaseDomain: "app.example.com"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeRedirectStore()
			if tc.add {
				store.addApp(tc.app)
			}
			svc, _ := newRedirectService(t, store)
			_, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
				ApplicationID: tc.app.ID,
				SourceDomain:  "www.example.com",
				TargetDomain:  "app.example.com",
			})
			if tc.name == "unknown application" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestCreateRedirectConflicts(t *testing.T) {
	server := uuid.New()
	owner := redirectApp(server)
	other := ApplicationInfo{ID: uuid.New(), BaseDomain: "landing.example.com", ServerID: server}

	t.Run("source shadows an application base domain", func(t *testing.T) {
		store := newFakeRedirectStore()
		store.addApp(owner)
		store.addApp(other)
		svc, _ := newRedirectService(t, store)
		_, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "landing.example.com",
			TargetDomain:  "app.example.com",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("source shadows its own base domain", func(t *testing.T) {
		store := newFakeRedirectStore()
		store.addApp(owner)
		svc, _ := newRedirectService(t, store)
		_, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "app.example.com",
			TargetDomain:  "other.example.com",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("duplicate source", func(t *testing.T) {
		store := newFakeRedirectStore()
		store.addApp(owner)
		svc, _ := newRedirectService(t, store)
		first, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "www.example.com",
			TargetDomain:  "app.example.com",
		})
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		secondApp := redirectApp(server)
		store.addApp(secondApp)
		_, err = svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: secondApp.ID,
			SourceDomain:  "WWW.Example.com",
			TargetDomain:  "elsewhere.example.com",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate source err = %v, want ErrConflict (first %s)", err, first.ID)
		}
	})

	t.Run("target chains into another enabled source", func(t *testing.T) {
		store := newFakeRedirectStore()
		store.addApp(owner)
		svc, _ := newRedirectService(t, store)
		if _, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "final.example.com",
			TargetDomain:  "app.example.com",
		}); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
		_, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "start.example.com",
			TargetDomain:  "final.example.com",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("chain err = %v, want ErrConflict", err)
		}
	})

	t.Run("disabled rule may target another source", func(t *testing.T) {
		store := newFakeRedirectStore()
		store.addApp(owner)
		svc, _ := newRedirectService(t, store)
		if _, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "final.example.com",
			TargetDomain:  "app.example.com",
		}); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
		disabled := false
		if _, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
			ApplicationID: owner.ID,
			SourceDomain:  "start.example.com",
			TargetDomain:  "final.example.com",
			Enabled:       &disabled,
		}); err != nil {
			t.Fatalf("disabled rule create: %v", err)
		}
	})
}

func TestUpdateRedirectPartialAndGuards(t *testing.T) {
	store := newFakeRedirectStore()
	app := redirectApp(uuid.New())
	store.addApp(app)
	svc, resyncs := newRedirectService(t, store)

	created, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
		ApplicationID: app.ID,
		SourceDomain:  "www.example.com",
		TargetDomain:  "app.example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	code := RedirectCodeTemporary
	disabled := false
	updated, err := svc.UpdateRedirect(context.Background(), created.ID, UpdateRedirectInput{Code: &code, Enabled: &disabled})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Code != RedirectCodeTemporary || updated.Enabled {
		t.Fatalf("updated = %#v, want 302/disabled", updated)
	}
	if updated.SourceDomain != "www.example.com" || !updated.PreservePath {
		t.Fatalf("partial update changed unrelated fields: %#v", updated)
	}
	if *resyncs != 2 {
		t.Fatalf("resyncs = %d, want 2", *resyncs)
	}

	// Re-enabling re-runs the chain guard: point the rule at itself is
	// rejected, and pointing it at a disabled sibling's source is allowed.
	blocked := false
	if _, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
		ApplicationID: app.ID,
		SourceDomain:  "held.example.com",
		TargetDomain:  "app.example.com",
		Enabled:       &blocked,
	}); err != nil {
		t.Fatalf("seed disabled: %v", err)
	}
	target := "held.example.com"
	if _, err := svc.UpdateRedirect(context.Background(), created.ID, UpdateRedirectInput{TargetDomain: &target, Enabled: boolPtr(true)}); err != nil {
		t.Fatalf("target at a disabled rule's source must be allowed: %v", err)
	}
	// A source equal to another rule's source is a duplicate.
	back := "app.example.com"
	if _, err := svc.UpdateRedirect(context.Background(), created.ID, UpdateRedirectInput{TargetDomain: &back}); err != nil {
		t.Fatalf("restore target: %v", err)
	}
	source := "held.example.com"
	if _, err := svc.UpdateRedirect(context.Background(), created.ID, UpdateRedirectInput{SourceDomain: &source}); !errors.Is(err, ErrConflict) {
		t.Fatalf("source onto another rule err = %v, want ErrConflict", err)
	}
	if _, err := svc.UpdateRedirect(context.Background(), created.ID, UpdateRedirectInput{Code: intPtr(999)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid code err = %v, want ErrValidation", err)
	}
	if _, err := svc.UpdateRedirect(context.Background(), uuid.New(), UpdateRedirectInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestDeleteRedirect(t *testing.T) {
	store := newFakeRedirectStore()
	app := redirectApp(uuid.New())
	store.addApp(app)
	svc, resyncs := newRedirectService(t, store)

	created, err := svc.CreateRedirect(context.Background(), CreateRedirectInput{
		ApplicationID: app.ID,
		SourceDomain:  "www.example.com",
		TargetDomain:  "app.example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.DeleteRedirect(context.Background(), created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetRedirect(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteRedirect(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	if *resyncs != 2 {
		t.Fatalf("resyncs = %d, want create+delete only", *resyncs)
	}
}

func TestListRedirectsByApplication(t *testing.T) {
	store := newFakeRedirectStore()
	app := redirectApp(uuid.New())
	other := redirectApp(uuid.New())
	store.addApp(app)
	store.addApp(other)
	svc, _ := newRedirectService(t, store)

	for _, in := range []CreateRedirectInput{
		{ApplicationID: app.ID, SourceDomain: "one.example.com", TargetDomain: "app.example.com"},
		{ApplicationID: app.ID, SourceDomain: "two.example.com", TargetDomain: "app.example.com"},
		{ApplicationID: other.ID, SourceDomain: "three.example.com", TargetDomain: "app.example.com"},
	} {
		if _, err := svc.CreateRedirect(context.Background(), in); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	all, err := svc.ListRedirects(context.Background(), uuid.Nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %d (%v), want 3", len(all), err)
	}
	mine, err := svc.ListRedirects(context.Background(), app.ID)
	if err != nil || len(mine) != 2 {
		t.Fatalf("mine = %d (%v), want 2", len(mine), err)
	}
}

// TestRedirectsForServerHoldsBackConflicts proves the generation-side safety
// net: domain-disabled owners, duplicate sources, node-domain shadows and
// chains are all held back as diagnostics instead of emitted.
func TestRedirectsForServerHoldsBackConflicts(t *testing.T) {
	server := uuid.New()
	app := uuid.New()
	apps := []ProxiedApplication{
		{ID: app, ServerID: server, BaseDomain: "app.example.com"},
	}
	otherServer := uuid.New()
	rules := []RedirectRule{
		// Healthy.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "www.example.com", TargetDomain: "app.example.com", Code: 301, PreservePath: true, Enabled: true, ServerID: server},
		// Other node: not this node's rule.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "other.example.com", TargetDomain: "app.example.com", Code: 301, Enabled: true, ServerID: otherServer},
		// Disabled: not emitted.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "paused.example.com", TargetDomain: "app.example.com", Code: 301, Enabled: false, ServerID: server},
		// Domain-disabled owner: held back.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "blocked.example.com", TargetDomain: "app.example.com", Code: 301, Enabled: true, ServerID: server, DomainDisabled: true},
		// Source shadows an application base domain on the node: held back.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "app.example.com", TargetDomain: "elsewhere.example.com", Code: 301, Enabled: true, ServerID: server},
		// Duplicate source: both held back.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "dupe.example.com", TargetDomain: "app.example.com", Code: 301, Enabled: true, ServerID: server},
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "dupe.example.com", TargetDomain: "app.example.com", Code: 301, Enabled: true, ServerID: server},
		// Chain: the rule pointing at another live source is held back.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "chain.example.com", TargetDomain: "www.example.com", Code: 301, Enabled: true, ServerID: server},
		// Invalid code: held back.
		{ID: uuid.New(), ApplicationID: app, SourceDomain: "bad-code.example.com", TargetDomain: "app.example.com", Code: 307, Enabled: true, ServerID: server},
	}

	redirects, diagnostics := redirectsForServer(apps, rules, server)
	if len(redirects) != 1 || redirects[0].Source != "www.example.com" {
		t.Fatalf("redirects = %#v, want only www.example.com", redirects)
	}
	// Held back: domain-disabled owner, node-domain shadow, two duplicates,
	// a chain target and an invalid code. The other node's rule and the
	// disabled rule are simply not this node's emitted set.
	if len(diagnostics) != 6 {
		t.Fatalf("diagnostics = %#v, want 6 held-back rules", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Reason == "" || diagnostic.Domain == "" {
			t.Fatalf("diagnostic %#v misses domain/reason", diagnostic)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }
