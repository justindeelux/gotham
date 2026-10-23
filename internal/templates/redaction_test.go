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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/templates"
)

type fakeRepository struct {
	service services.Service
	deploys []services.Deploy
}

var _ services.Repository = (*fakeRepository)(nil)

func (f *fakeRepository) CreateService(_ context.Context, service services.Service) (services.Service, error) {
	f.service = service
	return service, nil
}

func (f *fakeRepository) GetService(_ context.Context, serviceID uuid.UUID) (services.Service, error) {
	if f.service.ID != serviceID {
		return services.Service{}, services.ErrNotFound
	}
	return f.service, nil
}

func (f *fakeRepository) ListServices(_ context.Context, _ teams.Scope) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) UpdateServiceConfig(_ context.Context, service services.Service) (services.Service, error) {
	f.service = service
	return service, nil
}

func (f *fakeRepository) UpdateServiceStatus(_ context.Context, _ uuid.UUID, status services.Status) (services.Service, error) {
	f.service.Status = status
	return f.service, nil
}

func (f *fakeRepository) SoftDeleteService(_ context.Context, _ uuid.UUID) (services.Service, error) {
	f.service.Status = services.StatusDeleting
	return f.service, nil
}

func (f *fakeRepository) CreateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	f.deploys = append([]services.Deploy{deploy}, f.deploys...)
	return deploy, nil
}

func (f *fakeRepository) UpdateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	for i := range f.deploys {
		if f.deploys[i].ID == deploy.ID {
			f.deploys[i] = deploy
			return deploy, nil
		}
	}
	return services.Deploy{}, errors.New("deploy not found")
}

func (f *fakeRepository) ListServiceDeploys(_ context.Context, _ uuid.UUID, _ int32) ([]services.Deploy, error) {
	return f.deploys, nil
}

func (f *fakeRepository) ServerExists(_ context.Context, _ uuid.UUID, _ teams.Scope) (bool, error) {
	return true, nil
}

func (f *fakeRepository) ListServicesByEnvironment(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) ListServicesByProject(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

func (f *fakeRepository) NameInEnvironment(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeRepository) ResolveEnvironment(_ context.Context, environmentID, _ uuid.UUID) (services.EnvironmentRef, error) {
	return services.EnvironmentRef{ID: environmentID}, nil
}

func (f *fakeRepository) ResolveProject(_ context.Context, projectID, _ uuid.UUID) (uuid.UUID, error) {
	return projectID, nil
}

func (f *fakeRepository) HasActiveDeploy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeRepository) HasDeploys(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
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

	_, _, deployErr := svc.Deploy(ctx, userID, created.ID)
	if deployErr == nil {
		t.Fatal("Deploy succeeded, want the agent failure")
	}
	if !errors.Is(deployErr, services.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want ErrDeployFailed in the chain", deployErr)
	}
	if strings.Contains(deployErr.Error(), secret) || strings.Contains(deployErr.Error(), escapedSecret) {
		t.Fatalf("the returned error leaked the secret: %v", deployErr)
	}
	if !strings.Contains(deployErr.Error(), "<redacted>") {
		t.Fatalf("the returned error is not redacted: %v", deployErr)
	}

	deploys, err := svc.Deploys(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
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
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("deploy status = %d, want 502: %s", recorder.Code, recorder.Body)
	}
	body := recorder.Body.String()
	if strings.Contains(body, secret) || strings.Contains(body, escapedSecret) {
		t.Fatalf("the HTTP response leaked the secret: %s", body)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode the error body: %v", err)
	}
	if !strings.Contains(payload.Message, "<redacted>") {
		t.Fatalf("the HTTP response is not redacted: %q", payload.Message)
	}

	if strings.Contains(logged.String(), secret) || strings.Contains(logged.String(), escapedSecret) {
		t.Fatalf("a log line leaked the secret:\n%s", logged.String())
	}
}
