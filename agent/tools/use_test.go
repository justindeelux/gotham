package tools

import (
	"testing"

	"github.com/justindeelux/gotham/toolcatalog"
)

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
