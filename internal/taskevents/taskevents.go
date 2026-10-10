// Package taskevents publishes background-task lifecycle events (queued,
// running with step/progress, succeeded, failed) over the existing realtime
// channel (WebSocket + Redis pub/sub). It covers deploys and any other long
// task, including GitHub-webhook-triggered runs, which have no watching
// client: the UI subscribes once in the app shell and renders a progress
// card for every event on its team channel.
package taskevents

import (
	"context"
	"encoding/json"
	"sync"
)

// Status is the lifecycle state carried by an Event.
type Status string

// Event statuses. Terminal states leave the tracker; the UI auto-dismisses
// succeeded cards and keeps failed ones.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Terminal reports whether s ends a task.
func (s Status) Terminal() bool { return s == StatusSucceeded || s == StatusFailed }

// KindDeploy is the event kind of application deploys and rollbacks.
const KindDeploy = "deploy"

// Event is one task lifecycle transition.
type Event struct {
	TaskID        string `json:"task_id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Status        Status `json:"status"`
	Step          string `json:"step,omitempty"`
	Progress      int    `json:"progress,omitempty"` // 0-100
	AppID         string `json:"app_id,omitempty"`
	DeploymentID  string `json:"deployment_id,omitempty"`
	ServerID      string `json:"server_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	TeamID        string `json:"team_id,omitempty"`
	LogURL        string `json:"log_url,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Channel returns the realtime room of one team's task events. Clients
// subscribe once in the app shell, so webhook-triggered runs surface without
// user action, and every (re)subscribe replays the running snapshot.
func Channel(teamID string) string { return "tasks:" + teamID }

// TeamOf reports whether channel is a task room and returns its team ID.
func TeamOf(channel string) (string, bool) {
	if len(channel) <= 6 || channel[:6] != "tasks:" {
		return "", false
	}
	return channel[6:], true
}

// Publisher abstracts the realtime fan-out (Redis in production, hub direct
// in tests). It mirrors the deploy Publisher seam so either satisfies it.
type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

// Frame is the JSON envelope exchanged with WebSocket clients. It mirrors
// ws.Message without importing the HTTP server tree.
type Frame struct {
	Channel string `json:"channel"`
	Type    string `json:"type"` // "task" or "task_snapshot"
	Data    string `json:"data"`
}

// frameType is the realtime frame type of live task events.
const frameType = "task"

// SnapshotType is the frame type closing a running-task replay batch.
const SnapshotType = "task_snapshot"

// Emitter publishes task events to subscribers of the team channel.
// A nil publisher is a no-op.
type Emitter struct{ pub Publisher }

// NewEmitter returns an Emitter publishing through pub.
func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

// Emit publishes ev on its team channel. Errors are swallowed: realtime
// updates are best-effort and must never fail the run they describe.
func (e *Emitter) Emit(ctx context.Context, ev Event) {
	if e == nil || e.pub == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	frame, err := json.Marshal(Frame{Channel: Channel(ev.TeamID), Type: frameType, Data: string(data)})
	if err != nil {
		return
	}
	_ = e.pub.Publish(ctx, Channel(ev.TeamID), string(frame))
}

// Tracker keeps the currently running tasks per team so a fresh subscriber
// (initial mount, navigation, WebSocket reconnect) restores them.
type Tracker struct {
	mu    sync.Mutex
	tasks map[string]Event // by task ID
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker { return &Tracker{tasks: make(map[string]Event)} }

// Set records ev as running. Terminal events remove the task instead.
func (t *Tracker) Set(ev Event) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Status.Terminal() {
		delete(t.tasks, ev.TaskID)
		return
	}
	t.tasks[ev.TaskID] = ev
}

// Snapshot returns the running tasks of one team, oldest task ID first.
func (t *Tracker) Snapshot(teamID string) []Event {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Event
	for _, ev := range t.tasks {
		if ev.TeamID == teamID {
			out = append(out, ev)
		}
	}
	return out
}

// SnapshotFrames renders the team snapshot as realtime frames, closed by a
// task_snapshot marker so the client knows the replay batch ended.
func (t *Tracker) SnapshotFrames(teamID string) []string {
	events := t.Snapshot(teamID)
	out := make([]string, 0, len(events)+1)
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		frame, err := json.Marshal(Frame{Channel: Channel(teamID), Type: frameType, Data: string(data)})
		if err != nil {
			continue
		}
		out = append(out, string(frame))
	}
	end, _ := json.Marshal(Frame{Channel: Channel(teamID), Type: SnapshotType})
	return append(out, string(end))
}
