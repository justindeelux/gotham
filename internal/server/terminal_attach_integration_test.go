package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

const defaultTerminalTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func newTerminalRealAuthService(t *testing.T) *auth.Service {
	t.Helper()

	base := os.Getenv("GOTHAM_TEST_DSN")
	explicit := base != ""
	if !explicit {
		base = defaultTerminalTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	parsed, err := url.Parse(base)
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("parse test DSN: %v", err)
	}
	name := fmt.Sprintf("jus258_%d_%d", time.Now().UnixNano(), os.Getpid())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)

	parsed.Path = "/" + name
	dsn := parsed.String()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch store: %v", err)
	}
	t.Cleanup(pool.Close)

	signer, err := auth.NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return auth.New(store.New(pool), signer, logger)
}

func TestTerminalAttachRealStackSessionLineage(t *testing.T) {
	svc := newTerminalRealAuthService(t)
	ctx := context.Background()

	email := fmt.Sprintf("jus-258-%d@example.com", time.Now().UnixNano())
	register, err := svc.Register(ctx, email, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	oldClaims, err := svc.VerifyAccessToken(register.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken(old): %v", err)
	}
	authorizer := terminalAuthorizer{auth: svc}

	rotated, err := svc.Refresh(ctx, register.RefreshToken, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	marker, err := authorizer.Authorize(ctx, oldClaims, hostTarget())
	if err != nil {
		t.Fatalf("attach with the pre-rotation token: %v", err)
	}
	if ok, err := authorizer.Revalidate(ctx, oldClaims, marker, hostTarget()); err != nil || !ok {
		t.Fatalf("revalidate after rotation = (%v, %v), want (true, nil)", ok, err)
	}

	serverID := uuid.New()
	s := newTestServer(t, stubPinger{}, stubPinger{})
	s.auth = svc
	s.servers = &stubServerRegistry{registry: &servers.Server{ID: serverID}}
	router, err := s.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	live := httptest.NewServer(router)
	t.Cleanup(live.Close)
	createBody := bytes.NewBufferString(`{"target":"host","cols":80,"rows":24}`)
	createReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/servers/"+serverID.String()+"/terminal/sessions", createBody)
	createReq.Header.Set("Authorization", "Bearer "+register.AccessToken)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusUnauthorized {
		t.Fatalf("create with the pre-rotation token = %d, want 401", createRec.Code)
	}
	createReq = httptest.NewRequest(http.MethodPost,
		"/api/v1/servers/"+serverID.String()+"/terminal/sessions",
		bytes.NewBufferString(`{"target":"host","cols":80,"rows":24}`))
	createReq.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
	createRec = httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create with the rotated token = %d, want 201: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode create response: %v (%s)", err, createRec.Body.String())
	}
	attachPreflight := func() int {
		req, err := http.NewRequest(http.MethodGet,
			live.URL+"/api/v1/servers/"+serverID.String()+"/terminal/sessions/"+created.ID+"/ws", http.NoBody)
		if err != nil {
			t.Fatalf("attach request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
		resp, err := live.Client().Do(req)
		if err != nil {
			t.Fatalf("attach preflight: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := attachPreflight(); code != http.StatusBadRequest {
		t.Fatalf("attach preflight with the pre-rotation token = %d, want 400 (authorized, not upgraded)", code)
	}

	if _, err := svc.Login(ctx, email, "s3cret-password", auth.SessionMeta{}); err != nil {
		t.Fatalf("Login decoy: %v", err)
	}
	if err := svc.Logout(ctx, rotated.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := authorizer.Authorize(ctx, oldClaims, hostTarget()); err == nil {
		t.Fatal("a logged-out lineage attached while a decoy sign-in was live")
	}
	if code := attachPreflight(); code != http.StatusForbidden {
		t.Fatalf("attach preflight after logout = %d, want 403", code)
	}

	replay, err := svc.Login(ctx, email, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Login replay: %v", err)
	}
	replayClaims, err := svc.VerifyAccessToken(replay.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken(replay): %v", err)
	}
	if _, err := svc.Refresh(ctx, replay.RefreshToken, auth.SessionMeta{}); err != nil {
		t.Fatalf("Refresh replay: %v", err)
	}
	if _, err := svc.Refresh(ctx, replay.RefreshToken, auth.SessionMeta{}); err == nil {
		t.Fatal("replayed refresh token was accepted")
	}
	if _, err := authorizer.Authorize(ctx, replayClaims, hostTarget()); err == nil {
		t.Fatal("a pre-rotation token of a revoked family attached")
	}

	revoked, err := svc.Login(ctx, email, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Login revoked: %v", err)
	}
	revokedClaims, err := svc.VerifyAccessToken(revoked.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken(revoked): %v", err)
	}
	revokedSID, err := uuid.Parse(revokedClaims.SessionID)
	if err != nil {
		t.Fatalf("parse revoked sid: %v", err)
	}
	userID, err := uuid.Parse(revoked.User.ID)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	if err := svc.RevokeSession(ctx, userID, revokedSID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := authorizer.Authorize(ctx, revokedClaims, hostTarget()); err == nil {
		t.Fatal("a revoked session attached")
	}
}
