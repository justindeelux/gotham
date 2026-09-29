package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/teams"
)

// fakeTeamService is the middleware's narrow seam: only Membership is
// exercised, every other method reports an unimplemented call.
type fakeTeamService struct {
	roles map[[2]uuid.UUID]teams.Role
}

func newFakeTeamService() *fakeTeamService {
	return &fakeTeamService{roles: map[[2]uuid.UUID]teams.Role{}}
}

func (f *fakeTeamService) allow(teamID, userID uuid.UUID, role teams.Role) {
	f.roles[[2]uuid.UUID{teamID, userID}] = role
}

func (f *fakeTeamService) Membership(_ context.Context, teamID, userID uuid.UUID) (teams.Role, error) {
	role, ok := f.roles[[2]uuid.UUID{teamID, userID}]
	if !ok {
		return "", teams.ErrNotFound
	}
	return role, nil
}

func (f *fakeTeamService) Create(context.Context, uuid.UUID, string) (teams.Team, error) {
	return teams.Team{}, errors.New("not implemented")
}

func (f *fakeTeamService) List(context.Context, uuid.UUID) ([]teams.Team, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTeamService) Get(context.Context, uuid.UUID, uuid.UUID) (teams.Team, error) {
	return teams.Team{}, errors.New("not implemented")
}

func (f *fakeTeamService) Rename(context.Context, uuid.UUID, uuid.UUID, string) (teams.Team, error) {
	return teams.Team{}, errors.New("not implemented")
}

func (f *fakeTeamService) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeTeamService) Members(context.Context, uuid.UUID, uuid.UUID) ([]teams.Member, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTeamService) SetMemberRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, teams.Role) (teams.Member, error) {
	return teams.Member{}, errors.New("not implemented")
}

func (f *fakeTeamService) RemoveMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeTeamService) Invites(context.Context, uuid.UUID, uuid.UUID) ([]teams.Invite, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTeamService) Invite(context.Context, uuid.UUID, uuid.UUID, string, teams.Role) (teams.Invite, string, error) {
	return teams.Invite{}, "", errors.New("not implemented")
}

func (f *fakeTeamService) RevokeInvite(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeTeamService) Accept(context.Context, uuid.UUID, string) (teams.Team, error) {
	return teams.Team{}, errors.New("not implemented")
}

// newTeamTestServer builds a Server whose active-team middleware runs against
// the injected service, with no database behind it.
func newTeamTestServer(t *testing.T, svc teams.TeamService) *Server {
	t.Helper()
	cfg := &config.Config{Values: config.Values{Server: config.Server{Addr: "127.0.0.1"}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, newFakeAuthService(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.teamService = svc
	return s
}

// scopeHandler reports the scope the middleware resolved as JSON.
func scopeHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := teams.FromContext(r.Context())
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]string{"active": "false"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"user":   scope.UserID.String(),
			"team":   scope.TeamID.String(),
			"role":   string(scope.Role),
			"active": "true",
		})
	})
}

// serveTeam runs one request through RequireTeam (and optionally the write
// gate) with userID already authenticated, which is what RequireAuth stores.
func serveTeam(t *testing.T, s *Server, userID uuid.UUID, header, method string, next http.Handler) (*httptest.ResponseRecorder, map[string]string) {
	t.Helper()
	handler := s.teamWriteGate(next)
	handler = s.RequireTeam(handler)
	req := httptest.NewRequest(method, "/api/v1/databases", nil)
	if header != "" {
		req.Header.Set(TeamHeader, header)
	}
	if userID != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := map[string]string{}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode scope body %q: %v", rec.Body.String(), err)
		}
	}
	return rec, body
}

func TestRequireTeamFallsBackToPersonalTeam(t *testing.T) {
	userID := uuid.New()
	s := newTeamTestServer(t, newFakeTeamService())

	rec, body := serveTeam(t, s, userID, "", http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body["team"] != userID.String() || body["role"] != string(teams.RoleOwner) || body["active"] != "true" {
		t.Fatalf("scope = %+v, want the caller's personal team as owner", body)
	}
}

func TestRequireTeamResolvesMembership(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := newFakeTeamService()
	svc.allow(teamID, userID, teams.RoleReadOnly)
	s := newTeamTestServer(t, svc)

	rec, body := serveTeam(t, s, userID, teamID.String(), http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("member status = %d", rec.Code)
	}
	if body["team"] != teamID.String() || body["role"] != string(teams.RoleReadOnly) {
		t.Fatalf("scope = %+v, want the named team with the member role", body)
	}

	rec, _ = serveTeam(t, s, userID, uuid.New().String(), http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member status = %d, want 403", rec.Code)
	}

	rec, _ = serveTeam(t, s, userID, "not-a-uuid", http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad header status = %d, want 400", rec.Code)
	}
}

func TestRequireTeamWithoutResolverLeavesScopeAbsent(t *testing.T) {
	s := newTeamTestServer(t, nil)

	rec, body := serveTeam(t, s, uuid.New(), uuid.New().String(), http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body["active"] != "false" {
		t.Fatalf("scope = %+v, want no team context when no resolver is wired", body)
	}
}

func TestRequireTeamNeedsAnAuthenticatedUser(t *testing.T) {
	s := newTeamTestServer(t, newFakeTeamService())

	rec, _ := serveTeam(t, s, uuid.Nil, "", http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireTeamIgnoresTheHeaderWhenTheFeatureIsOff(t *testing.T) {
	t.Setenv(teams.FeatureEnv, "false")

	userID, teamID := uuid.New(), uuid.New()
	svc := newFakeTeamService()
	svc.allow(teamID, userID, teams.RoleAdmin)
	s := newTeamTestServer(t, svc)

	rec, body := serveTeam(t, s, userID, teamID.String(), http.MethodGet, scopeHandler(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body["team"] != userID.String() || body["role"] != string(teams.RoleOwner) {
		t.Fatalf("scope = %+v, want the personal team when FEATURE_TEAMS is off", body)
	}
}

func TestTeamWriteGate(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := newFakeTeamService()

	cases := []struct {
		name   string
		role   teams.Role
		header string
		method string
		want   int
		setup  func()
	}{
		{name: "read_only reads", role: teams.RoleReadOnly, method: http.MethodGet, want: http.StatusOK},
		{name: "read_only cannot write", role: teams.RoleReadOnly, method: http.MethodDelete, want: http.StatusForbidden},
		{name: "read_only cannot create", role: teams.RoleReadOnly, method: http.MethodPost, want: http.StatusForbidden},
		{name: "admin writes", role: teams.RoleAdmin, method: http.MethodDelete, want: http.StatusOK},
		{name: "owner writes", role: teams.RoleOwner, method: http.MethodPost, want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc.allow(teamID, userID, tc.role)
			rec, _ := serveTeam(t, newTeamTestServer(t, svc), userID, teamID.String(), tc.method, scopeHandler(t))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestContainerRoutesRunTheTeamChain proves the nested container routes are
// mounted through the team chain and the service: a stranger's request never
// reaches the agent (404, not a 502 existence oracle), and a read_only member
// may list but not stop a container.
func TestContainerRoutesRunTheTeamChain(t *testing.T) {
	teamID := uuid.New()
	fake := newFakeTeamService()
	fake.allow(teamID, testUserID, teams.RoleReadOnly)
	serverFake := newFakeServerService()
	nodeID := seedServer(t, serverFake, "team-node")
	serverFake.setTeam(nodeID, teamID)
	s := newServerRoutesTestServer(t, serverFake)
	s.teamService = fake

	withHeader := func(method, path, header string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", authHeader)
		if header != "" {
			req.Header.Set(TeamHeader, header)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	containersPath := "/api/v1/servers/" + nodeID.String() + "/containers"

	// A stranger's own personal team does not own the node: 404, not the
	// agent-unavailable 502 that an existing node would produce.
	if rec := withHeader(http.MethodGet, containersPath, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger container list = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := withHeader(http.MethodGet, containersPath, uuid.New().String()); rec.Code != http.StatusForbidden {
		t.Fatalf("non-member team header = %d, want 403", rec.Code)
	}
	// A read_only member may list (the agent is unavailable in tests: 502
	// proves the request passed authorization) but not stop containers.
	if rec := withHeader(http.MethodGet, containersPath, teamID.String()); rec.Code != http.StatusBadGateway {
		t.Fatalf("read_only container list = %d, want 502 after authorization (body %s)", rec.Code, rec.Body.String())
	}
	if rec := withHeader(http.MethodPost, containersPath+"/abc/stop", teamID.String()); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only container stop = %d, want 403", rec.Code)
	}
	// A missing node answers exactly like the foreign one.
	if rec := withHeader(http.MethodGet, "/api/v1/servers/"+uuid.New().String()+"/containers", teamID.String()); rec.Code != http.StatusNotFound {
		t.Fatalf("missing node = %d, want 404", rec.Code)
	}
}

// TestResourceRoutesRunTheTeamChain proves the resource groups are mounted with
// the team middleware: the same request is answered differently depending on
// the X-Team-Id header, before any domain handler runs.
func TestResourceRoutesRunTheTeamChain(t *testing.T) {
	teamID := uuid.New()
	fake := newFakeTeamService()
	fake.allow(teamID, testUserID, teams.RoleReadOnly)
	s := newServerRoutesTestServer(t, newFakeServerService())
	s.teamService = fake

	withHeader := func(method, path, header string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", authHeader)
		if header != "" {
			req.Header.Set(TeamHeader, header)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	// No header: the personal team, exactly the pre-teams behavior.
	if rec := withHeader(http.MethodGet, "/api/v1/servers", ""); rec.Code != http.StatusOK {
		t.Fatalf("list without header = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// A team the caller is not a member of is refused.
	if rec := withHeader(http.MethodGet, "/api/v1/servers", uuid.New().String()); rec.Code != http.StatusForbidden {
		t.Fatalf("list with a foreign team = %d, want 403", rec.Code)
	}
	// A read_only member may read but not mutate.
	if rec := withHeader(http.MethodGet, "/api/v1/servers", teamID.String()); rec.Code != http.StatusOK {
		t.Fatalf("read_only list = %d, want 200", rec.Code)
	}
	if rec := withHeader(http.MethodPost, "/api/v1/servers", teamID.String()); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only create = %d, want 403", rec.Code)
	}
	// A malformed header is rejected before the handler.
	if rec := withHeader(http.MethodGet, "/api/v1/servers", "not-a-uuid"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad header = %d, want 400", rec.Code)
	}
}
