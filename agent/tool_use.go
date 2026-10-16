package agent

import (
	"sync"

	"github.com/justindeelux/gotham/toolcatalog"
)

// ToolUseTracker records which host tools are busy on a node so an uninstall
// can refuse before it pulls a tool out from under running work. The build
// service marks a build in flight and the tool service reads it, so the two
// agree on what "in use" means.
type ToolUseTracker struct {
	mu     sync.Mutex
	builds int
}

// NewToolUseTracker returns an empty tracker.
func NewToolUseTracker() *ToolUseTracker { return &ToolUseTracker{} }

// BeginBuild marks a build in flight.
func (t *ToolUseTracker) BeginBuild() {
	t.mu.Lock()
	t.builds++
	t.mu.Unlock()
}

// EndBuild clears one in-flight build.
func (t *ToolUseTracker) EndBuild() {
	t.mu.Lock()
	if t.builds > 0 {
		t.builds--
	}
	t.mu.Unlock()
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
