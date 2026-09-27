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

	// applications CRUD and configuration.
	application Application
	createErr   error
	listApps    []Application
	listAppsErr error
	getErr      error
	updateErr   error
	deleteErr   error
	env         []EnvEntry
	envErr      error
	storages    []Storage
	storagesErr error
	stop        Deployment
	stopErr     error
	start       Deployment
	startErr    error

	// deploy keys.
	deployKey       DeployKey
	createKeyErr    error
	deleteKeyErr    error
	deletedKey      bool
	deletedKeyCalls int
	createdKeyFor   uuid.UUID

	seenUser        uuid.UUID
	seenApplication uuid.UUID
	seenRollback    uuid.UUID
	seenRollbackSet bool
	seenCreate      CreateApplicationInput
	seenUpdate      UpdateApplicationInput
	seenEntries     []EnvEntry
	seenStorages    []Storage
}

// Compile-time guarantee that fakeDeployService satisfies the route seam.
var _ DeployService = (*fakeDeployService)(nil)

// CreateApplication implements DeployService.
func (f *fakeDeployService) CreateApplication(_ context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error) {
	f.seenUser, f.seenCreate = userID, in
	if f.createErr != nil {
		return Application{}, f.createErr
	}
	return f.application, nil
}

// ListApplications implements DeployService.
func (f *fakeDeployService) ListApplications(_ context.Context, userID uuid.UUID) ([]Application, error) {
	f.seenUser = userID
	if f.listAppsErr != nil {
		return nil, f.listAppsErr
	}
	return f.listApps, nil
}

// GetApplication implements DeployService.
func (f *fakeDeployService) GetApplication(_ context.Context, userID, appID uuid.UUID) (Application, error) {
	f.seenUser, f.seenApplication = userID, appID
	if f.getErr != nil {
		return Application{}, f.getErr
	}
	return f.application, nil
}

// UpdateApplication implements DeployService.
func (f *fakeDeployService) UpdateApplication(_ context.Context, userID, appID uuid.UUID, in UpdateApplicationInput) (Application, error) {
	f.seenUser, f.seenApplication, f.seenUpdate = userID, appID, in
	if f.updateErr != nil {
		return Application{}, f.updateErr
	}
	return f.application, nil
}

// DeleteApplication implements DeployService.
func (f *fakeDeployService) DeleteApplication(_ context.Context, userID, appID uuid.UUID) error {
	f.seenUser, f.seenApplication = userID, appID
	return f.deleteErr
}

// CreateDeployKey implements DeployService.
func (f *fakeDeployService) CreateDeployKey(_ context.Context, userID, appID uuid.UUID) (DeployKey, error) {
	f.seenUser, f.createdKeyFor = userID, appID
	if f.createKeyErr != nil {
		return DeployKey{}, f.createKeyErr
	}
	return f.deployKey, nil
}

// DeleteDeployKey implements DeployService.
func (f *fakeDeployService) DeleteDeployKey(_ context.Context, userID, appID uuid.UUID) (bool, error) {
	f.seenUser, f.seenApplication = userID, appID
	f.deletedKeyCalls++
	if f.deleteKeyErr != nil {
		return false, f.deleteKeyErr
	}
	return f.deletedKey, nil
}

// GetEnv implements DeployService.
func (f *fakeDeployService) GetEnv(_ context.Context, userID, appID uuid.UUID) ([]EnvEntry, error) {
	f.seenUser, f.seenApplication = userID, appID
	if f.envErr != nil {
		return nil, f.envErr
	}
	return f.env, nil
}

// ReplaceEnv implements DeployService.
func (f *fakeDeployService) ReplaceEnv(_ context.Context, userID, appID uuid.UUID, entries []EnvEntry) ([]EnvEntry, error) {
	f.seenUser, f.seenApplication, f.seenEntries = userID, appID, entries
	if f.envErr != nil {
		return nil, f.envErr
	}
	if f.env != nil {
		return f.env, nil
	}
	return entries, nil
}

// GetStorages implements DeployService.
func (f *fakeDeployService) GetStorages(_ context.Context, userID, appID uuid.UUID) ([]Storage, error) {
	f.seenUser, f.seenApplication = userID, appID
	if f.storagesErr != nil {
		return nil, f.storagesErr
	}
	return f.storages, nil
}

// ReplaceStorages implements DeployService.
func (f *fakeDeployService) ReplaceStorages(_ context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error) {
	f.seenUser, f.seenApplication, f.seenStorages = userID, appID, storages
	if f.storagesErr != nil {
		return nil, f.storagesErr
	}
	if f.storages != nil {
		return f.storages, nil
	}
	return storages, nil
}

// Stop implements DeployService.
func (f *fakeDeployService) Stop(_ context.Context, userID, appID uuid.UUID) (Deployment, error) {
	f.seenUser, f.seenApplication = userID, appID
	return f.stop, f.stopErr
}

// Start implements DeployService.
func (f *fakeDeployService) Start(_ context.Context, userID, appID uuid.UUID) (Deployment, error) {
	f.seenUser, f.seenApplication = userID, appID
	return f.start, f.startErr
}

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
