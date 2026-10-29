package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const domainDocument = `services:
  web:
    image: nginx:1.27
    labels:
      gotham.domain: ${GOTHAM_DOMAIN:-}
    environment:
      PUBLIC_HOST: ${GOTHAM_DOMAIN:-}
      PUBLIC_HOSTS: ${GOTHAM_DOMAINS:-}
  api:
    image: nginx:1.27
`

func newDomainTestService(t *testing.T, repo *fakeRepository, agent *fakeAgent, sync *fakeRouteSync) ServiceService {
	t.Helper()
	svc := NewService(Config{
		Repository:    repo,
		Logger:        discardLogger(),
		DeployTimeout: 5 * time.Second,
		Proxy:         sync,
		Dial: func(context.Context, uuid.UUID) (ComposeAgent, error) {
			return agent, nil
		},
	})
	t.Cleanup(func() { closeTestService(t, svc) })
	return svc
}

func createDomainService(t *testing.T, svc ServiceService, repo *fakeRepository, userID uuid.UUID) Service {
	t.Helper()
	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:          "domains",
		EnvironmentID: mustEnvironment(t, repo),
		ServerID:      repo.seedServer(),
		ComposeYAML:   domainDocument,
		Env:           map[string]string{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func mustEnvironment(t *testing.T, repo *fakeRepository) uuid.UUID {
	t.Helper()
	envID, _ := repo.seedEnvironment()
	return envID
}

func TestRenderServiceInjectsDomainVariables(t *testing.T) {
	managed := []ServiceDomain{
		{Domain: "second.example.com", Disabled: true},
		{Domain: "first.example.com"},
		{Domain: "third.example.com"},
	}
	rendered, hosts, err := RenderService(domainDocument, map[string]string{}, managed)
	if err != nil {
		t.Fatalf("RenderService: %v", err)
	}
	want := []string{"first.example.com", "third.example.com"}
	if strings.Join(hosts, ",") != strings.Join(want, ",") {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
	if !strings.Contains(rendered.ComposeYAML, "PUBLIC_HOST: first.example.com") {
		t.Errorf("GOTHAM_DOMAIN not injected:\n%s", rendered.ComposeYAML)
	}
	if !strings.Contains(rendered.ComposeYAML, "PUBLIC_HOSTS: first.example.com,third.example.com") {
		t.Errorf("GOTHAM_DOMAINS not injected:\n%s", rendered.ComposeYAML)
	}
	if !strings.Contains(rendered.ComposeYAML, "gotham.domain: first.example.com") {
		t.Errorf("label not rendered:\n%s", rendered.ComposeYAML)
	}
}

func TestRenderServiceWithoutDomainsIsEmpty(t *testing.T) {
	rendered, hosts, err := RenderService(domainDocument, nil, nil)
	if err != nil {
		t.Fatalf("RenderService: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("hosts = %v, want empty", hosts)
	}
	if !strings.Contains(rendered.ComposeYAML, "PUBLIC_HOST: \"\"") {
		t.Errorf("GOTHAM_DOMAIN should render empty:\n%s", rendered.ComposeYAML)
	}
}

func TestCreateRejectsReservedDomainEnvironment(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	_, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name:          "reserved",
		EnvironmentID: mustEnvironment(t, repo),
		ServerID:      repo.seedServer(),
		ComposeYAML:   "services:\n  web:\n    image: nginx:1.27\n",
		Env:           map[string]string{EnvDomain: "example.com"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create err = %v, want ErrValidation", err)
	}
}

func TestAddDomainValidationAndConflicts(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "missing", Port: 80,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown compose service err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 0,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad port err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80, Protocol: "tcp",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad protocol err = %v, want ErrValidation", err)
	}

	created, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "App.Example.com", ComposeService: "web", Port: 80, Protocol: "https",
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if created.Domain != "app.example.com" || created.Protocol != "https" {
		t.Fatalf("created = %+v", created)
	}

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "api", Port: 80,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("same-service duplicate err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("same-service claim err = %v, want ErrValidation", err)
	}

	repo.applicationClaims = map[string]bool{"taken.example.com": true}
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "taken.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("application claim err = %v, want ErrDomainConflict", err)
	}

	repo.redirectSources = []string{"redirect.example.com"}
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "redirect.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("redirect claim err = %v, want ErrDomainConflict", err)
	}
}

func TestAddDomainRejectsAnotherServiceComposeLabel(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)

	other := Service{
		ID:          uuid.New(),
		UserID:      userID,
		Name:        "other",
		ComposeYAML: "services:\n  web:\n    image: nginx:1.27\n    labels:\n      gotham.domain: labelled.example.com\n",
		Env:         map[string]string{},
	}
	if _, err := repo.CreateService(context.Background(), other); err != nil {
		t.Fatalf("seed other service: %v", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, service.ID, DomainInput{
		Domain: "labelled.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("compose label claim err = %v, want ErrDomainConflict", err)
	}
}

func TestAddDomainSyncsProxy(t *testing.T) {
	repo := newFakeRepository()
	sync := &fakeRouteSync{}
	svc := newDomainTestService(t, repo, &fakeAgent{}, sync)
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80,
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if len(sync.calls()) != 1 {
		t.Fatalf("proxy syncs = %d, want 1", len(sync.calls()))
	}
}

func TestListDomainsReportsComposeAndManagedWithStatus(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	agent.ps = []ComposeContainer{
		{Service: "web", State: "running", Ports: []string{"0.0.0.0:8080->80/tcp"}},
		{Service: "api", State: "running", Ports: []string{"0.0.0.0:9090->90/tcp"}},
	}
	svc := newDomainTestService(t, repo, agent, &fakeRouteSync{})
	userID := uuid.New()
	service, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:          "domains",
		EnvironmentID: mustEnvironment(t, repo),
		ServerID:      repo.seedServer(),
		ComposeYAML:   testDocument,
		Env:           testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ctx := context.Background()

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "managed.example.com", ComposeService: "web", Port: 80, Protocol: "https",
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	views, err := svc.ListDomains(ctx, userID, service.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %+v, want a compose row and a managed row", views)
	}
	bySource := map[string]DomainView{}
	for _, view := range views {
		bySource[view.Source] = view
	}
	if bySource[SourceManaged].Status != DomainStatusRouted {
		t.Errorf("managed status = %q (%s)", bySource[SourceManaged].Status, bySource[SourceManaged].Reason)
	}
	compose := bySource[SourceCompose]
	if compose.Status != DomainStatusRouted {
		t.Errorf("compose row = %+v (%s), want routed", compose, compose.Reason)
	}
}

func TestListDomainsShadowsInjectedLabel(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{ps: []ComposeContainer{
		{Service: "web", State: "running", Ports: []string{"0.0.0.0:8080->80/tcp"}},
	}}
	svc := newDomainTestService(t, repo, agent, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()
	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "a.example.com", ComposeService: "web", Port: 80,
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	views, err := svc.ListDomains(ctx, userID, service.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	bySource := map[string]DomainView{}
	for _, view := range views {
		bySource[view.Source] = view
	}
	if managed := bySource[SourceManaged]; managed.Status != DomainStatusRouted {
		t.Errorf("managed row = %+v (%s), want routed", managed, managed.Reason)
	}
	if compose := bySource[SourceCompose]; compose.Status != DomainStatusShadowed {
		t.Errorf("compose row = %+v (%s), want shadowed", compose, compose.Reason)
	}
}

func TestEnableDisabledDomainRechecksClaim(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	disabled := true
	created, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80, Disabled: &disabled,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	repo.applicationClaims = map[string]bool{"app.example.com": true}
	enabled := false
	if _, err := svc.UpdateDomain(ctx, userID, service.ID, created.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80, Disabled: &enabled,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("re-enable conflict err = %v, want ErrDomainConflict", err)
	}
}

func TestUpdateHostChangeKeepsExplicitDisabled(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	disabled := true
	created, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "old.example.com", ComposeService: "web", Port: 80, Disabled: &disabled,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	updated, err := svc.UpdateDomain(ctx, userID, service.ID, created.ID, DomainInput{
		Domain: "new.example.com", ComposeService: "web", Port: 80, Disabled: &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateDomain: %v", err)
	}
	if !updated.Disabled {
		t.Fatalf("updated = %+v, want the explicit disabled flag kept", updated)
	}
}

func TestListDomainsSeesAnotherServiceLabel(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	other := Service{
		ID:          uuid.New(),
		UserID:      userID,
		Name:        "other",
		ComposeYAML: "services:\n  web:\n    image: nginx:1.27\n    labels:\n      gotham.domain: taken.example.com\n",
		Env:         map[string]string{},
	}
	if _, err := repo.CreateService(context.Background(), other); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, service.ID, DomainInput{
		Domain: "taken.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("write-time claim err = %v, want ErrDomainConflict", err)
	}

	// The label appears after the managed row (the write guard did not see it),
	// which is exactly the case the list diagnostic must explain.
	repo.domains[service.ID] = append(repo.domains[service.ID], ServiceDomain{
		ID: uuid.New(), ServiceID: service.ID, ComposeService: "web", Port: 80,
		Domain: "taken.example.com", Protocol: "http",
	})
	views, err := svc.ListDomains(context.Background(), userID, service.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	for _, view := range views {
		if view.Source == SourceManaged && view.Domain == "taken.example.com" {
			if view.Status != DomainStatusHeld || !strings.Contains(view.Reason, "another service") {
				t.Fatalf("managed row = %+v (%s), want held by another service", view, view.Reason)
			}
		}
	}
}

func TestDeployStoresDomainsAndPendingChanges(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newDomainTestService(t, repo, agent, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	first, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "one.example.com", ComposeService: "web", Port: 80,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if _, _, err := svc.Deploy(ctx, userID, service.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	deploy := waitForDeploy(t, svc, userID, service.ID)
	if deploy.State != DeployRunning {
		t.Fatalf("deploy state = %s (%s)", deploy.State, deploy.Error)
	}
	if len(deploy.Domains) != 1 || deploy.Domains[0] != first.Domain {
		t.Fatalf("deploy domains = %v, want [%s]", deploy.Domains, first.Domain)
	}
	loaded, err := svc.Get(ctx, userID, service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(loaded.PendingChanges) != 0 {
		t.Fatalf("pending changes = %v, want empty after the deploy", loaded.PendingChanges)
	}

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "two.example.com", ComposeService: "web", Port: 80,
	}); err != nil {
		t.Fatalf("second AddDomain: %v", err)
	}
	loaded, err = svc.Get(ctx, userID, service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(loaded.PendingChanges) != 1 || loaded.PendingChanges[0] != "domains" {
		t.Fatalf("pending changes = %v, want [domains]", loaded.PendingChanges)
	}
}

func TestDeleteRemovesManagedDomainsAndResyncs(t *testing.T) {
	repo := newFakeRepository()
	sync := &fakeRouteSync{}
	svc := newDomainTestService(t, repo, &fakeAgent{}, sync)
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	if _, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "app.example.com", ComposeService: "web", Port: 80,
	}); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := svc.Delete(ctx, userID, service.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if domains, err := repo.ListServiceDomains(ctx, service.ID); err != nil || len(domains) != 0 {
		t.Fatalf("managed domains survived delete: %+v err=%v", domains, err)
	}
	if len(sync.calls()) < 2 {
		t.Fatalf("proxy syncs = %d, want add and delete", len(sync.calls()))
	}
}

func TestUpdateDomainHostChangeRechecksClaimAndEnables(t *testing.T) {
	repo := newFakeRepository()
	svc := newDomainTestService(t, repo, &fakeAgent{}, &fakeRouteSync{})
	userID := uuid.New()
	service := createDomainService(t, svc, repo, userID)
	ctx := context.Background()

	disabled := true
	created, err := svc.AddDomain(ctx, userID, service.ID, DomainInput{
		Domain: "old.example.com", ComposeService: "web", Port: 80, Disabled: &disabled,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	repo.applicationClaims = map[string]bool{"taken.example.com": true}
	if _, err := svc.UpdateDomain(ctx, userID, service.ID, created.ID, DomainInput{
		Domain: "taken.example.com", ComposeService: "web", Port: 80,
	}); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("update conflict err = %v, want ErrDomainConflict", err)
	}
	updated, err := svc.UpdateDomain(ctx, userID, service.ID, created.ID, DomainInput{
		Domain: "new.example.com", ComposeService: "api", Port: 90, Protocol: "http",
	})
	if err != nil {
		t.Fatalf("UpdateDomain: %v", err)
	}
	if updated.Domain != "new.example.com" || updated.Disabled || updated.ComposeService != "api" {
		t.Fatalf("updated = %+v", updated)
	}

	if err := svc.RemoveDomain(ctx, userID, service.ID, created.ID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	views, err := svc.ListDomains(ctx, userID, service.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	for _, view := range views {
		if view.Source == SourceManaged {
			t.Fatalf("managed row survived delete: %+v", view)
		}
	}
}
