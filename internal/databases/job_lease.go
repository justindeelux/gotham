package databases

import (
	"sync"

	"github.com/google/uuid"
)

type JobLeaseKind string

const (
	JobLeaseBackup    JobLeaseKind = "backup"
	JobLeaseRestore   JobLeaseKind = "restore"
	JobLeaseLifecycle JobLeaseKind = "lifecycle"
)

type jobLease struct {
	kind JobLeaseKind
	run  uuid.UUID
}

type JobLeases struct {
	mu   sync.Mutex
	held map[uuid.UUID]jobLease
}

func NewJobLeases() *JobLeases {
	return &JobLeases{held: make(map[uuid.UUID]jobLease)}
}

func (l *JobLeases) Claim(id uuid.UUID, kind JobLeaseKind) bool {
	return l.ClaimRun(id, kind, uuid.Nil)
}

// ClaimRun records the lease together with the job run that owns it. The
// backup-protection observer needs the run id so another run on the same
// database cannot make an orphaned row look alive.
func (l *JobLeases) ClaimRun(id uuid.UUID, kind JobLeaseKind, runID uuid.UUID) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.held[id]; ok {
		return false
	}
	l.held[id] = jobLease{kind: kind, run: runID}
	return true
}

func (l *JobLeases) Release(id uuid.UUID) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.held, id)
}

func (l *JobLeases) Held(id uuid.UUID) JobLeaseKind {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[id].kind
}

// Run returns the run id that owns the database lease, or uuid.Nil when the
// lease has no run (for example a restore).
func (l *JobLeases) Run(id uuid.UUID) uuid.UUID {
	if l == nil {
		return uuid.Nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[id].run
}

func (l *JobLeases) HeldIDs() []uuid.UUID {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]uuid.UUID, 0, len(l.held))
	for id := range l.held {
		ids = append(ids, id)
	}
	return ids
}
