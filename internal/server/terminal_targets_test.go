package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

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

type fakeTargetResolver struct {
	sets map[uuid.UUID]deploy.TerminalTargetSet
	errs map[uuid.UUID]error
	list func(ctx context.Context, appID uuid.UUID) (deploy.TerminalTargetSet, error)
}

func (f fakeTargetResolver) ListTerminalContainers(ctx context.Context, _, appID uuid.UUID) (deploy.TerminalTargetSet, error) {
	if f.list != nil {
		return f.list(ctx, appID)
	}
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
			resolver: resolver, isOwner: false,
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Servers) != 0 {
			t.Fatalf("servers = %+v, want none for a non-operator", targets.Servers)
		}
		if len(targets.Applications) != 1 {
			t.Fatalf("applications = %+v, want the writable container", targets.Applications)
		}
	})

	t.Run("operator sees only ready hosts", func(t *testing.T) {
		source := terminalTargetsSource{servers: registry, isOwner: true}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Servers) != 1 || targets.Servers[0].Name != "prod-01" {
			t.Fatalf("servers = %+v, want only the ready node", targets.Servers)
		}
	})

	t.Run("read-only member sees no containers and no error", func(t *testing.T) {
		readOnly := fakeTargetResolver{errs: map[uuid.UUID]error{app.ID: terminal.ErrForbidden}}
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
			readOnlyApp.ID: terminal.ErrForbidden,
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

func liveApps(count int, serverID uuid.UUID) ([]deploy.Application, map[uuid.UUID]deploy.TerminalTargetSet) {
	applications := make([]deploy.Application, 0, count)
	sets := make(map[uuid.UUID]deploy.TerminalTargetSet, count)
	base := time.Now().UTC()
	for index := 0; index < count; index++ {
		app := deploy.Application{
			ID:        uuid.New(),
			Name:      fmt.Sprintf("app-%02d", index),
			ServerID:  serverID,
			UpdatedAt: base.Add(-time.Duration(index) * time.Minute),
		}
		applications = append(applications, app)
		containers := make([]deploy.TerminalContainer, 0, maxTerminalTargetContainers+10)
		for position := 0; position < maxTerminalTargetContainers+10; position++ {
			containers = append(containers, runningContainer("web-1"))
		}
		sets[app.ID] = targetSet(app, containers...)
	}
	return applications, sets
}

func TestTerminalTargetsSourceCapsAfterFiltering(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()

	t.Run("stopped applications do not consume the visible cap", func(t *testing.T) {
		applications, sets := liveApps(50, serverID)
		errs := make(map[uuid.UUID]error, 20)
		for index := 0; index < 20; index++ {
			stopped := deploy.Application{
				ID:        uuid.New(),
				Name:      fmt.Sprintf("stopped-%02d", index),
				ServerID:  serverID,
				UpdatedAt: time.Now().UTC().Add(time.Duration(index) * time.Minute),
			}
			applications = append([]deploy.Application{stopped}, applications...)
			errs[stopped.ID] = deploy.ErrNoRunningDeployment
		}
		source := terminalTargetsSource{
			apps:     fakeTargetDeployService{applications: applications},
			resolver: fakeTargetResolver{sets: sets, errs: errs},
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Applications) != 50 {
			t.Fatalf("applications = %d, want the 50 running ones", len(targets.Applications))
		}
		for _, group := range targets.Applications {
			if strings.HasPrefix(group.ApplicationName, "stopped-") {
				t.Fatalf("stopped application %q consumed a visible slot", group.ApplicationName)
			}
		}
		if len(targets.Errors) != 0 {
			t.Fatalf("errors = %+v, want no error for an empty application", targets.Errors)
		}
		if targets.Truncated {
			t.Fatal("truncated = true, want false: all 70 scanned rows were resolved")
		}
	})

	t.Run("more than 50 running applications are ordered newest first", func(t *testing.T) {
		applications, sets := liveApps(60, serverID)
		source := terminalTargetsSource{
			apps:     fakeTargetDeployService{applications: applications},
			resolver: fakeTargetResolver{sets: sets},
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Applications) != 60 {
			t.Fatalf("applications = %d, want all 60 resolvable groups (cap is applied after filtering)", len(targets.Applications))
		}
		for index := 0; index < len(targets.Applications)-1; index++ {
			if targets.Applications[index].ApplicationName > targets.Applications[index+1].ApplicationName {
				t.Fatalf("order = %q then %q, want newest first", targets.Applications[index].ApplicationName, targets.Applications[index+1].ApplicationName)
			}
		}
	})

	t.Run("the host bound reports truncation", func(t *testing.T) {
		registry := newFakeServerService()
		for index := 0; index < maxTerminalTargetServers+5; index++ {
			id := uuid.New()
			registry.items[id] = servers.Server{ID: id, Name: "node", Status: "ready"}
		}
		source := terminalTargetsSource{servers: registry, isOwner: true}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if len(targets.Servers) != maxTerminalTargetServers {
			t.Fatalf("servers = %d, want %d", len(targets.Servers), maxTerminalTargetServers)
		}
		if !targets.Truncated {
			t.Fatal("truncated = false, want true when ready hosts were cut")
		}
	})

	t.Run("an oversized scan reports truncation", func(t *testing.T) {
		applications, sets := liveApps(maxTerminalTargetScannedApplications+1, serverID)
		source := terminalTargetsSource{
			apps:     fakeTargetDeployService{applications: applications},
			resolver: fakeTargetResolver{sets: sets},
		}
		targets, err := source.ListTargets(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListTargets: %v", err)
		}
		if !targets.Truncated {
			t.Fatal("truncated = false, want true when the scan cap cuts the application list")
		}
	})
}

func TestTerminalTargetsSourceWorkerBound(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()
	applications, sets := liveApps(24, serverID)
	var current, peak int64
	resolver := fakeTargetResolver{list: func(ctx context.Context, appID uuid.UUID) (deploy.TerminalTargetSet, error) {
		running := atomic.AddInt64(&current, 1)
		for {
			observed := atomic.LoadInt64(&peak)
			if running <= observed || atomic.CompareAndSwapInt64(&peak, observed, running) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt64(&current, -1)
		return sets[appID], nil
	}}
	source := terminalTargetsSource{
		apps:     fakeTargetDeployService{applications: applications},
		resolver: resolver,
	}
	if _, err := source.ListTargets(context.Background(), userID); err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if peak > 4 {
		t.Fatalf("peak concurrency = %d, want <= 4", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency = %d, want the pool to run in parallel", peak)
	}
}

func TestTerminalTargetsSourceTimeoutCollapses(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()
	applications, _ := liveApps(60, serverID)
	resolver := fakeTargetResolver{list: func(ctx context.Context, _ uuid.UUID) (deploy.TerminalTargetSet, error) {
		<-ctx.Done()
		return deploy.TerminalTargetSet{}, ctx.Err()
	}}
	source := terminalTargetsSource{
		apps:     fakeTargetDeployService{applications: applications},
		resolver: resolver,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	targets, err := source.ListTargets(ctx, userID)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("ListTargets took %s, want it to honour the deadline", elapsed)
	}
	if len(targets.Errors) != 1 {
		t.Fatalf("errors = %+v, want one collapsed timeout entry", targets.Errors)
	}
	entry := targets.Errors[0]
	if entry.Code != terminal.TargetErrorTimeout {
		t.Fatalf("code = %q, want %q", entry.Code, terminal.TargetErrorTimeout)
	}
	if entry.Count < 1 {
		t.Fatalf("count = %d, want the number of cut-short applications", entry.Count)
	}
}

func TestTerminalTargetsRouteRequiresInteractiveSession(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	s.auth.(*fakeAuthService).user.Role = "owner"
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	adminToken := mustCreateAdminToken(t, tokens, testUserID)

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

func TestTerminalTargetsSourceForCapturesOwner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		logger:  logger,
		servers: newFakeServerService(),
		auth:    &fakeAuthService{user: &auth.User{ID: testUserID.String(), Email: "op@example.com", Role: auth.RoleOwner}},
	}
	registry := s.servers.(*fakeServerService)
	id := uuid.New()
	registry.items[id] = servers.Server{ID: id, Name: "prod-01", Status: "ready"}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/targets", nil)
	ctx := context.WithValue(request.Context(), userIDKey, testUserID)
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
