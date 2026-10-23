package taskevents

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

func (s Status) Terminal() bool { return s == StatusSucceeded || s == StatusFailed }

const KindDeploy = "deploy"

type Event struct {
	TaskID        string `json:"task_id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Status        Status `json:"status"`
	Step          string `json:"step,omitempty"`
	Progress      int    `json:"progress,omitempty"`
	Seq           uint64 `json:"seq,omitempty"`
	AppID         string `json:"app_id,omitempty"`
	DeploymentID  string `json:"deployment_id,omitempty"`
	ServerID      string `json:"server_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	TeamID        string `json:"team_id,omitempty"`
	LogURL        string `json:"log_url,omitempty"`
	Error         string `json:"error,omitempty"`
}

func Channel(teamID string) string { return "tasks:" + teamID }

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

type Frame struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Data    string `json:"data"`
}

const frameType = "task"

const SnapshotType = "task_snapshot"

type Emitter struct{ pub Publisher }

func NewEmitter(pub Publisher) *Emitter { return &Emitter{pub: pub} }

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

type Tracker struct {
	mu    sync.Mutex
	tasks map[string]Event
	seq   map[string]uint64
}

func NewTracker() *Tracker {
	return &Tracker{tasks: make(map[string]Event), seq: make(map[string]uint64)}
}

func (t *Tracker) Next(taskID string) uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq[taskID]++
	return t.seq[taskID]
}

func (t *Tracker) Set(ev Event) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Status.Terminal() {
		delete(t.tasks, ev.TaskID)
		delete(t.seq, ev.TaskID)
		return
	}
	t.tasks[ev.TaskID] = ev
}

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
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

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
