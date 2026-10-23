package toolchain

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

func Channel(serverID uuid.UUID) string {
	return "logs:" + serverID.String() + ":toolchain"
}

const (
	TypeJob = "toolchain_job"
	TypeLog = "toolchain_log"
)

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

type Frame struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data"`
}

type LogLine struct {
	JobID  string `json:"job_id"`
	Seq    int64  `json:"seq"`
	Stream string `json:"stream,omitempty"`
	Line   string `json:"line"`
}

type Emitter struct {
	pub Publisher
}

func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

func (e *Emitter) Job(ctx context.Context, job Job) {
	e.publish(ctx, job.ServerID, TypeJob, job)
}

func (e *Emitter) Log(ctx context.Context, serverID, jobID uuid.UUID, seq int64, stream, line string) {
	e.publish(ctx, serverID, TypeLog, LogLine{JobID: jobID.String(), Seq: seq, Stream: stream, Line: line})
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
