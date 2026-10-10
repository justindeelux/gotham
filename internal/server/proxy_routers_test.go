package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// fakeRouterService is a canned proxy.RouterService.
type fakeRouterService struct {
	response proxy.RouterListResponse
	err      error
	calls    []uuid.UUID
}

func (f *fakeRouterService) ListRouters(_ context.Context, serverID uuid.UUID) (proxy.RouterListResponse, error) {
	f.calls = append(f.calls, serverID)
	return f.response, f.err
}

func newRouterRoutes(svc proxy.RouterService) http.Handler {
	r := chi.NewRouter()
	MountProxyRouters(r, func(next http.Handler) http.Handler { return next }, svc)
	return r
}

// TestProxyRoutersRouteListsGeneratedRouters proves the read endpoint serves
// the service rows verbatim, with empty (never null) lists.
func TestProxyRoutersRouteListsGeneratedRouters(t *testing.T) {
	serverID, appID := uuid.New(), uuid.New()
	svc := &fakeRouterService{response: proxy.RouterListResponse{
		Routers: []proxy.RouterInfo{{
			Host: "app.example.com", Rule: "Host(`app.example.com`)",
			Service: "app-" + appID.String(), EntryPoints: []string{"web", "websecure"},
			TLSResolver: "letsencrypt", Kind: proxy.RouterKindApplication,
			OwnerID: appID, OwnerName: "shop", ServerID: serverID, ServerName: "node-1",
		}},
		Nodes: []proxy.RouterNodeState{{ServerID: serverID, ServerName: "node-1", SyncStatus: proxy.RouterSyncSynced}},
	}}
	request := httptest.NewRequest(http.MethodGet, "/v1/proxy/routers?server_id="+serverID.String(), nil)
	recorder := httptest.NewRecorder()
	newRouterRoutes(svc).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response proxy.RouterListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Routers) != 1 || response.Routers[0].Host != "app.example.com" ||
		response.Routers[0].TLSResolver != "letsencrypt" || response.Routers[0].OwnerName != "shop" {
		t.Fatalf("routers = %#v", response.Routers)
	}
	if len(response.Nodes) != 1 || response.Nodes[0].SyncStatus != proxy.RouterSyncSynced {
		t.Fatalf("nodes = %#v", response.Nodes)
	}
	if len(svc.calls) != 1 || svc.calls[0] != serverID {
		t.Fatalf("calls = %v, want [%s]", svc.calls, serverID)
	}
}

// TestProxyRoutersRouteErrorMapping proves an unreachable node answers 502
// (never fabricated rows) and a bad server_id answers 400.
func TestProxyRoutersRouteErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "agent unavailable", err: proxy.ErrAgentUnavailable, status: http.StatusBadGateway},
		{name: "server not found", err: proxy.ErrServerNotFound, status: http.StatusNotFound},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeRouterService{err: tc.err}
			request := httptest.NewRequest(http.MethodGet, "/v1/proxy/routers?server_id="+uuid.NewString(), nil)
			recorder := httptest.NewRecorder()
			newRouterRoutes(svc).ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.status, recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/proxy/routers?server_id=not-a-uuid", nil)
	recorder := httptest.NewRecorder()
	newRouterRoutes(&fakeRouterService{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// TestProxyRoutersRouteNilService proves a nil service mounts nothing.
func TestProxyRoutersRouteNilService(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/proxy/routers", nil)
	recorder := httptest.NewRecorder()
	newRouterRoutes(nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
