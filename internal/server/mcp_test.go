package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

func TestMCPStrictContextAndReads(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	read := mustCreateOwnerToken(t, tokens, auth.ScopeRead)
	request := func(method, path, token, contract string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(mcpContractHeader, contract)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/api/v1/mcp/context", "/api/v1/servers"} {
		rec := request("GET", path, read, "1")
		if rec.Code != 200 {
			t.Fatalf("strict response %s: %d %s %+v", path, rec.Code, rec.Body.String(), rec.Header())
		}
		if path == "/api/v1/mcp/context" {
			if rec.Header().Get(mcpContractHeader) != "1" {
				t.Fatalf("strict context missing contract header: %+v", rec.Header())
			}
			var body struct {
				Capabilities []string `json:"capabilities"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Capabilities) != 2 {
				t.Fatalf("context %+v %v", body, err)
			}
		}
	}
	for _, tc := range []struct {
		name, token, contract string
		want                  int
	}{
		{"missing token", "", "1", 401},
		{"invalid token", "gt1_invalid", "1", 401},
		{"missing contract", read, "", 400},
		{"wrong contract", read, "2", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request("GET", "/api/v1/mcp/context", tc.token, tc.contract)
			if rec.Code != tc.want {
				t.Fatalf("%d want %d body %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	if rec := request("POST", "/api/v1/servers", read, "1"); rec.Code != 403 {
		t.Fatalf("read mutation=%d", rec.Code)
	}
	memberRead := mustCreateToken(t, tokens, auth.ScopeRead)
	if rec := request("GET", "/api/v1/servers", memberRead, "1"); rec.Code != 404 {
		t.Fatalf("member read=%d", rec.Code)
	}
}

func TestMCPConnection(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	type connectionBody struct {
		BaseURL      string   `json:"base_url"`
		MCPURL       string   `json:"mcp_url"`
		HTTPS        bool     `json:"https"`
		Role         string   `json:"role"`
		Capabilities []string `json:"capabilities"`
	}
	parse := func(rec *httptest.ResponseRecorder) connectionBody {
		t.Helper()
		var body connectionBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		return body
	}

	for _, tc := range []struct {
		name, target, bearer, role string
		secure                     bool
	}{
		{"member over http", "http://gotham.test/api/v1/mcp/connection", "Bearer valid-token", auth.RoleMember, false},
		{"owner over https", "https://gotham.test/api/v1/mcp/connection", "Bearer owner-token", auth.RoleOwner, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, "GET", tc.target, "", tc.bearer)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			body := parse(rec)
			scheme := "http://"
			if tc.secure {
				scheme = "https://"
			}
			wantBase := scheme + "gotham.test"
			if body.BaseURL != wantBase || body.MCPURL != wantBase+"/mcp" {
				t.Fatalf("base=%q mcp=%q, want %q", body.BaseURL, body.MCPURL, wantBase+"/mcp")
			}
			if body.HTTPS != tc.secure || body.Role != tc.role {
				t.Fatalf("https=%v role=%q", body.HTTPS, body.Role)
			}
			if len(body.Capabilities) != 2 {
				t.Fatalf("capabilities = %v", body.Capabilities)
			}
			if strings.Contains(rec.Body.String(), "gt1_") || strings.Contains(rec.Body.String(), tc.bearer) {
				t.Fatalf("response leaked a credential: %s", rec.Body.String())
			}
		})
	}

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	for _, tc := range []struct {
		name, bearer string
		want         int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"api token", "Bearer " + readToken, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, "GET", "/api/v1/mcp/connection", "", tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestMCPRevokedToken(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	token, err := tokens.Create(context.Background(), testUserID, "mcp", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	send := func() int {
		req := httptest.NewRequest("GET", "/api/v1/mcp/context", nil)
		req.Header.Set("Authorization", "Bearer "+token.Token)
		req.Header.Set(mcpContractHeader, "1")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if send() != 200 {
		t.Fatal("initial context failed")
	}
	if err := tokens.Revoke(context.Background(), testUserID, token.ID); err != nil {
		t.Fatal(err)
	}
	if got := send(); got != 401 {
		t.Fatalf("revoked token=%d", got)
	}
}

func TestMCPContextNotMountedWithoutAuth(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	for _, token := range []string{"", "Bearer login-token", "Bearer gt1_test"} {
		req := httptest.NewRequest("GET", "/api/v1/mcp/context", nil)
		req.Header.Set("Authorization", token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("nil-auth context status=%d want 404", rec.Code)
		}
	}
}
