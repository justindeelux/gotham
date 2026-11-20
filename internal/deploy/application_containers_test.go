package deploy

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/services"
)

type fakeApplicationContainers struct {
	containers.ContainerService

	mu         sync.Mutex
	listed     []containers.Container
	states     map[string]string
	calls      []string
	listLabels map[string]string
	listErr    error
	actionErr  error
}

func (f *fakeApplicationContainers) ListFreshByLabel(_ context.Context, _ uuid.UUID, labels map[string]string) ([]containers.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listLabels = labels
	if f.listErr != nil {
		return nil, f.listErr
	}
	rows := make([]containers.Container, 0, len(f.listed))
	for _, row := range f.listed {
		if state, ok := f.states[row.ID]; ok {
			row.State = state
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (f *fakeApplicationContainers) labels() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listLabels
}

func (f *fakeApplicationContainers) Start(_ context.Context, _ uuid.UUID, containerID string) error {
	return f.apply(containerID, "start", "running")
}

func (f *fakeApplicationContainers) Stop(_ context.Context, _ uuid.UUID, containerID string) error {
	return f.apply(containerID, "stop", "exited")
}

func (f *fakeApplicationContainers) Restart(_ context.Context, _ uuid.UUID, containerID string) error {
	return f.apply(containerID, "restart", "running")
}

func (f *fakeApplicationContainers) apply(containerID, action, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action+":"+containerID)
	if f.actionErr != nil {
		return f.actionErr
	}
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[containerID] = state
	return nil
}

func (f *fakeApplicationContainers) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestListApplicationContainersSingleApp(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	ids := []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	svc := newTestService(t, &fakeRepository{app: app})
	fake := &fakeApplicationContainers{listed: []containers.Container{
		{ID: ids[0], Name: "demo-web", Image: "gotham/demo:1", State: "running", Status: "Up 2 minutes",
			Labels: map[string]string{labelAppID: app.ID.String()}},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "other",
			State: "running", Labels: map[string]string{labelAppID: uuid.New().String()}},
	}}
	svc.containers = fake

	rows, err := svc.ListApplicationContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("list application containers: %v", err)
	}
	if labels := fake.labels(); len(labels) != 1 || labels[labelAppID] != app.ID.String() {
		t.Errorf("node filter = %v, want the application ownership label", labels)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want the application's single container", len(rows))
	}
	row := rows[0]
	if row.ID != ids[0] || row.Name != "demo-web" || row.Image != "gotham/demo:1" || row.State != "running" {
		t.Errorf("row = %+v", row)
	}
	if row.Service != "" {
		t.Errorf("single app service = %q, want empty", row.Service)
	}
}

func TestListApplicationContainersComposeApp(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceCompose
	project := services.ProjectName(app.ID)
	svc := newTestService(t, &fakeRepository{app: app})
	fake := &fakeApplicationContainers{listed: []containers.Container{
		{ID: "web", Name: "shop-web-1", State: "running", Labels: map[string]string{
			composeProjectLabel: project, composeServiceLabel: "web",
		}},
		{ID: "db", Name: "shop-db-1", State: "exited", Labels: map[string]string{
			composeProjectLabel: project, composeServiceLabel: "db",
		}},
		{ID: "foreign", Name: "other-web-1", State: "running", Labels: map[string]string{
			composeProjectLabel: services.ProjectName(uuid.New()), composeServiceLabel: "web",
		}},
	}}
	svc.containers = fake

	rows, err := svc.ListApplicationContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("list application containers: %v", err)
	}
	if labels := fake.labels(); len(labels) != 1 || labels[composeProjectLabel] != project {
		t.Errorf("node filter = %v, want the compose project label", labels)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want one row per compose service", len(rows))
	}
	if rows[0].Service != "db" || rows[1].Service != "web" {
		t.Errorf("services = %q, %q, want db then web", rows[0].Service, rows[1].Service)
	}
}

func TestApplicationContainerActionRejectsForeignContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	fake := &fakeApplicationContainers{listed: []containers.Container{
		{ID: "mine", State: "running", Labels: map[string]string{labelAppID: app.ID.String()}},
		{ID: "foreign", State: "running", Labels: map[string]string{labelAppID: uuid.New().String()}},
	}}
	svc := newTestService(t, &fakeRepository{app: app})
	svc.containers = fake
	monitor := &recordingRuntimeMonitor{desired: "running"}
	svc.runtimeMonitor = monitor

	err := svc.StopApplicationContainer(context.Background(), userID, app.ID, "foreign")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("err = %v, want ErrContainerNotFound", err)
	}
	if calls := fake.actions(); len(calls) != 0 {
		t.Errorf("actions = %v, want no container action", calls)
	}
	if got := monitor.last(); got != "running" {
		t.Errorf("desired state = %q, want it unchanged", got)
	}
}

func TestContainerActionsKeepDesiredStateConsistent(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	fake := &fakeApplicationContainers{listed: []containers.Container{{
		ID: "mine", State: "running", Labels: map[string]string{labelAppID: app.ID.String()},
	}}}
	svc := newTestService(t, &fakeRepository{app: app})
	svc.containers = fake
	monitor := &recordingRuntimeMonitor{desired: "running"}
	svc.runtimeMonitor = monitor

	if err := svc.StopApplicationContainer(context.Background(), userID, app.ID, "mine"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := monitor.last(); got != "stopped" {
		t.Fatalf("desired state after stop = %q, want stopped", got)
	}

	if err := svc.StartApplicationContainer(context.Background(), userID, app.ID, "mine"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := monitor.last(); got != "running" {
		t.Fatalf("desired state after start = %q, want running", got)
	}
}

func TestComposeContainerStopKeepsRunningUntilEveryServiceStops(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceCompose
	project := services.ProjectName(app.ID)
	fake := &fakeApplicationContainers{listed: []containers.Container{
		{ID: "web", State: "running", Labels: map[string]string{composeProjectLabel: project, composeServiceLabel: "web"}},
		{ID: "db", State: "running", Labels: map[string]string{composeProjectLabel: project, composeServiceLabel: "db"}},
	}}
	svc := newTestService(t, &fakeRepository{app: app})
	svc.containers = fake
	monitor := &recordingRuntimeMonitor{desired: "running"}
	svc.runtimeMonitor = monitor

	if err := svc.StopApplicationContainer(context.Background(), userID, app.ID, "web"); err != nil {
		t.Fatalf("stop web: %v", err)
	}
	if got := monitor.last(); got != "running" {
		t.Fatalf("desired state = %q, want running while db still runs", got)
	}

	if err := svc.StopApplicationContainer(context.Background(), userID, app.ID, "db"); err != nil {
		t.Fatalf("stop db: %v", err)
	}
	if got := monitor.last(); got != "stopped" {
		t.Fatalf("desired state = %q, want stopped once every service stopped", got)
	}
}
