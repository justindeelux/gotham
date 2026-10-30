package docker

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRemoveVolumeConfinesNames(t *testing.T) {
	fake := &fakeDockerClient{}
	server := NewDockerServer(fake, discardLogger())

	valid := []string{
		"gotham-db-11111111-2222-3333-4444-555555555555",
		"gotham-app-11111111-2222-3333-4444-555555555555-uploads",
	}
	for _, name := range valid {
		if _, err := server.RemoveVolume(context.Background(), &agentv1.VolumeActionRequest{Name: name}); err != nil {
			t.Fatalf("RemoveVolume(%q): %v", name, err)
		}
	}
	if strings.Join(fake.removedVolumes, ",") != strings.Join(valid, ",") {
		t.Fatalf("removed volumes = %v, want %v", fake.removedVolumes, valid)
	}

	for _, name := range []string{
		"",
		"   ",
		"etc",
		"gotham-services-data",
		"gotham-db-not-a-uuid",
		"gotham-db-11111111-2222-3333-4444-55555555555",
		"gotham-app-not-a-uuid-data",
		"gotham-app-11111111-2222-3333-4444-555555555555",
		"gotham-app-11111111-2222-3333-4444-555555555555../etc",
		"../../etc",
		"x-gotham-db-11111111-2222-3333-4444-555555555555",
		"gotham-db-11111111-2222-3333-4444-555555555555x",
		"gotham-db-11111111-2222-3333-4444-555555555555\n",
		"gotham-db-11111111-2222-3333-4444-555555555555 extra",
	} {
		if _, err := server.RemoveVolume(context.Background(), &agentv1.VolumeActionRequest{Name: name}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("RemoveVolume(%q) code = %v, want InvalidArgument", name, status.Code(err))
		}
	}
	if len(fake.removedVolumes) != len(valid) {
		t.Errorf("removed volumes = %v, want only the valid managed names", fake.removedVolumes)
	}
}

func TestRemoveVolumeMapsAgentFailure(t *testing.T) {
	fake := &fakeDockerClient{err: errors.New("daemon down")}
	server := NewDockerServer(fake, discardLogger())

	_, err := server.RemoveVolume(context.Background(),
		&agentv1.VolumeActionRequest{Name: "gotham-db-11111111-2222-3333-4444-555555555555"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal for an unexpected engine error", status.Code(err))
	}
}

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
