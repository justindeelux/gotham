package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
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
// the active-uniqueness de-dupe, oldest-first per-node claiming, lease-based
// recovery, the append-only log, and the state guards on cancel and finish.
type fakeStore struct {
	mu    sync.Mutex
	jobs  map[uuid.UUID]*sqlc.ToolchainJob
	logs  map[uuid.UUID][]sqlc.ListToolchainJobLogsRow
	order []uuid.UUID
	seq   int64
	// finished records terminal job ids in completion order.
	finished []uuid.UUID
	// finishFailures makes the next N FinishToolchainJob calls fail.
	finishFailures int
	failStaleErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		jobs: make(map[uuid.UUID]*sqlc.ToolchainJob),
		logs: make(map[uuid.UUID][]sqlc.ListToolchainJobLogsRow),
	}
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
	id := uuid.New()
	job := &sqlc.ToolchainJob{
		ID:        pgtype.UUID{Bytes: id, Valid: true},
		ServerID:  p.ServerID,
		ToolID:    p.ToolID,
		Action:    p.Action,
		Status:    string(StatusQueued),
		CreatedAt: f.nextTime(),
	}
	f.jobs[id] = job
	f.order = append(f.order, id)
	return *job, nil
}

func (f *fakeStore) GetToolchainJobForServer(_ context.Context, p sqlc.GetToolchainJobForServerParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[p.ID.Bytes]
	if !ok || job.ServerID != p.ServerID {
		return sqlc.ToolchainJob{}, pgx.ErrNoRows
	}
	return *job, nil
}

func (f *fakeStore) ListToolchainJobsByServer(_ context.Context, p sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sqlc.ToolchainJob
	for _, job := range f.jobs {
		if job.ServerID == p.ServerID {
			out = append(out, *job)
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

func (f *fakeStore) ClaimNextQueuedToolchainJob(_ context.Context, p sqlc.ClaimNextQueuedToolchainJobParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	running := make(map[pgtype.UUID]bool)
	for _, job := range f.jobs {
		if job.Status == string(StatusRunning) {
			running[job.ServerID] = true
		}
	}
	for _, id := range f.order {
		job := f.jobs[id]
		if job.Status != string(StatusQueued) || running[job.ServerID] {
			continue
		}
		job.Status = string(StatusRunning)
		job.StartedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		job.ClaimedBy = p.ClaimedBy
		job.LeaseExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(p.LeaseSeconds) * time.Second), Valid: true}
		return *job, nil
	}
	return sqlc.ToolchainJob{}, pgx.ErrNoRows
}

func (f *fakeStore) RenewToolchainJobLease(_ context.Context, p sqlc.RenewToolchainJobLeaseParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[p.ID.Bytes]
	if !ok || job.Status != string(StatusRunning) {
		return nil
	}
	job.LeaseExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(p.LeaseSeconds) * time.Second), Valid: true}
	return nil
}

func (f *fakeStore) FailOwnedToolchainJobs(_ context.Context, claimedBy string) ([]pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStaleErr != nil {
		return nil, f.failStaleErr
	}
	var ids []pgtype.UUID
	for id, job := range f.jobs {
		if job.Status == string(StatusRunning) && job.ClaimedBy == claimedBy {
			job.Status = string(StatusFailed)
			job.FinishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
			f.finished = append(f.finished, id)
			ids = append(ids, pgtype.UUID{Bytes: id, Valid: true})
		}
	}
	return ids, nil
}

func (f *fakeStore) FailStaleToolchainJobs(_ context.Context) ([]pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStaleErr != nil {
		return nil, f.failStaleErr
	}
	now := time.Now()
	var ids []pgtype.UUID
	for id, job := range f.jobs {
		if job.Status != string(StatusRunning) {
			continue
		}
		if job.LeaseExpiresAt.Valid && !job.LeaseExpiresAt.Time.Before(now) {
			continue
		}
		job.Status = string(StatusFailed)
		job.FinishedAt = pgtype.Timestamptz{Time: now.UTC(), Valid: true}
		f.finished = append(f.finished, id)
		ids = append(ids, pgtype.UUID{Bytes: id, Valid: true})
	}
	return ids, nil
}

func (f *fakeStore) AppendToolchainJobLog(_ context.Context, p sqlc.AppendToolchainJobLogParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.logs[p.JobID.Bytes] {
		if row.Seq == p.Seq {
			return nil // ON CONFLICT DO NOTHING
		}
	}
	f.logs[p.JobID.Bytes] = append(f.logs[p.JobID.Bytes], sqlc.ListToolchainJobLogsRow{Seq: p.Seq, Stream: p.Stream, Line: p.Line})
	return nil
}

func (f *fakeStore) AppendToolchainJobLogTail(_ context.Context, p sqlc.AppendToolchainJobLogTailParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.logs[p.JobID.Bytes]
	var maxSeq int64
	for _, row := range rows {
		if row.Seq > maxSeq {
			maxSeq = row.Seq
		}
	}
	f.logs[p.JobID.Bytes] = append(rows, sqlc.ListToolchainJobLogsRow{Seq: maxSeq + 1, Stream: p.Stream, Line: p.Line})
	return nil
}

func (f *fakeStore) ListToolchainJobLogs(_ context.Context, p sqlc.ListToolchainJobLogsParams) ([]sqlc.ListToolchainJobLogsRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sqlc.ListToolchainJobLogsRow
	for _, row := range f.logs[p.JobID.Bytes] {
		if row.Seq > p.Seq {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	if int(p.Limit) < len(out) {
		out = out[:p.Limit]
	}
	return out, nil
}

func (f *fakeStore) FinishToolchainJob(_ context.Context, p sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finishFailures > 0 {
		f.finishFailures--
		return sqlc.ToolchainJob{}, errors.New("finish failed")
	}
	job, ok := f.jobs[p.ID.Bytes]
	if !ok {
		return sqlc.ToolchainJob{}, pgx.ErrNoRows
	}
	job.Status = p.Status
	job.FinishedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	f.finished = append(f.finished, p.ID.Bytes)
	return *job, nil
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
	return *job, nil
}

func (f *fakeStore) status(t *testing.T, id uuid.UUID) Status {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return Status(f.jobs[id].Status)
}

// logText joins the stored log lines of a job.
func (f *fakeStore) logText(id uuid.UUID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, row := range f.logs[id] {
		b.WriteString(row.Line)
	}
	return b.String()
}

func (f *fakeStore) logBytes(id uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, row := range f.logs[id] {
		n += len(row.Line)
	}
	return n
}

func (f *fakeStore) finishOrder() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.finished...)
}

// seedRunning inserts an already-running job owned by claimedBy with the given
// lease expiry, simulating a control plane that died (or is still alive).
func (f *fakeStore) seedRunning(serverID uuid.UUID, toolID, claimedBy string, lease time.Time) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.jobs[id] = &sqlc.ToolchainJob{
		ID:             pgtype.UUID{Bytes: id, Valid: true},
		ServerID:       pgtype.UUID{Bytes: serverID, Valid: true},
		ToolID:         toolID,
		Action:         string(ActionInstall),
		Status:         string(StatusRunning),
		CreatedAt:      f.nextTime(),
		StartedAt:      pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ClaimedBy:      claimedBy,
		LeaseExpiresAt: pgtype.Timestamptz{Time: lease, Valid: true},
	}
	f.order = append(f.order, id)
	return id
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

// fakeAgent is an AgentConn serving a fixed catalog and per-tool event streams.
// delay lets a test make one tool slow so parallel-vs-sequential behaviour is
// observable.
type fakeAgent struct {
	mu      sync.Mutex
	catalog []*agentv1.ToolInfo
	events  map[string][]*agentv1.ToolEvent
	delay   map[string]time.Duration
	openErr map[string]error
	listErr error
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
	err := f.openErr[key]
	delay := f.delay[key]
	events := f.events[key]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return &fakeStream{events: events}, nil
}

func (f *fakeAgent) Close() error {
	return nil
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
	return testServiceN(st, agent, reg, pub, 1)
}

func testServiceN(st *fakeStore, agent *fakeAgent, reg *fakeRegistry, pub Publisher, workers int) *Service {
	return NewService(Config{
		Store:        st,
		Registry:     reg,
		Dial:         func(context.Context, uuid.UUID) (AgentConn, error) { return agent, nil },
		Publisher:    pub,
		PollInterval: 2 * time.Millisecond,
		RPCTimeout:   time.Second,
		InstanceID:   "test-instance",
		Lease:        2 * time.Second,
		Workers:      workers,
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

// TestGetReturnsJobAndPagedLog covers the new job-detail shape: the log lives
// outside the Job and is fetched in pages via the after cursor.
func TestGetReturnsJobAndPagedLog(t *testing.T) {
	agent := &fakeAgent{catalog: []*agentv1.ToolInfo{catalogTool("docker", true, true)}}
	st := newFakeStore()
	svc := testService(st, agent, &fakeRegistry{}, nil)
	serverID := uuid.New()

	result, err := svc.Enqueue(context.Background(), serverID, ActionInstall, []string{"docker"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job := result.Jobs[0]
	for i, line := range []string{"one", "two", "three"} {
		if err := st.AppendToolchainJobLog(context.Background(), appendLogParams(job.ID, int64(i+1), "stdout", line+"\n")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	full, err := svc.Get(context.Background(), serverID, job.ID, 0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if full.Log != "one\ntwo\nthree\n" || full.NextSeq != 3 {
		t.Fatalf("Get log = %q next = %d, want all three lines, next 3", full.Log, full.NextSeq)
	}
	page, err := svc.Get(context.Background(), serverID, job.ID, 1)
	if err != nil {
		t.Fatalf("Get after: %v", err)
	}
	if page.Log != "two\nthree\n" || page.NextSeq != 3 {
		t.Fatalf("Get after=1 log = %q next = %d, want two/three, next 3", page.Log, page.NextSeq)
	}
	if _, err := svc.Get(context.Background(), uuid.New(), job.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign node get err = %v, want ErrNotFound", err)
	}
}

// waitTerminal polls a job until it reaches a terminal status.
func waitTerminal(t *testing.T, st *fakeStore, id uuid.UUID) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := st.status(t, id); s.Terminal() {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal status (last %s)", id, st.status(t, id))
	return ""
}
