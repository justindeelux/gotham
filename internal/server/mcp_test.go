package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
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
	if rec := request("GET", "/api/v1/servers", memberRead, "1"); rec.Code != 403 {
		t.Fatalf("member read=%d", rec.Code)
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
