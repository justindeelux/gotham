package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/cleanup"
	"github.com/justindeelux/gotham/internal/instance"
)

type fakeCleanupHostResolver struct {
	host instance.NodeHost
}

func (f fakeCleanupHostResolver) NodeHost(context.Context, uuid.UUID) (instance.NodeHost, error) {
	return f.host, nil
}

type fakeCleanupApplier struct {
	instance.HostApplier
	statusErr error
	cleanErr  error
}

func (f fakeCleanupApplier) CleanupStatus(context.Context) (instance.HostCleanupStatus, error) {
	return instance.HostCleanupStatus{}, f.statusErr
}

func (f fakeCleanupApplier) CleanupHost(context.Context, string, time.Duration) (int64, error) {
	return 0, f.cleanErr
}

func cleanupHostWith(err error, statusPath bool) cleanupHost {
	applier := fakeCleanupApplier{cleanErr: err}
	if statusPath {
		applier = fakeCleanupApplier{statusErr: err}
	}
	return cleanupHost{nodes: fakeCleanupHostResolver{
		host: instance.NodeHost{Applier: applier},
	}}
}

func TestCleanupHostStatusMapsHostFailureToUnsupported(t *testing.T) {
	host := cleanupHostWith(fmt.Errorf("%w: helper exit 1", instance.ErrHost), true)
	_, err := host.CleanupStatus(context.Background(), uuid.New())
	if !errors.Is(err, cleanup.ErrHostUnsupported) {
		t.Fatalf("status error = %v; want cleanup.ErrHostUnsupported", err)
	}
}

func TestCleanupHostCleanKeepsRealHostFailure(t *testing.T) {
	host := cleanupHostWith(fmt.Errorf("%w: helper exit 1", instance.ErrHost), false)
	_, err := host.CleanupHost(context.Background(), uuid.New(), "journal", 0)
	if !errors.Is(err, instance.ErrHost) {
		t.Fatalf("clean error = %v; want the host failure", err)
	}
	if errors.Is(err, cleanup.ErrHostUnsupported) {
		t.Fatalf("clean error = %v; a real failure must not look unsupported", err)
	}
}

func TestCleanupHostCleanKeepsOutdatedSentinels(t *testing.T) {
	cases := map[error]error{
		instance.ErrHelperOutdated: cleanup.ErrHostHelperOutdated,
		instance.ErrAgentOutdated:  cleanup.ErrHostAgentOutdated,
		instance.ErrUnsupported:    cleanup.ErrHostUnsupported,
	}
	for in, want := range cases {
		host := cleanupHostWith(in, false)
		_, err := host.CleanupHost(context.Background(), uuid.New(), "journal", 0)
		if !errors.Is(err, want) {
			t.Errorf("%v: clean error = %v; want %v", in, err, want)
		}
	}
}
