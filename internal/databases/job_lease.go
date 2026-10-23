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

type JobLeases struct {
	mu   sync.Mutex
	held map[uuid.UUID]JobLeaseKind
}

func NewJobLeases() *JobLeases {
	return &JobLeases{held: make(map[uuid.UUID]JobLeaseKind)}
}

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
	return l.held[id]
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
