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
	"github.com/justindeelux/gotham/toolcatalog"
)

const (
	defaultRPCTimeout    = 30 * time.Second
	installTimeout       = 30 * time.Minute
	defaultPollInterval  = time.Second
	defaultListLimit     = 100
	maxListLimit         = 500
	defaultWorkers       = 4
	defaultLease         = 90 * time.Second
	defaultSweepInterval = 30 * time.Second
	maxJobLogBytes       = 256 << 10
	maxLogPage           = 2000
	logTruncatedMsg      = "[log truncated at 256 KiB]"
)

type ToolchainService interface {
	ListTools(ctx context.Context, serverID uuid.UUID) ([]Tool, error)
	Enqueue(ctx context.Context, serverID uuid.UUID, action Action, toolIDs []string) (EnqueueResult, error)
	Cancel(ctx context.Context, serverID, jobID uuid.UUID) (Job, error)
	Get(ctx context.Context, serverID, jobID uuid.UUID, after int64) (JobDetail, error)
	List(ctx context.Context, serverID uuid.UUID, limit int) ([]Job, error)
}

type AgentClient interface {
	ListTools(ctx context.Context, in *agentv1.ListToolsRequest, opts ...grpc.CallOption) (*agentv1.ListToolsResponse, error)
	InstallTool(ctx context.Context, in *agentv1.InstallToolRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error)
	UninstallTool(ctx context.Context, in *agentv1.InstallToolRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ToolEvent], error)
}

type AgentConn interface {
	AgentClient
	io.Closer
}

type DialFunc func(ctx context.Context, serverID uuid.UUID) (AgentConn, error)

type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

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

var (
	_ Store            = (*store.Store)(nil)
	_ ToolchainService = (*Service)(nil)
	_ io.Closer        = (*Service)(nil)
)

type Config struct {
	Store        Store
	Registry     Registry
	Dial         DialFunc
	Publisher    Publisher
	RedisAddr    string
	Logger       *slog.Logger
	RPCTimeout   time.Duration
	PollInterval time.Duration
	InstanceID   string
	Lease        time.Duration
	Workers      int
}

type Service struct {
	store     Store
	registry  Registry
	dial      DialFunc
	events    *Emitter
	publisher Publisher
	logger    *slog.Logger

	rpcTimeout    time.Duration
	pollInterval  time.Duration
	instanceID    string
	lease         time.Duration
	workers       int
	renewInterval time.Duration
	maxLogBytes   int

	wake chan struct{}

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewDefaultService(cfg Config) *Service {
	if cfg.Store == nil {
		return nil
	}
	return NewService(cfg)
}

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

func (s *Service) ListTools(ctx context.Context, serverID uuid.UUID) ([]Tool, error) {
	if err := s.resolve(ctx, serverID, false); err != nil {
		return nil, err
	}
	return s.catalog(ctx, serverID)
}

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
	if action == ActionUninstall {
		for _, id := range ids {
			if tool, known := toolcatalog.Lookup(id); known && !tool.Uninstallable {
				return EnqueueResult{}, fmt.Errorf("%w: cannot uninstall %q: required by the platform; %s", ErrValidation, id, tool.Description)
			}
		}
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
		return Job{}, fmt.Errorf("%w: job is no longer queued", ErrConflict)
	}
	if err != nil {
		return Job{}, fmt.Errorf("toolchain: cancel job: %w", err)
	}
	job := jobFromRow(row)
	s.events.Job(ctx, job)
	return job, nil
}

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

func (s *Service) wakeWorkers() {
	for i := 0; i < cap(s.wake); i++ {
		select {
		case s.wake <- struct{}{}:
		default:
			return
		}
	}
}

func defaultInstanceID() string {
	return uuid.NewString()
}

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
