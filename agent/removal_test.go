package agent

import (
	"context"
	"errors"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRemoveVolumeConfinesNames proves the agent only deletes managed database
// volumes: an arbitrary node volume cannot be removed even by a compromised
// control plane.
func TestRemoveVolumeConfinesNames(t *testing.T) {
	fake := &fakeDockerClient{}
	server := NewDockerServer(fake, discardLogger())

	valid := "gotham-db-11111111-2222-3333-4444-555555555555"
	if _, err := server.RemoveVolume(context.Background(), &agentv1.VolumeActionRequest{Name: valid}); err != nil {
		t.Fatalf("RemoveVolume(%q): %v", valid, err)
	}
	if len(fake.removedVolumes) != 1 || fake.removedVolumes[0] != valid {
		t.Fatalf("removed volumes = %v, want [%s]", fake.removedVolumes, valid)
	}

	for _, name := range []string{
		"",
		"   ",
		"etc",
		"gotham-services-data",
		"gotham-db-not-a-uuid",
		"gotham-db-11111111-2222-3333-4444-55555555555", // too short
		"../../etc",
	} {
		if _, err := server.RemoveVolume(context.Background(), &agentv1.VolumeActionRequest{Name: name}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("RemoveVolume(%q) code = %v, want InvalidArgument", name, status.Code(err))
		}
	}
	if len(fake.removedVolumes) != 1 {
		t.Errorf("removed volumes = %v, want only the valid managed name", fake.removedVolumes)
	}
}

// TestRemoveVolumeMapsAgentFailure: an engine error is surfaced as a gRPC
// error rather than a silent success.
func TestRemoveVolumeMapsAgentFailure(t *testing.T) {
	fake := &fakeDockerClient{err: errors.New("daemon down")}
	server := NewDockerServer(fake, discardLogger())

	_, err := server.RemoveVolume(context.Background(),
		&agentv1.VolumeActionRequest{Name: "gotham-db-11111111-2222-3333-4444-555555555555"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal for an unexpected engine error", status.Code(err))
	}
}

// TestContainerHealthParsing pins the Docker summary-status mapping the
// readiness gate depends on.
func TestContainerHealthParsing(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"Up 2 seconds", ""},
		{"Up 30 seconds (health: starting)", "starting"},
		{"Up 2 minutes (healthy)", "healthy"},
		{"Up 5 minutes (unhealthy)", "unhealthy"},
	}
	for _, tt := range tests {
		if got := containerHealth(tt.status); got != tt.want {
			t.Errorf("containerHealth(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
