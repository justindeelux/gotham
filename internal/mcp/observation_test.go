package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const observationNodeID = "22222222-2222-4222-8222-222222222222"

func observationClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			if r.Header.Get("Authorization") != "Bearer gt1_test-secret" || r.Header.Get("X-Team-Id") != testConfig("").TeamID.String() || r.Header.Get(contractHeader) != "1" {
				t.Error("fixed strict authority missing")
			}
			acknowledge(w, r)
		}
		if r.URL.Path == "/api/v1/servers/agents" {
			t.Error("global map requested")
		}
		handler(w, r)
	}))
	t.Cleanup(api.Close)
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestObservationProjectionAndBounds(t *testing.T) {
	seen := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	client := observationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/servers" {
			nodes := make([]map[string]any, 101)
			for i := range nodes {
				nodes[i] = map[string]any{"id": fmt.Sprintf("%03d", 100-i), "name": "node", "status": "offline", "last_seen": seen, "cpu_usage": 0.5, "ssh_user": "secret-user", "ssh_key_id": "secret-key", "host_key_fingerprint": "secret-fingerprint", "has_password": true}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"servers": nodes})
		} else {
			_, _ = fmt.Fprintf(w, `{"server":{"id":%q,"status":"pending","stale":false,"agent_version":null}}`, observationNodeID)
		}
	})
	out, err := client.nodesList(context.Background(), NodesInput{})
	if err != nil || len(out.Nodes) != 100 || out.Total != 101 || !out.Truncated || out.NextOffset == nil || *out.NextOffset != 100 {
		t.Fatalf("list=%+v %v", out, err)
	}
	node := out.Nodes[0]
	if node.ID != "000" || node.Stale == nil || !*node.Stale || !node.LastSeen.Equal(seen) || node.CPUUsage == nil || *node.CPUUsage != 0.5 || node.AgentVersion != nil || node.MemUsage != nil {
		t.Fatalf("snapshot=%+v", node)
	}
	body, _ := json.Marshal(out)
	for _, secret := range []string{"ssh_user", "secret-user", "ssh_key_id", "host_key_fingerprint", "has_password"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("unsafe projection %s", secret)
		}
	}
	page, err := client.nodesList(context.Background(), NodesInput{Offset: 100, Limit: 1})
	if err != nil || len(page.Nodes) != 1 || page.Truncated || page.NextOffset != nil {
		t.Fatalf("page=%+v %v", page, err)
	}
	detail, err := client.nodeOverview(context.Background(), NodeInput{NodeID: observationNodeID})
	if err != nil || detail.Node.Stale != nil || detail.Node.AgentVersion != nil {
		t.Fatalf("missing source timestamp=%+v %v", detail, err)
	}
	for _, input := range []NodesInput{{Limit: 101}, {Limit: -1}, {Offset: -1}} {
		if _, err := client.nodesList(context.Background(), input); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
	for _, id := range []string{"", "../agents", "00000000-0000-0000-0000-000000000000"} {
		if _, err := client.nodeOverview(context.Background(), NodeInput{NodeID: id}); err == nil {
			t.Fatal("invalid node accepted")
		}
	}
}

func TestSystemOverviewDegradedAndPartial(t *testing.T) {
	for _, versionStatus := range []int{200, 503, 401, 403} {
		t.Run(fmt.Sprint(versionStatus), func(t *testing.T) {
			client := observationClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/mcp/context":
					_, _ = fmt.Fprintf(w, `{"team_id":%q}`, testConfig("").TeamID)
				case "/healthz":
					w.WriteHeader(503)
					_, _ = w.Write([]byte(`{"status":"degraded","db":"error","redis":"ok"}`))
				case "/api/v1/version":
					w.WriteHeader(versionStatus)
					_, _ = w.Write([]byte(`{"version":"v1.2.3"}`))
				case "/api/v1/servers":
					_, _ = w.Write([]byte(`{"servers":[]}`))
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
				}
			})
			out, err := client.systemOverview(context.Background())
			if versionStatus == 401 || versionStatus == 403 {
				if err == nil || out.Health != nil {
					t.Fatalf("auth failure softened=%+v %v", out, err)
				}
				return
			}
			if err != nil || out.Health == nil || out.Health.Status != "degraded" || out.Health.DB != "error" || out.HealthError != nil {
				t.Fatalf("health503=%+v %v", out, err)
			}
			if versionStatus == 503 && (!out.Partial || out.VersionError == nil || out.VersionError.Status != 503 || out.Version != nil) {
				t.Fatalf("partial=%+v", out)
			}
			if versionStatus == 200 && (out.Partial || out.Version == nil || *out.Version != "v1.2.3") {
				t.Fatalf("version=%+v", out)
			}
		})
	}
}

func TestMetricsGapsNullsAndSeriesBounds(t *testing.T) {
	from := time.Now().UTC().Truncate(time.Minute).Add(-24 * time.Hour)
	input := MetricsInput{NodeID: observationNodeID, From: from, To: from.Add(24 * time.Hour), Step: "1m"}
	for _, count := range []int{0, 2, 1440, 1443} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			client := observationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("step") != "1m" || r.URL.Query().Get("from") != from.Format(time.RFC3339Nano) {
					t.Error("range changed")
				}
				points := make([]map[string]any, count)
				for i := range points {
					offset := i
					if count == 2 {
						offset = i * 3
					}
					points[i] = map[string]any{"bucket": from.Add(time.Duration(offset) * time.Minute), "cpu_usage": nil, "mem_usage": 0.25, "net_rx_bps": 1024}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"step": "1m", "points": points})
			})
			out, err := client.nodeMetrics(context.Background(), input)
			if count > 1440 {
				if err == nil {
					t.Fatal("oversize series accepted")
				}
				return
			}
			if err != nil || len(out.Points) != count || out.Units.Usage != "fraction" || out.Units.Rate != "bytes/second" {
				t.Fatalf("metrics=%+v %v", out, err)
			}
			if count == 0 && out.State != "no_samples" {
				t.Fatalf("empty=%+v", out)
			}
			if count > 0 && (out.Points[0].CPUUsage != nil || out.Points[0].NetRxBps == nil || *out.Points[0].NetRxBps != 1024) {
				t.Fatal("null/units lost")
			}
			if count == 2 && !out.Points[1].Bucket.Equal(from.Add(3*time.Minute)) {
				t.Fatal("gap replaced")
			}
		})
	}
	client := observationClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid range reached upstream") })
	for _, tc := range []MetricsInput{
		{NodeID: observationNodeID, From: from, To: from.Add(24*time.Hour + time.Second), Step: "1m"},
		{NodeID: observationNodeID, From: from, To: from.Add(31 * 24 * time.Hour), Step: "1h"},
		{NodeID: observationNodeID, From: from, To: from.Add(31 * 24 * time.Hour), Step: "1d"},
		{NodeID: observationNodeID, From: from, To: from, Step: "1m"},
		{NodeID: observationNodeID, From: from, To: from.Add(time.Hour), Step: "1s"},
	} {
		if _, err := client.nodeMetrics(context.Background(), tc); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}

func TestMetricsUnavailablePreservesAuthorityAndStatus(t *testing.T) {
	for _, status := range []int{401, 403, 404, 503} {
		client := observationClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("secret upstream text"))
		})
		now := time.Now().UTC()
		out, err := client.nodeMetrics(context.Background(), MetricsInput{NodeID: observationNodeID, From: now.Add(-time.Hour), To: now})
		if status == 401 || status == 403 {
			if err == nil {
				t.Fatalf("auth %d softened", status)
			}
		} else {
			if err != nil || out.Error == nil || out.Error.Status != status {
				t.Fatalf("status %d=%+v %v", status, out, err)
			}
			if status == 404 && out.State != "unsupported_or_disabled" {
				t.Fatalf("ambiguous 404=%+v", out)
			}
			if out.State == "disabled" {
				t.Fatal("invented disabled state")
			}
		}
	}
}

func TestObservationDeadlines(t *testing.T) {
	client := observationClient(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.nodeOverview(ctx, NodeInput{NodeID: observationNodeID})
	if err == nil || err.Error() != "deadline_exceeded" {
		t.Fatalf("deadline=%v", err)
	}
}

func TestObservationProtocolAndAfterInitDisablement(t *testing.T) {
	var disabled atomic.Bool
	client := observationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if disabled.Load() {
			w.Header().Del(contractHeader)
			w.Header().Del(teamHeader)
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/api/v1/mcp/context":
			_, _ = fmt.Fprintf(w, `{"team_id":%q}`, testConfig("").TeamID)
		case "/api/v1/servers":
			_, _ = fmt.Fprintf(w, `{"servers":[{"id":%q,"status":"offline"}]}`, observationNodeID)
		case "/api/v1/servers/" + observationNodeID:
			_, _ = fmt.Fprintf(w, `{"server":{"id":%q,"status":"offline"}}`, observationNodeID)
		case "/api/v1/servers/" + observationNodeID + "/metrics":
			_, _ = w.Write([]byte(`{"step":"1m","points":[]}`))
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok","db":"ok","redis":"ok"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok","version":"v1.2.3"}`))
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, err := NewServer(ctx, client.config, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil || len(list.Tools) != 18 {
		t.Fatalf("tools=%+v %v", list, err)
	}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || (!strings.HasSuffix(tool.Name, "_create") && tool.Name != "application_deploy" && !tool.Annotations.ReadOnlyHint) || tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("contract=%+v", tool)
		}
	}
	result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "nodes_list", Arguments: map[string]any{}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("typed result=%+v %v", result, err)
	}
	for _, args := range []map[string]any{{"team_id": testConfig("").TeamID.String()}, {"limit": 101}, {"limit": "1"}} {
		result, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "nodes_list", Arguments: args})
		if err == nil && !result.IsError {
			t.Fatalf("invalid schema accepted=%+v", args)
		}
	}
	for _, name := range []string{"system_overview", "node_overview", "node_metrics"} {
		args := map[string]any{}
		if name != "system_overview" {
			args["node_id"] = observationNodeID
		}
		if name == "node_metrics" {
			args["from"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			args["to"] = time.Now().Format(time.RFC3339)
		}
		result, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError || result.StructuredContent == nil {
			t.Fatalf("typed nullable %s=%+v %v", name, result, err)
		}
	}
	disabled.Store(true)
	for _, name := range []string{"system_overview", "nodes_list", "node_overview", "node_metrics"} {
		args := map[string]any{}
		if name == "node_overview" || name == "node_metrics" {
			args["node_id"] = observationNodeID
		}
		if name == "node_metrics" {
			args["from"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			args["to"] = time.Now().Format(time.RFC3339)
		}
		result, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err == nil && !result.IsError {
			body, _ := json.Marshal(result.StructuredContent)
			if name != "node_metrics" || !strings.Contains(string(body), `"state":"unavailable"`) {
				t.Fatalf("disabled context leaked %s: %s", name, body)
			}
		}
	}
}

func TestNodeStaleness(t *testing.T) {
	now := time.Now().UTC()
	recent, old := now.Add(-time.Second), now.Add(-time.Hour)
	for _, tc := range []struct {
		status string
		seen   *time.Time
		want   *bool
	}{
		{"ready", &recent, boolPointer(false)}, {"ready", &old, boolPointer(true)},
		{"offline", nil, boolPointer(true)}, {"pending", nil, nil},
	} {
		node := Node{Status: tc.status, LastSeen: tc.seen}
		markStale(&node, now)
		if (node.Stale == nil) != (tc.want == nil) || (tc.want != nil && *node.Stale != *tc.want) {
			t.Fatalf("staleness=%+v want=%v", node, tc.want)
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestSystemUnreachableHealthIsPartial(t *testing.T) {
	client := observationClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		case "/api/v1/mcp/context":
			_, _ = fmt.Fprintf(w, `{"team_id":%q}`, testConfig("").TeamID)
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"v1.2.3"}`))
		case "/api/v1/servers":
			_, _ = w.Write([]byte(`{"servers":[]}`))
		}
	})
	out, err := client.systemOverview(context.Background())
	if err != nil || !out.Partial || out.Health != nil || out.HealthError == nil || out.HealthError.Code != "upstream_unavailable" || out.Version == nil || out.Nodes == nil {
		t.Fatalf("unreachable=%+v %v", out, err)
	}
}
