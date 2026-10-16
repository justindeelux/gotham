package toolchain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestWorkerRunsJobsSequentiallyPerNode pins the per-node ordering: a node's
// jobs run one at a time even with several workers, so a slow first job keeps
// the second one queued (a global pool that ignored the node guard would finish
// the fast second job first).
func TestWorkerRunsJobsSequentiallyPerNode(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("slow", true, true), catalogTool("fast", true, true)},
		events: map[string][]*agentv1.ToolEvent{
			"install:slow": okStream("slow done"),
			"install:fast": okStream("fast done"),
		},
		delay: map[string]time.Duration{"install:slow": 200 * time.Millisecond},
	}
	st := newFakeStore()
	svc := testServiceN(st, agent, &fakeRegistry{}, nil, 4)
	svc.Start()
	defer func() { _ = svc.Close() }()

	serverID := uuid.New()
	result, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"slow", "fast"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	slow, fast := result.Jobs[0].ID, result.Jobs[1].ID
	for _, job := range result.Jobs {
		if got := waitTerminal(t, st, job.ID); got != StatusSucceeded {
			t.Fatalf("job %s status = %s, want succeeded", job.ToolID, got)
		}
	}

	order := st.finishOrder()
	if len(order) != 2 || order[0] != slow || order[1] != fast {
		t.Fatalf("finish order = %v, want [slow fast] (per-node sequential)", order)
	}
}

// TestWorkerRunsNodesInParallel pins the cross-node parallelism: a slow install
// on one node does not block another node's job.
func TestWorkerRunsNodesInParallel(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("slow", true, true), catalogTool("fast", true, true)},
		events: map[string][]*agentv1.ToolEvent{
			"install:slow": okStream("slow done"),
			"install:fast": okStream("fast done"),
		},
		delay: map[string]time.Duration{"install:slow": 400 * time.Millisecond},
	}
	st := newFakeStore()
	svc := testServiceN(st, agent, &fakeRegistry{}, nil, 2)
	svc.Start()
	defer func() { _ = svc.Close() }()

	ctx := context.Background()
	slow, err := svc.Enqueue(ctx, uuid.New(), ActionInstall, []string{"slow"})
	if err != nil {
		t.Fatalf("enqueue slow: %v", err)
	}
	fast, err := svc.Enqueue(ctx, uuid.New(), ActionInstall, []string{"fast"})
	if err != nil {
		t.Fatalf("enqueue fast: %v", err)
	}
	waitTerminal(t, st, slow.Jobs[0].ID)
	waitTerminal(t, st, fast.Jobs[0].ID)

	order := st.finishOrder()
	if len(order) == 0 || order[0] != fast.Jobs[0].ID {
		t.Fatalf("finish order = %v, want the fast node's job to finish first (cross-node parallel)", order)
	}
}

// TestWorkerCapsJobLog pins the log cap: a chatty install stops persisting past
// the cap and records one truncation marker instead of growing without bound.
func TestWorkerCapsJobLog(t *testing.T) {
	lines := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		lines = append(lines, strings.Repeat("x", 40))
	}
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("chatty", true, true)},
		events:  map[string][]*agentv1.ToolEvent{"install:chatty": okStream(lines...)},
	}
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.maxLogBytes = 200
	svc.Start()
	defer func() { _ = svc.Close() }()

	result, err := svc.Enqueue(context.Background(), uuid.New(), ActionInstall, []string{"chatty"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job := result.Jobs[0].ID
	if got := waitTerminal(t, st, job); got != StatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", got)
	}
	log := st.logText(job)
	if !strings.Contains(log, logTruncatedMsg) {
		t.Fatalf("log does not contain the truncation marker: %q", log)
	}
	if n := st.logBytes(job); n > svc.maxLogBytes+len(logTruncatedMsg)+2 {
		t.Fatalf("persisted log = %d bytes, want <= cap %d + marker", n, svc.maxLogBytes)
	}
}

func TestWorkerFailsResultAndUnreachableAgent(t *testing.T) {
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
	// "unreachable" cannot run while "bad" still owns the node's slot; assert
	// both end failed.
	if got := waitTerminal(t, st, result.Jobs[0].ID); got != StatusFailed {
		t.Fatalf("bad job status = %s, want failed", got)
	}
	if got := waitTerminal(t, st, result.Jobs[1].ID); got != StatusFailed {
		t.Fatalf("unreachable job status = %s, want failed", got)
	}
	if log := st.logText(result.Jobs[1].ID); !strings.Contains(log, "failed to start install") {
		t.Fatalf("unreachable job log = %q, want a start-failure line", log)
	}
}

// TestWorkerRecoversOnlyOwnOrStaleJobs pins multi-instance-safe recovery: on
// start the sweeper fails jobs this instance owned and jobs whose lease lapsed,
// but never a job a live peer is still renewing.
func TestWorkerRecoversOnlyOwnOrStaleJobs(t *testing.T) {
	agent := &fakeAgent{}
	st := newFakeStore()

	now := time.Now()
	mine := st.seedRunning(uuid.New(), "mine", "test-instance", now.Add(time.Hour))
	stale := st.seedRunning(uuid.New(), "stale", "other-instance", now.Add(-time.Minute))
	livePeer := st.seedRunning(uuid.New(), "live", "other-instance", now.Add(time.Hour))

	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.Start()
	defer func() { _ = svc.Close() }()

	if got := waitTerminal(t, st, mine); got != StatusFailed {
		t.Fatalf("own job status = %s, want failed", got)
	}
	if got := waitTerminal(t, st, stale); got != StatusFailed {
		t.Fatalf("stale job status = %s, want failed", got)
	}
	if got := st.status(t, livePeer); got != StatusRunning {
		t.Fatalf("live peer job status = %s, want running (untouched)", got)
	}
	if log := st.logText(stale); !strings.Contains(log, "interrupted") {
		t.Fatalf("stale job log = %q, want an interruption marker", log)
	}
}

// TestWorkerRetriesFinishOnFailure pins the finish retry: a transient DB error
// must not strand the job as running.
func TestWorkerRetriesFinishOnFailure(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)},
		events:  map[string][]*agentv1.ToolEvent{"install:docker": okStream("done")},
	}
	st := newFakeStore()
	st.finishFailures = 1
	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.Start()
	defer func() { _ = svc.Close() }()

	result, err := svc.Enqueue(context.Background(), uuid.New(), ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := waitTerminal(t, st, result.Jobs[0].ID); got != StatusSucceeded {
		t.Fatalf("job status = %s, want succeeded after a finish retry", got)
	}
}

// TestRealtimeFramesCarryOnlyNewLines pins that a job state frame carries no
// log and a log frame carries exactly one line (never the accumulated log).
func TestRealtimeFramesCarryOnlyNewLines(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)},
		events:  map[string][]*agentv1.ToolEvent{"install:docker": okStream("alpha", "beta")},
	}
	st := newFakeStore()
	pub := &memPublisher{}
	svc := testService(st, agent, &fakeRegistry{}, pub)
	svc.Start()
	defer func() { _ = svc.Close() }()

	serverID := uuid.New()
	result, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitTerminal(t, st, result.Jobs[0].ID)

	frames := pub.framesOf(Channel(serverID))
	for _, frame := range frames {
		switch frame.Type {
		case TypeJob:
			if strings.Contains(frame.Data, `"log"`) {
				t.Fatalf("job frame carries a log field: %s", frame.Data)
			}
		case TypeLog:
			if strings.Contains(frame.Data, "alpha") && strings.Contains(frame.Data, "beta") {
				t.Fatalf("log frame carries more than one line: %s", frame.Data)
			}
			if !strings.Contains(frame.Data, `"seq"`) {
				t.Fatalf("log frame missing seq: %s", frame.Data)
			}
		}
	}
}
