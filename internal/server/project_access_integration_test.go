package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
)

type accessFixture struct {
	server                                                                            *Server
	store                                                                             *store.Store
	owner, member                                                                     *auth.AuthResult
	projects, environments, apps, databases, services, deployments, backups, channels [2]uuid.UUID
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	t.Setenv("FEATURE_SERVICES", "true")
	t.Setenv("FEATURE_UPDATES", "true")
	base, st, owner := scratchProjectsStack(t)
	s, err := New(base.cfg, base.logger, base.auth, nil, base.tokens, accessRegistry{fakeAgentUpdateRegistry: &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService()}}, st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.closer)
	ctx := context.Background()
	member, err := s.auth.Register(ctx, uuid.NewString()+"@example.test", "member-password", auth.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	f := &accessFixture{server: s, store: st, owner: owner, member: member}
	nodeID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'hidden-node','127.0.0.1','root','ready')`, nodeID); err != nil {
		t.Fatal(err)
	}
	for i := range f.projects {
		response := doRequest(t, s, "POST", "/api/v1/projects", fmt.Sprintf(`{"name":"project-%s"}`, uuid.NewString()), "Bearer "+owner.AccessToken)
		if response.Code != 201 {
			t.Fatal(response.Body.String())
		}
		var project wiringProjectEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &project); err != nil {
			t.Fatal(err)
		}
		f.projects[i] = uuid.MustParse(project.Project.ID)
		f.environments[i] = uuid.MustParse(project.Environments[0].ID)
		f.apps[i], f.databases[i], f.services[i], f.deployments[i], f.backups[i] = uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		statements := []struct {
			query string
			args  []any
		}{
			{`INSERT INTO applications(id,user_id,server_id,environment_id,name) VALUES($1,$2,$3,$4,$5)`, []any{f.apps[i], owner.User.ID, nodeID, f.environments[i], fmt.Sprintf("app-%d", i)}},
			{`INSERT INTO databases(id,user_id,server_id,environment_id,name,engine) VALUES($1,$2,$3,$4,$5,'postgres')`, []any{f.databases[i], owner.User.ID, nodeID, f.environments[i], fmt.Sprintf("db-%d", i)}},
			{`INSERT INTO services(id,user_id,server_id,environment_id,name,compose_yaml) VALUES($1,$2,$3,$4,$5,'services: {}')`, []any{f.services[i], owner.User.ID, nodeID, f.environments[i], fmt.Sprintf("service-%d", i)}},
			{`INSERT INTO deployments(id,application_id,state,container_id) VALUES($1,$2,'running',$3)`, []any{f.deployments[i], f.apps[i], fmt.Sprintf("container-%d", i)}},
			{`INSERT INTO backups(id,database_id,status) VALUES($1,$2,'completed')`, []any{f.backups[i], f.databases[i]}},
		}
		for _, stmt := range statements {
			if _, err := st.DB.Exec(ctx, stmt.query, stmt.args...); err != nil {
				t.Fatal(err)
			}
		}
		response = doRequest(t, s, "POST", "/api/v1/notification-channels", fmt.Sprintf(`{"name":"channel-%d","kind":"discord","resource_type":"application","resource_id":"%s","config":{"webhook_url":"https://discord.com/api/webhooks/1/test"}}`, i, f.apps[i]), "Bearer "+owner.AccessToken)
		if response.Code != 201 {
			t.Fatalf("channel create=%d %s", response.Code, response.Body.String())
		}
		var channel struct {
			Channel struct {
				ID uuid.UUID `json:"id"`
			} `json:"channel"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &channel); err != nil {
			t.Fatal(err)
		}
		f.channels[i] = channel.Channel.ID
	}
	return f
}

func (f *accessFixture) grant(t *testing.T) {
	t.Helper()
	response := doRequest(t, f.server, "PUT", "/api/v1/projects/"+f.projects[0].String()+"/members", `{"user_id":"`+f.member.User.ID+`"}`, "Bearer "+f.owner.AccessToken)
	if response.Code != 200 {
		t.Fatalf("grant=%d %s", response.Code, response.Body.String())
	}
}

func TestProjectAccessListsAndReferences(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	bearer := "Bearer " + f.member.AccessToken
	token, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.member.User.ID), "member-deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{bearer, "Bearer " + token.Token} {
		for _, tc := range []struct {
			path, key string
			id        uuid.UUID
		}{
			{"projects", "projects", f.projects[0]},
			{"applications", "applications", f.apps[0]},
			{"databases", "databases", f.databases[0]},
			{"services", "services", f.services[0]},
			{"deployments", "deployments", f.deployments[0]},
			{"notification-channels", "channels", f.channels[0]},
		} {
			rec := doRequest(t, f.server, "GET", "/api/v1/"+tc.path, "", credential)
			if rec.Code != 200 {
				t.Fatalf("list %s=%d %s", tc.path, rec.Code, rec.Body.String())
			}
			var envelope map[string][]map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			rows := envelope[tc.key]
			if len(rows) != 1 || rows[0]["id"] != tc.id.String() {
				t.Fatalf("%s list=%s", tc.path, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "hidden-node") || strings.Contains(rec.Body.String(), "server_id") {
				t.Fatalf("operator resource leaked: %s", rec.Body.String())
			}
		}
		for _, tc := range []struct{ method, path, body string }{
			{"PUT", "applications/" + f.apps[0].String(), `{"environment_id":"` + f.environments[1].String() + `"}`},
			{"PUT", "applications/" + f.apps[0].String(), `{"ENVIRONMENT_ID":"` + f.environments[1].String() + `"}`},
			{"POST", "applications/" + f.apps[0].String() + "/rollback", `{"deployment_id":"` + f.deployments[1].String() + `"}`},
			{"POST", "databases/" + f.databases[0].String() + "/restore", `{"backup_id":"` + f.backups[1].String() + `"}`},
			{"PATCH", "notification-channels/" + f.channels[0].String(), `{"resource_type":"application","resource_id":"` + f.apps[1].String() + `"}`},
			{"PATCH", "notification-channels/" + f.channels[0].String(), `{"resource_type":"","resource_id":""}`},
			{"POST", "notification-channels", `{"name":"global","kind":"discord"}`},
		} {
			rec := doRequest(t, f.server, tc.method, "/api/v1/"+tc.path, tc.body, credential)
			if rec.Code != 404 {
				t.Fatalf("cross-project %s %s=%d %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		}
	}
	for _, path := range []string{"projects/" + f.projects[0].String(), "applications/" + f.apps[0].String(), "databases/" + f.databases[0].String(), "services/" + f.services[0].String(), "notification-channels/" + f.channels[0].String()} {
		rec := doRequest(t, f.server, "GET", "/api/v1/"+path, "", bearer)
		if rec.Code != 200 {
			t.Fatalf("granted get %s=%d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := doRequest(t, f.server, "PATCH", "/api/v1/projects/"+f.projects[0].String(), `{"description":"member edit"}`, bearer)
	if rec.Code != 200 {
		t.Fatalf("member update=%d %s", rec.Code, rec.Body.String())
	}
	read, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.member.User.ID), "member-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	rec = doRequest(t, f.server, "PATCH", "/api/v1/projects/"+f.projects[0].String(), `{"description":"denied"}`, "Bearer "+read.Token)
	if rec.Code != 403 {
		t.Fatalf("read-only token mutation=%d", rec.Code)
	}
}

// Walking the mounted router makes additions fail until they have an explicit
// reviewed policy in the matrix, even when the central default would deny them.
func TestProjectAccessRouteMatrix(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	member := "Bearer " + f.member.AccessToken
	token, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.member.User.ID), "matrix", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/project_access_routes.txt")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	matrix := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		policy, key, ok := strings.Cut(line, " ")
		if ok {
			matrix[key] = policy
		}
	}
	seen := map[string]bool{}
	params := regexp.MustCompile(`\{[^}]+\}`)
	if err := chi.Walk(f.server.router.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + path
		seen[key] = true
		class, ok := matrix[key]
		if !ok {
			t.Errorf("route needs an explicit access policy: %s", key)
			return nil
		}
		policy, project := projectPolicies[key]
		if (class == "project") != project {
			t.Errorf("matrix/policy mismatch for %s", key)
		}
		if class == "public" || class == "personal" {
			return nil
		}
		url := params.ReplaceAllStringFunc(path, func(param string) string {
			switch param {
			case "{id}":
				switch policy.kind {
				case "project":
					return f.projects[1].String()
				case "environment":
					return f.environments[1].String()
				case "application":
					return f.apps[1].String()
				case "database":
					return f.databases[1].String()
				case "service":
					return f.services[1].String()
				case "channel":
					return f.channels[1].String()
				}
			case "{deployment_id}":
				return f.deployments[1].String()
			case "{backupId}":
				return f.backups[1].String()
			}
			return uuid.NewString()
		})
		body := fmt.Sprintf(`{"environment_id":"%s","resource_type":"application","resource_id":"%s"}`, f.environments[1], f.apps[1])
		if method == http.MethodGet {
			body = ""
		}
		for _, bearer := range []string{member, "Bearer " + token.Token} {
			rec := doRequest(t, f.server, method, url, body, bearer)
			want := 404
			if project && policy.listKey != "" {
				want = 200
			}
			if rec.Code != want {
				t.Errorf("ungranted %s=%d want %d: %s", key, rec.Code, want, rec.Body.String())
			}
			for _, hidden := range []uuid.UUID{f.projects[1], f.apps[1], f.databases[1], f.services[1], f.deployments[1], f.backups[1], f.channels[1]} {
				if strings.Contains(rec.Body.String(), hidden.String()) {
					t.Errorf("ungranted resource leak: %s", rec.Body.String())
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for key := range projectPolicies {
		if !seen[key] {
			t.Errorf("unmounted project policy: %s", key)
		}
	}
	for key := range matrix {
		if !seen[key] {
			t.Errorf("stale matrix route: %s", key)
		}
	}
}

func TestProjectGrantRevocationClosesSocketAndNextRequest(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	live := httptest.NewServer(f.server.Handler())
	t.Cleanup(live.Close)
	token, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.member.User.ID), "socket", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{f.member.AccessToken, token.Token} {
		connection, err := websocket.Dial("ws"+strings.TrimPrefix(live.URL, "http")+"/api/v1/ws?token="+credential+"&channel=tasks:"+f.projects[0].String(), "", live.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		var frame map[string]any
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Fatal(err)
		}
		if frame["type"] != "task_snapshot" {
			t.Fatalf("snapshot=%v", frame)
		}
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			t.Fatal(err)
		}
		if frame["type"] != "subscribed" {
			t.Fatalf("subscription=%v", frame)
		}
		rec := doRequest(t, f.server, "DELETE", "/api/v1/projects/"+f.projects[0].String()+"/members", `{"user_id":"`+f.member.User.ID+`"}`, "Bearer "+f.owner.AccessToken)
		if rec.Code != 200 {
			t.Fatalf("revoke=%d %s", rec.Code, rec.Body.String())
		}
		if err := websocket.JSON.Receive(connection, &frame); err == nil {
			t.Fatalf("revoked socket stayed open: %v", frame)
		}
		rec = doRequest(t, f.server, "GET", "/api/v1/projects/"+f.projects[0].String(), "", "Bearer "+credential)
		if rec.Code != 404 {
			t.Fatalf("revoked get=%d %s", rec.Code, rec.Body.String())
		}
		f.grant(t)
	}
}

type accessRegistry struct{ *fakeAgentUpdateRegistry }

func (accessRegistry) DialComposeClient(context.Context, uuid.UUID, ...servers.DockerDialOption) (*servers.ComposeClient, error) {
	return nil, fmt.Errorf("no agent in authorization test")
}

func TestProjectGrantReplacementIsAtomicAndCascades(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	path := "/api/v1/projects/" + f.projects[0].String() + "/members"
	bearer := "Bearer " + f.owner.AccessToken
	rec := doRequest(t, f.server, "PUT", path, `{"user_ids":["`+uuid.NewString()+`"]}`, bearer)
	if rec.Code != 404 {
		t.Fatalf("invalid replacement=%d %s", rec.Code, rec.Body.String())
	}
	allowed, err := f.store.HasProjectGrant(context.Background(), uuid.MustParse(f.member.User.ID), f.projects[0])
	if err != nil || !allowed {
		t.Fatalf("failed replacement changed grants=%v %v", allowed, err)
	}
	rec = doRequest(t, f.server, "PUT", path, `{"user_ids":[]}`, bearer)
	if rec.Code != 200 {
		t.Fatalf("empty replacement=%d %s", rec.Code, rec.Body.String())
	}
	allowed, err = f.store.HasProjectGrant(context.Background(), uuid.MustParse(f.member.User.ID), f.projects[0])
	if err != nil || allowed {
		t.Fatalf("replacement failed to revoke=%v %v", allowed, err)
	}
	f.grant(t)
	if _, err := f.store.DB.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, f.member.User.ID); err != nil {
		t.Fatal(err)
	}
	members, err := f.store.ListProjectMembers(context.Background(), f.projects[0])
	if err != nil || len(members) != 0 {
		t.Fatalf("deleted user grants=%v %v", members, err)
	}
}

func TestProjectChannelResolutionAndMetadata(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	memberID := uuid.MustParse(f.member.User.ID)
	var nodeID uuid.UUID
	if err := f.store.DB.QueryRow(context.Background(), `SELECT server_id FROM applications WHERE id=$1`, f.apps[0]).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		channel string
		allowed bool
	}{
		{"tasks:" + f.projects[0].String(), true},
		{"tasks:" + f.projects[1].String(), false},
		{"logs:" + nodeID.String() + ":" + f.deployments[0].String(), true},
		{"logs:" + nodeID.String() + ":" + f.deployments[1].String(), false},
		{"logs:" + nodeID.String() + ":container-0", true},
		{"logs:" + nodeID.String() + ":container-1", false},
		{"logs:" + uuid.NewString() + ":" + f.deployments[0].String(), false},
		{"toolchain:guessed", false},
		{"tasks:", false},
	} {
		err := f.server.authorizeRealtimeChannel(context.Background(), tc.channel, memberID)
		if (err == nil) != tc.allowed {
			t.Errorf("channel %q allowed=%v err=%v", tc.channel, tc.allowed, err)
		}
	}
	payload := []byte(fmt.Sprintf(`{"channel":"tasks:%s","type":"task","data":"{\"server_id\":\"hidden-node\",\"error\":\"hidden-node failed\",\"name\":\"app\"}"}`, f.projects[0]))
	filtered := f.server.filterProjectFrame(context.Background(), memberID, payload)
	if !json.Valid(filtered) || strings.Contains(string(filtered), "hidden-node") {
		t.Fatalf("task metadata=%s", filtered)
	}
	ownerFiltered := f.server.filterProjectFrame(context.Background(), uuid.MustParse(f.owner.User.ID), payload)
	if string(ownerFiltered) != string(payload) {
		t.Fatal("owner frame changed")
	}
}

type accessLogService struct{ services.ServiceService }

func (accessLogService) Logs(ctx context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ int64, _ bool) (services.LogStream, error) {
	chunks := make(chan []byte, 1)
	chunks <- []byte("granted\n")
	go func() { <-ctx.Done(); close(chunks) }()
	return accessLogStream{chunks: chunks}, nil
}

type accessLogStream struct{ chunks <-chan []byte }

func (s accessLogStream) Chunks() <-chan []byte { return s.chunks }
func (accessLogStream) Err() error              { return nil }
func (accessLogStream) Close() error            { return nil }

func TestProjectServiceStreamRevocation(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) { services.Mount(r, f.server.resourceAuth(), UserIDFromContext, accessLogService{}) })
	live := httptest.NewServer(router)
	defer live.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", live.URL+"/api/v1/services/"+f.services[0].String()+"/logs?follow=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.member.AccessToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		t.Fatalf("stream=%d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "granted\n" {
		t.Fatalf("granted log=%q %v", line, err)
	}
	// Direct SQL simulates revocation by another control-plane process.
	if err := f.store.RevokeProject(ctx, f.projects[0], uuid.MustParse(f.member.User.ID)); err != nil {
		t.Fatal(err)
	}
	revokedAt := time.Now()
	remaining, err := io.ReadAll(reader)
	if err != nil || len(remaining) != 0 || time.Since(revokedAt) > 3*time.Second {
		t.Fatalf("revoked stream=%q %v elapsed=%s", remaining, err, time.Since(revokedAt))
	}
}

type accessContainerRegistry struct {
	ServerService
	containers []*agentv1.ContainerInfo
}

func (r accessContainerRegistry) DialDockerClient(context.Context, uuid.UUID, ...servers.DockerDialOption) (*servers.DockerClient, error) {
	return &servers.DockerClient{DockerServiceClient: accessInventory{containers: r.containers}}, nil
}

type accessInventory struct {
	agentv1.DockerServiceClient
	containers []*agentv1.ContainerInfo
}

func (c accessInventory) ListContainers(context.Context, *agentv1.ListContainersRequest, ...grpc.CallOption) (*agentv1.ListContainersResponse, error) {
	return &agentv1.ListContainersResponse{Containers: c.containers}, nil
}

func TestProjectRuntimeContainerResolution(t *testing.T) {
	f := newAccessFixture(t)
	f.grant(t)
	ctx := context.Background()
	var nodeID uuid.UUID
	if err := f.store.DB.QueryRow(ctx, `SELECT server_id FROM applications WHERE id=$1`, f.apps[0]).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	registry := accessContainerRegistry{ServerService: f.server.servers, containers: []*agentv1.ContainerInfo{
		{Id: "granted-compose", Labels: map[string]string{"com.docker.compose.project": services.ProjectName(f.services[0])}},
		{Id: "ungranted-compose", Labels: map[string]string{"com.docker.compose.project": services.ProjectName(f.services[1])}},
		{Id: "granted-app", Labels: map[string]string{"gotham.app_id": f.apps[0].String()}},
		{Id: "projectless"},
	}}
	resolver := &Server{auth: f.server.auth, persistence: f.store, servers: registry}
	for _, tc := range []struct {
		id      string
		node    uuid.UUID
		allowed bool
	}{
		{"granted-compose", nodeID, true},
		{"ungranted-compose", nodeID, false},
		{"granted-app", nodeID, true},
		{"projectless", nodeID, false},
		{"guessed", nodeID, false},
		{"granted-compose", uuid.New(), false},
	} {
		err := resolver.authorizeRuntimeContainer(ctx, tc.node, tc.id, uuid.MustParse(f.member.User.ID))
		if (err == nil) != tc.allowed {
			t.Errorf("runtime %s allowed=%v err=%v", tc.id, tc.allowed, err)
		}
	}
}
