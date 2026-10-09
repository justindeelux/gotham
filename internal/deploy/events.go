package deploy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DeployChannel mirrors ws.LogChannel ("logs:{serverID}:{deploymentID}") so the
// existing WebSocket bridge (logs:*:* PSubscribe) forwards deploy logs without
// any change. Deployment IDs are namespaced by server ID for that pattern.
func DeployChannel(serverID, deploymentID uuid.UUID) string {
	return "logs:" + serverID.String() + ":" + deploymentID.String()
}

// Publisher abstracts the realtime fan-out (Redis in production, direct map in tests).
type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

// Target identifies the deployment an event belongs to.
type Target struct {
	ServerID     uuid.UUID
	DeploymentID uuid.UUID
}

// Event mirrors the ws.Message envelope {channel, type, data} redeclared
// locally so the domain package never imports the HTTP server tree.
type Event struct {
	Channel string `json:"channel"`
	Type    string `json:"type"` // "log"
	Data    string `json:"data"`
}

// Emitter fans deploy log lines out to subscribers of the deployment channel.
type Emitter struct {
	pub Publisher
}

// NewEmitter returns an Emitter publishing through pub. A nil pub is a no-op.
func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

// Log publishes one log line as a ws-compatible "log" event.
// Errors are swallowed: realtime logs are best-effort.
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

// stateLine formats the synthetic transition marker Emitter.State publishes.
func stateLine(from, to State) string {
	return time.Now().UTC().Format(time.RFC3339) + " " + string(from) + " → " + string(to)
}

// State publishes a synthetic log line marking a state transition.
func (e *Emitter) State(ctx context.Context, t Target, from, to State) {
	e.Log(ctx, t, stateLine(from, to))
}
