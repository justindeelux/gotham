package tools

import (
	"fmt"
	"sync"

	"github.com/justindeelux/gotham/toolcatalog"
)

type ToolUseTracker struct {
	mu           sync.Mutex
	builds       int
	uninstalling bool
}

func NewToolUseTracker() *ToolUseTracker { return &ToolUseTracker{} }

func (t *ToolUseTracker) BeginBuild() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.uninstalling {
		return fmt.Errorf("a tool uninstall is in progress")
	}
	t.builds++
	return nil
}

func (t *ToolUseTracker) EndBuild() {
	t.mu.Lock()
	if t.builds > 0 {
		t.builds--
	}
	t.mu.Unlock()
}

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
