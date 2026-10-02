package databases

import (
	"sync"

	"github.com/google/uuid"
)

// JobLeaseKind says which backup-surface job holds a database.
type JobLeaseKind string

const (
	// JobLeaseBackup — a dump is running and owns the volume.
	JobLeaseBackup JobLeaseKind = "backup"
	// JobLeaseRestore — a restore is running and owns the volume.
	JobLeaseRestore JobLeaseKind = "restore"
	// JobLeaseLifecycle — a Start/Restart is in flight; a job must not claim
	// the database under it.
	JobLeaseLifecycle JobLeaseKind = "lifecycle"
)

// JobLeases is the exclusion registry shared by the database service and the
// backup manager of one control plane: a dump or restore stops the database
// container and mounts its volume, so a concurrent Start/Restart (or a second
// job) must be refused while the claim is held. The map is in-memory by
// design — after a restart it is empty and the boot-time reconciliation sweep
// cleans up whatever an interrupted job left behind.
type JobLeases struct {
	mu   sync.Mutex
	held map[uuid.UUID]JobLeaseKind
}

// NewJobLeases returns an empty registry.
func NewJobLeases() *JobLeases {
	return &JobLeases{held: make(map[uuid.UUID]JobLeaseKind)}
}

// Claim marks id busy with kind unless a job already holds it. A nil registry
// never blocks, which keeps tests that do not exercise exclusion simple.
func (l *JobLeases) Claim(id uuid.UUID, kind JobLeaseKind) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.held[id]; ok {
		return false
	}
	l.held[id] = kind
	return true
}

// Release drops id's claim; releasing an unclaimed id is a no-op.
func (l *JobLeases) Release(id uuid.UUID) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.held, id)
}

// Held reports the kind currently holding id, or "" when it is free.
func (l *JobLeases) Held(id uuid.UUID) JobLeaseKind {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[id]
}
