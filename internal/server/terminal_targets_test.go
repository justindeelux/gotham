package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/terminal"
)

// fakeTargetDeployService implements only the application listing the picker
// aggregation reads; the embedded interface supplies the rest.
type fakeTargetDeployService struct {
	deploy.DeployService
	applications []deploy.Application
	err          error
}

func (f fakeTargetDeployService) ListApplications(context.Context, uuid.UUID, deploy.ApplicationFilter) ([]deploy.Application, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.applications, nil
}

// fakeTargetResolver is a faithful fake of the per-application resolver: it
// returns a server-shaped target set, or the same sentinels the deploy service
// returns on the real path.
type fakeTargetResolver struct {
	sets map[uuid.UUID]deploy.TerminalTargetSet
	errs map[uuid.UUID]error
}

func (f fakeTargetResolver) ListTerminalContainers(_ context.Context, _, appID uuid.UUID) (deploy.TerminalTargetSet, error) {
	if err := f.errs[appID]; err != nil {
		return deploy.TerminalTargetSet{}, err
	}
	return f.sets[appID], nil
}

func (f fakeTargetResolver) ResolveTerminalContainer(context.Context, uuid.UUID, uuid.UUID, string, string) (deploy.TerminalTargetSet, deploy.TerminalContainer, error) {
	return deploy.TerminalTargetSet{}, deploy.TerminalContainer{}, nil
}

func (f fakeTargetResolver) TerminalApplicationAccess(context.Context, uuid.UUID) (deploy.TerminalApplicationAccess, error) {
	return deploy.TerminalApplicationAccess{}, nil
}

// targetSet builds one running application target set.
func targetSet(app deploy.Application, containers ...deploy.TerminalContainer) deploy.TerminalTargetSet {
	return deploy.TerminalTargetSet{
		ApplicationID:   app.ID,
		ApplicationName: app.Name,
		ServerID:        app.ServerID,
		Containers:      containers,
	}
}

func runningContainer(name string) deploy.TerminalContainer {
	return deploy.TerminalContainer{
		Service:     "web",
		ContainerID: strings.Repeat("a", 64),
		Name:        name,
		Image:       "nginx",
		State:       "running",
		Replica:     1,
		Label:       "web · " + name,
	}
}

func TestTerminalTargetsSourceAuthorization(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()
	app := deploy.Application{ID: uuid.New(), Name: "shop", ServerID: serverID}
	set := targetSet(app, runningContainer("shop-web-1"))
	resolver := fakeTargetResolver{sets: map[uuid.UUID]deploy.TerminalTargetSet{app.ID: set}}
	registry := newFakeServerService()
	registry.items[serverID] = servers.Server{ID: serverID, Name: "prod-01", Status: "ready"}
	pendingID := uuid.New()
	registry.items[pendingID] = servers.Server{ID: pendingID, Name: "pending", Status: "pending"}

	t.Run("non-operator sees no hosts", func(t *testing.T) {
		source := terminalTargetsSource{
			servers: registry, apps: fakeTargetDeployService{applications: []deploy.Application{app}},
			resolver: resolver, isOperator: false,
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Servers) != 0 {
			t.Fatalf("servers = %+v, want none for a non-operator", targets.Servers)
		}
		// The same caller still gets the application container they may write.
		if len(targets.Applications) != 1 {
			t.Fatalf("applications = %+v, want the writable container", targets.Applications)
		}
	})

	t.Run("operator sees only ready hosts", func(t *testing.T) {
		source := terminalTargetsSource{servers: registry, isOperator: true}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Servers) != 1 || targets.Servers[0].Name != "prod-01" {
			t.Fatalf("servers = %+v, want only the ready node", targets.Servers)
		}
	})

	t.Run("read-only member sees no containers and no error", func(t *testing.T) {
		readOnly := fakeTargetResolver{errs: map[uuid.UUID]error{app.ID: teams.ErrForbidden}}
		source := terminalTargetsSource{
			apps:     fakeTargetDeployService{applications: []deploy.Application{app}},
			resolver: readOnly,
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Applications) != 0 || len(targets.Errors) != 0 {
			t.Fatalf("targets = %+v, want no containers and no error", targets)
		}
	})
}

func TestTerminalTargetsSourcePerApplicationFailures(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()
	okApp := deploy.Application{ID: uuid.New(), Name: "shop", ServerID: serverID}
	emptyApp := deploy.Application{ID: uuid.New(), Name: "blog", ServerID: serverID}
	downApp := deploy.Application{ID: uuid.New(), Name: "crm", ServerID: serverID}
	readOnlyApp := deploy.Application{ID: uuid.New(), Name: "legacy", ServerID: serverID}
	resolver := fakeTargetResolver{
		sets: map[uuid.UUID]deploy.TerminalTargetSet{
			okApp.ID:    targetSet(okApp, runningContainer("shop-web-1")),
			emptyApp.ID: targetSet(emptyApp),
		},
		errs: map[uuid.UUID]error{
			downApp.ID:     deploy.ErrAgentUnavailable,
			readOnlyApp.ID: teams.ErrForbidden,
		},
	}
	source := terminalTargetsSource{
		apps: fakeTargetDeployService{applications: []deploy.Application{
			okApp, emptyApp, downApp, readOnlyApp,
		}},
		resolver: resolver,
	}
	targets, err := source.ListTargets(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	// One healthy group, the empty one omitted, the forbidden one silent.
	if len(targets.Applications) != 1 || targets.Applications[0].ApplicationName != "shop" {
		t.Fatalf("applications = %+v", targets.Applications)
	}
	if len(targets.Errors) != 1 {
		t.Fatalf("errors = %+v, want only the unreachable node", targets.Errors)
	}
	entry := targets.Errors[0]
	if entry.Source != "application" || entry.ID != downApp.ID.String() ||
		entry.Name != "crm" || entry.Code != terminal.TargetErrorUnavailable {
		t.Fatalf("error = %+v", entry)
	}
}

func TestTerminalTargetsSourceListFailureIsReported(t *testing.T) {
	source := terminalTargetsSource{
		apps:     fakeTargetDeployService{err: errors.New("database is down")},
		resolver: fakeTargetResolver{},
	}
	targets, err := source.ListTargets(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets.Errors) != 1 || targets.Errors[0].Source != "applications" ||
		targets.Errors[0].Code != terminal.TargetErrorUnavailable {
		t.Fatalf("errors = %+v", targets.Errors)
	}

	disabled := terminalTargetsSource{
		apps:     fakeTargetDeployService{err: deploy.ErrDisabled},
		resolver: fakeTargetResolver{},
	}
	targets, err = disabled.ListTargets(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets.Errors) != 1 || targets.Errors[0].Code != terminal.TargetErrorDisabled {
		t.Fatalf("errors = %+v", targets.Errors)
	}
}

func TestTerminalTargetsSourceBoundsAndOrder(t *testing.T) {
	serverID := uuid.New()
	registry := newFakeServerService()
	applications := make([]deploy.Application, 0, 60)
	sets := make(map[uuid.UUID]deploy.TerminalTargetSet, 60)
	for index := 0; index < 60; index++ {
		app := deploy.Application{ID: uuid.New(), Name: "app", ServerID: serverID}
		applications = append(applications, app)
		containers := make([]deploy.TerminalContainer, 0, 60)
		for position := 0; position < 60; position++ {
			containers = append(containers, runningContainer("web-1"))
		}
		sets[app.ID] = targetSet(app, containers...)
	}
	for index := 0; index < 60; index++ {
		id := uuid.New()
		registry.items[id] = servers.Server{ID: id, Name: "node", Status: "ready"}
	}
	source := terminalTargetsSource{
		servers: registry, isOperator: true,
		apps:     fakeTargetDeployService{applications: applications},
		resolver: fakeTargetResolver{sets: sets},
	}
	targets, err := source.ListTargets(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets.Servers) != maxTerminalTargetServers {
		t.Fatalf("servers = %d, want %d", len(targets.Servers), maxTerminalTargetServers)
	}
	if len(targets.Applications) != maxTerminalTargetApplications {
		t.Fatalf("applications = %d, want %d", len(targets.Applications), maxTerminalTargetApplications)
	}
	if len(targets.Applications[0].Containers) != maxTerminalTargetContainers {
		t.Fatalf("containers = %d, want %d", len(targets.Applications[0].Containers), maxTerminalTargetContainers)
	}
}

// TestTerminalTargetsRouteRequiresInteractiveSession is the transport gate:
// an API token (even read/admin) cannot enumerate shells, while a real JWT
// session can read the (empty) list.
func TestTerminalTargetsRouteRequiresInteractiveSession(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	adminToken := mustCreateAdminToken(t, tokens)

	cases := map[string]struct {
		bearer string
		want   int
	}{
		"anonymous":   {"", http.StatusUnauthorized},
		"read token":  {"Bearer " + readToken, http.StatusForbidden},
		"admin token": {"Bearer " + adminToken.Token, http.StatusForbidden},
		"jwt session": {"Bearer valid-token", http.StatusOK},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			recorder := doRequest(t, s, http.MethodGet, "/api/v1/terminal/targets", "", tc.bearer)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.want, recorder.Body.String())
			}
			if tc.want == http.StatusOK {
				var body struct {
					Servers      []json.RawMessage `json:"servers"`
					Applications []json.RawMessage `json:"applications"`
					Errors       []json.RawMessage `json:"errors"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Servers == nil || body.Applications == nil || body.Errors == nil {
					t.Fatalf("body = %s, want non-null lists", recorder.Body.String())
				}
			}
		})
	}
}

// TestTerminalTargetsSourceForCapturesOperator keeps the request decision with
// the request: an operator's source lists hosts, a member's does not.
func TestTerminalTargetsSourceForCapturesOperator(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		logger:  logger,
		servers: newFakeServerService(),
		auth:    &fakeAuthService{user: &auth.User{ID: testUserID.String(), Email: "op@example.com", Role: "admin"}},
	}
	registry := s.servers.(*fakeServerService)
	id := uuid.New()
	registry.items[id] = servers.Server{ID: id, Name: "prod-01", Status: "ready"}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	ctx := context.WithValue(request.Context(), userIDKey, testUserID)
	ctx = context.WithValue(ctx, roleKey, "admin")
	request = request.WithContext(ctx)
	operator := s.terminalTargetsSourceFor(request)
	targets, err := operator.ListTargets(request.Context(), testUserID)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets.Servers) != 1 {
		t.Fatalf("servers = %+v, want the ready node for the operator", targets.Servers)
	}
}
