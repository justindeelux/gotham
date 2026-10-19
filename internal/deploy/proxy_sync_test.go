package deploy

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeProxySync records the nodes the deploy lifecycle asked to resync.
type fakeProxySync struct {
	mu      sync.Mutex
	servers []uuid.UUID
	err     error
}

// SyncServer records the call and returns the canned error.
func (f *fakeProxySync) SyncServer(_ context.Context, serverID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers = append(f.servers, serverID)
	return f.err
}

// calls returns a copy of the recorded server ids.
func (f *fakeProxySync) calls() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID{}, f.servers...)
}

// newProxyTestService builds a Service with the proxy hook wired.
func newProxyTestService(t *testing.T, repo *fakeRepository, proxySync ProxySync) *Service {
	t.Helper()
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Proxy:      proxySync,
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func withPrimaryDomain(app Application, domain string) Application {
	app.PrimaryDomain = &ApplicationDomain{
		ApplicationID: app.ID, Domain: domain, IsPrimary: true, Port: 3000, Protocol: ProtocolHTTP,
	}
	return app
}

func TestDomainChangesSyncProxy(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	proxySync := &fakeProxySync{}
	svc := newProxyTestService(t, repo, proxySync)

	// Create is domainless: no route, no sync.
	in := validCreateInput(uuid.New())
	created, err := svc.CreateApplication(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 0 {
		t.Fatalf("proxy calls = %v, want none for a domainless application", calls)
	}

	domain, err := svc.AddDomain(context.Background(), userID, created.ID, domainInput("app.example.com"))
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 1 || calls[0] != created.ServerID {
		t.Fatalf("proxy calls = %v, want [%s]", calls, created.ServerID)
	}

	if _, err := svc.UpdateDomain(context.Background(), userID, created.ID, domain.ID, DomainInput{
		Domain: "www.example.com", Port: 8080, Protocol: ProtocolHTTPS,
	}); err != nil {
		t.Fatalf("update domain: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 2 || calls[1] != created.ServerID {
		t.Fatalf("proxy calls = %v, want a second sync for the edit", calls)
	}

	if err := svc.RemoveDomain(context.Background(), userID, created.ID, domain.ID); err != nil {
		t.Fatalf("remove domain: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 3 || calls[2] != created.ServerID {
		t.Fatalf("proxy calls = %v, want a third sync for the removal", calls)
	}
}

func TestServiceUpdateApplicationMovedBetweenNodesSyncsBoth(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app = withPrimaryDomain(app, "app.example.com")
	repo := &fakeRepository{app: app}
	if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
		ApplicationID: app.ID, Domain: "app.example.com", IsPrimary: true, Port: 3000, Protocol: ProtocolHTTP,
	}); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	proxySync := &fakeProxySync{}
	svc := newProxyTestService(t, repo, proxySync)

	// A rename does not touch routing.
	name := "renamed app"
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{Name: &name}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 0 {
		t.Fatalf("proxy calls = %v, want none for a rename", calls)
	}

	newServer := uuid.New()
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{ServerID: &newServer}); err != nil {
		t.Fatalf("move: %v", err)
	}
	calls := proxySync.calls()
	if len(calls) != 2 {
		t.Fatalf("proxy calls = %v, want both the old and the new node", calls)
	}
	seen := map[uuid.UUID]bool{calls[0]: true, calls[1]: true}
	if !seen[app.ServerID] || !seen[newServer] {
		t.Fatalf("proxy calls = %v, want %s and %s", calls, app.ServerID, newServer)
	}
}

func TestServiceDeleteApplicationSyncsProxyForDomain(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app = withPrimaryDomain(app, "app.example.com")
	repo := &fakeRepository{app: app}
	if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
		ApplicationID: app.ID, Domain: "app.example.com", IsPrimary: true, Port: 3000, Protocol: ProtocolHTTP,
	}); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	proxySync := &fakeProxySync{}
	svc := newProxyTestService(t, repo, proxySync)

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if calls := proxySync.calls(); len(calls) != 1 || calls[0] != app.ServerID {
		t.Fatalf("proxy calls = %v, want [%s]", calls, app.ServerID)
	}

	plain := testApplication(userID)
	plain.ID = uuid.New()
	plain.Name = "plain"
	repo2 := &fakeRepository{app: plain}
	proxySync2 := &fakeProxySync{}
	svc2 := newProxyTestService(t, repo2, proxySync2)
	if err := svc2.DeleteApplication(context.Background(), userID, plain.ID); err != nil {
		t.Fatalf("delete without domain: %v", err)
	}
	if calls := proxySync2.calls(); len(calls) != 0 {
		t.Fatalf("proxy calls = %v, want none without a domain", calls)
	}
}

func TestAddDomainRejectsInvalidDomain(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newProxyTestService(t, repo, &fakeProxySync{})

	invalid := "app.example.com`) || Host(`evil.example.com"
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, domainInput(invalid)); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{}); err == nil {
		t.Fatal("empty update = nil error, want ErrValidation")
	}
}

func TestOrchestratorSyncsProxyAfterSuccessfulDeployment(t *testing.T) {
	app := testApplication(uuid.New())
	app = withPrimaryDomain(app, "app.example.com")
	repo := &fakeRepository{app: app}
	if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
		ApplicationID: app.ID, Domain: "app.example.com", IsPrimary: true, Port: 3000, Protocol: ProtocolHTTP,
	}); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	proxySync := &fakeProxySync{}
	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
		Proxy:      proxySync,
	})

	o.run(context.Background(), job{app: app, dep: dep})
	if stored, ok := repo.deployment(dep.ID); !ok || stored.State != StateRunning {
		t.Fatalf("deployment state = %v (ok=%t), want running", stored.State, ok)
	}
	if calls := proxySync.calls(); len(calls) != 1 || calls[0] != app.ServerID {
		t.Fatalf("proxy calls = %v, want [%s] after a successful deployment", calls, app.ServerID)
	}
}

func TestOrchestratorDoesNotSyncProxyWithoutDomain(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	proxySync := &fakeProxySync{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(newMockNode()),
		Emitter:    NewEmitter(&recordPublisher{}),
		Proxy:      proxySync,
	})

	o.run(context.Background(), job{app: app, dep: dep})
	if calls := proxySync.calls(); len(calls) != 0 {
		t.Fatalf("proxy calls = %v, want none without a domain", calls)
	}
}
