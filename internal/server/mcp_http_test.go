package server

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

	"github.com/justindeelux/gotham/internal/auth"
)

type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func connectMCPHTTP(t *testing.T, endpoint, token string) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := sdk.NewClient(&sdk.Implementation{Name: "http-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPHTTPProtocolAllTools(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	token := mustCreateOwnerToken(t, tokens, auth.ScopeRead, auth.ScopeDeploy)
	live := httptest.NewServer(s.Handler())
	t.Cleanup(live.Close)
	s.mcpBaseURL = live.URL

	session := connectMCPHTTP(t, live.URL+"/mcp", token)
	list, err := session.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]map[string]any{
		"system_overview":       {},
		"nodes_list":            {"limit": 10},
		"node_overview":         {"node_id": testUserID.String()},
		"node_metrics":          {"node_id": testUserID.String(), "from": "2026-10-23T00:00:00Z", "to": "2026-10-23T01:00:00Z"},
		"projects_list":         {},
		"environments_list":     {"project_id": testUserID.String()},
		"applications_list":     {},
		"environment_resources": {"environment_id": testUserID.String()},
		"project_overview":      {"project_id": testUserID.String()},
		"application_overview":  {"application_id": testUserID.String()},
		"application_logs":      {"application_id": testUserID.String()},
		"deployment_logs":       {"application_id": testUserID.String(), "deployment_id": testOwnerID.String()},
		"deployments_list":      {"application_id": testUserID.String()},
		"deployment_status":     {"application_id": testUserID.String(), "deployment_id": testOwnerID.String()},
		"project_create":        {"request_id": "mcp-project", "name": "shop"},
		"environment_create":    {"request_id": "mcp-env", "project_id": testUserID.String(), "name": "staging"},
		"application_create": {"request_id": "mcp-app", "name": "app", "environment_id": testUserID.String(),
			"server_id": testOwnerID.String(), "source_type": "image", "image_ref": "busybox:1.36"},
		"application_deploy": {"application_id": testUserID.String(), "request_id": "mcp-deploy"},
	}
	if len(list.Tools) != len(calls) {
		t.Fatalf("tools=%d want=%d", len(list.Tools), len(calls))
	}
	seen := map[string]bool{}
	for _, tool := range list.Tools {
		seen[tool.Name] = true
	}
	for name := range calls {
		if !seen[name] {
			t.Errorf("missing tool %s", name)
		}
	}
	for name, args := range calls {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatalf("%s transport: %v", name, err)
			}
			if result == nil {
				t.Fatalf("%s returned no result", name)
			}
			for _, content := range result.Content {
				if strings.Contains(contentString(content), token) {
					t.Fatalf("%s leaked the bearer token", name)
				}
			}
		})
	}
}

func contentString(content sdk.Content) string {
	switch value := content.(type) {
	case *sdk.TextContent:
		return value.Text
	default:
		return ""
	}
}

func contentStrings(content []sdk.Content) []string {
	out := make([]string, 0, len(content))
	for _, item := range content {
		out = append(out, contentString(item))
	}
	return out
}

func TestMCPHTTPScopeSurface(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	read := mustCreateToken(t, tokens, auth.ScopeRead)
	live := httptest.NewServer(s.Handler())
	t.Cleanup(live.Close)
	s.mcpBaseURL = live.URL

	session := connectMCPHTTP(t, live.URL+"/mcp", read)
	list, err := session.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 14 {
		t.Fatalf("read token tools=%d want 14", len(list.Tools))
	}
	for _, tool := range list.Tools {
		switch tool.Name {
		case "project_create", "environment_create", "application_create", "application_deploy":
			t.Errorf("read token listed mutation %s", tool.Name)
		}
	}
	for _, name := range []string{"project_create", "environment_create", "application_create", "application_deploy"} {
		if _, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}}); err == nil {
			t.Errorf("read token called %s", name)
		}
	}
}

func TestMCPHTTPRejectsNonAPICredentialsAndForeignOrigin(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	token := mustCreateOwnerToken(t, tokens, auth.ScopeRead)

	cases := []struct {
		name   string
		target string
		header string
		origin string
		want   int
	}{
		{"missing token", "/mcp", "", "", http.StatusUnauthorized},
		{"jwt session", "/mcp", "Bearer valid-token", "", http.StatusUnauthorized},
		{"query token", "/mcp?token=" + token, "", "", http.StatusUnauthorized},
		{"invalid token", "/mcp", "Bearer gt1_invalid", "", http.StatusUnauthorized},
		{"foreign origin", "/mcp", "Bearer " + token, "https://evil.example", http.StatusForbidden},
		{"malformed origin", "/mcp", "Bearer " + token, "https://evil.example/path", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(`{}`))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("same-origin request refused: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPHTTPOwnerAndMemberProjectAccess(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	live := httptest.NewServer(f.server.Handler())
	t.Cleanup(live.Close)
	f.server.mcpBaseURL = live.URL

	owner, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.owner.User.ID), "mcp-owner", []string{auth.ScopeRead, auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	member, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.member.User.ID), "mcp-member", []string{auth.ScopeRead, auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	ownerSession := connectMCPHTTP(t, live.URL+"/mcp", owner.Token)
	memberSession := connectMCPHTTP(t, live.URL+"/mcp", member.Token)

	projectIDs := func(session *sdk.ClientSession) map[string]bool {
		t.Helper()
		result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "projects_list", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("projects_list: %v %v", contentStrings(result.Content), err)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, item := range page.Items {
			ids[item.ID] = true
		}
		return ids
	}
	if ids := projectIDs(ownerSession); !ids[f.projects[0].String()] || !ids[f.projects[1].String()] {
		t.Fatalf("owner projects=%v", ids)
	}
	memberIDs := projectIDs(memberSession)
	if !memberIDs[f.projects[0].String()] || memberIDs[f.projects[1].String()] {
		t.Fatalf("member projects=%v", memberIDs)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"application_overview", map[string]any{"application_id": f.apps[1].String()}},
		{"application_deploy", map[string]any{"application_id": f.apps[1].String(), "request_id": "member-deploy"}},
		{"application_create", map[string]any{
			"request_id": "member-create", "name": "denied-app", "environment_id": f.environments[1].String(),
			"server_id": f.projects[1].String(), "source_type": "image", "image_ref": "busybox:1.36",
		}},
		{"project_create", map[string]any{"request_id": "member-project", "name": "denied"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := memberSession.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil {
				t.Fatalf("%s transport: %v", tc.name, err)
			}
			if !result.IsError {
				t.Fatalf("member reached ungranted resource through %s: %+v", tc.name, result.StructuredContent)
			}
			for _, hidden := range []string{f.projects[1].String(), f.apps[1].String()} {
				if strings.Contains(fmt.Sprint(result.Content), hidden) {
					t.Fatalf("%s leaked %s", tc.name, hidden)
				}
			}
		})
	}
}

func TestMCPHTTPRevokedToken(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	token, err := tokens.Create(context.Background(), testUserID, "mcp-http", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	live := httptest.NewServer(s.Handler())
	t.Cleanup(live.Close)
	s.mcpBaseURL = live.URL
	if session := connectMCPHTTP(t, live.URL+"/mcp", token.Token); session == nil {
		t.Fatal("initial connect failed")
	}
	if err := tokens.Revoke(context.Background(), testUserID, token.ID); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "http-test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             live.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token.Token}},
		DisableStandaloneSSE: true,
	}, nil); err == nil {
		t.Fatal("revoked token connected")
	}
}
