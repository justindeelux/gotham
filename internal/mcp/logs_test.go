package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func logSession(t *testing.T, handler http.HandlerFunc) *sdk.ClientSession {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gt1_test-secret" || r.Header.Get("X-Team-Id") != testConfig("").TeamID.String() {
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
	registerLogTools(server, client)
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
	return cs
}
func TestLogToolsProtocolAndProjection(t *testing.T) {
	app, dep, container := uuid.NewString(), uuid.NewString(), strings.Repeat("a", 64)
	cs := logSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("tail") != "100" || r.URL.Query().Get("max_bytes") != "65536" {
			t.Errorf("unbounded request: %s %s", r.Method, r.URL)
		}
		if strings.Contains(r.URL.Path, "/deployments/") {
			if r.URL.Path != "/api/v1/applications/"+app+"/deployments/"+dep+"/logs" {
				t.Error("wrong exact target")
			}
			_, _ = fmt.Fprintf(w, `{"application_id":%q,"deployment_id":%q,"source":"persisted_build","availability":"available","prior_truncation":"unknown","log":"build output","complete":true,"truncated":false,"password":"secret-sentinel"}`, app, dep)
		} else {
			if r.URL.Query().Get("service") != "web" || r.URL.Query().Get("replica") != "2" {
				t.Error("missing selectors")
			}
			_, _ = fmt.Fprintf(w, `{"application_id":%q,"source":"runtime","containers":[{"id":%q,"service":"web","replica":"2","state":"exited","health":"unhealthy","deployment_id":null,"log":"runtime output","complete":true,"truncated":false,"password":"secret-sentinel"}],"complete":true,"partial":false,"password":"secret-sentinel"}`, app, container)
		}
	})
	list, err := cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil || len(list.Tools) != 2 {
		t.Fatalf("tools=%+v %v", list, err)
	}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.OutputSchema == nil {
			t.Fatalf("unsafe schema %+v", tool)
		}
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{"application_logs", map[string]any{"application_id": app, "service": "web", "replica": "2"}, "runtime output"},
		{"deployment_logs", map[string]any{"application_id": app, "deployment_id": dep}, "build output"},
	} {
		result := callDiscovery(t, cs, tc.name, tc.args)
		if result.IsError {
			t.Fatalf("call failed %+v", result)
		}
		raw := resultJSON(t, result)
		if !strings.Contains(raw, tc.text) || strings.Contains(raw, "secret-sentinel") {
			t.Fatalf("projection=%s", raw)
		}
		content, _ := json.Marshal(result.Content)
		if strings.Contains(string(content), tc.text) {
			t.Fatal("log duplicated in content fallback")
		}
	}
}
func TestLogToolsRejectOverridesAndInvalidLimits(t *testing.T) {
	cs := logSession(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached API") })
	app := uuid.NewString()
	for _, args := range []map[string]any{
		{"application_id": app, "tail": -1}, {"application_id": app, "tail": 1001}, {"application_id": app, "max_bytes": 262145},
		{"application_id": "invalid"}, {"application_id": app, "replica": "2"}, {"application_id": app, "service": "web", "replica": "-1"},
		{"application_id": app, "container_id": "foreign"}, {"application_id": app, "team_id": uuid.NewString()}, {"application_id": app, "url": "https://foreign.example"},
	} {
		if result := callDiscovery(t, cs, "application_logs", args); !result.IsError {
			t.Fatalf("accepted %+v", args)
		}
	}
}
func TestLogToolsSafeUpstreamErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			cs := logSession(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("sensitive upstream text"))
			})
			result := callDiscovery(t, cs, "application_logs", map[string]any{"application_id": uuid.NewString()})
			data, _ := json.Marshal(result)
			if !result.IsError || strings.Contains(string(data), "sensitive") {
				t.Fatalf("unsafe error=%s", data)
			}
		})
	}
}
