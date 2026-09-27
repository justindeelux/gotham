package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeDeployService is a scriptable DeployService for route tests.
type fakeDeployService struct {
	deploy      Deployment
	deployErr   error
	list        []Deployment
	listErr     error
	rollback    Deployment
	rollbackErr error

	seenUser        uuid.UUID
	seenApplication uuid.UUID
	seenRollback    uuid.UUID
	seenRollbackSet bool
}

// Compile-time guarantee that fakeDeployService satisfies the route seam.
var _ DeployService = (*fakeDeployService)(nil)

// Deploy implements DeployService.
func (f *fakeDeployService) Deploy(_ context.Context, userID, appID uuid.UUID) (Deployment, error) {
	f.seenUser, f.seenApplication = userID, appID
	return f.deploy, f.deployErr
}

// ListDeployments implements DeployService.
func (f *fakeDeployService) ListDeployments(_ context.Context, userID, appID uuid.UUID) ([]Deployment, error) {
	f.seenUser, f.seenApplication = userID, appID
	return f.list, f.listErr
}

// Rollback implements DeployService.
func (f *fakeDeployService) Rollback(_ context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error) {
	f.seenUser, f.seenApplication = userID, appID
	f.seenRollback, f.seenRollbackSet = deploymentID, true
	return f.rollback, f.rollbackErr
}

// newRouteServer mounts the deploy routes with a no-op auth middleware.
func newRouteServer(svc DeployService, userID UserIDFunc) http.Handler {
	r := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	Mount(r, auth, userID, svc)
	return r
}

// alwaysUser returns a UserIDFunc for one user.
func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

// deploymentPath renders one of the application deployment routes.
func deploymentPath(appID uuid.UUID, action string) string {
	path := "/v1/applications/" + appID.String()
	if action == "" {
		return path + "/deployments"
	}
	return path + "/" + action
}

func TestRoutesDeployAccepted(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	deploymentID := uuid.New()
	svc := &fakeDeployService{deploy: Deployment{
		ID:            deploymentID,
		ApplicationID: appID,
		Kind:          KindDeploy,
		State:         StateQueued,
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(appID, "deploy"), nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	var body deploymentEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Deployment.ID != deploymentID.String() {
		t.Errorf("id = %q, want %q", body.Deployment.ID, deploymentID)
	}
	if body.Deployment.State != StateQueued {
		t.Errorf("state = %s, want queued", body.Deployment.State)
	}
	if svc.seenUser != userID || svc.seenApplication != appID {
		t.Errorf("service saw user %s / app %s, want %s / %s",
			svc.seenUser, svc.seenApplication, userID, appID)
	}
}

func TestRoutesListDeployments(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{list: []Deployment{
		{ID: uuid.New(), ApplicationID: appID, Kind: KindDeploy, State: StateRunning, ImageTag: "gotham/app:2"},
		{ID: uuid.New(), ApplicationID: appID, Kind: KindRollback, State: StateFailed, Error: "boom"},
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, deploymentPath(appID, ""), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body deploymentListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Deployments) != 2 {
		t.Fatalf("deployments = %d, want 2", len(body.Deployments))
	}
	if body.Deployments[0].State != StateRunning || body.Deployments[1].State != StateFailed {
		t.Errorf("states = %s / %s, want running / failed",
			body.Deployments[0].State, body.Deployments[1].State)
	}
}

func TestRoutesRollback(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	targetID := uuid.New()
	svc := &fakeDeployService{rollback: Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          KindRollback,
		State:         StateQueued,
		ImageTag:      "gotham/app:1",
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	t.Run("explicit deployment id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := strings.NewReader(`{"deployment_id":"` + targetID.String() + `"}`)
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(appID, "rollback"), body))

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
		}
		if !svc.seenRollbackSet || svc.seenRollback != targetID {
			t.Errorf("service saw target %s (set=%v), want %s",
				svc.seenRollback, svc.seenRollbackSet, targetID)
		}
	})

	t.Run("empty body picks the previous release", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(appID, "rollback"), nil))

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
		}
		if svc.seenRollback != uuid.Nil {
			t.Errorf("service saw target %s, want the zero UUID (automatic pick)", svc.seenRollback)
		}
	})

	t.Run("invalid deployment id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := strings.NewReader(`{"deployment_id":"not-a-uuid"}`)
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(appID, "rollback"), body))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("unknown body field", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := strings.NewReader(`{"nope":1}`)
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(appID, "rollback"), body))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

func TestRoutesInvalidApplicationID(t *testing.T) {
	svc := &fakeDeployService{}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	for _, action := range []string{"deploy", "deployments", "rollback"} {
		method := http.MethodPost
		if action == "deployments" {
			method = http.MethodGet
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, "/v1/applications/not-a-uuid/"+action, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", action, rec.Code)
		}
	}
}

func TestRoutesUnauthorized(t *testing.T) {
	svc := &fakeDeployService{}
	srv := newRouteServer(svc, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false })
	appID := uuid.New()

	for _, tc := range []struct {
		method string
		action string
	}{
		{http.MethodPost, "deploy"},
		{http.MethodGet, "deployments"},
		{http.MethodPost, "rollback"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, deploymentPath(appID, tc.action), nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", tc.action, rec.Code)
		}
	}
}

func TestRoutesServiceErrors(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	boom := errors.New("boom")
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"server not found", ErrServerNotFound, http.StatusNotFound},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"conflict", ErrConflict, http.StatusConflict},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"agent unavailable", ErrAgentUnavailable, http.StatusBadGateway},
		{"internal", boom, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeDeployService{listErr: tc.err, deployErr: tc.err, rollbackErr: tc.err}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, deploymentPath(appID, ""), nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if errors.Is(tc.err, boom) && strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("response leaked an internal error: %s", rec.Body.String())
			}
		})
	}
}

func TestMountNilService(t *testing.T) {
	srv := newRouteServer(nil, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(uuid.New(), "deploy"), nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (routes not mounted)", rec.Code)
	}
}

func TestMountDisabledByFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	svc := &fakeDeployService{}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deploymentPath(uuid.New(), "deploy"), nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (routes not mounted)", rec.Code)
	}
}
