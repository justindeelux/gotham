package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func deploymentSession(t *testing.T, handler http.HandlerFunc) *sdk.ClientSession {
	t.Helper()
	cs, _ := deploymentSessionWithClient(t, handler)
	return cs
}

func deploymentSessionWithClient(t *testing.T, handler http.HandlerFunc) (*sdk.ClientSession, *Client) {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gt1_test-secret" {
			t.Error("fixed credentials missing")
		}
		acknowledge(w, r)
		handler(w, r)
	}))
	t.Cleanup(api.Close)
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	registerDeploymentTools(server, client, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
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
	return cs, client
}

func TestDeploymentLifecycleAndProjection(t *testing.T) {
	app, dep := uuid.NewString(), uuid.NewString()
	state := "queued"
	calls := 0
	stamp := "2026-10-22T03:04:05Z"
	cs := deploymentSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/applications/"+app+"/deployments/"+dep {
			t.Error("not an exact bounded read")
		}
		_, _ = fmt.Fprintf(w, `{"deployment":{"id":%q,"application_id":%q,"kind":"deploy","state":%q,"created_at":%q,"updated_at":%q,"started_at":%q,"finished_at":%q,"terminal":true,"error":"secret-sentinel","build_log":"secret-sentinel","compose_document":"secret-sentinel"}}`, dep, app, state, stamp, stamp, stamp, stamp)
	})
	for _, s := range []string{"queued", "cloning", "building", "pushing", "starting", "running", "failed", "unknown"} {
		state = s
		out := callDiscovery(t, cs, "deployment_status", map[string]any{"application_id": app, "deployment_id": dep})
		if out.IsError {
			t.Fatalf("status=%+v", out)
		}
		raw := resultJSON(t, out)
		var got deploymentMetadata
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		if got.State != s || got.Terminal != (s == "running" || s == "failed") || got.ID != dep || got.ApplicationID != app || got.StartedAt == nil || got.FinishedAt == nil || got.CreatedAt.Format(time.RFC3339) != stamp || strings.Contains(raw, "secret-sentinel") {
			t.Fatalf("status=%s", raw)
		}
	}
	if calls != 8 {
		t.Fatalf("implicit polling: %d", calls)
	}
}

func TestDeploymentListBoundAndErrors(t *testing.T) {
	app := uuid.NewString()
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	cs := deploymentSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/applications/"+app+"/deployments" || r.URL.Query().Get("limit") != "2" {
			t.Errorf("unbounded list: %s", r.URL)
		}
		_, _ = fmt.Fprintf(w, `{"deployments":[{"id":%q,"application_id":%q,"state":"running"},{"id":%q,"application_id":%q,"state":"building"},{"id":%q,"application_id":%q,"state":"failed"}]}`, ids[0], app, ids[1], app, ids[2], app)
	})
	out := callDiscovery(t, cs, "deployments_list", map[string]any{"application_id": app, "limit": 2})
	var got deploymentsListResult
	if err := json.Unmarshal([]byte(resultJSON(t, out)), &got); err != nil {
		t.Fatal(err)
	}
	if out.IsError || len(got.Deployments) != 2 || got.WindowLimit != 2 || got.Coverage != "newest_window_not_complete_history" || !got.Deployments[0].Terminal || got.Deployments[1].Terminal {
		t.Fatalf("list=%+v", got)
	}
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			cs := deploymentSession(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "secret-sentinel")
			})
			out := callDiscovery(t, cs, "deployment_status", map[string]any{"application_id": app, "deployment_id": uuid.NewString()})
			if !out.IsError || strings.Contains(resultJSON(t, out), "secret-sentinel") || calls != 1 {
				t.Fatalf("unsafe error=%+v calls=%d", out, calls)
			}
		})
	}
}

func TestDeploymentToolsRejectInvalidInputs(t *testing.T) {
	cs := deploymentSession(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached API") })
	app := uuid.NewString()
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"deployment_status", map[string]any{"application_id": app, "deployment_id": "../foreign"}},
		{"deployment_status", map[string]any{"application_id": app, "deployment_id": uuid.Nil.String()}},
		{"deployment_status", map[string]any{"application_id": app, "deployment_id": uuid.NewString(), "owner_id": uuid.NewString()}},
		{"deployments_list", map[string]any{"application_id": app, "limit": 101}},
		{"deployments_list", map[string]any{"application_id": app, "limit": -1}},
	} {
		result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err == nil && !result.IsError {
			t.Fatalf("accepted invalid args=%+v", tc)
		}
	}
}

func TestDeploymentReadsRejectMismatchedIDs(t *testing.T) {
	app, dep := uuid.NewString(), uuid.NewString()
	for _, tc := range []struct{ name, tool, path, body string }{
		{"status wrong deployment", "deployment_status", "/api/v1/applications/" + app + "/deployments/" + dep,
			fmt.Sprintf(`{"deployment":{"id":%q,"application_id":%q,"state":"running"}}`, uuid.NewString(), app)},
		{"status wrong application", "deployment_status", "/api/v1/applications/" + app + "/deployments/" + dep,
			fmt.Sprintf(`{"deployment":{"id":%q,"application_id":%q,"state":"running"}}`, dep, uuid.NewString())},
		{"list wrong application", "deployments_list", "/api/v1/applications/" + app + "/deployments",
			fmt.Sprintf(`{"deployments":[{"id":%q,"application_id":%q,"state":"running"}]}`, uuid.NewString(), uuid.NewString())},
		{"list invalid id", "deployments_list", "/api/v1/applications/" + app + "/deployments",
			fmt.Sprintf(`{"deployments":[{"id":"not-a-uuid","application_id":%q,"state":"running"}]}`, app)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := deploymentSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("unexpected read %s", r.URL)
				}
				_, _ = fmt.Fprint(w, tc.body)
			})
			args := map[string]any{"application_id": app, "deployment_id": dep}
			if tc.tool == "deployments_list" {
				args = map[string]any{"application_id": app}
			}
			out := callDiscovery(t, cs, tc.tool, args)
			if !out.IsError || len(out.Content) != 1 {
				t.Fatalf("mismatched response accepted: %+v", out)
			}
			if text, ok := out.Content[0].(*sdk.TextContent); !ok || text.Text != "invalid_response" {
				t.Fatalf("want sanitized invalid_response, got %+v", out.Content)
			}
		})
	}
}

func TestApplicationDeployLostResponseOnReusedConnectionSendsOnce(t *testing.T) {
	app := uuid.NewString()
	var posts atomic.Int32
	cs, client := deploymentSessionWithClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{}`)
			return
		}
		posts.Add(1)
		if r.URL.Path != "/api/v1/mcp/applications/"+app+"/deploy" || r.Header.Get("Idempotency-Key") != "stable-request" {
			t.Error("wrong mutation target/identity")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	var reused atomic.Bool
	var firstConnection atomic.Bool
	client.http.Transport = traceRoundTripper{base: transport, gotConn: func(info httptrace.GotConnInfo) {
		if firstConnection.CompareAndSwap(false, true) {
			reused.Store(info.Reused)
		}
	}}
	var warm struct{}
	if err := client.readJSON(context.Background(), "/warm", &warm); err != nil {
		t.Fatal(err)
	}
	out := callDiscovery(t, cs, "application_deploy", map[string]any{"application_id": app, "request_id": "stable-request"})
	if !reused.Load() {
		t.Fatal("POST did not use the warmed connection")
	}
	if !out.IsError || len(out.Content) != 1 {
		t.Fatalf("lost response error=%+v", out)
	}
	if text, ok := out.Content[0].(*sdk.TextContent); !ok || text.Text != "upstream_unavailable" {
		t.Fatalf("want upstream_unavailable, got %+v", out.Content)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("lost response caused %d POSTs; want exactly one", got)
	}
}

type traceRoundTripper struct {
	base    http.RoundTripper
	gotConn func(httptrace.GotConnInfo)
}

func (t traceRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost {
		return t.base.RoundTrip(r)
	}
	return t.base.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{GotConn: t.gotConn})))
}

func TestApplicationDeployAcceptanceReplayAndResponseLoss(t *testing.T) {
	app, dep := uuid.NewString(), uuid.NewString()
	var posts atomic.Int32
	cs := deploymentSession(t, func(w http.ResponseWriter, r *http.Request) {
		count := posts.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/mcp/applications/"+app+"/deploy" || r.Header.Get("Idempotency-Key") != "stable-request" {
			t.Error("wrong mutation target/identity")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 0 {
			t.Errorf("deploy fingerprint must be empty: %v %v", body, err)
		}
		if count == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"outcome":"accepted","accepted_at":"2026-10-22T03:04:05Z","replayed":true},"deployment":{"id":%q,"application_id":%q,"state":"failed","error":"secret-sentinel","build_log":"secret-sentinel"}}`, dep, dep, app)
	})
	args := map[string]any{"application_id": app, "request_id": "stable-request"}
	first := callDiscovery(t, cs, "application_deploy", args)
	if !first.IsError || posts.Load() != 1 {
		t.Fatalf("lost response retried implicitly: %+v posts=%d", first, posts.Load())
	}
	replay := callDiscovery(t, cs, "application_deploy", args)
	var got applicationDeployResult
	if err := json.Unmarshal([]byte(resultJSON(t, replay)), &got); err != nil {
		t.Fatal(err)
	}
	if replay.IsError || !got.Accepted || !got.Receipt.Replayed || got.Receipt.ResourceID != dep || got.Deployment == nil || got.Deployment.State != "failed" || !got.Deployment.Terminal || strings.Contains(resultJSON(t, replay), "secret-sentinel") || posts.Load() != 2 {
		t.Fatalf("replay=%+v posts=%d", got, posts.Load())
	}
}

func TestApplicationDeployQueuedAndReceiptOnly(t *testing.T) {
	app, dep := uuid.NewString(), uuid.NewString()
	for _, receiptOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(receiptOnly), func(t *testing.T) {
			cs := deploymentSession(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				if receiptOnly {
					_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"outcome":"accepted_not_confirmed"}}`, dep)
					return
				}
				_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"outcome":"accepted"},"deployment":{"id":%q,"application_id":%q,"state":"queued"}}`, dep, dep, app)
			})
			out := callDiscovery(t, cs, "application_deploy", map[string]any{"application_id": app, "request_id": "stable"})
			var got applicationDeployResult
			if err := json.Unmarshal([]byte(resultJSON(t, out)), &got); err != nil {
				t.Fatal(err)
			}
			if out.IsError || !got.Accepted || got.Receipt.ResourceID != dep || (got.Deployment != nil && got.Deployment.Terminal) {
				t.Fatalf("accepted job misrepresented: %+v", got)
			}
		})
	}
}

func TestApplicationDeployFailClosed(t *testing.T) {
	app := uuid.NewString()
	for _, status := range []int{403, 404, 409, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			cs := deploymentSession(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !strings.HasPrefix(r.URL.Path, "/api/v1/mcp/") {
					t.Error("legacy fallback")
				}
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"code":"request_expired","message":"secret-sentinel"}`)
			})
			out := callDiscovery(t, cs, "application_deploy", map[string]any{"application_id": app, "request_id": "stable"})
			if !out.IsError || calls != 1 || strings.Contains(resultJSON(t, out), "secret-sentinel") {
				t.Fatalf("error=%+v calls=%d", out, calls)
			}
		})
	}
	cs := deploymentSession(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid deploy reached API") })
	for _, args := range []map[string]any{
		{"application_id": app},
		{"application_id": app, "request_id": ""},
		{"application_id": app, "request_id": "invalid key"},
		{"application_id": app, "request_id": strings.Repeat("x", 129)},
		{"application_id": app, "request_id": "stable", "owner_id": uuid.NewString()},
	} {
		out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "application_deploy", Arguments: args})
		if err == nil && !out.IsError {
			t.Fatalf("accepted invalid deploy=%+v", args)
		}
	}
}

func TestDeploymentCancellationIsOnlyACanceledRead(t *testing.T) {
	app, dep := uuid.NewString(), uuid.NewString()
	started, finished := make(chan struct{}), make(chan struct{})
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/applications/"+app+"/deployments/"+dep {
			t.Error("cancellation sent a mutation")
		}
		close(started)
		<-r.Context().Done()
		close(finished)
	}))
	defer api.Close()
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		var out any
		done <- client.readJSON(ctx, "/api/v1/applications/"+app+"/deployments/"+dep, &out)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || err.Error() != "cancelled" {
			t.Fatalf("read error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read did not cancel")
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP read did not close")
	}
	if calls != 1 {
		t.Fatalf("unexpected extra requests: %d", calls)
	}
}

func TestDeploymentToolAnnotations(t *testing.T) {
	cs := deploymentSession(t, func(http.ResponseWriter, *http.Request) { t.Error("listing tools called API") })
	list, err := cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil || len(list.Tools) != 3 {
		t.Fatalf("tools=%+v err=%v", list, err)
	}
	for _, tool := range list.Tools {
		if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil || !tool.Annotations.IdempotentHint {
			t.Fatalf("missing schema/annotations: %+v", tool)
		}
		mutation := tool.Name == "application_deploy"
		if tool.Annotations.ReadOnlyHint == mutation || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != mutation {
			t.Fatalf("incorrect mutation annotations: %+v", tool)
		}
	}
}
