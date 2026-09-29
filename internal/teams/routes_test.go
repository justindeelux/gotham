package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// testRouter mounts the team routes for one authenticated user. The auth
// middleware is a pass-through: authentication itself is RequireAuth's job and
// is covered by the server package's tests.
func testRouter(svc *Service, userID uuid.UUID) http.Handler {
	r := chi.NewRouter()
	pass := func(next http.Handler) http.Handler { return next }
	accessor := func(_ context.Context) (uuid.UUID, bool) { return userID, userID != uuid.Nil }
	Mount(r, pass, accessor, svc)
	return r
}

// request drives one JSON request through h and returns the recorder.
func request(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals a response body into a generic map.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestRoutesCreateListAndGetTeam(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	repo.seedUser(userID, "user@example.com")
	svc := newTestService(repo)
	h := testRouter(svc, userID)

	rec := request(t, h, http.MethodPost, "/v1/teams", map[string]string{"name": "Platform"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body %s", rec.Code, rec.Body)
	}
	created := decode(t, rec)["team"].(map[string]any)
	teamID := created["id"].(string)
	if created["role"] != string(RoleOwner) || created["is_personal"] != false {
		t.Fatalf("created team = %+v", created)
	}

	rec = request(t, h, http.MethodGet, "/v1/teams", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if teams := decode(t, rec)["teams"].([]any); len(teams) != 1 {
		t.Fatalf("teams = %v, want the created team", teams)
	}

	rec = request(t, h, http.MethodGet, "/v1/teams/"+teamID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}

	rec = request(t, h, http.MethodPatch, "/v1/teams/"+teamID, map[string]string{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d, body %s", rec.Code, rec.Body)
	}
	if team := decode(t, rec)["team"].(map[string]any); team["name"] != "Renamed" {
		t.Fatalf("renamed team = %+v", team)
	}

	rec = request(t, h, http.MethodDelete, "/v1/teams/"+teamID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, body %s", rec.Code, rec.Body)
	}
}

func TestRoutesNotFoundAndUnauthorized(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	repo.seedUser(userID, "user@example.com")
	svc := newTestService(repo)
	h := testRouter(svc, userID)

	rec := request(t, h, http.MethodGet, "/v1/teams/"+uuid.New().String(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown team = %d, want 404", rec.Code)
	}
	rec = request(t, h, http.MethodGet, "/v1/teams/not-a-uuid", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad team id = %d, want 400", rec.Code)
	}
	rec = request(t, h, http.MethodPost, "/v1/teams", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing body = %d, want 400", rec.Code)
	}

	unauthenticated := testRouter(svc, uuid.Nil)
	rec = request(t, unauthenticated, http.MethodGet, "/v1/teams", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", rec.Code)
	}
}

func TestRoutesReadOnlyIsForbiddenOnMutations(t *testing.T) {
	owner, viewer := uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(viewer, "viewer@example.com")
	teamID := seedTeam(t, repo, owner, map[uuid.UUID]Role{viewer: RoleReadOnly})
	svc := newTestService(repo)
	h := testRouter(svc, viewer)

	if rec := request(t, h, http.MethodGet, "/v1/teams/"+teamID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("read_only get = %d, want 200", rec.Code)
	}
	if rec := request(t, h, http.MethodGet, "/v1/teams/"+teamID.String()+"/members", nil); rec.Code != http.StatusOK {
		t.Fatalf("read_only members = %d, want 200", rec.Code)
	}
	cases := []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodPatch, path: "/v1/teams/" + teamID.String(), body: map[string]string{"name": "x"}},
		{method: http.MethodDelete, path: "/v1/teams/" + teamID.String()},
		{method: http.MethodPost, path: "/v1/teams/" + teamID.String() + "/invites", body: map[string]string{"email": "a@example.com", "role": "admin"}},
		{method: http.MethodPatch, path: "/v1/teams/" + teamID.String() + "/members/" + owner.String(), body: map[string]string{"role": "admin"}},
	}
	for _, tc := range cases {
		if rec := request(t, h, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRoutesInviteAndAccept(t *testing.T) {
	owner, invitee := uuid.New(), uuid.New()
	repo := newFakeRepository()
	repo.seedUser(owner, "owner@example.com")
	repo.seedUser(invitee, "invitee@example.com")
	teamID := seedTeam(t, repo, owner, nil)
	svc := newTestService(repo)
	ownerRouter := testRouter(svc, owner)
	inviteeRouter := testRouter(svc, invitee)

	rec := request(t, ownerRouter, http.MethodPost, "/v1/teams/"+teamID.String()+"/invites",
		map[string]string{"email": "invitee@example.com", "role": "read_only"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite = %d, body %s", rec.Code, rec.Body)
	}
	invite := decode(t, rec)["invite"].(map[string]any)
	token, _ := invite["token"].(string)
	if token == "" {
		t.Fatalf("invite response carries no token: %+v", invite)
	}
	if acceptURL, _ := invite["accept_url"].(string); acceptURL != "/v1/invites/"+token+"/accept" {
		t.Fatalf("accept_url = %q", acceptURL)
	}
	if _, leaked := invite["token_hash"]; leaked {
		t.Fatal("the response must not carry the token hash")
	}

	// The invite is listed for the team.
	rec = request(t, ownerRouter, http.MethodGet, "/v1/teams/"+teamID.String()+"/invites", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list invites = %d", rec.Code)
	}
	if invites := decode(t, rec)["invites"].([]any); len(invites) != 1 {
		t.Fatalf("invites = %v, want one", invites)
	}

	// A different account cannot accept it.
	rec = request(t, ownerRouter, http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign accept = %d, want 403", rec.Code)
	}

	// The invited account joins with the invited role.
	rec = request(t, inviteeRouter, http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept = %d, body %s", rec.Code, rec.Body)
	}
	if team := decode(t, rec)["team"].(map[string]any); team["role"] != string(RoleReadOnly) {
		t.Fatalf("accepted team = %+v", team)
	}
	// A replay is refused.
	rec = request(t, inviteeRouter, http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("replayed accept = %d, want 409", rec.Code)
	}
	// The membership is visible to the owner.
	rec = request(t, ownerRouter, http.MethodGet, "/v1/teams/"+teamID.String()+"/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("members = %d", rec.Code)
	}
	if members := decode(t, rec)["members"].([]any); len(members) != 2 {
		t.Fatalf("members = %v, want the owner and the invitee", members)
	}
}

func TestRoutesPersonalTeamDeleteIsRefused(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	repo.seedUser(userID, "user@example.com")
	repo.seedTeam(Team{ID: PersonalTeamID(userID), Name: "user@example.com's team", IsPersonal: true}, userID)
	svc := newTestService(repo)
	h := testRouter(svc, userID)

	rec := request(t, h, http.MethodDelete, "/v1/teams/"+PersonalTeamID(userID).String(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete personal = %d, want 409, body %s", rec.Code, rec.Body)
	}
}

func TestRoutesAreUnmountedWhenTheFeatureIsOff(t *testing.T) {
	t.Setenv(FeatureEnv, "false")

	repo := newFakeRepository()
	userID := uuid.New()
	repo.seedUser(userID, "user@example.com")
	svc := newTestService(repo)
	h := testRouter(svc, userID)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/teams"},
		{method: http.MethodGet, path: "/v1/teams"},
		{method: http.MethodPost, path: "/v1/invites/sometoken/accept"},
	} {
		if rec := request(t, h, tc.method, tc.path, map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want the routes unmounted (404)", tc.method, tc.path, rec.Code)
		}
	}
}
