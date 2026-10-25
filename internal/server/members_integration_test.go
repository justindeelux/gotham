package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
)

func pgUUIDForTest(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

type inviteCreateResponse struct {
	Invite struct {
		ID       uuid.UUID `json:"id"`
		Email    string    `json:"email"`
		Projects []struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"projects"`
	} `json:"invite"`
	Token string `json:"token"`
}

func createInvite(t *testing.T, s *Server, ownerBearer, email string, projectIDs ...uuid.UUID) inviteCreateResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"email": email, "project_ids": projectIDs})
	if err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, s, http.MethodPost, "/api/v1/invites", string(body), ownerBearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite = %d: %s", rec.Code, rec.Body.String())
	}
	var created inviteCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	return created
}

func TestInviteEndpointsCreateMemberWithGrants(t *testing.T) {
	f := newAccessFixture(t)
	ownerBearer := "Bearer " + f.owner.AccessToken
	email := uuid.NewString() + "@example.test"

	created := createInvite(t, f.server, ownerBearer, email, f.projects[1])
	if created.Token == "" || created.Invite.Email != email {
		t.Fatalf("created invite = %+v", created)
	}
	if len(created.Invite.Projects) != 1 || created.Invite.Projects[0].ID != f.projects[1] {
		t.Fatalf("invite projects = %+v", created.Invite.Projects)
	}

	rec := doRequest(t, f.server, http.MethodGet, "/api/v1/auth/invites/validate?token="+created.Token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("validate = %d: %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || preview.Email != email {
		t.Fatalf("preview = %+v, %v", preview, err)
	}

	rec = doRequest(t, f.server, http.MethodGet, "/api/v1/invites", "", ownerBearer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), email) {
		t.Fatalf("list invites = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, f.server, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+email+`","password":"member-password","invite_token":"`+created.Token+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register invited = %d: %s", rec.Code, rec.Body.String())
	}
	var registered struct {
		User struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"user"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if registered.User.Role != auth.RoleMember {
		t.Fatalf("invited role = %q, want member", registered.User.Role)
	}
	memberBearer := "Bearer " + registered.AccessToken
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[1].String(), "", memberBearer).Code; code != http.StatusOK {
		t.Fatalf("granted project = %d, want 200", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[0].String(), "", memberBearer).Code; code != http.StatusNotFound {
		t.Fatalf("ungranted project = %d, want 404", code)
	}

	rec = doRequest(t, f.server, http.MethodGet, "/api/v1/users", "", ownerBearer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), registered.User.ID) ||
		!strings.Contains(rec.Body.String(), f.projects[1].String()) {
		t.Fatalf("list users = %d: %s", rec.Code, rec.Body.String())
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/users", "", memberBearer).Code; code != http.StatusNotFound {
		t.Fatalf("member list users = %d, want 404", code)
	}
}

func TestRemoveMemberRevokesSessionsTokensAndGrants(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	ctx := context.Background()
	memberID := uuid.MustParse(f.member.User.ID)
	ownerBearer := "Bearer " + f.owner.AccessToken

	token, err := f.server.tokens.Create(ctx, memberID, "member-token", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}

	live := httptest.NewServer(f.server.Handler())
	t.Cleanup(live.Close)
	connection, err := websocket.Dial("ws"+strings.TrimPrefix(live.URL, "http")+"/api/v1/ws?token="+f.member.AccessToken+"&channel=tasks:"+f.projects[0].String(), "", live.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	var frame map[string]any
	if err := websocket.JSON.Receive(connection, &frame); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Receive(connection, &frame); err != nil {
		t.Fatal(err)
	}
	if frame["type"] != "subscribed" {
		t.Fatalf("subscription frame = %v", frame)
	}

	rec := doRequest(t, f.server, http.MethodDelete, "/api/v1/users/"+memberID.String(), "", ownerBearer)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member = %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := f.store.GetUserByID(ctx, pgUUIDForTest(memberID)); err == nil {
		t.Fatal("member account still exists")
	}
	var sessions, tokens int
	if err := f.store.DB.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, memberID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB.QueryRow(ctx, `SELECT count(*) FROM api_tokens WHERE user_id=$1`, memberID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || tokens != 0 {
		t.Fatalf("removed member left sessions=%d tokens=%d", sessions, tokens)
	}
	if allowed, err := f.store.HasProjectGrant(ctx, memberID, f.projects[0]); err != nil || allowed {
		t.Fatalf("removed member grant = %v, %v", allowed, err)
	}
	if _, err := f.server.tokens.Authenticate(ctx, token.Token); err == nil {
		t.Fatal("removed member API token still authenticates")
	}
	if _, err := f.server.auth.Refresh(ctx, f.member.RefreshToken, auth.SessionMeta{}); err == nil {
		t.Fatal("removed member refresh token still works")
	}
	if err := websocket.JSON.Receive(connection, &frame); err == nil {
		t.Fatalf("removed member socket stayed open: %v", frame)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[0].String(), "", "Bearer "+f.member.AccessToken).Code; code != http.StatusNotFound {
		t.Fatalf("removed member request = %d, want 404", code)
	}
}

func TestOwnerCannotBeRemovedResetOrRegranted(t *testing.T) {
	f := newAccessFixture(t)
	ownerID := f.owner.User.ID
	ownerBearer := "Bearer " + f.owner.AccessToken

	if code := doRequest(t, f.server, http.MethodDelete, "/api/v1/users/"+ownerID, "", ownerBearer).Code; code != http.StatusForbidden {
		t.Fatalf("remove owner = %d, want 403", code)
	}
	if code := doRequest(t, f.server, http.MethodPost, "/api/v1/users/"+ownerID+"/reset", "", ownerBearer).Code; code != http.StatusForbidden {
		t.Fatalf("reset owner = %d, want 403", code)
	}
	if code := doRequest(t, f.server, http.MethodPut, "/api/v1/users/"+ownerID+"/projects",
		`{"project_ids":["`+f.projects[0].String()+`"]}`, ownerBearer).Code; code != http.StatusForbidden {
		t.Fatalf("regrant owner = %d, want 403", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/auth/me", "", ownerBearer).Code; code != http.StatusOK {
		t.Fatalf("owner session after refusals = %d, want 200", code)
	}
	if _, err := f.store.GetUserByID(context.Background(), pgUUIDForTest(uuid.MustParse(ownerID))); err != nil {
		t.Fatalf("owner account missing: %v", err)
	}
}

func TestMemberProjectGrantsCanBeReplaced(t *testing.T) {
	f := newAccessFixture(t)
	ownerBearer := "Bearer " + f.owner.AccessToken
	memberID := f.member.User.ID

	rec := doRequest(t, f.server, http.MethodPut, "/api/v1/users/"+memberID+"/projects",
		`{"project_ids":["`+f.projects[1].String()+`"]}`, ownerBearer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), f.projects[1].String()) {
		t.Fatalf("set grants = %d: %s", rec.Code, rec.Body.String())
	}
	memberBearer := "Bearer " + f.member.AccessToken
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[1].String(), "", memberBearer).Code; code != http.StatusOK {
		t.Fatalf("new grant = %d, want 200", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[0].String(), "", memberBearer).Code; code != http.StatusNotFound {
		t.Fatalf("old grant = %d, want 404", code)
	}
	if code := doRequest(t, f.server, http.MethodPut, "/api/v1/users/"+memberID+"/projects",
		`{"project_ids":[]}`, ownerBearer).Code; code != http.StatusOK {
		t.Fatalf("clear grants = %d, want 200", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[1].String(), "", memberBearer).Code; code != http.StatusNotFound {
		t.Fatalf("cleared grant = %d, want 404", code)
	}
	if code := doRequest(t, f.server, http.MethodPut, "/api/v1/users/"+memberID+"/projects", `{}`, ownerBearer).Code; code != http.StatusBadRequest {
		t.Fatalf("missing project_ids = %d, want 400", code)
	}
}

func TestResetMemberDropsSessionsAndTOTP(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	memberID := uuid.MustParse(f.member.User.ID)
	ownerBearer := "Bearer " + f.owner.AccessToken

	if _, err := f.store.DB.Exec(ctx, `INSERT INTO user_totp (user_id, secret_enc, enabled) VALUES ($1,'sealed',true)`, memberID); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, f.server, http.MethodPost, "/api/v1/users/"+memberID.String()+"/reset", "", ownerBearer)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset member = %d: %s", rec.Code, rec.Body.String())
	}
	var sessions, totp int
	if err := f.store.DB.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, memberID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB.QueryRow(ctx, `SELECT count(*) FROM user_totp WHERE user_id=$1`, memberID).Scan(&totp); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || totp != 0 {
		t.Fatalf("reset left sessions=%d totp=%d", sessions, totp)
	}
	if _, err := f.server.auth.Refresh(ctx, f.member.RefreshToken, auth.SessionMeta{}); err == nil {
		t.Fatal("reset member refresh token still works")
	}
	if code := doRequest(t, f.server, http.MethodPost, "/api/v1/users/"+memberID.String()+"/reset", "", ownerBearer).Code; code != http.StatusNoContent {
		t.Fatalf("second reset = %d, want idempotent 204", code)
	}
}

func TestRegistrationStaysClosedAfterOwner(t *testing.T) {
	f := newAccessFixture(t)
	rec := doRequest(t, f.server, http.MethodGet, "/api/v1/auth/config", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"registrationOpen":false`) {
		t.Fatalf("auth config = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, f.server, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+uuid.NewString()+`@example.test","password":"member-password"}`, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("closed registration = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
