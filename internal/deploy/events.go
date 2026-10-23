package deploy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

func DeployChannel(serverID, deploymentID uuid.UUID) string {
	return "logs:" + serverID.String() + ":" + deploymentID.String()
}

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

type Target struct {
	ServerID     uuid.UUID
	DeploymentID uuid.UUID
}

type Event struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data"`
}

type Emitter struct {
	pub Publisher
}

func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

func (e *Emitter) Log(ctx context.Context, t Target, line string) {
	if e == nil || e.pub == nil {
		return
	}
	ch := DeployChannel(t.ServerID, t.DeploymentID)
	ev := Event{Channel: ch, Type: "log", Data: line}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = e.pub.Publish(ctx, ch, string(payload))
}

func stateLine(from, to State) string {
	return time.Now().UTC().Format(time.RFC3339) + " " + string(from) + " → " + string(to)
}

func (e *Emitter) State(ctx context.Context, t Target, from, to State) {
	e.Log(ctx, t, stateLine(from, to))
}
