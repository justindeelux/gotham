package databases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func readinessDatabase(cs *fakeContainers, state, health string) Database {
	cs.listed = []containers.Container{{
		ID:     "container-1",
		State:  state,
		Status: "Up 2 seconds",
		Health: health,
	}}
	return Database{
		ID:          uuid.New(),
		ServerID:    uuid.New(),
		Name:        "orders",
		Engine:      EnginePostgres,
		Status:      StatusCreating,
		ContainerID: "container-1",
	}
}

func TestWaitHealthyRejectsRunningButStarting(t *testing.T) {
	cs := &fakeContainers{}
	svc := newTestService(newFakeRepository(), cs)
	database := readinessDatabase(cs, "running", containers.HealthStarting)
	engine, _ := LookupEngine(EnginePostgres)

	err := svc.waitHealthy(context.Background(), database, engine.Healthcheck())
	if !errors.Is(err, ErrHealthcheck) {
		t.Fatalf("waitHealthy error = %v, want ErrHealthcheck for a starting engine", err)
	}
}

func TestWaitHealthyRejectsUnhealthy(t *testing.T) {
	cs := &fakeContainers{}
	svc := newTestService(newFakeRepository(), cs)
	database := readinessDatabase(cs, "running", containers.HealthUnhealthy)
	engine, _ := LookupEngine(EnginePostgres)

	if err := svc.waitHealthy(context.Background(), database, engine.Healthcheck()); !errors.Is(err, ErrHealthcheck) {
		t.Fatalf("waitHealthy error = %v, want ErrHealthcheck for an unhealthy engine", err)
	}
}

func TestWaitHealthyAcceptsHealthy(t *testing.T) {
	cs := &fakeContainers{}
	svc := newTestService(newFakeRepository(), cs)
	database := readinessDatabase(cs, "running", containers.HealthHealthy)
	engine, _ := LookupEngine(EnginePostgres)

	if err := svc.waitHealthy(context.Background(), database, engine.Healthcheck()); err != nil {
		t.Fatalf("waitHealthy: %v", err)
	}
}

func TestWaitHealthyLegacyContainerWithoutHealthcheck(t *testing.T) {
	cs := &fakeContainers{}
	svc := newTestService(newFakeRepository(), cs)
	database := readinessDatabase(cs, "running", "")
	engine, _ := LookupEngine(EnginePostgres)

	if err := svc.waitHealthy(context.Background(), database, engine.Healthcheck()); err != nil {
		t.Fatalf("waitHealthy: %v", err)
	}
}

func TestWaitHealthyStoppedContainerIsNotReady(t *testing.T) {
	cs := &fakeContainers{}
	svc := newTestService(newFakeRepository(), cs)
	database := readinessDatabase(cs, "exited", containers.HealthHealthy)
	engine, _ := LookupEngine(EnginePostgres)

	if err := svc.waitHealthy(context.Background(), database, engine.Healthcheck()); !errors.Is(err, ErrHealthcheck) {
		t.Fatalf("waitHealthy error = %v, want ErrHealthcheck for a stopped container", err)
	}
}

func TestRunHealthcheckRendersProbe(t *testing.T) {
	database, secrets := sealedSample(t, "orders", EnginePostgres, 0)
	engine, _ := LookupEngine(EnginePostgres)

	options, err := buildRunOptions(database, engine, secrets, testSecret)
	if err != nil {
		t.Fatalf("buildRunOptions: %v", err)
	}
	if options.Healthcheck == nil {
		t.Fatal("run options carry no healthcheck: readiness would fall back to container state")
	}
	if len(options.Healthcheck.Test) == 0 || options.Healthcheck.Test[0] != "pg_isready" {
		t.Errorf("healthcheck test = %v, want it to start with pg_isready", options.Healthcheck.Test)
	}
	if options.Healthcheck.StartPeriod != engine.Healthcheck().Timeout {
		t.Errorf("start period = %s, want the engine window %s",
			options.Healthcheck.StartPeriod, engine.Healthcheck().Timeout)
	}
	if options.Healthcheck.Interval != 10*time.Second {
		t.Errorf("healthcheck interval = %s, want 10s", options.Healthcheck.Interval)
	}
}
