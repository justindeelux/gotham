package agent

import (
	"context"
	"io"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestToolUseTrackerUninstallBlocksBuild proves the uninstall mark refuses a
// build for its whole lifetime and clears afterwards.
func TestToolUseTrackerUninstallBlocksBuild(t *testing.T) {
	tracker := NewToolUseTracker()

	if err := tracker.BeginBuild(); err != nil {
		t.Fatalf("BeginBuild (idle) = %v; want nil", err)
	}
	tracker.EndBuild()

	release := tracker.BeginUninstall(toolcatalog.IDPack)
	if err := tracker.BeginBuild(); err == nil {
		t.Fatal("BeginBuild during an uninstall = nil; want a refusal")
	}
	release()
	if err := tracker.BeginBuild(); err != nil {
		t.Fatalf("BeginBuild after the uninstall = %v; want nil", err)
	}
	tracker.EndBuild()
}

// fakeBuildStream is enough of a BuildImage stream for the refusal test: the
// call is refused before the first Recv.
type fakeBuildStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeBuildStream) Context() context.Context { return s.ctx }

func (s *fakeBuildStream) Recv() (*agentv1.BuildImageRequest, error) { return nil, io.EOF }

func (s *fakeBuildStream) Send(*agentv1.BuildImageResponse) error { return nil }

// TestBuildServerRefusesDuringUninstall proves a build started while a tool
// uninstall is in flight is refused with a retryable status instead of running
// against a toolchain being removed.
func TestBuildServerRefusesDuringUninstall(t *testing.T) {
	tracker := NewToolUseTracker()
	release := tracker.BeginUninstall(toolcatalog.IDDocker)
	defer release()

	server := NewBuildServer(&fakeBuildClient{}, discardLogger(), WithToolUseTracker(tracker))
	err := server.BuildImage(&fakeBuildStream{ctx: context.Background()})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("BuildImage during uninstall = %v; want Unavailable", err)
	}
}
