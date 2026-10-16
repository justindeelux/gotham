package toolchain

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestWorkerRunsJobsSequentiallyInOrder(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{
			catalogTool("a", true, true),
			catalogTool("b", true, true),
			catalogTool("c", true, true),
		},
		events: map[string][]*agentv1.ToolEvent{
			"install:a": okStream("a line 1", "a line 2"),
			"install:b": okStream("b line"),
			"install:c": okStream("c line"),
		},
	}
	st := newFakeStore()
	pub := &memPublisher{}
	svc := testService(st, agent, &fakeRegistry{}, pub)
	svc.Start()
	defer func() { _ = svc.Close() }()

	ctx := context.Background()
	serverID := uuid.New()
	result, err := svc.Enqueue(ctx, serverID, ActionInstall, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	for _, job := range result.Jobs {
		if got := waitTerminal(t, st, job.ID); got != StatusSucceeded {
			t.Fatalf("job %s status = %s, want succeeded", job.ToolID, got)
		}
	}

	// Sequential per node, in enqueue order.
	ran := agent.ranList()
	want := []string{"install:a", "install:b", "install:c"}
	if len(ran) != len(want) {
		t.Fatalf("ran = %v, want %v", ran, want)
	}
	for i := range want {
		if ran[i] != want[i] {
			t.Fatalf("ran = %v, want %v", ran, want)
		}
	}

	// Log lines are persisted on the job.
	if log := st.log(t, result.Jobs[0].ID); !strings.Contains(log, "a line 2") {
		t.Fatalf("job log = %q, want it to contain the streamed lines", log)
	}

	// Realtime frames: running + terminal job frames and log frames on the
	// node's channel.
	frames := pub.framesOf(Channel(serverID))
	var sawRunning, sawSucceeded, sawLog bool
	for _, frame := range frames {
		switch frame.Type {
		case TypeJob:
			if strings.Contains(frame.Data, `"status":"running"`) {
				sawRunning = true
			}
			if strings.Contains(frame.Data, `"status":"succeeded"`) {
				sawSucceeded = true
			}
		case TypeLog:
			sawLog = true
		}
	}
	if !sawRunning || !sawSucceeded || !sawLog {
		t.Fatalf("missing realtime frames: running=%v succeeded=%v log=%v (frames %+v)", sawRunning, sawSucceeded, sawLog, frames)
	}
}

func TestWorkerMarksFailedResultAndUnreachableAgent(t *testing.T) {
	// A tool whose stream reports a failed result.
	failed := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("bad", true, true), catalogTool("unreachable", true, true)},
		events: map[string][]*agentv1.ToolEvent{
			"install:bad": {
				{Event: &agentv1.ToolEvent_Output{Output: &agentv1.ToolOutput{Line: "boom", Stream: "stderr"}}},
				{Event: &agentv1.ToolEvent_Result{Result: &agentv1.ToolResult{Status: string(StatusFailed), ExitCode: 1}}},
			},
		},
		openErr: map[string]error{"install:unreachable": context.DeadlineExceeded},
	}
	st := newFakeStore()
	svc := testService(st, failed, &fakeRegistry{}, nil)
	svc.Start()
	defer func() { _ = svc.Close() }()

	serverID := uuid.New()
	result, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"bad", "unreachable"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := waitTerminal(t, st, result.Jobs[0].ID); got != StatusFailed {
		t.Fatalf("bad job status = %s, want failed", got)
	}
	if got := waitTerminal(t, st, result.Jobs[1].ID); got != StatusFailed {
		t.Fatalf("unreachable job status = %s, want failed", got)
	}
	if log := st.log(t, result.Jobs[1].ID); !strings.Contains(log, "failed to start install") {
		t.Fatalf("unreachable job log = %q, want a start-failure line", log)
	}
}

func TestWorkerRecoversInterruptedJobsOnStart(t *testing.T) {
	agent := &fakeAgent{}
	st := newFakeStore()
	interrupted := st.seedRunning(uuid.New(), "docker")
	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.Start()
	defer func() { _ = svc.Close() }()

	if got := waitTerminal(t, st, interrupted); got != StatusFailed {
		t.Fatalf("interrupted job status = %s, want failed", got)
	}
	if log := st.log(t, interrupted); !strings.Contains(log, "interrupted") {
		t.Fatalf("interrupted job log = %q, want an interruption reason", log)
	}
}
