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

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

// TestConnectionApplicationsOfMapsIdentity pins the disconnect in-use wiring:
// the provider slug and clone URL must land in the connection identity the
// check matches on, so a rename cannot silently disable the 409.
func TestConnectionApplicationsOfMapsIdentity(t *testing.T) {
	applications := []deploy.Application{
		{Name: "shop", Provider: "gitlab", CloneURL: "https://git.example/acme/shop.git"},
		{Name: "docs", Provider: "github", CloneURL: "git@github.com:acme/docs.git"},
	}

	got := connectionApplicationsOf(applications)

	want := []providers.ConnectionApplication{
		{Name: "shop", Provider: "gitlab", CloneURL: "https://git.example/acme/shop.git"},
		{Name: "docs", Provider: "github", CloneURL: "git@github.com:acme/docs.git"},
	}
	if len(got) != len(want) {
		t.Fatalf("mapped = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mapped[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestProviderConnectionApplicationsWithoutDeploy pins the disabled path: no
// deploy service lists nothing, so disconnect stays allowed where
// applications are off.
func TestProviderConnectionApplicationsWithoutDeploy(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	if s.deploy != nil {
		t.Fatalf("test server holds a deploy service")
	}

	got, err := s.providerConnectionApplications(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("wiring: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("applications = %+v, want none without a deploy service", got)
	}
}

// TestProviderDisconnectInUseEndToEnd proves the disconnect guard through the
// real server wiring: a gitlab provider with an application on the same host
// answers 409 naming the app, and deletes cleanly once the app is gone. It
// needs Postgres and skips without one, like the projects wiring tests.
func TestProviderDisconnectInUseEndToEnd(t *testing.T) {
	s, st, owner := scratchProjectsStack(t)
	bearer := "Bearer " + owner.AccessToken

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}
	teamID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO teams (id, name) VALUES ($1, 'disconnect-probe')`, teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'owner')`,
		teamID, ownerID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	team := teamID.String()
	withTeam := func(method, path, body string) *httptest.ResponseRecorder {
		return doTeamRequest(t, s, method, path, body, bearer, team)
	}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/providers",
		`{"provider":"gitlab","base_url":"https://git.example","client_id":"id",`+
			`"client_secret":"secret",`+
			`"redirect_url":"https://example.com/api/v1/providers/gitlab/callback"}`, bearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("provider create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var provider struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	rec = withTeam(http.MethodPost, "/api/v1/servers",
		`{"name":"node-1","ip":"10.0.0.5","ssh_user":"root"}`)
	_ = rec
	// Node routes are not mounted on this stack (no node registry), so the
	// server row is seeded directly; a NULL team stays shared across teams.
	serverID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers (id, name, ip, ssh_user) VALUES ($1, 'node-1', '10.0.0.5', 'root')`,
		serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	var server struct {
		Server struct {
			ID string `json:"id"`
		} `json:"server"`
	}
	server.Server.ID = serverID.String()

	rec = withTeam(http.MethodPost, "/api/v1/projects", `{"name":"Shop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("project create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var project wiringProjectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if len(project.Environments) != 1 {
		t.Fatalf("environments = %+v, want one", project.Environments)
	}

	// The hook install against the unreachable instance fails best effort;
	// the application row is still created.
	rec = withTeam(http.MethodPost, "/api/v1/applications",
		`{"name":"shop","environment_id":"`+project.Environments[0].ID+`",`+
			`"server_id":"`+server.Server.ID+`","provider":"gitlab",`+
			`"repo":"acme/shop","clone_url":"https://git.example/acme/shop.git",`+
			`"source_type":"gitlab_app","branch":"main","port":3000}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("application create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var application struct {
		Application struct {
			ID string `json:"id"`
		} `json:"application"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &application); err != nil {
		t.Fatalf("decode application: %v", err)
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/providers/"+provider.ID, "", bearer)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "shop") || strings.Contains(body, "providers:") {
		t.Fatalf("409 body = %s, want the app name without the internal prefix", body)
	}

	rec = withTeam(http.MethodDelete, "/api/v1/applications/"+application.Application.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("application delete = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/providers/"+provider.ID, "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete after app removal = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"deleted":true`) {
		t.Fatalf("delete body = %s, want deleted:true", rec.Body.String())
	}
}
