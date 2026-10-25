package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/projects/"+f.projects[0].String(), "", "Bearer "+f.member.AccessToken).Code; code != http.StatusUnauthorized {
		t.Fatalf("removed member request = %d, want 401", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/tokens", "", "Bearer "+f.member.AccessToken).Code; code != http.StatusUnauthorized {
		t.Fatalf("removed member personal route = %d, want 401", code)
	}
	if code := doRequest(t, f.server, http.MethodGet, "/api/v1/templates", "", "Bearer "+f.member.AccessToken).Code; code != http.StatusUnauthorized {
		t.Fatalf("removed member template route = %d, want 401", code)
	}
}

func TestRemoveMemberKeepsResourcesUnderOwner(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	memberID := uuid.MustParse(f.member.User.ID)
	ownerID := uuid.MustParse(f.owner.User.ID)
	ownerBearer := "Bearer " + f.owner.AccessToken

	appID, databaseID, serviceID := uuid.New(), uuid.New(), uuid.New()
	deploymentID, backupID := uuid.New(), uuid.New()
	var nodeID uuid.UUID
	if err := f.store.DB.QueryRow(ctx, `SELECT server_id FROM applications WHERE id=$1`, f.apps[0]).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO applications(id,user_id,server_id,environment_id,name) VALUES($1,$2,$3,$4,'member-app')`,
			[]any{appID, memberID, nodeID, f.environments[0]}},
		{`INSERT INTO databases(id,user_id,server_id,environment_id,name,engine) VALUES($1,$2,$3,$4,'member-db','postgres')`,
			[]any{databaseID, memberID, nodeID, f.environments[0]}},
		{`INSERT INTO services(id,user_id,server_id,environment_id,name,compose_yaml) VALUES($1,$2,$3,$4,'member-service','services: {}')`,
			[]any{serviceID, memberID, nodeID, f.environments[0]}},
		{`INSERT INTO deployments(id,application_id,state,container_id) VALUES($1,$2,'running','member-container')`,
			[]any{deploymentID, appID}},
		{`INSERT INTO backups(id,database_id,status) VALUES($1,$2,'completed')`,
			[]any{backupID, databaseID}},
	} {
		if _, err := f.store.DB.Exec(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed member resource: %v", err)
		}
	}

	if code := doRequest(t, f.server, http.MethodDelete, "/api/v1/users/"+memberID.String(), "", ownerBearer).Code; code != http.StatusNoContent {
		t.Fatalf("remove member = %d, want 204", code)
	}

	var creator uuid.UUID
	if err := f.store.DB.QueryRow(ctx, `SELECT user_id FROM applications WHERE id=$1`, appID).Scan(&creator); err != nil {
		t.Fatalf("member application vanished: %v", err)
	}
	if creator != ownerID {
		t.Fatalf("application creator = %s, want owner %s", creator, ownerID)
	}
	for _, tc := range []struct {
		label, query string
		id           uuid.UUID
	}{
		{"database", `SELECT user_id FROM databases WHERE id=$1`, databaseID},
		{"service", `SELECT user_id FROM services WHERE id=$1`, serviceID},
	} {
		if err := f.store.DB.QueryRow(ctx, tc.query, tc.id).Scan(&creator); err != nil {
			t.Fatalf("member %s vanished: %v", tc.label, err)
		}
		if creator != ownerID {
			t.Fatalf("%s creator = %s, want owner %s", tc.label, creator, ownerID)
		}
	}
	var backups int
	if err := f.store.DB.QueryRow(ctx, `SELECT count(*) FROM backups WHERE id=$1`, backupID).Scan(&backups); err != nil || backups != 1 {
		t.Fatalf("member backup vanished: %d, %v", backups, err)
	}
	var state string
	if err := f.store.DB.QueryRow(ctx, `SELECT state FROM deployments WHERE id=$1`, deploymentID).Scan(&state); err != nil {
		t.Fatalf("member deployment vanished: %v", err)
	}
	if state != "running" {
		t.Fatalf("deployment state = %q, want running", state)
	}
	for _, path := range []string{
		"/api/v1/applications/" + appID.String(),
		"/api/v1/databases/" + databaseID.String(),
		"/api/v1/services/" + serviceID.String(),
	} {
		if code := doRequest(t, f.server, http.MethodGet, path, "", ownerBearer).Code; code != http.StatusOK {
			t.Fatalf("owner read %s = %d, want 200", path, code)
		}
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

func TestOwnerMutationsWriteAuditLines(t *testing.T) {
	f := newAccessFixture(t)
	var buf bytes.Buffer
	f.server.logger = slog.New(slog.NewJSONHandler(&buf, nil))
	ownerBearer := "Bearer " + f.owner.AccessToken
	memberID := f.member.User.ID

	created := createInvite(t, f.server, ownerBearer, uuid.NewString()+"@example.test", f.projects[0])
	if code := doRequest(t, f.server, http.MethodDelete, "/api/v1/invites/"+created.Invite.ID.String(), "", ownerBearer).Code; code != http.StatusNoContent {
		t.Fatalf("revoke invite = %d, want 204", code)
	}
	if code := doRequest(t, f.server, http.MethodPost, "/api/v1/users/"+memberID+"/reset", "", ownerBearer).Code; code != http.StatusNoContent {
		t.Fatalf("reset member = %d, want 204", code)
	}
	if code := doRequest(t, f.server, http.MethodPut, "/api/v1/users/"+memberID+"/projects",
		`{"project_ids":["`+f.projects[0].String()+`"]}`, ownerBearer).Code; code != http.StatusOK {
		t.Fatalf("set projects = %d, want 200", code)
	}
	if code := doRequest(t, f.server, http.MethodDelete, "/api/v1/users/"+memberID, "", ownerBearer).Code; code != http.StatusNoContent {
		t.Fatalf("remove member = %d, want 204", code)
	}

	logged := buf.String()
	for _, action := range []string{"invite.create", "invite.revoke", "member.reset", "member.projects", "member.remove"} {
		if !strings.Contains(logged, `"action":"`+action+`"`) {
			t.Errorf("audit log missing action %q: %s", action, logged)
		}
	}
	if !strings.Contains(logged, `"actor_id":"`+f.owner.User.ID+`"`) {
		t.Errorf("audit log missing actor %s: %s", f.owner.User.ID, logged)
	}
	if strings.Contains(logged, created.Token) {
		t.Error("audit log leaked the invite token")
	}
	if strings.Contains(logged, "password") {
		t.Error("audit log leaked a password field")
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
