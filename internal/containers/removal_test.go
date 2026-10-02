package containers

import (
	"context"
	"errors"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestRemoveVolumeRoutesToAgent proves the service forwards the volume name
// and rejects an empty one before dialing.
func TestRemoveVolumeRoutesToAgent(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{}
	svc := fixture(registry, mock, newFakeCache())

	if err := svc.RemoveVolume(context.Background(), server.ID, "gotham-db-abc"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
	if mock.removeVolumes != 1 || mock.removeVolumeSeen != "gotham-db-abc" {
		t.Errorf("volume removals = %d seen %q, want 1 and gotham-db-abc", mock.removeVolumes, mock.removeVolumeSeen)
	}

	_, starts, _, _ := mock.counts()
	if starts != 0 {
		t.Errorf("starts = %d, want no container action", starts)
	}
}

// TestRemoveVolumeRejectsEmptyName: a blank name is a validation error and
// never reaches the agent.
func TestRemoveVolumeRejectsEmptyName(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{}
	svc := fixture(registry, mock, newFakeCache())

	if err := svc.RemoveVolume(context.Background(), server.ID, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("RemoveVolume error = %v, want ErrValidation", err)
	}
	if mock.removeVolumes != 0 {
		t.Errorf("volume removals = %d, want 0 for an invalid name", mock.removeVolumes)
	}
}

// TestNewContainerCarriesHealth pins the DTO mapping the readiness gate reads.
func TestNewContainerCarriesHealth(t *testing.T) {
	container := newContainer(&agentv1.ContainerInfo{
		Id:     "abc123",
		State:  "running",
		Status: "Up 30 seconds (health: starting)",
		Health: HealthStarting,
	})
	if container.Health != HealthStarting {
		t.Errorf("Health = %q, want %q", container.Health, HealthStarting)
	}
	empty := newContainer(&agentv1.ContainerInfo{Id: "def456", State: "running", Status: "Up 2 hours"})
	if empty.Health != "" {
		t.Errorf("Health = %q, want empty for a container without a healthcheck", empty.Health)
	}
}

// TestHealthcheckToProto pins the seconds conversion and the disabled cases.
func TestHealthcheckToProto(t *testing.T) {
	proto := (&Healthcheck{
		Test:        []string{"pg_isready", "-U", "app"},
		Interval:    2 * time.Second,
		Timeout:     5 * time.Second,
		Retries:     3,
		StartPeriod: 60 * time.Second,
	}).toProto()
	if proto == nil {
		t.Fatal("toProto returned nil for a configured healthcheck")
	}
	if got := proto.GetIntervalSeconds(); got != 2 {
		t.Errorf("interval = %d, want 2", got)
	}
	if got := proto.GetStartPeriodSeconds(); got != 60 {
		t.Errorf("start period = %d, want 60", got)
	}
	if proto.GetRetries() != 3 {
		t.Errorf("retries = %d, want 3", proto.GetRetries())
	}

	if (*Healthcheck)(nil).toProto() != nil {
		t.Error("a nil healthcheck must map to nil")
	}
	if (&Healthcheck{}).toProto() != nil {
		t.Error("an empty healthcheck must map to nil")
	}
}
