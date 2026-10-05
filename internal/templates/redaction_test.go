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

// This file is the BE-7.2 fix-round regression for the secret redaction
// boundary: it renders the real WordPress template, creates a service with the
// rendered document and the returned environment, and drives a failing deploy
// through the real services surface (service + routes). The failing agent
// quotes the secret in raw and dollar-escaped form, exactly like a compose CLI
// error can; the returned error, the HTTP response body, the stored deploy
// history and every captured log line must carry the redaction placeholder
// instead of the secret, and errors.Is must keep matching the services
// sentinel.

// fakeRepository is the minimal services.Repository for the regression: one
// service and its deploy history in memory.
type fakeRepository struct {
	service services.Service
	deploys []services.Deploy
}

// Compile-time guarantee.
var _ services.Repository = (*fakeRepository)(nil)

// CreateService implements services.Repository.
func (f *fakeRepository) CreateService(_ context.Context, service services.Service) (services.Service, error) {
	f.service = service
	return service, nil
}

// GetService implements services.Repository.
func (f *fakeRepository) GetService(_ context.Context, serviceID uuid.UUID) (services.Service, error) {
	if f.service.ID != serviceID {
		return services.Service{}, services.ErrNotFound
	}
	return f.service, nil
}

// ListServices implements services.Repository.
func (f *fakeRepository) ListServices(_ context.Context, _ teams.Scope) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

// UpdateServiceConfig implements services.Repository.
func (f *fakeRepository) UpdateServiceConfig(_ context.Context, service services.Service) (services.Service, error) {
	f.service = service
	return service, nil
}

// UpdateServiceStatus implements services.Repository.
func (f *fakeRepository) UpdateServiceStatus(_ context.Context, _ uuid.UUID, status services.Status) (services.Service, error) {
	f.service.Status = status
	return f.service, nil
}

// SoftDeleteService implements services.Repository.
func (f *fakeRepository) SoftDeleteService(_ context.Context, _ uuid.UUID) (services.Service, error) {
	f.service.Status = services.StatusDeleting
	return f.service, nil
}

// CreateServiceDeploy implements services.Repository.
func (f *fakeRepository) CreateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	f.deploys = append([]services.Deploy{deploy}, f.deploys...)
	return deploy, nil
}

// UpdateServiceDeploy implements services.Repository.
func (f *fakeRepository) UpdateServiceDeploy(_ context.Context, deploy services.Deploy) (services.Deploy, error) {
	for i := range f.deploys {
		if f.deploys[i].ID == deploy.ID {
			f.deploys[i] = deploy
			return deploy, nil
		}
	}
	return services.Deploy{}, errors.New("deploy not found")
}

// ListServiceDeploys implements services.Repository.
func (f *fakeRepository) ListServiceDeploys(_ context.Context, _ uuid.UUID, _ int32) ([]services.Deploy, error) {
	return f.deploys, nil
}

// ServerExists implements services.Repository.
func (f *fakeRepository) ServerExists(_ context.Context, _ uuid.UUID, _ teams.Scope) (bool, error) {
	return true, nil
}

// ListServicesByEnvironment implements services.Repository.
func (f *fakeRepository) ListServicesByEnvironment(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

// ListServicesByProject implements services.Repository.
func (f *fakeRepository) ListServicesByProject(_ context.Context, _ uuid.UUID) ([]services.Service, error) {
	return []services.Service{f.service}, nil
}

// NameInEnvironment implements services.Repository.
func (f *fakeRepository) NameInEnvironment(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (bool, error) {
	return false, nil
}

// ResolveEnvironment implements services.Repository.
func (f *fakeRepository) ResolveEnvironment(_ context.Context, environmentID, _ uuid.UUID) (services.EnvironmentRef, error) {
	return services.EnvironmentRef{ID: environmentID}, nil
}

// ResolveProject implements services.Repository.
func (f *fakeRepository) ResolveProject(_ context.Context, projectID, _ uuid.UUID) (uuid.UUID, error) {
	return projectID, nil
}

// HasActiveDeploy implements services.Repository.
func (f *fakeRepository) HasActiveDeploy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

// HasDeploys implements services.Repository.
func (f *fakeRepository) HasDeploys(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

// failingAgent is a node whose compose up fails while quoting the secret.
type failingAgent struct {
	message string
}

// Compile-time guarantee.
var _ services.ComposeAgent = (*failingAgent)(nil)

// Validate implements services.ComposeAgent.
func (a *failingAgent) Validate(_ context.Context, _ string, _ []byte) ([]string, error) {
	return nil, nil
}

// Up implements services.ComposeAgent.
func (a *failingAgent) Up(_ context.Context, _ string, _ []byte, _ bool) error {
	return fmt.Errorf("%w: up: %s", services.ErrDeployFailed, a.message)
}

// Down implements services.ComposeAgent.
func (a *failingAgent) Down(_ context.Context, _ string, _ []byte) error { return nil }

// Ps implements services.ComposeAgent.
func (a *failingAgent) Ps(_ context.Context, _ string) ([]services.ComposeContainer, error) {
	return nil, nil
}

// Logs implements services.ComposeAgent.
func (a *failingAgent) Logs(_ context.Context, _, _ string, _ int64, _ bool) (services.LogStream, error) {
	return nil, errors.New("not used")
}

// Close implements services.ComposeAgent.
func (a *failingAgent) Close() error { return nil }

// TestRenderDeployRedactionBoundary is the fix-round regression for finding 1.
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
	// The boundary starts at the renderer: the document references the
	// secrets, the values travel in the returned environment. Both the raw
	// and the dollar-escaped form must be absent.
	for _, leaked := range []string{secret, escapedSecret, rootSecret, escapedRoot} {
		if strings.Contains(rendered.ComposeYAML, leaked) {
			t.Fatalf("a secret leaked into the rendered document:\n%s", rendered.ComposeYAML)
		}
	}
	if rendered.Env["db_password"] != secret || rendered.Env["db_root_password"] != rootSecret {
		t.Fatalf("Env = %v, want both secrets", rendered.Env)
	}

	// Capture every log line written while the deploy fails.
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

	// The gallery flow starts with a real create: the rendered document and
	// environment go through services.Create unchanged.
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

	// The programmatic deploy: the returned error is redacted and keeps the
	// services sentinel in its chain.
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

	// The stored deploy history keeps the redacted message, never the secret.
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

	// The HTTP surface (what the gallery calls) answers with the redacted
	// message only.
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

	// No log line written during the failing flow may carry the secret.
	if strings.Contains(logged.String(), secret) || strings.Contains(logged.String(), escapedSecret) {
		t.Fatalf("a log line leaked the secret:\n%s", logged.String())
	}
}
