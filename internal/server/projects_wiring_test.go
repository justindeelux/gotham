package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/store"
)

// projectsDSNForDatabase points a DSN at another database on the same
// server (the maintenance connection and the disposable database).
func projectsDSNForDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + database
	return parsed.String()
}

// scratchProjectsStack builds a Server on a private scratch database with the
// real auth and API-token services, and returns it with the store and the
// owner's session pair. Unlike scratchProfileStack it wires the token
// service, so API-token scope probes run through the real chain.
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
	// Registration closes after the bootstrap account; reopen it so the
	// viewer account can register (test/dev override, same as the config
	// flag the server would pass).
	authSvc.AllowOpenRegistration = true

	email := fmt.Sprintf("pe1-srv-%d@example.com", time.Now().UnixNano())
	owner, err := authSvc.Register(ctx, email, "s3cret-password", "", nil, auth.SessionMeta{})
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

// wiringProjectEnvelope decodes the project create/get responses just far
// enough to prove the PE-1 mount is live through the real server chain.
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

// TestProjectsWiringEndToEnd replays the PE-1 contract against the real
// stack (real auth, real team chain, real service on a scratch database):
// unauthenticated reads are refused, and a session creates, reads and
// deletes a project with its production environment.
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

// TestProjectsWiringViewerAndTokenScopes probes the real withTeam chain: a
// read_only member reads but cannot mutate (403 from teamWriteGate), and a
// read-scoped API token reads but cannot mutate (403 from the scope
// boundary). Both run against the scratch stack with real auth, teams and
// tokens.
func TestProjectsWiringViewerAndTokenScopes(t *testing.T) {
	s, st, owner := scratchProjectsStack(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}
	ownerBearer := "Bearer " + owner.AccessToken

	// A second account joins a fresh team as read_only; the owner stays
	// owner. Membership rows are seeded directly: the invite flow is a
	// teams-package concern, not this probe's.
	viewerEmail := fmt.Sprintf("pe1-viewer-%d@example.com", time.Now().UnixNano())
	viewer, err := s.auth.Register(ctx, viewerEmail, "s3cret-password", "", nil, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}
	viewerID, err := uuid.Parse(viewer.User.ID)
	if err != nil {
		t.Fatalf("parse viewer id: %v", err)
	}
	teamID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO teams (id, name) VALUES ($1, 'scope-probe')`, teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'read_only')`,
		teamID, ownerID, viewerID); err != nil {
		t.Fatalf("insert memberships: %v", err)
	}
	teamHeader := teamID.String()

	viewerBearer := "Bearer " + viewer.AccessToken
	doWithTeam := func(method, path, body, authorization string) *httptest.ResponseRecorder {
		t.Helper()
		return doTeamRequest(t, s, method, path, body, authorization, teamHeader)
	}

	// The owner creates a project in the team scope; the viewer reads it.
	rec := doTeamRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Shop"}`, ownerBearer, teamHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created wiringProjectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if code := doWithTeam(http.MethodGet, "/api/v1/projects", "", viewerBearer).Code; code != http.StatusOK {
		t.Fatalf("viewer list = %d, want 200", code)
	}
	if code := doWithTeam(http.MethodGet, "/api/v1/projects/"+created.Project.ID, "", viewerBearer).Code; code != http.StatusOK {
		t.Fatalf("viewer get = %d, want 200", code)
	}
	// Mutations are refused by the real teamWriteGate before the handler.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/projects", `{"name":"Other"}`},
		{http.MethodPatch, "/api/v1/projects/" + created.Project.ID, `{"name":"x"}`},
		{http.MethodDelete, "/api/v1/projects/" + created.Project.ID, ""},
	} {
		if code := doWithTeam(tc.method, tc.path, tc.body, viewerBearer).Code; code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", tc.method, tc.path, code)
		}
	}

	// A read-scoped API token reads but cannot mutate: the scope boundary
	// answers 403 with "insufficient scope".
	tokens := auth.NewAPITokenService(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	minted, err := tokens.Create(ctx, ownerID, "probe-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("mint read token: %v", err)
	}
	tokenBearer := "Bearer " + minted.Token
	if code := doWithTeam(http.MethodGet, "/api/v1/projects", "", tokenBearer).Code; code != http.StatusOK {
		t.Errorf("read token list = %d, want 200", code)
	}
	rec = doTeamRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Token"}`, tokenBearer, teamHeader)
	if rec.Code != http.StatusForbidden {
		t.Errorf("read token create = %d, want 403", rec.Code)
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token 403 body: %v", err)
	}
	if body.Message != "insufficient scope" {
		t.Errorf("token 403 body = %q, want insufficient scope", body.Message)
	}
}

// doTeamRequest runs one request against the server handler with the
// X-Team-Id header set, selecting the request's active team.
func doTeamRequest(t *testing.T, s *Server, method, path, body, authorization, teamID string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set(TeamHeader, teamID)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}
