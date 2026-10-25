package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type runtimeTestNode struct {
	Node
	inventory []*agentv1.ContainerInfo
	err       error
	inspect   func(map[string]string, bool, bool)
}

func (n *runtimeTestNode) ContainersByLabel(_ context.Context, labels map[string]string, all, resolve bool) ([]*agentv1.ContainerInfo, error) {
	if n.inspect != nil {
		n.inspect(labels, all, resolve)
	}
	return n.inventory, n.err
}
func (n *runtimeTestNode) Close() error { return nil }
func runtimeService(t *testing.T, repo *fakeRepository, node *runtimeTestNode) *Service {
	t.Helper()
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger(), Dial: func(_ context.Context, id uuid.UUID) (Node, error) {
		if id != repo.app.ServerID {
			t.Errorf("dialed unexpected node %s", id)
		}
		return node, nil
	}})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}
func runtimeFixture(app Application, id, state, health string) *agentv1.ContainerInfo {
	return &agentv1.ContainerInfo{Id: id, State: state, Health: health, Labels: map[string]string{labelAppID: app.ID.String()}, Mounts: []*agentv1.ContainerMount{{Source: "do-not-expose"}}}
}
func TestRuntimePipelineAndLiveStateIndependent(t *testing.T) {
	for _, state := range []State{StateRunning, StateFailed, StateQueued} {
		t.Run(string(state), func(t *testing.T) {
			user := uuid.New()
			app := testApplication(user)
			older, newer := uuid.New(), uuid.New()
			id := strings.Repeat("a", 64)
			c := runtimeFixture(app, id, "exited", "")
			c.Labels[labelDeploymentID] = older.String()
			repo := &fakeRepository{app: app, deployments: []Deployment{{ID: older, ApplicationID: app.ID, State: StateRunning, ContainerID: id}, {ID: newer, ApplicationID: app.ID, State: state}}}
			node := &runtimeTestNode{inventory: []*agentv1.ContainerInfo{c}, inspect: func(labels map[string]string, all, resolve bool) {
				if len(labels) != 1 || labels[labelAppID] != app.ID.String() || !all || resolve {
					t.Fatalf("unsafe inventory request: %v %v %v", labels, all, resolve)
				}
			}}
			svc := runtimeService(t, repo, node)
			out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
			if err != nil || out.Coverage != "complete" || out.ObservedAt == nil || len(out.Containers) != 1 {
				t.Fatalf("runtime=%+v %v", out, err)
			}
			row := out.Containers[0]
			if row.State != "exited" || row.Health != "unknown" || row.DeploymentID == nil || *row.DeploymentID != older {
				t.Fatalf("runtime falsely followed latest pipeline: %+v", row)
			}
			data, _ := json.Marshal(out)
			if strings.Contains(string(data), "do-not-expose") || strings.Contains(string(data), labelAppID) {
				t.Fatalf("raw evidence leaked: %s", data)
			}
		})
	}
}
func TestRuntimeComposeReplicasAndReuse(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	app.SourceType = SourceCompose
	inventory := []*agentv1.ContainerInfo{}
	for i, state := range []string{"running", "exited", "dead"} {
		c := runtimeFixture(app, fmt.Sprintf("%064x", i+1), state, "unhealthy")
		c.Labels = map[string]string{composeProjectLabel: services.ProjectName(app.ID), composeServiceLabel: "web", "com.docker.compose.container-number": fmt.Sprint(i + 1)}
		inventory = append(inventory, c)
	}
	node := &runtimeTestNode{inventory: inventory, inspect: func(labels map[string]string, all, resolve bool) {
		if labels[composeProjectLabel] != services.ProjectName(app.ID) || !all || resolve {
			t.Fatalf("wrong Compose request")
		}
	}}
	repo := &fakeRepository{app: app, deployments: []Deployment{{ID: uuid.New(), ApplicationID: app.ID, State: StateRunning}}}
	svc := runtimeService(t, repo, node)
	for i := 0; i < 2; i++ {
		out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
		if err != nil || len(out.Containers) != 3 {
			t.Fatalf("runtime=%+v %v", out, err)
		}
		for j, row := range out.Containers {
			if row.Service != "web" || row.Replica != fmt.Sprint(j+1) || row.Health != "unhealthy" || row.DeploymentID != nil {
				t.Fatalf("Compose association/readiness invented: %+v", row)
			}
		}
	}
}
func TestRuntimeOwnershipAndMetadata(t *testing.T) {
	app := testApplication(uuid.New())
	valid := runtimeFixture(app, strings.Repeat("b", 64), "running", "")
	foreign := runtimeFixture(testApplication(uuid.New()), strings.Repeat("a", 64), "running", "healthy")
	old := runtimeFixture(app, "short-id", "running", "")
	noLabels := runtimeFixture(app, strings.Repeat("c", 64), "running", "")
	noLabels.Labels = nil
	selected, limited := selectRuntimeContainers(app, []*agentv1.ContainerInfo{nil, old, foreign, noLabels, valid, valid})
	if !limited || len(selected) != 1 || selected[0] != valid {
		t.Fatalf("unsafe selection=%+v limited=%v", selected, limited)
	}
}
func TestRuntimeSnapshotAuthorization(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	node := &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "running", "")}}
	repo := &fakeRepository{app: app}
	svc := runtimeService(t, repo, node)
	viewer := context.Background()
	if out, err := svc.GetApplicationRuntime(viewer, user, app.ID); err != nil || len(out.Containers) != 1 {
		t.Fatalf("read-only denied: %+v %v", out, err)
	}
}
func TestRuntimeSnapshotRace(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			user := uuid.New()
			app := testApplication(user)
			repo := &fakeRepository{app: app}
			node := &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "running", "")}, inspect: func(map[string]string, bool, bool) {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				if remove {
					repo.app = Application{}
				} else {
					repo.app.ServerID = uuid.New()
				}
			}}
			svc := runtimeService(t, repo, node)
			out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
			expected := ErrConflict
			if remove {
				expected = ErrNotFound
			}
			if !errors.Is(err, expected) || len(out.Containers) != 0 {
				t.Fatalf("race leaked/substituted inventory: %+v %v", out, err)
			}
		})
	}
}
func TestRuntimeUnavailableAndLargeInventory(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	node := &runtimeTestNode{err: errors.New("sensitive upstream failure")}
	repo := &fakeRepository{app: app}
	svc := runtimeService(t, repo, node)
	out, err := svc.GetApplicationRuntime(context.Background(), user, app.ID)
	if err != nil || out.Coverage != "unavailable" || out.ObservedAt != nil || out.Reason != "inventory_unavailable" {
		t.Fatalf("invented observation: %+v %v", out, err)
	}
	node.err = nil
	for i := 0; i < 105; i++ {
		node.inventory = append(node.inventory, runtimeFixture(app, fmt.Sprintf("%064x", 105-i), "running", ""))
	}
	out, err = svc.GetApplicationRuntime(context.Background(), user, app.ID)
	if err != nil || out.Coverage != "limited" || len(out.Containers) != 100 || out.Containers[0].ID != fmt.Sprintf("%064x", 1) {
		t.Fatalf("unbounded/unstable selection: %+v %v", out, err)
	}
	node.inventory = nil
	out, err = svc.GetApplicationRuntime(context.Background(), user, app.ID)
	if err != nil || out.Coverage != "complete" || len(out.Containers) != 0 {
		t.Fatalf("empty inventory: %+v %v", out, err)
	}
}
func TestRuntimeRoute(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {ErrNotFound, 404}, {ErrConflict, 409}} {
		router := chi.NewRouter()
		user, app := uuid.New(), uuid.New()
		svc := &fakeDeployService{getErr: test.err}
		Mount(router, func(h http.Handler) http.Handler { return h }, func(context.Context) (uuid.UUID, bool) { return user, true }, svc)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/runtime", nil))
		if recorder.Code != test.status || svc.seenUser != user || svc.seenApplication != app {
			t.Fatalf("route status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
