package agent

import (
	"fmt"
	"sync"

	"github.com/justindeelux/gotham/toolcatalog"
)

// ToolUseTracker records which host tools are busy on a node so an uninstall
// refuses before it pulls a tool out from under running work, and so a build
// cannot start into an uninstall's removal window. The build service marks a
// build in flight and the tool service reads it; the tool service marks an
// uninstall in flight and the build service refuses while it runs.
type ToolUseTracker struct {
	mu           sync.Mutex
	builds       int
	uninstalling bool
}

// NewToolUseTracker returns an empty tracker.
func NewToolUseTracker() *ToolUseTracker { return &ToolUseTracker{} }

// BeginBuild marks a build in flight, refusing when an uninstall is running so
// a build never starts against a toolchain being removed.
func (t *ToolUseTracker) BeginBuild() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.uninstalling {
		return fmt.Errorf("a tool uninstall is in progress")
	}
	t.builds++
	return nil
}

// EndBuild clears one in-flight build.
func (t *ToolUseTracker) EndBuild() {
	t.mu.Lock()
	if t.builds > 0 {
		t.builds--
	}
	t.mu.Unlock()
}

// BeginUninstall marks an uninstall in flight and returns the release function
// that clears it. While it runs, BeginBuild is refused.
func (t *ToolUseTracker) BeginUninstall(string) func() {
	t.mu.Lock()
	t.uninstalling = true
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.uninstalling = false
		t.mu.Unlock()
	}
}

// InUse reports whether a tool is busy. While any build runs, every tool a
// build engine uses is in use: Dockerfile builds need docker, Railpack needs
// railpack with buildkit and docker, and buildpacks needs pack.
func (t *ToolUseTracker) InUse(id string) (bool, string) {
	switch id {
	case toolcatalog.IDDocker, toolcatalog.IDBuildKit, toolcatalog.IDRailpack, toolcatalog.IDPack:
		t.mu.Lock()
		busy := t.builds > 0
		t.mu.Unlock()
		if busy {
			return true, "a build is running"
		}
	}
	return false, ""
}
