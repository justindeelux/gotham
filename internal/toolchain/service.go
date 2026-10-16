package toolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Timeouts and limits.
const (
	// defaultRPCTimeout bounds a catalog listing.
	defaultRPCTimeout = 30 * time.Second
	// installTimeout bounds a whole install/uninstall stream, which is not a
	// single RPC: an apt install can legitimately run for minutes.
	installTimeout = 30 * time.Minute
	// defaultPollInterval is how often a worker re-checks the queue when no
	// enqueue has woken it (multi-instance control planes, or a lost wakeup).
	defaultPollInterval = time.Second
	// defaultListLimit / maxListLimit bound the job list.
	defaultListLimit = 100
	maxListLimit     = 500
	// defaultWorkers is the worker pool size: nodes run in parallel, and the
	// per-node claim keeps one node's jobs sequential.
	defaultWorkers = 4
	// defaultLease is how long a claimed job stays owned before another instance
	// may recover it. The worker renews it every lease/3 while running.
	defaultLease = 90 * time.Second
	// defaultSweepInterval is how often the worker reconciles stale jobs.
	defaultSweepInterval = 30 * time.Second
	// maxJobLogBytes caps a job's persisted log; beyond it the worker stops
	// appending and records one truncation marker. maxLogPage bounds one log
	// fetch so a client pages with ?after=seq instead of pulling everything.
	maxJobLogBytes  = 256 << 10
	maxLogPage      = 2000
	logTruncatedMsg = "[log truncated at 256 KiB]"
)

// ToolchainService is the slice of the domain the HTTP layer depends on. It
// keeps the handler testable with a fake that needs no database.
type ToolchainService interface {
	// ListTools returns the node's catalog with each entry's detected state.
	ListTools(ctx context.Context, serverID uuid.UUID) ([]Tool, error)
	// Enqueue validates action/toolIDs against the node's catalog, then queues
	// one job per tool, skipping a tool the node already has an active job for.
	Enqueue(ctx context.Context, serverID uuid.UUID, action Action, toolIDs []string) (EnqueueResult, error)
	// Cancel cancels one queued job (a running or terminal job is a conflict).
	Cancel(ctx context.Context, serverID, jobID uuid.UUID) (Job, error)
	// Get returns one job plus a page of its log (lines with seq > after, at
	// most maxLogPage), so the job list and realtime frames never carry the log.
	Get(ctx context.Context, serverID, jobID uuid.UUID, after int64) (JobDetail, error)
	// List returns a node's jobs, newest first.
	List(ctx context.Context, serverID uuid.UUID, limit int) ([]Job, error)
}

// AgentClient is the slice of agentv1.ToolServiceClient the worker uses.
type AgentClient interface {
	ListTools(ctx context.Context, in *agentv1.ListToolsRequest, opts ...grpc.CallOption) (*agentv1.ListToolsResponse, error)
	InstallTool(ctx context.Context, in *agentv1.InstallToolRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error)
	UninstallTool(ctx context.Context, in *agentv1.InstallToolRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error)
}

// AgentConn is an AgentClient that owns a connection to close when done.
type AgentConn interface {
	AgentClient
	io.Closer
}

// DialFunc dials one node's agent ToolService.
type DialFunc func(ctx context.Context, serverID uuid.UUID) (AgentConn, error)

// Registry resolves a server row for its team and node identity.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

// Store is the queue persistence the service needs. *store.Store satisfies it.
type Store interface {
	CreateToolchainJob(ctx context.Context, params sqlc.CreateToolchainJobParams) (sqlc.ToolchainJob, error)
	GetToolchainJobForServer(ctx context.Context, params sqlc.GetToolchainJobForServerParams) (sqlc.ToolchainJob, error)
	ListToolchainJobsByServer(ctx context.Context, params sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error)
	ClaimNextQueuedToolchainJob(ctx context.Context, params sqlc.ClaimNextQueuedToolchainJobParams) (sqlc.ToolchainJob, error)
	RenewToolchainJobLease(ctx context.Context, params sqlc.RenewToolchainJobLeaseParams) (int64, error)
	FailStaleToolchainJobs(ctx context.Context) ([]pgtype.UUID, error)
	AppendToolchainJobLog(ctx context.Context, params sqlc.AppendToolchainJobLogParams) error
	AppendToolchainJobLogTail(ctx context.Context, params sqlc.AppendToolchainJobLogTailParams) error
	ListToolchainJobLogs(ctx context.Context, params sqlc.ListToolchainJobLogsParams) ([]sqlc.ListToolchainJobLogsRow, error)
	FinishToolchainJob(ctx context.Context, params sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error)
	CancelToolchainJob(ctx context.Context, id pgtype.UUID) (sqlc.ToolchainJob, error)
}

// Compile-time checks: the production store is a Store, and *Service is the
// ToolchainService the HTTP layer mounts.
var (
	_ Store            = (*store.Store)(nil)
	_ ToolchainService = (*Service)(nil)
	_ io.Closer        = (*Service)(nil)
)

// Config wires a Service. Store and Registry are required (NewDefaultService
// returns nil without a store). Dial may be nil until the mTLS dialer is
// plugged in; Publisher/RedisAddr default to a Redis publisher, InstanceID to a
// random per-process id, Workers to a small pool.
type Config struct {
	Store      Store
	Registry   Registry
	Dial       DialFunc
	Publisher  Publisher
	RedisAddr  string
	Logger     *slog.Logger
	RPCTimeout time.Duration
	// PollInterval overrides the worker's queue poll (tests use a short one).
	PollInterval time.Duration
	// InstanceID identifies this process in claimed_by. It is random per process
	// by default and is only used to scope lease renewal and finish; recovery is
	// keyed on the lease, never on the id.
	InstanceID string
	// Lease overrides how long a claimed job stays owned (tests shorten it).
	Lease time.Duration
	// Workers overrides the pool size (nodes run in parallel up to this many).
	Workers int
}

// Service is the control-plane toolchain domain service: the queue API plus a
// pool of workers that drive node agents.
type Service struct {
	store     Store
	registry  Registry
	dial      DialFunc
	events    *Emitter
	publisher Publisher
	logger    *slog.Logger

	rpcTimeout   time.Duration
	pollInterval time.Duration
	instanceID   string
	lease        time.Duration
	workers      int
	// renewInterval is how often a running job's lease is renewed (lease/3,
	// floored at a second); a field so tests can shorten it.
	renewInterval time.Duration
	// maxLogBytes caps a job's persisted log (a field so tests can shrink it).
	maxLogBytes int

	// wake nudges idle workers as soon as a job is enqueued, so a job starts
	// without waiting for the poll tick and tests do not have to sleep.
	wake chan struct{}

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewDefaultService builds the production Service for the HTTP wiring. It
// returns nil (so the routes are not mounted) when no store is configured.
func NewDefaultService(cfg Config) *Service {
	if cfg.Store == nil {
		return nil
	}
	return NewService(cfg)
}

// NewService builds a Service from cfg.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	pub := cfg.Publisher
	if pub == nil {
		pub = NewRedisPublisher(cfg.RedisAddr)
	}
	rpcTimeout := cfg.RPCTimeout
	if rpcTimeout <= 0 {
		rpcTimeout = defaultRPCTimeout
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = defaultPollInterval
	}
	lease := cfg.Lease
	if lease <= 0 {
		lease = defaultLease
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	instanceID := strings.TrimSpace(cfg.InstanceID)
	if instanceID == "" {
		instanceID = defaultInstanceID()
	}
	renewInterval := lease / 3
	if renewInterval < time.Second {
		renewInterval = time.Second
	}
	return &Service{
		store:         cfg.Store,
		registry:      cfg.Registry,
		dial:          cfg.Dial,
		events:        NewEmitter(pub),
		publisher:     pub,
		logger:        logger,
		rpcTimeout:    rpcTimeout,
		pollInterval:  poll,
		instanceID:    instanceID,
		lease:         lease,
		workers:       workers,
		renewInterval: renewInterval,
		maxLogBytes:   maxJobLogBytes,
		wake:          make(chan struct{}, workers),
	}
}

// ListTools returns the node's catalog with each entry's detected state. It
// authorizes the node for read and dials its agent.
func (s *Service) ListTools(ctx context.Context, serverID uuid.UUID) ([]Tool, error) {
	if err := s.resolve(ctx, serverID, false); err != nil {
		return nil, err
	}
	return s.catalog(ctx, serverID)
}

// InstalledToolIDs returns the catalog ids installed on the node, dialing its
// agent without re-checking the caller's team scope (the caller has already
// resolved the node). The deploy gate uses it to refuse a build pack whose tool
// is absent, so the create answers a clear 4xx instead of a build that fails at
// "railpack is not on PATH".
func (s *Service) InstalledToolIDs(ctx context.Context, serverID uuid.UUID) (map[string]bool, error) {
	catalog, err := s.catalog(ctx, serverID)
	if err != nil {
		return nil, err
	}
	installed := make(map[string]bool, len(catalog))
	for _, tool := range catalog {
		installed[tool.ID] = tool.Installed
	}
	return installed, nil
}

// catalog dials the node's agent and returns its tool list, without authorizing
// (the caller has already resolved the node).
func (s *Service) catalog(ctx context.Context, serverID uuid.UUID) ([]Tool, error) {
	client, err := s.dialClient(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	rpcCtx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	defer cancel()
	resp, err := client.ListTools(rpcCtx, &agentv1.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("%w: list tools: %v", ErrAgentUnavailable, err)
	}
	tools := make([]Tool, 0, len(resp.GetTools()))
	for _, t := range resp.GetTools() {
		if t != nil {
			tools = append(tools, toolFromProto(t))
		}
	}
	return tools, nil
}

// Enqueue validates the request against the node's live catalog, then queues one
// job per tool. The whole request is rejected (no jobs created) when any tool is
// unknown or cannot perform the action on this node, so a partial selection
// never silently enqueues a subset.
func (s *Service) Enqueue(ctx context.Context, serverID uuid.UUID, action Action, toolIDs []string) (EnqueueResult, error) {
	if !action.Valid() {
		return EnqueueResult{}, fmt.Errorf("%w: unknown action %q", ErrValidation, action)
	}
	ids := normalizeToolIDs(toolIDs)
	if len(ids) == 0 {
		return EnqueueResult{}, fmt.Errorf("%w: at least one tool is required", ErrValidation)
	}
	if err := s.resolve(ctx, serverID, true); err != nil {
		return EnqueueResult{}, err
	}

	catalog, err := s.catalog(ctx, serverID)
	if err != nil {
		return EnqueueResult{}, err
	}
	byID := make(map[string]Tool, len(catalog))
	for _, t := range catalog {
		byID[t.ID] = t
	}
	for _, id := range ids {
		tool, ok := byID[id]
		if !ok {
			return EnqueueResult{}, fmt.Errorf("%w: unknown tool %q", ErrValidation, id)
		}
		if action == ActionInstall && !tool.Installable {
			return EnqueueResult{}, fmt.Errorf("%w: tool %q is not installable on this node", ErrValidation, id)
		}
		if action == ActionUninstall && !tool.Uninstallable {
			return EnqueueResult{}, fmt.Errorf("%w: tool %q is not uninstallable on this node", ErrValidation, id)
		}
	}

	result := EnqueueResult{Jobs: []Job{}, Skipped: []string{}}
	for _, id := range ids {
		row, err := s.store.CreateToolchainJob(ctx, sqlc.CreateToolchainJobParams{
			ServerID: pgUUID(serverID),
			ToolID:   id,
			Action:   string(action),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The node already has an active job for this tool and action (the
			// partial unique index refused the insert).
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if err != nil {
			return EnqueueResult{}, fmt.Errorf("toolchain: enqueue %q: %w", id, err)
		}
		job := jobFromRow(row)
		result.Jobs = append(result.Jobs, job)
		s.events.Job(ctx, job)
	}
	s.wakeWorkers()
	return result, nil
}

// Cancel cancels one queued job. A job that is running, terminal, missing, or
// owned by another node answers ErrConflict/ErrNotFound.
func (s *Service) Cancel(ctx context.Context, serverID, jobID uuid.UUID) (Job, error) {
	if err := s.resolve(ctx, serverID, true); err != nil {
		return Job{}, err
	}
	existing, err := s.store.GetToolchainJobForServer(ctx, sqlc.GetToolchainJobForServerParams{
		ID:       pgUUID(jobID),
		ServerID: pgUUID(serverID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("toolchain: load job: %w", err)
	}
	if Status(existing.Status) != StatusQueued {
		return Job{}, fmt.Errorf("%w: job is %s", ErrConflict, existing.Status)
	}
	row, err := s.store.CancelToolchainJob(ctx, pgUUID(jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		// Raced with the worker: it claimed the job between our read and write.
		return Job{}, fmt.Errorf("%w: job is no longer queued", ErrConflict)
	}
	if err != nil {
		return Job{}, fmt.Errorf("toolchain: cancel job: %w", err)
	}
	job := jobFromRow(row)
	s.events.Job(ctx, job)
	return job, nil
}

// Get returns one job plus a page of its log (lines with seq > after). A job
// owned by another node, or on a node the caller may not see, answers
// ErrNotFound.
func (s *Service) Get(ctx context.Context, serverID, jobID uuid.UUID, after int64) (JobDetail, error) {
	if err := s.resolve(ctx, serverID, false); err != nil {
		return JobDetail{}, err
	}
	row, err := s.store.GetToolchainJobForServer(ctx, sqlc.GetToolchainJobForServerParams{
		ID:       pgUUID(jobID),
		ServerID: pgUUID(serverID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return JobDetail{}, ErrNotFound
	}
	if err != nil {
		return JobDetail{}, fmt.Errorf("toolchain: load job: %w", err)
	}
	if after < 0 {
		after = 0
	}
	lines, err := s.store.ListToolchainJobLogs(ctx, sqlc.ListToolchainJobLogsParams{
		JobID: pgUUID(jobID),
		Seq:   after,
		Limit: maxLogPage,
	})
	if err != nil {
		return JobDetail{}, fmt.Errorf("toolchain: load job log: %w", err)
	}
	detail := JobDetail{Job: jobFromRow(row), NextSeq: after}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Line)
		if l.Seq > detail.NextSeq {
			detail.NextSeq = l.Seq
		}
	}
	detail.Log = b.String()
	return detail, nil
}

// List returns a node's jobs, newest first. A limit of zero uses the default.
func (s *Service) List(ctx context.Context, serverID uuid.UUID, limit int) ([]Job, error) {
	if err := s.resolve(ctx, serverID, false); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	rows, err := s.store.ListToolchainJobsByServer(ctx, sqlc.ListToolchainJobsByServerParams{
		ServerID: pgUUID(serverID),
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("toolchain: list jobs: %w", err)
	}
	jobs := make([]Job, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, jobFromRow(row))
	}
	return jobs, nil
}

// resolve loads the node and authorizes the caller's active team against it:
// read access needs membership, write access needs the owner/admin role. A node
// the caller may not touch answers ErrNotFound, so node ids cannot be probed,
// exactly like the container and server surfaces. A request without a team
// scope (background jobs, tests, or teams disabled) keeps the pre-teams
// behavior, and a nil registry (worker-only construction) skips the check.
func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, write bool) error {
	if s.registry == nil {
		return nil
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("toolchain: resolve node: %w", err)
	}
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return ErrForbidden
		}
		return ErrNotFound
	}
	return nil
}

// dialClient dials the node's agent ToolService.
func (s *Service) dialClient(ctx context.Context, serverID uuid.UUID) (AgentConn, error) {
	if s.dial == nil {
		return nil, fmt.Errorf("%w: dialer is not configured", ErrAgentUnavailable)
	}
	conn, err := s.dial(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	return conn, nil
}

// wakeWorkers nudges every idle worker (a full send buffer means they are
// already awake) so an enqueue does not wait for the poll tick.
func (s *Service) wakeWorkers() {
	for i := 0; i < cap(s.wake); i++ {
		select {
		case s.wake <- struct{}{}:
		default:
			return
		}
	}
}

// defaultInstanceID returns a per-process identity for claimed jobs. It is
// deliberately random: an instance must never be able to fail a job another
// (possibly restart-overlapping) process still owns, so recovery is keyed only
// on the lease, not on the instance id.
func defaultInstanceID() string {
	return uuid.NewString()
}

// normalizeToolIDs trims, drops empties, and removes duplicates while keeping
// the caller's order, so one request cannot enqueue the same tool twice.
func normalizeToolIDs(toolIDs []string) []string {
	seen := make(map[string]bool, len(toolIDs))
	out := make([]string, 0, len(toolIDs))
	for _, raw := range toolIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
