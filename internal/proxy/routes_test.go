package proxy

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

// fakeProxyService records the route calls and returns canned outcomes.
type fakeProxyService struct {
	syncCalls   []uuid.UUID
	syncErr     error
	allResults  []SyncResult
	allErr      error
	revertCalls []uuid.UUID
	revertErr   error
}

func (f *fakeProxyService) SyncServer(_ context.Context, serverID uuid.UUID) error {
	f.syncCalls = append(f.syncCalls, serverID)
	return f.syncErr
}

func (f *fakeProxyService) SyncAll(context.Context) ([]SyncResult, error) {
	return f.allResults, f.allErr
}

func (f *fakeProxyService) RevertServer(_ context.Context, serverID uuid.UUID) error {
	f.revertCalls = append(f.revertCalls, serverID)
	return f.revertErr
}

// newRoutes returns a router with the proxy routes mounted behind a no-op
// auth middleware.
func newRoutes(svc ProxyService) http.Handler {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, svc, nil, nil)
	return r
}

func postSync(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/proxy/sync", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestSyncRouteServerID(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeProxyService{}
	recorder := postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(svc.syncCalls) != 1 || svc.syncCalls[0] != serverID {
		t.Fatalf("sync calls = %v, want [%s]", svc.syncCalls, serverID)
	}
	var response syncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 1 || !response.Results[0].Synced || response.Results[0].ServerID != serverID.String() {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSyncRouteAllNodes(t *testing.T) {
	okID, badID := uuid.New(), uuid.New()
	svc := &fakeProxyService{allResults: []SyncResult{
		{ServerID: okID},
		{ServerID: badID, Error: "traefik did not answer"},
	}}
	recorder := postSync(t, newRoutes(svc), "")
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s, want 502 on a partial failure", recorder.Code, recorder.Body.String())
	}
	var response syncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 2 || !response.Results[0].Synced || response.Results[1].Synced {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSyncRouteInvalidServerID(t *testing.T) {
	recorder := postSync(t, newRoutes(&fakeProxyService{}), `{"server_id":"not-a-uuid"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSyncRouteInvalidBody(t *testing.T) {
	recorder := postSync(t, newRoutes(&fakeProxyService{}), `{"server_id":`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSyncRouteErrorMapping(t *testing.T) {
	serverID := uuid.New()
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "validation", err: ErrValidation, status: http.StatusBadRequest},
		{name: "not found", err: ErrServerNotFound, status: http.StatusNotFound},
		{name: "agent unavailable", err: ErrAgentUnavailable, status: http.StatusBadGateway},
		{name: "reload", err: ErrReload, status: http.StatusBadGateway},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeProxyService{syncErr: tc.err}
			recorder := postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`"}`)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.status, recorder.Body.String())
			}
		})
	}
}

func TestSyncRouteAllLookupFailure(t *testing.T) {
	svc := &fakeProxyService{allErr: errors.New("db down")}
	recorder := postSync(t, newRoutes(svc), "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestMountRespectsFeatureFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	recorder := postSync(t, newRoutes(&fakeProxyService{}), "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 while %s=false", recorder.Code, FeatureEnv)
	}
}

func TestEnabled(t *testing.T) {
	t.Setenv(FeatureEnv, "")
	if !Enabled() {
		t.Error("Enabled() = false with an unset flag")
	}
	t.Setenv(FeatureEnv, "true")
	if !Enabled() {
		t.Error("Enabled() = false with the flag true")
	}
	t.Setenv(FeatureEnv, "false")
	if Enabled() {
		t.Error("Enabled() = true with the flag false")
	}
}

func TestSyncRouteReportsPartialDiagnostics(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	svc := &fakeProxyService{syncErr: &PartialError{Diagnostics: []Diagnostic{{
		ApplicationID: appID,
		Domain:        "pending.example.com",
		Reason:        "no running deployment yet",
	}}}}
	recorder := postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 with diagnostics", recorder.Code, recorder.Body.String())
	}
	var response syncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 1 || !response.Results[0].Synced {
		t.Fatalf("results = %#v, want one synced node", response.Results)
	}
	diagnostics := response.Results[0].Diagnostics
	if len(diagnostics) != 1 || diagnostics[0].ApplicationID != appID ||
		diagnostics[0].Reason != "no running deployment yet" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestSyncRouteRevert(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeProxyService{}
	recorder := postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`","revert":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(svc.revertCalls) != 1 || svc.revertCalls[0] != serverID {
		t.Fatalf("revert calls = %v, want [%s]", svc.revertCalls, serverID)
	}
	if len(svc.syncCalls) != 0 {
		t.Fatalf("sync calls = %v, want none for a revert", svc.syncCalls)
	}
}

func TestSyncRouteRevertRequiresServerID(t *testing.T) {
	recorder := postSync(t, newRoutes(&fakeProxyService{}), `{"revert":true}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSyncRouteRevertNoVersion(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeProxyService{revertErr: ErrVersionNotFound}
	recorder := postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`","revert":true}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestSyncRouteAllNodesPartialDiagnostics(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	svc := &fakeProxyService{allResults: []SyncResult{{
		ServerID:    serverID,
		Error:       "proxy: partial sync: application x",
		Diagnostics: []Diagnostic{{ApplicationID: appID, Reason: "pending"}},
	}}}
	recorder := postSync(t, newRoutes(svc), "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 for a partial result", recorder.Code, recorder.Body.String())
	}
	var response syncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 1 || len(response.Results[0].Diagnostics) != 1 {
		t.Fatalf("results = %#v, want the per-app diagnostics", response.Results)
	}
}
