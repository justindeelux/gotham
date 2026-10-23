package toolchain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

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

func TestWorkerRecoversOnlyStaleJobs(t *testing.T) {
	agent := &fakeAgent{}
	st := newFakeStore()

	now := time.Now()
	stale := st.seedRunning(uuid.New(), "stale", "other-instance", now.Add(-time.Minute))
	nullLease := st.seedRunning(uuid.New(), "nolease", "other-instance", time.Time{})
	live := st.seedRunning(uuid.New(), "live", "some-instance", now.Add(time.Hour))

	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.Start()
	defer func() { _ = svc.Close() }()

	if got := waitTerminal(t, st, stale); got != StatusFailed {
		t.Fatalf("stale job status = %s, want failed", got)
	}
	if got := waitTerminal(t, st, nullLease); got != StatusFailed {
		t.Fatalf("null-lease job status = %s, want failed", got)
	}
	if got := st.status(t, live); got != StatusRunning {
		t.Fatalf("live job status = %s, want running (untouched, lease-only recovery)", got)
	}
	if log := st.logText(stale); !strings.Contains(log, "interrupted") {
		t.Fatalf("stale job log = %q, want an interruption marker", log)
	}
}

func TestFinishNoOpForForeignOrStaleLease(t *testing.T) {
	st := newFakeStore()
	svc := testService(st, &fakeAgent{}, &fakeRegistry{}, nil)

	foreign := st.seedRunning(uuid.New(), "foreign", "other-instance", time.Now().Add(time.Hour))
	lapsed := st.seedRunning(uuid.New(), "lapsed", "test-instance", time.Now().Add(-time.Minute))

	svc.finish(context.Background(), jobFromRow(st.job(foreign)), StatusSucceeded)
	svc.finish(context.Background(), jobFromRow(st.job(lapsed)), StatusSucceeded)

	if got := st.status(t, foreign); got != StatusRunning {
		t.Fatalf("foreign job status = %s, want running (finish must be a no-op)", got)
	}
	if got := st.status(t, lapsed); got != StatusRunning {
		t.Fatalf("lapsed-lease job status = %s, want running (finish must be a no-op)", got)
	}
	if n := len(st.finishOrder()); n != 0 {
		t.Fatalf("finish recorded %d terminal writes, want 0", n)
	}
}

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

func TestWorkerAbortsOnLostLease(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)},
		block:   map[string]bool{"install:docker": true},
	}
	st := newFakeStore()
	st.renewZero = true
	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.renewInterval = 20 * time.Millisecond

	id := st.seedRunning(uuid.New(), "docker", "test-instance", time.Now().Add(time.Hour))

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.run(context.Background(), jobFromRow(st.job(id)))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not abort after its lease was lost")
	}
	if got := st.status(t, id); got != StatusRunning {
		t.Fatalf("job status = %s, want running (a lost lease must not finish the job)", got)
	}
	if n := len(st.finishOrder()); n != 0 {
		t.Fatalf("recorded %d terminal writes, want 0 (run must not finish an unowned job)", n)
	}
}

func TestWorkerRetriesContendedClaimImmediately(t *testing.T) {
	agent := &fakeAgent{
		catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)},
		events:  map[string][]*agentv1.ToolEvent{"install:docker": okStream("done")},
	}
	st := newFakeStore()
	st.contendedOnce = true
	svc := testService(st, agent, &fakeRegistry{}, nil)
	svc.pollInterval = 30 * time.Second
	svc.Start()
	defer func() { _ = svc.Close() }()

	result, err := svc.Enqueue(context.Background(), uuid.New(), ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := waitTerminal(t, st, result.Jobs[0].ID); got != StatusSucceeded {
		t.Fatalf("job status = %s, want succeeded (contended claim must retry immediately)", got)
	}
}
