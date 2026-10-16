package toolchain

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Channel is the realtime room carrying a node's toolchain job events. It
// deliberately reuses the logs:{serverID}:{stream} namespace so the existing
// WebSocket bridge (logs:*:* PSubscribe) forwards these frames unchanged and
// the existing per-node subscription authorizer applies: RBAC is exactly the
// container/deploy log RBAC (the node's team), with no new WS code.
func Channel(serverID uuid.UUID) string {
	return "logs:" + serverID.String() + ":toolchain"
}

// Realtime frame types. They are distinct from the container-log "log" type so
// a client can tell job state transitions from output lines on the same
// channel; both carry a JSON payload in Data.
const (
	// TypeJob carries a Job (state transition).
	TypeJob = "toolchain_job"
	// TypeLog carries a LogLine (one output line of a job).
	TypeLog = "toolchain_log"
)

// Publisher abstracts the realtime fan-out (Redis in production, hub or memory
// in tests). It mirrors the deploy Publisher seam.
type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

// Frame is the JSON envelope exchanged with WebSocket clients. It mirrors
// ws.Message without importing the HTTP server tree.
type Frame struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data"`
}

// LogLine is the payload of a TypeLog frame: which job the line belongs to and
// which stream produced it.
type LogLine struct {
	JobID  string `json:"job_id"`
	Stream string `json:"stream,omitempty"`
	Line   string `json:"line"`
}

// Emitter publishes toolchain events to subscribers of a node's channel. A nil
// publisher makes every method a no-op, and errors are swallowed: realtime
// updates are best-effort and must never fail the job they describe.
type Emitter struct {
	pub Publisher
}

// NewEmitter returns an Emitter publishing through pub.
func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

// Job publishes a job state transition.
func (e *Emitter) Job(ctx context.Context, job Job) {
	e.publish(ctx, job.ServerID, TypeJob, job)
}

// Log publishes one output line of a job.
func (e *Emitter) Log(ctx context.Context, serverID, jobID uuid.UUID, stream, line string) {
	e.publish(ctx, serverID, TypeLog, LogLine{JobID: jobID.String(), Stream: stream, Line: line})
}

func (e *Emitter) publish(ctx context.Context, serverID uuid.UUID, kind string, payload any) {
	if e == nil || e.pub == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame, err := json.Marshal(Frame{Channel: Channel(serverID), Type: kind, Data: string(data)})
	if err != nil {
		return
	}
	_ = e.pub.Publish(ctx, Channel(serverID), string(frame))
}
