package toolchain

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const interruptedLog = "interrupted: the owning control plane stopped while this job was running\n"

func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	workers := s.workers
	s.mu.Unlock()

	ready := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.recoverStale(ctx)
		close(ready)
	}()
	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx, ready)
	}
	s.wg.Add(1)
	go s.sweeper(ctx)
}

func (s *Service) Close() error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
	if closer, ok := s.publisher.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *Service) worker(ctx context.Context, ready <-chan struct{}) {
	defer s.wg.Done()

	select {
	case <-ctx.Done():
		return
	case <-ready:
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		row, err := s.store.ClaimNextQueuedToolchainJob(ctx, s.claimParams())
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if !s.idle(ctx, ticker) {
				return
			}
		case errors.Is(err, store.ErrClaimContended):
			continue
		case err != nil:
			s.logger.Error("toolchain: claim queued job", "error", err)
			if !s.idle(ctx, ticker) {
				return
			}
		default:
			s.run(ctx, jobFromRow(row))
		}
	}
}

func (s *Service) idle(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker.C:
		return true
	case <-s.wake:
		return true
	}
}

func (s *Service) sweeper(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(defaultSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverStale(ctx)
		}
	}
}

func (s *Service) recoverStale(ctx context.Context) {
	ids, err := s.store.FailStaleToolchainJobs(ctx)
	if err != nil {
		s.logger.Error("toolchain: recover stale jobs", "error", err)
		return
	}
	for _, id := range ids {
		jobID := uuidFromPG(id)
		if err := s.store.AppendToolchainJobLogTail(ctx, sqlc.AppendToolchainJobLogTailParams{
			JobID:  pgUUID(jobID),
			Stream: "stderr",
			Line:   interruptedLog,
		}); err != nil {
			s.logger.Warn("toolchain: append interrupted marker", "error", err, "job_id", jobID.String())
		}
	}
	if len(ids) > 0 {
		s.logger.Warn("toolchain: failed interrupted jobs", "count", len(ids))
	}
}

func (s *Service) claimParams() sqlc.ClaimNextQueuedToolchainJobParams {
	return sqlc.ClaimNextQueuedToolchainJobParams{
		ClaimedBy:    s.instanceID,
		LeaseSeconds: int32(s.lease / time.Second),
	}
}

func (s *Service) run(ctx context.Context, job Job) {
	s.events.Job(ctx, job)

	log := &jobLog{svc: s, ctx: ctx, job: job}

	streamCtx, cancelStream := context.WithTimeout(ctx, installTimeout)
	defer cancelStream()

	var lostLease atomic.Bool
	stopLease := s.startLeaseRenewal(ctx, job.ID, func() {
		lostLease.Store(true)
		cancelStream()
	})
	defer stopLease()

	client, err := s.dialClient(ctx, job.ServerID)
	if err != nil {
		if lostLease.Load() {
			return
		}
		log.line("stderr", "failed to reach the node agent: "+err.Error())
		s.finish(ctx, job, StatusFailed)
		return
	}
	defer func() { _ = client.Close() }()

	req := &agentv1.InstallToolRequest{Id: job.ToolID}
	var stream grpc.ServerStreamingClient[agentv1.ToolEvent]
	if job.Action == ActionUninstall {
		stream, err = client.UninstallTool(streamCtx, req)
	} else {
		stream, err = client.InstallTool(streamCtx, req)
	}
	if err != nil {
		if lostLease.Load() {
			return
		}
		log.line("stderr", "failed to start "+string(job.Action)+": "+err.Error())
		s.finish(ctx, job, StatusFailed)
		return
	}

	outcome := StatusFailed
	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if lostLease.Load() {
				return
			}
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return
			}
			log.line("stderr", "stream error: "+recvErr.Error())
			outcome = StatusFailed
			break
		}
		switch ev := event.GetEvent().(type) {
		case *agentv1.ToolEvent_Output:
			log.line(ev.Output.GetStream(), ev.Output.GetLine())
		case *agentv1.ToolEvent_Result:
			if ev.Result.GetStatus() == string(StatusSucceeded) {
				outcome = StatusSucceeded
			} else {
				outcome = StatusFailed
			}
		}
	}
	if lostLease.Load() {
		return
	}
	s.finish(ctx, job, outcome)
}

func (s *Service) startLeaseRenewal(ctx context.Context, jobID uuid.UUID, onLost func()) func() {
	leaseCtx, cancel := context.WithCancel(ctx)
	interval := s.renewInterval
	if interval <= 0 {
		interval = time.Second
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				rows, err := s.store.RenewToolchainJobLease(leaseCtx, sqlc.RenewToolchainJobLeaseParams{
					ID:           pgUUID(jobID),
					ClaimedBy:    s.instanceID,
					LeaseSeconds: int32(s.lease / time.Second),
				})
				if err != nil {
					s.logger.Warn("toolchain: renew job lease", "error", err, "job_id", jobID.String())
					continue
				}
				if rows == 0 {
					s.logger.Warn("toolchain: job lease lost, aborting", "job_id", jobID.String())
					onLost()
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}

func (s *Service) finish(ctx context.Context, job Job, status Status) {
	finishCtx := context.WithoutCancel(ctx)
	params := sqlc.FinishToolchainJobParams{
		ID:        pgUUID(job.ID),
		ClaimedBy: s.instanceID,
		Status:    string(status),
	}
	var (
		row sqlc.ToolchainJob
		err error
	)
	for attempt := 0; attempt < 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(finishCtx, 5*time.Second)
		row, err = s.store.FinishToolchainJob(attemptCtx, params)
		cancel()
		if err == nil {
			s.events.Job(finishCtx, jobFromRow(row))
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			s.logger.Warn("toolchain: finish skipped, job no longer owned",
				"job_id", job.ID.String(), "status", string(status))
			return
		}
		s.logger.Warn("toolchain: finish job attempt failed",
			"error", err, "job_id", job.ID.String(), "attempt", attempt+1)
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	s.logger.Error("toolchain: finish job", "error", err, "job_id", job.ID.String(), "status", string(status))
}

type jobLog struct {
	svc       *Service
	ctx       context.Context
	job       Job
	seq       int64
	written   int
	truncated bool
}

func (l *jobLog) line(stream, text string) {
	if l.truncated || l.ctx.Err() != nil {
		return
	}
	if l.written+len(text)+1 > l.svc.maxLogBytes {
		l.truncated = true
		l.write("stderr", logTruncatedMsg)
		return
	}
	l.write(stream, text)
}

func (l *jobLog) write(stream, text string) {
	l.seq++
	if err := l.svc.store.AppendToolchainJobLog(l.ctx, appendLogParams(l.job.ID, l.seq, stream, text+"\n")); err != nil {
		l.svc.logger.Warn("toolchain: append job log", "error", err, "job_id", l.job.ID.String())
	}
	l.svc.events.Log(l.ctx, l.job.ServerID, l.job.ID, l.seq, stream, text)
	l.written += len(text) + 1
}
