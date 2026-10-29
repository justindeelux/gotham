// Package backupwatch evaluates persisted backup schedules against completed
// backup results. It reuses the incident lifecycle and never polls an agent,
// starts a backup or mutates a running job.
package backupwatch

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

// Detector is the stable incident detector key.
const Detector = "backup_protection"

// Incident resource types shown by the Needs attention surface. The id is the
// resource's own id, so the existing detail-page links work for every type.
const (
	ResourceApplication = "application"
	ResourceDatabase    = "database"
	ResourceService     = "service"
)

// Protection statuses shown by the API and UI.
const (
	StatusNotConfigured = "not_configured"
	StatusScheduled     = "scheduled"
	StatusPending       = "pending"
	StatusProtected     = "protected"
	StatusRunning       = "running"
	StatusOverdue       = "overdue"
	StatusStuck         = "stuck"
)

const (
	// OverdueGrace is the fixed window after an expected slot before a missing
	// success becomes an incident. It is deliberately independent of the cron
	// cadence so weekly and monthly schedules are not held to a 24-hour rule.
	OverdueGrace = 30 * time.Minute

	// MaxRunDefault bounds a stale in-progress run even while a live lease is
	// still held. The maximum is enforced for every lease state, so a live job
	// cannot hide a hung backup forever.
	MaxRunDefault = 2 * time.Hour
	MaxRunMin     = 15 * time.Minute
	MaxRunMax     = 24 * time.Hour
)

var ErrNotFound = errors.New("backupwatch: not found")

// LeaseState is the control-plane evidence for a running backup job.
type LeaseState int

const (
	// LeaseUnknown means no lease reader is available (for example a standalone
	// observer); the MaxRun window still decides when the job is stuck.
	LeaseUnknown LeaseState = iota
	LeaseLive
	LeaseExpired
)

// ScheduleEvidence is one persisted schedule plus its newest qualifying
// success and newest running run for the same resource/target.
type ScheduleEvidence struct {
	ScheduleID      uuid.UUID
	ResourceType    string
	ResourceID      uuid.UUID
	ResourceName    string
	ProjectID       uuid.UUID
	Cron            string
	TargetID        uuid.UUID
	TargetName      string
	Enabled         bool
	NextRunAt       time.Time
	LastSlotAt      time.Time
	LastRunAt       time.Time
	TargetChangedAt time.Time
	MaxRunSeconds   int
	LastSuccessAt   *time.Time
	LastSuccessID   uuid.UUID
	RunningID       uuid.UUID
	RunningSince    *time.Time
}

// Evaluation is the pure result of one schedule at one instant.
type Evaluation struct {
	Status          string
	DueSlotAt       *time.Time
	DeadlineAt      *time.Time
	OverdueSince    *time.Time
	LastSuccessAt   *time.Time
	LastSuccessID   uuid.UUID
	RunningSince    *time.Time
	RunningBackupID uuid.UUID
	MaxRun          time.Duration
	Evidence        string
}

// ScheduleView is the API representation of one evaluated schedule.
type ScheduleView struct {
	ScheduleID      string     `json:"schedule_id"`
	Cron            string     `json:"cron"`
	TargetID        string     `json:"target_id,omitempty"`
	TargetName      string     `json:"target_name,omitempty"`
	Status          string     `json:"status"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	LastBackupID    string     `json:"last_backup_id,omitempty"`
	ExpectedSlot    *time.Time `json:"expected_slot_at,omitempty"`
	DeadlineAt      *time.Time `json:"deadline_at,omitempty"`
	OverdueSince    *time.Time `json:"overdue_since,omitempty"`
	OverdueSeconds  int64      `json:"overdue_seconds"`
	RunningSince    *time.Time `json:"running_since,omitempty"`
	RunningBackupID string     `json:"running_backup_id,omitempty"`
	MaxRunSeconds   int        `json:"max_run_seconds"`
}

// Summary is the protection view of one resource.
type Summary struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	// DatabaseID stays for the existing database API consumers; it is empty
	// for applications and services.
	DatabaseID string         `json:"database_id,omitempty"`
	Status     string         `json:"status"`
	Schedules  []ScheduleView `json:"schedules"`
}

// Identity is stable per schedule, not per slot: a later missed slot opens a
// new occurrence only after the previous one resolved. Including the resource
// type keeps application and service schedules from sharing a database's
// occurrence; the database fingerprint is unchanged.
func Identity(resourceType string, scheduleID uuid.UUID) string {
	return incidents.Identity(Detector, resourceType, scheduleID)
}

// Evaluate applies the persisted deadline rules. It is pure so the observer and
// the API always agree.
func Evaluate(now time.Time, ev ScheduleEvidence, lease LeaseState) Evaluation {
	return evaluate(now, ev, lease, false)
}

// EvaluateOpen evaluates an already-open occurrence without the target-change
// grace, so editing a schedule cannot close a firing incident.
func EvaluateOpen(now time.Time, ev ScheduleEvidence, lease LeaseState) Evaluation {
	return evaluate(now, ev, lease, true)
}

func evaluate(now time.Time, ev ScheduleEvidence, lease LeaseState, keepOpen bool) Evaluation {
	now = now.UTC()
	maxRun := maxRunDuration(ev.MaxRunSeconds)
	result := Evaluation{
		LastSuccessAt: ev.LastSuccessAt,
		LastSuccessID: ev.LastSuccessID,
		MaxRun:        maxRun,
	}
	result.Status = StatusScheduled

	slot := dueSlot(ev, now)
	if slot == nil {
		result.Evidence = evidence(ev, "next slot "+formatTime(ev.NextRunAt), "no success needed yet")
		return result
	}
	// A slot covered by the old target is not evidence about the new target.
	// Wait for the next slot instead of alerting for a slot that was in fact
	// protected before the edit. An already-open incident bypasses this.
	if !keepOpen && !ev.TargetChangedAt.IsZero() && !slot.After(ev.TargetChangedAt) {
		result.Evidence = evidence(ev, "target changed "+formatTime(ev.TargetChangedAt),
			"waiting for the next slot")
		return result
	}
	deadline := slot.Add(OverdueGrace)
	result.DueSlotAt = slot
	result.DeadlineAt = &deadline

	if ev.LastSuccessAt != nil && !ev.LastSuccessAt.Before(*slot) {
		result.Status = StatusProtected
		result.Evidence = evidence(ev, "slot "+formatTime(*slot)+" covered")
		return result
	}

	if ev.RunningSince != nil && !ev.RunningSince.Before(*slot) {
		result.RunningSince = ev.RunningSince
		result.RunningBackupID = ev.RunningID
		stuck := false
		reason := ""
		switch lease {
		case LeaseLive:
			if now.Sub(*ev.RunningSince) > maxRun {
				stuck = true
				reason = fmt.Sprintf("live lease, running for %s (max %s)",
					formatDuration(now.Sub(*ev.RunningSince)), formatDuration(maxRun))
			}
		case LeaseExpired:
			stuck = true
			reason = "lease expired"
		default:
			if now.Sub(*ev.RunningSince) > maxRun {
				stuck = true
				reason = fmt.Sprintf("no live lease, running for %s (max %s)",
					formatDuration(now.Sub(*ev.RunningSince)), formatDuration(maxRun))
			}
		}
		if stuck {
			result.Status = StatusStuck
			result.OverdueSince = &deadline
			result.Evidence = evidence(ev, "running since "+formatTime(*ev.RunningSince)+", "+reason,
				"slot "+formatTime(*slot)+" not completed")
			return result
		}
		result.Status = StatusRunning
		result.Evidence = evidence(ev, "running since "+formatTime(*ev.RunningSince),
			"slot "+formatTime(*slot)+" deferred until the job ends")
		return result
	}

	if now.Before(deadline) {
		result.Status = StatusPending
		result.Evidence = evidence(ev, "waiting for deadline "+formatTime(deadline),
			"slot "+formatTime(*slot)+" is not overdue yet")
		return result
	}

	result.Status = StatusOverdue
	result.OverdueSince = &deadline
	result.Evidence = evidence(ev, "no successful backup for slot "+formatTime(*slot),
		"deadline "+formatTime(deadline)+" passed")
	return result
}

func dueSlot(ev ScheduleEvidence, now time.Time) *time.Time {
	if !ev.NextRunAt.IsZero() && !ev.NextRunAt.After(now) {
		slot := ev.NextRunAt.UTC()
		return &slot
	}
	if !ev.LastSlotAt.IsZero() && !ev.LastSlotAt.After(now) {
		slot := ev.LastSlotAt.UTC()
		return &slot
	}
	return nil
}

func maxRunDuration(seconds int) time.Duration {
	if seconds < int(MaxRunMin/time.Second) || seconds > int(MaxRunMax/time.Second) {
		return MaxRunDefault
	}
	return time.Duration(seconds) * time.Second
}

func evidence(ev ScheduleEvidence, parts ...string) string {
	segments := make([]string, 0, len(parts)+3)
	segments = append(segments, parts...)
	segments = append(segments, "target "+targetLabel(ev))
	if ev.LastSuccessAt != nil {
		segments = append(segments, "last success "+formatTime(*ev.LastSuccessAt))
	} else {
		segments = append(segments, "last success none")
	}
	return strings.Join(segments, "; ")
}

func targetLabel(ev ScheduleEvidence) string {
	if strings.TrimSpace(ev.TargetName) != "" {
		return ev.TargetName
	}
	if ev.TargetID == uuid.Nil {
		return "local disk"
	}
	return "target " + ev.TargetID.String()
}

// OverdueElapsed clamps the elapsed time after the deadline to zero.
func OverdueElapsed(now time.Time, deadline time.Time) time.Duration {
	elapsed := now.UTC().Sub(deadline.UTC())
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// Summarize reduces the per-schedule evaluations to one resource status. The
// worst state wins so a protected sibling cannot hide a missed schedule. An
// open occurrence is evaluated from its original slot even after a target
// edit, matching the observer.
func Summarize(sqlcRows []ScheduleEvidence, now time.Time, lease func(uuid.UUID, uuid.UUID) LeaseState, open func(uuid.UUID) bool) Summary {
	enabled := make([]ScheduleEvidence, 0, len(sqlcRows))
	for _, row := range sqlcRows {
		if row.Enabled {
			enabled = append(enabled, row)
		}
	}
	if len(enabled) == 0 {
		return Summary{Status: StatusNotConfigured, Schedules: []ScheduleView{}}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].ScheduleID.String() < enabled[j].ScheduleID.String() })
	summary := Summary{Status: StatusProtected, Schedules: make([]ScheduleView, 0, len(enabled))}
	for _, row := range enabled {
		state := LeaseUnknown
		if lease != nil {
			state = lease(row.ResourceID, row.RunningID)
		}
		evaluated := Evaluate(now, row, state)
		if open != nil && open(row.ScheduleID) {
			evaluated = EvaluateOpen(now, row, state)
		}
		view := viewOf(row, evaluated, now)
		summary.Schedules = append(summary.Schedules, view)
		if statusRank(view.Status) > statusRank(summary.Status) {
			summary.Status = view.Status
		}
	}
	return summary
}

func viewOf(row ScheduleEvidence, evaluated Evaluation, now time.Time) ScheduleView {
	view := ScheduleView{
		ScheduleID:    row.ScheduleID.String(),
		Cron:          row.Cron,
		TargetName:    row.TargetName,
		Status:        evaluated.Status,
		LastSuccessAt: evaluated.LastSuccessAt,
		ExpectedSlot:  evaluated.DueSlotAt,
		DeadlineAt:    evaluated.DeadlineAt,
		OverdueSince:  evaluated.OverdueSince,
		RunningSince:  evaluated.RunningSince,
		MaxRunSeconds: int(evaluated.MaxRun / time.Second),
	}
	if row.TargetID != uuid.Nil {
		view.TargetID = row.TargetID.String()
	}
	if evaluated.LastSuccessID != uuid.Nil {
		view.LastBackupID = evaluated.LastSuccessID.String()
	}
	if evaluated.RunningBackupID != uuid.Nil {
		view.RunningBackupID = evaluated.RunningBackupID.String()
	}
	if evaluated.DeadlineAt != nil {
		view.OverdueSeconds = int64(OverdueElapsed(now, *evaluated.DeadlineAt) / time.Second)
	}
	return view
}

func statusRank(status string) int {
	switch status {
	case StatusStuck:
		return 6
	case StatusOverdue:
		return 5
	case StatusRunning:
		return 4
	case StatusPending:
		return 3
	case StatusScheduled:
		return 2
	case StatusProtected:
		return 1
	default:
		return 0
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "none"
	}
	return t.UTC().Format(time.RFC3339)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
