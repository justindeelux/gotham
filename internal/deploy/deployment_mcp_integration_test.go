package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	adapter "github.com/justindeelux/gotham/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPDeployReplayRestartAndExactHistory(t *testing.T) {
	st, user, env, node := replayDeployFixture(t)
	ctx := context.Background()
	repo := newStoreRepository(st, testSecretKey)
	app, err := repo.CreateApplication(ctx, Application{ID: uuid.New(), UserID: user, EnvironmentID: env, ServerID: node, Name: uuid.NewString(), SourceType: SourceImage, ImageRef: "busybox:latest"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{Store: st, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })
	svc.startOnce.Do(func() {})
	svc.queue = make(chan job)
	svc.baseCancel()
	router := chi.NewRouter()
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer gt1_test" || r.Header.Get("X-Gotham-MCP-Contract") != "1" {
				t.Error("adapter omitted strict credentials")
			}
			w.Header().Set("X-Gotham-MCP-Contract", "1")
			next.ServeHTTP(w, r)
		})
	}
	router.Route("/api", func(api chi.Router) {
		api.With(auth).Get("/v1/mcp/context", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"capabilities":["context","strict_reads","strict_mutations","request_replay"]}`)
		})
		api.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			routes := chi.NewRouter()
			Mount(routes, auth, func(context.Context) (uuid.UUID, bool) { return user, true }, svc)
			routes.ServeHTTP(w, r)
		}))
	})
	var dropResponse atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !dropResponse.Swap(false) {
			router.ServeHTTP(w, r)
			return
		}
		accepted := httptest.NewRecorder()
		router.ServeHTTP(accepted, r)
		if accepted.Code != http.StatusAccepted {
			t.Errorf("lost response was not accepted: %d %s", accepted.Code, accepted.Body.String())
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(api.Close)
	server, err := adapter.NewServer(ctx, adapter.Config{URL: api.URL, Token: "gt1_test"}, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	args := map[string]any{"application_id": app.ID.String(), "request_id": "stable-mcp"}
	accepted := call("application_deploy", args)
	receipt := accepted["receipt"].(map[string]any)
	id, err := uuid.Parse(receipt["resource_id"].(string))
	if err != nil || accepted["accepted"] != true || accepted["deployment"] != nil || receipt["outcome"] != "accepted_not_confirmed" {
		t.Fatalf("acceptance=%+v err=%v", accepted, err)
	}
	dropResponse.Store(true)
	lost, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "application_deploy", Arguments: args})
	if err != nil || !lost.IsError {
		t.Fatalf("accepted response loss not reported: %+v %v", lost, err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE applications SET image_ref='' WHERE id=$1", app.ID); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()
	svc = NewService(Config{Store: st, Secret: testSecretKey, Logger: discardLogger()})
	replay := call("application_deploy", args)
	replayReceipt := replay["receipt"].(map[string]any)
	release := replay["deployment"].(map[string]any)
	if replayReceipt["resource_id"] != id.String() || replayReceipt["replayed"] != true || release["state"] != "failed" || release["terminal"] != true || len(svc.queue) != 0 {
		t.Fatalf("restart replay=%+v", replay)
	}
	for i := 0; i < 101; i++ {
		if _, err := repo.CreateDeployment(ctx, Deployment{ApplicationID: app.ID, Kind: KindDeploy, State: StateRunning}); err != nil {
			t.Fatal(err)
		}
	}
	listed := call("deployments_list", map[string]any{"application_id": app.ID.String(), "limit": 100})
	if len(listed["deployments"].([]any)) != 100 {
		t.Fatal("history window not bounded")
	}
	status := call("deployment_status", map[string]any{"application_id": app.ID.String(), "deployment_id": id.String()})
	if status["id"] != id.String() || status["state"] != "failed" || status["terminal"] != true {
		t.Fatalf("exact old ID=%+v", status)
	}
	var count int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM deployments WHERE application_id=$1", app.ID).Scan(&count); err != nil || count != 102 {
		t.Fatalf("re-enqueued replay: count=%d err=%v", count, err)
	}
	otherApp, err := repo.CreateApplication(ctx, Application{ID: uuid.New(), UserID: user, EnvironmentID: env, ServerID: node, Name: uuid.NewString(), SourceType: SourceImage, ImageRef: "busybox:latest"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mismatched, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "deployment_status", Arguments: map[string]any{"application_id": otherApp.ID.String(), "deployment_id": id.String()}})
	if err != nil || !mismatched.IsError {
		t.Fatalf("mismatched app/release visible: %+v %v", mismatched, err)
	}
}
