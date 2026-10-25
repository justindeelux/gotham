package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/store"
)

func projectsDSNForDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + database
	return parsed.String()
}

func scratchProjectsStack(t *testing.T) (*Server, *store.Store, *auth.AuthResult) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		base = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	admin, err := sql.Open("pgx", projectsDSNForDatabase(t, base, "postgres"))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	name := fmt.Sprintf("pe1srv%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	dsn := projectsDSNForDatabase(t, base, name)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch store: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	signer, err := auth.NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	authSvc := auth.New(st, signer, logger)
	authSvc.AllowOpenRegistration = true

	email := fmt.Sprintf("pe1-srv-%d@example.com", time.Now().UnixNano())
	owner, err := authSvc.Register(ctx, email, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	tokens := auth.NewAPITokenService(st, logger)
	s, err := New(cfg, logger, authSvc, nil, tokens, nil, st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s, st, owner
}

type wiringProjectEnvelope struct {
	Project struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		EnvironmentCount int    `json:"environment_count"`
	} `json:"project"`
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"environments"`
}

func TestProjectsWiringEndToEnd(t *testing.T) {
	s, _, pair := scratchProfileStack(t)
	bearer := "Bearer " + pair.AccessToken

	if rec := doRequest(t, s, http.MethodGet, "/api/v1/projects", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Shop"}`, bearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created wiringProjectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Project.Name != "Shop" || created.Project.EnvironmentCount != 1 {
		t.Fatalf("project = %+v, want Shop with one environment", created.Project)
	}
	if len(created.Environments) != 1 || created.Environments[0].Name != "production" {
		t.Fatalf("environments = %+v, want one production", created.Environments)
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+created.Project.ID, "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+created.Project.ID, "", bearer)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestProjectsWiringViewerAndTokenScopes(t *testing.T) {
	s, st, owner := scratchProjectsStack(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}
	ownerBearer := "Bearer " + owner.AccessToken

	viewerEmail := fmt.Sprintf("pe1-viewer-%d@example.com", time.Now().UnixNano())
	viewer, err := s.auth.Register(ctx, viewerEmail, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}
	viewerBearer := "Bearer " + viewer.AccessToken

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Shop"}`, ownerBearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created wiringProjectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if code := doRequest(t, s, http.MethodGet, "/api/v1/projects", "", viewerBearer).Code; code != http.StatusOK {
		t.Fatalf("viewer list = %d, want 200", code)
	}
	if code := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+created.Project.ID, "", viewerBearer).Code; code != http.StatusNotFound {
		t.Fatalf("viewer get = %d, want 404", code)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/projects", `{"name":"Other"}`},
		{http.MethodPatch, "/api/v1/projects/" + created.Project.ID, `{"name":"x"}`},
		{http.MethodDelete, "/api/v1/projects/" + created.Project.ID, ""},
	} {
		if code := doRequest(t, s, tc.method, tc.path, tc.body, viewerBearer).Code; code != http.StatusNotFound {
			t.Errorf("viewer %s %s = %d, want 403", tc.method, tc.path, code)
		}
	}

	tokens := auth.NewAPITokenService(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	minted, err := tokens.Create(ctx, ownerID, "probe-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("mint read token: %v", err)
	}
	tokenBearer := "Bearer " + minted.Token
	if code := doRequest(t, s, http.MethodGet, "/api/v1/projects", "", tokenBearer).Code; code != http.StatusOK {
		t.Errorf("read token list = %d, want 200", code)
	}
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Token"}`, tokenBearer); rec.Code != http.StatusForbidden {
		t.Errorf("read token create = %d, want 403", rec.Code)
	}
}
