package toolchain

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Action is what a job does to a tool.
type Action string

// Job actions. Valid reports whether an action is one of them.
const (
	ActionInstall   Action = "install"
	ActionUninstall Action = "uninstall"
)

// Valid reports whether a is a known action.
func (a Action) Valid() bool { return a == ActionInstall || a == ActionUninstall }

// Status is a job's lifecycle state.
type Status string

// Job lifecycle states. queued -> running -> one terminal state.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Terminal reports whether s ends a job.
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

// Job is one queued install/uninstall. The JSON shape is the wire contract the
// Web UI reads (job list, job detail, realtime frames). The log is deliberately
// NOT part of it: it lives in the append-only toolchain_job_logs table and is
// fetched separately (JobDetail) so a listed or broadcast job never carries
// megabytes of output.
type Job struct {
	ID         uuid.UUID  `json:"id"`
	ServerID   uuid.UUID  `json:"server_id"`
	ToolID     string     `json:"tool_id"`
	Action     Action     `json:"action"`
	Status     Status     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// JobDetail is a job plus a page of its log. Log is the assembled text of the
// returned lines (each already newline-terminated); NextSeq is the highest seq
// in the page, so a client can poll `?after=NextSeq` for the next lines.
type JobDetail struct {
	Job     Job
	Log     string
	NextSeq int64
}

// EnqueueResult reports the outcome of one enqueue request: the jobs it created
// (in request order) and the tool ids it skipped because the node already had an
// active job for that tool and action.
type EnqueueResult struct {
	Jobs    []Job    `json:"jobs"`
	Skipped []string `json:"skipped"`
}

// jobFromRow converts a sqlc row to the domain/wire type.
func jobFromRow(row sqlc.ToolchainJob) Job {
	return Job{
		ID:         uuidFromPG(row.ID),
		ServerID:   uuidFromPG(row.ServerID),
		ToolID:     row.ToolID,
		Action:     Action(row.Action),
		Status:     Status(row.Status),
		CreatedAt:  row.CreatedAt.Time.UTC(),
		StartedAt:  timeFromPG(row.StartedAt),
		FinishedAt: timeFromPG(row.FinishedAt),
	}
}

// pgUUID converts a domain id to a sqlc parameter (zero -> NULL).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL -> zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

// timeFromPG converts a nullable sqlc timestamp to a pointer (NULL -> nil).
func timeFromPG(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

// appendLogParams builds the sqlc parameters for appending one log line.
func appendLogParams(jobID uuid.UUID, seq int64, stream, line string) sqlc.AppendToolchainJobLogParams {
	return sqlc.AppendToolchainJobLogParams{JobID: pgUUID(jobID), Seq: seq, Stream: stream, Line: line}
}
