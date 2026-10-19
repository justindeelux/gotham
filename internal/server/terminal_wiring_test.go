package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
)

// stubServerRegistry satisfies the server registry for wiring tests. The
// embedded interface supplies the methods this test never calls.
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

func TestTerminalOperator(t *testing.T) {
	t.Setenv("PLATFORM_ADMINS", "ops@example.com")
	userID := uuid.New()
	sessionID := uuid.New()
	server := &Server{auth: stubAuthService{
		user:     &auth.User{Email: "ops@example.com", Role: "admin"},
		sessions: []auth.SessionInfo{{ID: sessionID}},
	}}

	claims := func(role string) *auth.Claims {
		return &auth.Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}}
	}
	claimsWithSession := func(role string, sid string) *auth.Claims {
		return &auth.Claims{Role: role, SessionID: sid, RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}}
	}
	withEmail := func(email string) *Server {
		return &Server{auth: stubAuthService{user: &auth.User{Email: email}}}
	}

	if server.terminalOperator(context.Background(), nil) {
		t.Fatal("nil claims passed the operator check")
	}
	if !server.terminalOperator(context.Background(), claims("admin")) {
		t.Fatal("admin role was refused")
	}
	if !server.terminalOperator(context.Background(), claims("member")) {
		t.Fatal("PLATFORM_ADMINS email was refused")
	}
	if withEmail("someone@example.com").terminalOperator(context.Background(), claims("member")) {
		t.Fatal("a non-operator email passed the operator check")
	}
	badSubject := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "not-a-uuid"}}
	if server.terminalOperator(context.Background(), badSubject) {
		t.Fatal("a token without a user id passed the operator check")
	}
	if !server.terminalOperator(context.Background(), claimsWithSession("admin", sessionID.String())) {
		t.Fatal("a live refresh session was refused")
	}
	if server.terminalOperator(context.Background(), claimsWithSession("admin", uuid.NewString())) {
		t.Fatal("a revoked/unknown refresh session passed the operator check")
	}
	if server.terminalOperator(context.Background(), claimsWithSession("admin", "not-a-uuid")) {
		t.Fatal("a malformed sid passed the operator check")
	}
	deletedUser := &Server{auth: stubAuthService{}}
	if deletedUser.terminalOperator(context.Background(), claims("admin")) {
		t.Fatal("a deleted account passed the operator check")
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

func TestTerminalOperatorUsesCurrentAccountRole(t *testing.T) {
	t.Setenv("PLATFORM_ADMINS", "")
	userID := uuid.New()
	claims := func(role string) *auth.Claims {
		return &auth.Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}}
	}

	// A demoted account must not keep operator access from the stale role claim
	// baked into the handshake token.
	demoted := &Server{auth: stubAuthService{user: &auth.User{Email: "user@example.com", Role: "member"}}}
	if demoted.terminalOperator(context.Background(), claims("admin")) {
		t.Fatal("a demoted account kept operator access via the token role claim")
	}

	// Conversely, a promoted account is accepted even when its handshake token
	// still carries the old member role.
	promoted := &Server{auth: stubAuthService{user: &auth.User{Email: "user@example.com", Role: "admin"}}}
	if !promoted.terminalOperator(context.Background(), claims("member")) {
		t.Fatal("a promoted account was refused because of its stale token role")
	}
}
