package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/projects"
)

func TestReplayBackedMutationAuthorization(t *testing.T) {
	s, st, owner := scratchProjectsStack(t)
	ctx := context.Background()
	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}

	projectSvc := projects.NewService(projects.Config{Store: st, Counter: projects.StoreCounter{Store: st}, Logger: s.logger})
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		projects.Mount(api, s.resourceAuth(), UserIDFromContext, projectSvc)
	})

	deployToken, err := s.tokens.Create(ctx, ownerID, "replay-deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	readToken, err := s.tokens.Create(ctx, ownerID, "replay-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.tokens.Revoke(ctx, ownerID, deployToken.ID) })

	send := func(token, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/projects", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(readToken.Token, "read-create", `{"name":"read"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("read token create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := send(deployToken.Token, "project", `{"name":"shop"}`); rec.Code != http.StatusCreated {
		t.Fatalf("deploy token create=%d %s", rec.Code, rec.Body.String())
	}
	var first struct {
		Receipt struct {
			ResourceID string `json:"resource_id"`
			Replayed   bool   `json:"replayed"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(send(deployToken.Token, "project", `{"name":"shop"}`).Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Receipt.ResourceID == "" || !first.Receipt.Replayed {
		t.Fatalf("first replay=%+v", first)
	}

	if _, err := st.DB.Exec(ctx, `UPDATE users SET role='member' WHERE id=$1`, ownerID); err != nil {
		t.Fatal(err)
	}
	denied := send(deployToken.Token, "project", `{"name":"shop"}`)
	if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), first.Receipt.ResourceID) {
		t.Fatalf("demoted replay=%d %s", denied.Code, denied.Body.String())
	}
	if _, err := st.DB.Exec(ctx, `UPDATE users SET role='owner' WHERE id=$1`, ownerID); err != nil {
		t.Fatal(err)
	}

	replay := send(deployToken.Token, "project", `{"name":"shop"}`)
	if replay.Code != http.StatusCreated {
		t.Fatalf("owner replay=%d %s", replay.Code, replay.Body.String())
	}
	var second struct {
		Receipt struct {
			ResourceID string `json:"resource_id"`
			Replayed   bool   `json:"replayed"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if !second.Receipt.Replayed || second.Receipt.ResourceID != first.Receipt.ResourceID {
		t.Fatalf("replay identity=%+v want %s", second, first.Receipt.ResourceID)
	}

	read := httptest.NewRequest("GET", "/api/v1/projects", nil)
	read.Header.Set("Authorization", "Bearer "+readToken.Token)
	readRec := httptest.NewRecorder()
	router.ServeHTTP(readRec, read)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read token list=%d %s", readRec.Code, readRec.Body.String())
	}

	if err := s.tokens.Revoke(ctx, ownerID, deployToken.ID); err != nil {
		t.Fatal(err)
	}
	if rec := send(deployToken.Token, "project", `{"name":"shop"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token=%d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPCreationBackedAndScoped(t *testing.T) {
	s, st, owner := scratchProjectsStack(t)
	ctx := context.Background()
	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}

	projectSvc := projects.NewService(projects.Config{Store: st, Counter: projects.StoreCounter{Store: st}, Logger: s.logger})
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		projects.Mount(api, s.resourceAuth(), UserIDFromContext, projectSvc)
	})

	deployToken, err := s.tokens.Create(ctx, ownerID, "mcp-deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	readToken, err := s.tokens.Create(ctx, ownerID, "mcp-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.auth.Register(ctx, "mcp-member@example.com", "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	memberID, err := uuid.Parse(member.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberToken, err := s.tokens.Create(ctx, memberID, "mcp-member", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}

	send := func(token, key, body, contract string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/mcp/projects", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if contract != "" {
			req.Header.Set(mcpContractHeader, contract)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	first := send(deployToken.Token, "mcp-project", `{"name":"mcp"}`+"\n", "1")
	if first.Code != http.StatusCreated {
		t.Fatalf("owner deploy create=%d %s", first.Code, first.Body.String())
	}
	var created struct {
		Receipt struct {
			ResourceID string `json:"resource_id"`
			Replayed   bool   `json:"replayed"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	replay := send(deployToken.Token, "mcp-project", `{"name":"mcp"}`+"\n", "1")
	if replay.Code != http.StatusCreated {
		t.Fatalf("owner deploy replay=%d %s", replay.Code, replay.Body.String())
	}
	var replayed struct {
		Receipt struct {
			ResourceID string `json:"resource_id"`
			Replayed   bool   `json:"replayed"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if !replayed.Receipt.Replayed || replayed.Receipt.ResourceID != created.Receipt.ResourceID {
		t.Fatalf("mcp replay=%+v want %s", replayed, created.Receipt.ResourceID)
	}

	if rec := send(readToken.Token, "mcp-read", `{"name":"read"}`+"\n", "1"); rec.Code != http.StatusForbidden {
		t.Fatalf("owner read create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := send(memberToken.Token, "mcp-member", `{"name":"member"}`+"\n", "1"); rec.Code != http.StatusForbidden {
		t.Fatalf("member deploy create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := send(deployToken.Token, "mcp-contract", `{"name":"contract"}`+"\n", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing contract=%d %s", rec.Code, rec.Body.String())
	}
}
