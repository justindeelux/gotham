package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// --- fakes ------------------------------------------------------------------

// fakeStore is an in-memory Store that reproduces the SQL queue's semantics:
// the active-uniqueness de-dupe, oldest-first claiming, and the state guards on
// cancel and boot recovery.
type fakeStore struct {
	mu      sync.Mutex
	jobs    map[uuid.UUID]sqlc.ToolchainJob
	order   []uuid.UUID
	seq     int64
	failErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{jobs: make(map[uuid.UUID]sqlc.ToolchainJob)}
}

func (f *fakeStore) nextTime() pgtype.Timestamptz {
	f.seq++
	return pgtype.Timestamptz{Time: time.Unix(f.seq, 0).UTC(), Valid: true}
}

func (f *fakeStore) CreateToolchainJob(_ context.Context, p sqlc.CreateToolchainJobParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, job := range f.jobs {
		if job.ServerID == p.ServerID && job.ToolID == p.ToolID && job.Action == p.Action &&
			(job.Status == string(StatusQueued) || job.Status == string(StatusRunning)) {
			return sqlc.ToolchainJob{}, pgx.ErrNoRows
		}
	}
	job := sqlc.ToolchainJob{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ServerID:  p.ServerID,
		ToolID:    p.ToolID,
		Action:    p.Action,
		Status:    string(StatusQueued),
		CreatedAt: f.nextTime(),
	}
	f.jobs[job.ID.Bytes] = job
	f.order = append(f.order, job.ID.Bytes)
	return job, nil
}

func (f *fakeStore) GetToolchainJobForServer(_ context.Context, p sqlc.GetToolchainJobForServerParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[p.ID.Bytes]
	if !ok || job.ServerID != p.ServerID {
		return sqlc.ToolchainJob{}, pgx.ErrNoRows
	}
	return job, nil
}

func (f *fakeStore) ListToolchainJobsByServer(_ context.Context, p sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sqlc.ToolchainJob
	for _, job := range f.jobs {
		if job.ServerID == p.ServerID {
			out = append(out, job)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Time.Equal(out[j].CreatedAt.Time) {
			return uuid.UUID(out[i].ID.Bytes).String() > uuid.UUID(out[j].ID.Bytes).String()
		}
		return out[i].CreatedAt.Time.After(out[j].CreatedAt.Time)
	})
	if int(p.Limit) < len(out) {
		out = out[:p.Limit]
	}
	return out, nil
}

func (f *fakeStore) ClaimNextQueuedToolchainJob(_ context.Context) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.order {
		job := f.jobs[id]
		if job.Status != string(StatusQueued) {
			continue
		}
		job.Status = string(StatusRunning)
		job.StartedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		f.jobs[id] = job
		return job, nil
	}
	return sqlc.ToolchainJob{}, pgx.ErrNoRows
}

func (f *fakeStore) AppendToolchainJobLog(_ context.Context, p sqlc.AppendToolchainJobLogParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job := f.jobs[p.ID.Bytes]
	job.Log += p.Log
	f.jobs[p.ID.Bytes] = job
	return job, nil
}

func (f *fakeStore) FinishToolchainJob(_ context.Context, p sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job := f.jobs[p.ID.Bytes]
	job.Status = p.Status
	job.Log += p.Log
	job.FinishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	f.jobs[p.ID.Bytes] = job
	return job, nil
}

func (f *fakeStore) CancelToolchainJob(_ context.Context, id pgtype.UUID) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id.Bytes]
	if !ok || job.Status != string(StatusQueued) {
		return sqlc.ToolchainJob{}, pgx.ErrNoRows
	}
	job.Status = string(StatusCanceled)
	job.FinishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	f.jobs[id.Bytes] = job
	return job, nil
}

func (f *fakeStore) FailInterruptedToolchainJobs(_ context.Context, reason string) ([]pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return nil, f.failErr
	}
	var ids []pgtype.UUID
	for id, job := range f.jobs {
		if job.Status != string(StatusRunning) {
			continue
		}
		job.Status = string(StatusFailed)
		job.Log += reason
		job.FinishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		f.jobs[id] = job
		ids = append(ids, pgtype.UUID{Bytes: id, Valid: true})
	}
	return ids, nil
}

// seedRunning inserts an already-running job, simulating a control plane that
// died mid-install. It returns the job id.
func (f *fakeStore) seedRunning(serverID uuid.UUID, toolID string) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.jobs[id] = sqlc.ToolchainJob{
		ID:        pgtype.UUID{Bytes: id, Valid: true},
		ServerID:  pgtype.UUID{Bytes: serverID, Valid: true},
		ToolID:    toolID,
		Action:    string(ActionInstall),
		Status:    string(StatusRunning),
		CreatedAt: f.nextTime(),
		StartedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.order = append(f.order, id)
	return id
}

func (f *fakeStore) status(t *testing.T, id uuid.UUID) Status {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return Status(f.jobs[id].Status)
}

func (f *fakeStore) log(t *testing.T, id uuid.UUID) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[id].Log
}

// fakeStream feeds a fixed event slice and then EOF.
type fakeStream struct {
	events []*agentv1.ToolEvent
	i      int
}

func (s *fakeStream) Recv() (*agentv1.ToolEvent, error) {
	if s.i >= len(s.events) {
		return nil, io.EOF
	}
	ev := s.events[s.i]
	s.i++
	return ev, nil
}

func (s *fakeStream) Header() (metadata.MD, error) { return nil, nil }
func (s *fakeStream) Trailer() metadata.MD         { return nil }
func (s *fakeStream) CloseSend() error             { return nil }
func (s *fakeStream) Context() context.Context     { return context.Background() }
func (s *fakeStream) SendMsg(any) error            { return nil }
func (s *fakeStream) RecvMsg(any) error            { return nil }

// fakeAgent is an AgentConn serving a fixed catalog and per-tool event streams,
// recording the tool/action it was asked to run, in order.
type fakeAgent struct {
	mu      sync.Mutex
	catalog []*agentv1.ToolInfo
	events  map[string][]*agentv1.ToolEvent
	openErr map[string]error
	listErr error
	ran     []string
	closed  int
}

func (f *fakeAgent) ListTools(context.Context, *agentv1.ListToolsRequest, ...grpc.CallOption) (*agentv1.ListToolsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &agentv1.ListToolsResponse{Tools: f.catalog}, nil
}

func (f *fakeAgent) InstallTool(_ context.Context, in *agentv1.InstallToolRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error) {
	return f.open("install:" + in.GetId())
}

func (f *fakeAgent) UninstallTool(_ context.Context, in *agentv1.InstallToolRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error) {
	return f.open("uninstall:" + in.GetId())
}

func (f *fakeAgent) open(key string) (grpc.ServerStreamingClient[agentv1.ToolEvent], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, key)
	if err := f.openErr[key]; err != nil {
		return nil, err
	}
	return &fakeStream{events: f.events[key]}, nil
}

func (f *fakeAgent) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeAgent) ranList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

// fakeRegistry resolves every id to a server, optionally on a team.
type fakeRegistry struct {
	teamID  uuid.UUID
	missing bool
}

func (r *fakeRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if r.missing {
		return nil, servers.ErrNotFound
	}
	return &servers.Server{ID: id, TeamID: r.teamID}, nil
}

// memPublisher records published frames per channel.
type memPublisher struct {
	mu     sync.Mutex
	frames map[string][]Frame
}

func (p *memPublisher) Publish(_ context.Context, channel, payload string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.frames == nil {
		p.frames = make(map[string][]Frame)
	}
	var frame Frame
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return err
	}
	p.frames[channel] = append(p.frames[channel], frame)
	return nil
}

func (p *memPublisher) framesOf(channel string) []Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Frame(nil), p.frames[channel]...)
}

func testService(st *fakeStore, agent *fakeAgent, reg *fakeRegistry, pub Publisher) *Service {
	return NewService(Config{
		Store:        st,
		Registry:     reg,
		Dial:         func(context.Context, uuid.UUID) (AgentConn, error) { return agent, nil },
		Publisher:    pub,
		PollInterval: 2 * time.Millisecond,
		RPCTimeout:   time.Second,
	})
}

func catalogTool(id string, installable, uninstallable bool) *agentv1.ToolInfo {
	return &agentv1.ToolInfo{Id: id, Name: id, Installable: installable, Uninstallable: uninstallable}
}

func okStream(lines ...string) []*agentv1.ToolEvent {
	events := make([]*agentv1.ToolEvent, 0, len(lines)+1)
	for _, l := range lines {
		events = append(events, &agentv1.ToolEvent{Event: &agentv1.ToolEvent_Output{
			Output: &agentv1.ToolOutput{Line: l, Stream: "stdout"},
		}})
	}
	return append(events, &agentv1.ToolEvent{Event: &agentv1.ToolEvent_Result{
		Result: &agentv1.ToolResult{Status: string(StatusSucceeded)},
	}})
}

// --- tests ------------------------------------------------------------------

func TestNormalizeToolIDs(t *testing.T) {
	got := normalizeToolIDs([]string{" a ", "", "b", "a", "  ", "b"})
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("normalizeToolIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeToolIDs = %v, want %v", got, want)
		}
	}
}

func TestEnqueueRejectsUnknownAndDetectOnlyTools(t *testing.T) {
	agent := &fakeAgent{catalog: []*agentv1.ToolInfo{
		catalogTool("docker", true, true),
		catalogTool("detect", false, false),
	}}
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{}, nil)
	serverID := uuid.New()

	if _, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"nope"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown tool err = %v, want ErrValidation", err)
	}
	if _, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"detect"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("detect-only install err = %v, want ErrValidation", err)
	}
	if _, err := svc.Enqueue(context.Background(), serverID, ActionUninstall, []string{"detect"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("detect-only uninstall err = %v, want ErrValidation", err)
	}
	if _, err := svc.Enqueue(context.Background(), serverID, Action("frobnicate"), []string{"docker"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad action err = %v, want ErrValidation", err)
	}
	// A rejected request must not have queued anything.
	if len(st.order) != 0 {
		t.Fatalf("expected no jobs queued, got %d", len(st.order))
	}
}

func TestEnqueueDedupesWithinRequestAndAgainstActiveJobs(t *testing.T) {
	agent := &fakeAgent{catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)}}
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{}, nil)
	serverID := uuid.New()

	first, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"docker", "docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(first.Jobs) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first enqueue = %+v, want 1 job, 0 skipped", first)
	}
	// The same tool is already active: it is skipped, not duplicated.
	second, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(second.Jobs) != 0 || len(second.Skipped) != 1 || second.Skipped[0] != "docker" {
		t.Fatalf("second enqueue = %+v, want 0 jobs, 1 skipped docker", second)
	}
}

func TestEnqueueRequiresTeamMembership(t *testing.T) {
	agent := &fakeAgent{catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)}}
	team := uuid.New()
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{teamID: team}, nil)

	// A caller scoped to another team cannot see the node: not-found.
	foreign := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: uuid.New(), Role: teams.RoleOwner})
	if _, err := svc.Enqueue(foreign, uuid.New(), ActionInstall, []string{"docker"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign team err = %v, want ErrNotFound", err)
	}
	// A read-only member of the node's team cannot mutate.
	readOnly := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: team, Role: teams.RoleReadOnly})
	if _, err := svc.Enqueue(readOnly, uuid.New(), ActionInstall, []string{"docker"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only err = %v, want ErrForbidden", err)
	}
	// An owner of the node's team can.
	owner := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: team, Role: teams.RoleOwner})
	if _, err := svc.Enqueue(owner, uuid.New(), ActionInstall, []string{"docker"}); err != nil {
		t.Fatalf("owner enqueue: %v", err)
	}
}

func TestCancelOnlyCancelsQueuedJobs(t *testing.T) {
	agent := &fakeAgent{catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)}}
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{}, nil)
	serverID := uuid.New()

	result, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job := result.Jobs[0]

	canceled, err := svc.Cancel(context.Background(), serverID, job.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if canceled.Status != StatusCanceled {
		t.Fatalf("canceled status = %s, want canceled", canceled.Status)
	}
	// Cancelling again is a conflict (it is terminal, not queued).
	if _, err := svc.Cancel(context.Background(), serverID, job.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("re-cancel err = %v, want ErrConflict", err)
	}
	// A job on another node answers not-found, not conflict.
	if _, err := svc.Cancel(context.Background(), uuid.New(), job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign node cancel err = %v, want ErrNotFound", err)
	}
}

// waitTerminal polls a job until it reaches a terminal status.
func waitTerminal(t *testing.T, st *fakeStore, id uuid.UUID) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := st.status(t, id); s.Terminal() {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal status (last %s)", id, st.status(t, id))
	return ""
}
