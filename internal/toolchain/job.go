package toolchain

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Action string

const (
	ActionInstall   Action = "install"
	ActionUninstall Action = "uninstall"
)

func (a Action) Valid() bool { return a == ActionInstall || a == ActionUninstall }

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

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

type JobDetail struct {
	Job     Job
	Log     string
	NextSeq int64
}

type EnqueueResult struct {
	Jobs    []Job    `json:"jobs"`
	Skipped []string `json:"skipped"`
}

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

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func timeFromPG(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func appendLogParams(jobID uuid.UUID, seq int64, stream, line string) sqlc.AppendToolchainJobLogParams {
	return sqlc.AppendToolchainJobLogParams{JobID: pgUUID(jobID), Seq: seq, Stream: stream, Line: line}
}
