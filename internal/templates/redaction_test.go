package templates_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/templates"
)

type fakeRepository struct {
	mu      sync.Mutex
	service services.Service
	deploys []services.Deploy
}

var _ services.Repository = (*fakeRepository)(nil)

func (f *fakeRepository) CreateService(_ context.Context, service services.Service) (services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.service = service
	return service, nil
}

func (f *fakeRepository) GetService(_ context.Context, serviceID uuid.UUID) (services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.service.ID != serviceID {
		return services.Service{}, services.ErrNotFound
	}
	return f.service, nil
}

func (f *fakeRepository) ListServices(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) UpdateServiceConfig(_ context.Context, service services.Service) (services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.service = service
	return service, nil
}

func (f *fakeRepository) UpdateServiceStatus(_ context.Context, _ uuid.UUID, status services.Status) (services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.service.Status = status
	return f.service, nil
}

func (f *fakeRepository) SoftDeleteService(_ context.Context, _ uuid.UUID) (services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.service.Status = services.StatusDeleting
	return f.service, nil
}

func (f *fakeRepository) CreateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deploys = append([]services.Deploy{deploy}, f.deploys...)
	return deploy, nil
}

func (f *fakeRepository) UpdateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deploys {
		if f.deploys[i].ID == deploy.ID {
			f.deploys[i] = deploy
			return deploy, nil
		}
	}
	return services.Deploy{}, errors.New("deploy not found")
}

func (f *fakeRepository) ListServiceDeploys(_ context.Context, _ uuid.UUID, _ int32) ([]services.Deploy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.Deploy{}, f.deploys...), nil
}

func (f *fakeRepository) ListServiceDeploysByState(_ context.Context, _ services.DeployState) ([]services.Deploy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.Deploy{}, f.deploys...), nil
}

func (f *fakeRepository) ServerExists(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (f *fakeRepository) ListServicesByEnvironment(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) ListServicesByProject(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) NameInEnvironment(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeRepository) ResolveEnvironment(_ context.Context, environmentID uuid.UUID) (services.EnvironmentRef, error) {
	return services.EnvironmentRef{ID: environmentID}, nil
}

func (f *fakeRepository) ResolveProject(_ context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	return projectID, nil
}

func (f *fakeRepository) HasActiveDeploy(_ context.Context, _ uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, deploy := range f.deploys {
		if deploy.State == services.DeployDeploying {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepository) HasDeploys(_ context.Context, _ uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deploys) > 0, nil
}

func (f *fakeRepository) ListRoutableServices(_ context.Context) ([]services.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) ListServiceDomains(_ context.Context, _ uuid.UUID) ([]services.ServiceDomain, error) {
	return nil, nil
}

func (f *fakeRepository) CreateServiceDomain(_ context.Context, domain services.ServiceDomain) (services.ServiceDomain, error) {
	return domain, nil
}

func (f *fakeRepository) UpdateServiceDomain(_ context.Context, domain services.ServiceDomain) (services.ServiceDomain, error) {
	return domain, nil
}

func (f *fakeRepository) DeleteServiceDomain(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (f *fakeRepository) DeleteServiceDomains(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (f *fakeRepository) ServiceDomainClaim(_ context.Context, _ string) (services.ServiceDomain, error) {
	return services.ServiceDomain{}, services.ErrNotFound
}

func (f *fakeRepository) ApplicationDomainClaim(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeRepository) ListRedirectSources(_ context.Context) ([]string, error) {
	return nil, nil
}

type failingAgent struct {
	message string
}

var _ services.ComposeAgent = (*failingAgent)(nil)

func (a *failingAgent) Validate(_ context.Context, _ string, _ []byte) ([]string, error) {
	return nil, nil
}

func (a *failingAgent) Up(_ context.Context, _ string, _ []byte, _ bool) error {
	return fmt.Errorf("%w: up: %s", services.ErrDeployFailed, a.message)
}

func (a *failingAgent) Down(_ context.Context, _ string, _ []byte) error { return nil }

func (a *failingAgent) Ps(_ context.Context, _ string) ([]services.ComposeContainer, error) {
	return nil, nil
}

func (a *failingAgent) Logs(_ context.Context, _, _ string, _ int64, _ bool) (services.LogStream, error) {
	return nil, errors.New("not used")
}

func (a *failingAgent) Close() error { return nil }

func TestRenderDeployRedactionBoundary(t *testing.T) {
	t.Setenv(services.FeatureEnv, "")

	catalog := templates.NewDefaultService(nil)
	if catalog == nil {
		t.Fatal("the built-in template catalog failed to load")
	}
	secret := "review-inline-$secret$$x"
	rootSecret := "root-" + secret
	escapedSecret := strings.ReplaceAll(secret, "$", "$$")
	escapedRoot := strings.ReplaceAll(rootSecret, "$", "$$")
	rendered, err := catalog.Render("wordpress", map[string]any{
		"domain":           "redaction.example.test",
		"db_password":      secret,
		"db_root_password": rootSecret,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, leaked := range []string{secret, escapedSecret, rootSecret, escapedRoot} {
		if strings.Contains(rendered.ComposeYAML, leaked) {
			t.Fatalf("a secret leaked into the rendered document:\n%s", rendered.ComposeYAML)
		}
	}
	if rendered.Env["db_password"] != secret || rendered.Env["db_root_password"] != rootSecret {
		t.Fatalf("Env = %v, want both secrets", rendered.Env)
	}

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	userID := uuid.New()
	repo := &fakeRepository{}
	agent := &failingAgent{message: fmt.Sprintf(
		"compose exited: invalid db_password %s (escaped %s)", secret, escapedSecret)}
	svc := services.NewService(services.Config{
		Repository: repo,
		Logger:     slog.Default(),
		Dial: func(context.Context, uuid.UUID) (services.ComposeAgent, error) {
			return agent, nil
		},
	})

	ctx := context.Background()
	created, err := svc.Create(ctx, userID, services.CreateRequest{
		Name:          "wordpress",
		EnvironmentID: uuid.New(),
		ServerID:      uuid.New(),
		ComposeYAML:   rendered.ComposeYAML,
		Env:           rendered.Env,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Env["db_password"] != secret || created.Env["db_root_password"] != rootSecret {
		t.Fatalf("created Env = %v, want both secrets", created.Env)
	}

	t.Cleanup(func() {
		if closer, ok := svc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	if _, _, deployErr := svc.Deploy(ctx, userID, created.ID); deployErr != nil {
		t.Fatalf("Deploy must accept the attempt and fail in the background: %v", deployErr)
	}
	var deploys []services.Deploy
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		deploys, err = svc.Deploys(ctx, userID, created.ID)
		if err != nil {
			t.Fatalf("Deploys: %v", err)
		}
		if len(deploys) > 0 && deploys[0].State != services.DeployDeploying {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(deploys) != 1 || deploys[0].State != services.DeployFailed {
		t.Fatalf("deploys = %+v", deploys)
	}
	if strings.Contains(deploys[0].Error, secret) || strings.Contains(deploys[0].Error, escapedSecret) {
		t.Fatalf("the stored deploy error leaked the secret: %q", deploys[0].Error)
	}
	if !strings.Contains(deploys[0].Error, "<redacted>") {
		t.Fatalf("the stored deploy error is not redacted: %q", deploys[0].Error)
	}

	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	userIDFunc := func(context.Context) (uuid.UUID, bool) { return userID, true }
	services.Mount(router, auth, userIDFunc, svc)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/v1/services/"+created.ID.String()+"/deploy", nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("deploy status = %d, want 202: %s", recorder.Code, recorder.Body)
	}
	var payload struct {
		Deploy struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"deploy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode the deploy response: %v", err)
	}
	if payload.Deploy.State != string(services.DeployDeploying) || payload.Deploy.Error != "" {
		t.Fatalf("deploy payload = %+v, want a pending deploy without an error", payload.Deploy)
	}
	if strings.Contains(payload.Deploy.Error, secret) || strings.Contains(payload.Deploy.Error, escapedSecret) {
		t.Fatalf("the deploy error leaked the secret: %q", payload.Deploy.Error)
	}

	if strings.Contains(logged.String(), secret) || strings.Contains(logged.String(), escapedSecret) {
		t.Fatalf("a log line leaked the secret:\n%s", logged.String())
	}
}
