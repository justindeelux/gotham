package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

type stubServerRegistry struct {
	ServerService
	registry *servers.Server
	dialed   bool
}

func (s *stubServerRegistry) Get(context.Context, uuid.UUID) (*servers.Server, error) {
	return s.registry, nil
}

func (s *stubServerRegistry) DialTerminalClient(_ context.Context, _ uuid.UUID, _ ...servers.DockerDialOption) (*servers.TerminalClient, error) {
	s.dialed = true
	return nil, errors.New("not used in this test")
}

type stubAuthService struct {
	AuthService
	user     *auth.User
	sessions []auth.SessionInfo
}

func (s stubAuthService) Me(context.Context, uuid.UUID) (*auth.User, error) {
	if s.user == nil {
		return nil, errors.New("not found")
	}
	return s.user, nil
}

func (s stubAuthService) ListSessions(context.Context, uuid.UUID, uuid.UUID) ([]auth.SessionInfo, error) {
	return s.sessions, nil
}

type rotatingAuth struct {
	AuthService

	mu         sync.Mutex
	user       *auth.User
	sessions   []auth.SessionInfo
	lineages   map[uuid.UUID]time.Time
	meErr      error
	listErr    error
	lineageErr error
}

func (r *rotatingAuth) Me(context.Context, uuid.UUID) (*auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.meErr != nil {
		return nil, r.meErr
	}
	if r.user == nil {
		return nil, auth.ErrUnauthorized
	}
	return r.user, nil
}

func (r *rotatingAuth) ListSessions(context.Context, uuid.UUID, uuid.UUID) ([]auth.SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]auth.SessionInfo, len(r.sessions))
	copy(out, r.sessions)
	return out, nil
}

func (r *rotatingAuth) SessionSignInAt(_ context.Context, _ uuid.UUID, sessionID uuid.UUID) (time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lineageErr != nil {
		return time.Time{}, false, r.lineageErr
	}
	signInAt, ok := r.lineages[sessionID]
	return signInAt, ok, nil
}

func (r *rotatingAuth) rotate(oldID, newID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lineages == nil {
		r.lineages = make(map[uuid.UUID]time.Time)
	}
	for i := range r.sessions {
		if r.sessions[i].ID == oldID {
			r.lineages[oldID] = r.sessions[i].CreatedAt
			r.sessions[i].ID = newID
			r.lineages[newID] = r.sessions[i].CreatedAt
			return
		}
	}
}

func (r *rotatingAuth) logout(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.sessions[:0]
	for _, session := range r.sessions {
		if session.ID != id {
			kept = append(kept, session)
		}
	}
	r.sessions = kept
	delete(r.lineages, id)
}

func (r *rotatingAuth) revokeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = nil
}

func (r *rotatingAuth) addSession(id uuid.UUID, createdAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, auth.SessionInfo{ID: id, CreatedAt: createdAt})
	if r.lineages == nil {
		r.lineages = make(map[uuid.UUID]time.Time)
	}
	r.lineages[id] = createdAt
}

func (r *rotatingAuth) setMeErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meErr = err
}

func (r *rotatingAuth) setListErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listErr = err
}

func (r *rotatingAuth) setLineageErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lineageErr = err
}

func newRotatingAuthorizer(t *testing.T, user *auth.User, signIn time.Time) (*rotatingAuth, terminalAuthorizer, uuid.UUID) {
	t.Helper()
	sessionID := uuid.New()
	store := &rotatingAuth{
		user:     user,
		sessions: []auth.SessionInfo{{ID: sessionID, CreatedAt: signIn}},
		lineages: map[uuid.UUID]time.Time{sessionID: signIn},
	}
	return store, terminalAuthorizer{auth: store}, sessionID
}

func authorizerClaims(userID, sessionID uuid.UUID) *auth.Claims {
	return &auth.Claims{
		SessionID:        sessionID.String(),
		Role:             auth.RoleOwner,
		RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()},
	}
}

func TestTerminalAuthorizerSurvivesSessionRotation(t *testing.T) {
	userID := uuid.New()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	sessionID := uuid.New()
	replacementID := uuid.New()
	store := &rotatingAuth{
		user:     &auth.User{Email: "op@example.com", Role: auth.RoleOwner},
		sessions: []auth.SessionInfo{{ID: sessionID, CreatedAt: signIn}},
	}
	authorizer := terminalAuthorizer{auth: store}
	claims := authorizerClaims(userID, sessionID)

	marker, err := authorizer.Authorize(context.Background(), claims, hostTarget())
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !marker.SignInAt.Equal(signIn) {
		t.Fatalf("marker = %s, want %s", marker.SignInAt, signIn)
	}

	store.rotate(sessionID, replacementID)
	ok, err := authorizer.Revalidate(context.Background(), claims, marker, hostTarget())
	if err != nil || !ok {
		t.Fatalf("after rotation = (%v, %v), want (true, nil)", ok, err)
	}

	store.addSession(uuid.New(), signIn.Add(time.Hour))
	store.logout(replacementID)
	ok, err = authorizer.Revalidate(context.Background(), claims, marker, hostTarget())
	if err != nil || ok {
		t.Fatalf("after logout = (%v, %v), want (false, nil)", ok, err)
	}
	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a logged-out lineage authorized attach while another sign-in was live")
	}
}

func TestTerminalAuthorizerAcceptsPreRotationToken(t *testing.T) {
	userID := uuid.New()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	store, authorizer, oldSessionID := newRotatingAuthorizer(t,
		&auth.User{Email: "op@example.com", Role: auth.RoleOwner}, signIn)
	claims := authorizerClaims(userID, oldSessionID)

	replacementID := uuid.New()
	store.rotate(oldSessionID, replacementID)

	marker, err := authorizer.Authorize(context.Background(), claims, hostTarget())
	if err != nil {
		t.Fatalf("Authorize with a pre-rotation token: %v", err)
	}
	if !marker.SignInAt.Equal(signIn) {
		t.Fatalf("marker = %s, want %s", marker.SignInAt, signIn)
	}
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, hostTarget()); err != nil || !ok {
		t.Fatalf("revalidate after attach = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestTerminalAuthorizerRefusesPreRotationTokenOfRevokedFamily(t *testing.T) {
	userID := uuid.New()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	store, authorizer, oldSessionID := newRotatingAuthorizer(t,
		&auth.User{Email: "op@example.com", Role: auth.RoleOwner}, signIn)
	claims := authorizerClaims(userID, oldSessionID)
	store.rotate(oldSessionID, uuid.New())
	store.revokeAll()

	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a revoked family authorized a pre-rotation token")
	}
}

func TestTerminalAuthorizerRefusesPreRotationTokenOfDeletedRow(t *testing.T) {
	userID := uuid.New()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	store, authorizer, sessionID := newRotatingAuthorizer(t,
		&auth.User{Email: "op@example.com", Role: auth.RoleOwner}, signIn)
	claims := authorizerClaims(userID, sessionID)
	store.logout(sessionID)

	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a deleted session row authorized attach")
	}
}

func TestTerminalAuthorizerLineageLookupFailure(t *testing.T) {
	userID := uuid.New()
	signIn := time.Now().UTC().Truncate(time.Microsecond)
	store, authorizer, sessionID := newRotatingAuthorizer(t,
		&auth.User{Email: "op@example.com", Role: auth.RoleOwner}, signIn)
	claims := authorizerClaims(userID, sessionID)
	store.rotate(sessionID, uuid.New())
	store.setLineageErr(errors.New("database is down"))

	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a transient lineage lookup failure was treated as success")
	}
}

func TestTerminalAuthorizerTransientErrors(t *testing.T) {
	user := &auth.User{Email: "op@example.com", Role: auth.RoleOwner}
	store, authorizer, sessionID := newRotatingAuthorizer(t, user, time.Now().UTC())
	claims := authorizerClaims(uuid.New(), sessionID)
	marker, err := authorizer.Authorize(context.Background(), claims, hostTarget())
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	store.setMeErr(errors.New("database is down"))
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, hostTarget()); err == nil || ok {
		t.Fatalf("transient Me error = (%v, %v), want (false, err)", ok, err)
	}
	store.setMeErr(auth.ErrUnauthorized)
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, hostTarget()); err != nil || ok {
		t.Fatalf("deleted account = (%v, %v), want (false, nil)", ok, err)
	}
	store.setMeErr(nil)

	store.setListErr(errors.New("database is down"))
	if ok, err := authorizer.Revalidate(context.Background(), claims, marker, hostTarget()); err == nil || ok {
		t.Fatalf("transient ListSessions error = (%v, %v), want (false, err)", ok, err)
	}
}

func TestTerminalAuthorizerRefusesTokenWithoutSID(t *testing.T) {
	store, authorizer, _ := newRotatingAuthorizer(t, &auth.User{Email: "op@example.com", Role: auth.RoleOwner}, time.Now().UTC())
	_ = store
	claims := &auth.Claims{
		Role:             auth.RoleOwner,
		RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()},
	}
	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a token without a sid was authorized")
	}
}

func TestTerminalAuthorizerRejectsRevokedOrUnknownSession(t *testing.T) {
	store, authorizer, sessionID := newRotatingAuthorizer(t, &auth.User{Email: "op@example.com", Role: auth.RoleOwner}, time.Now().UTC())
	claims := authorizerClaims(uuid.New(), sessionID)
	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	store.logout(sessionID)
	if _, err := authorizer.Authorize(context.Background(), claims, hostTarget()); err == nil {
		t.Fatal("a logged-out session was authorized")
	}
}

func TestTerminalAuthorizerUsesCurrentAccountRole(t *testing.T) {
	now := time.Now().UTC()

	demotedClaims := authorizerClaims(uuid.New(), uuid.New())
	demoted, err := (&Server{auth: &rotatingAuth{
		user:     &auth.User{Email: "user@example.com", Role: auth.RoleMember},
		sessions: []auth.SessionInfo{{ID: mustUUID(demotedClaims.SessionID), CreatedAt: now}},
	}}).terminalAuthorizer().Authorize(context.Background(), demotedClaims, hostTarget())
	if err == nil {
		t.Fatalf("demoted account authorized with marker %+v", demoted)
	}

	promotedClaims := authorizerClaims(uuid.New(), uuid.New())
	promotedClaims.Role = "member"
	if _, err := (&Server{auth: &rotatingAuth{
		user:     &auth.User{Email: "user@example.com", Role: auth.RoleOwner},
		sessions: []auth.SessionInfo{{ID: mustUUID(promotedClaims.SessionID), CreatedAt: now}},
	}}).terminalAuthorizer().Authorize(context.Background(), promotedClaims, hostTarget()); err != nil {
		t.Fatalf("promoted account refused: %v", err)
	}
}

func hostTarget() terminal.Target { return terminal.Target{Kind: terminal.TargetHost} }

func mustUUID(value string) uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		panic(err)
	}
	return id
}

func TestTerminalAuthorizerRejectsBadClaims(t *testing.T) {
	now := time.Now().UTC()
	claims := authorizerClaims(uuid.New(), uuid.New())
	server := &Server{auth: &rotatingAuth{
		user:     &auth.User{Email: "ops@example.com", Role: auth.RoleOwner},
		sessions: []auth.SessionInfo{{ID: mustUUID(claims.SessionID), CreatedAt: now}},
	}}
	if _, err := server.terminalAuthorizer().Authorize(context.Background(), nil, hostTarget()); err == nil {
		t.Fatal("nil claims authorized")
	}
	bad := authorizerClaims(uuid.New(), uuid.New())
	bad.Subject = "not-a-uuid"
	if _, err := server.terminalAuthorizer().Authorize(context.Background(), bad, hostTarget()); err == nil {
		t.Fatal("invalid subject authorized")
	}
}

func TestTerminalRoutesMountOnlyWithAuthAndRegistry(t *testing.T) {
	serverID := uuid.New()
	s := newTestServer(t, stubPinger{}, stubPinger{})
	s.auth = stubAuthService{user: &auth.User{Email: "ops@example.com"}}
	s.servers = &stubServerRegistry{registry: &servers.Server{ID: serverID}}

	router, err := s.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	if s.terminal == nil {
		t.Fatal("terminal service was not built")
	}

	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/servers/"+serverID.String()+"/terminal/sessions",
		http.NoBody)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", recorder.Code)
	}
}
