package toolchain

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// interruptedLog is appended to a job whose owning control plane died while it
// was running: the half-finished install is not safe to retry automatically.
const interruptedLog = "interrupted: the owning control plane stopped while this job was running\n"

// Start launches the worker pool and the stale-job sweeper. It is idempotent:
// a second call while running does nothing. Several workers claim independently
// (parallel across nodes) and the claim query keeps each node's jobs
// sequential.
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

	// Boot recovery runs once, before any worker claims, so an "owned" job can
	// never be a live one. Workers wait on ready.
	ready := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.recoverOwned(ctx)
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

// Close stops the pool and the sweeper, waits for in-flight jobs, then releases
// the realtime publisher. It is idempotent and safe on a service that never
// started. A job interrupted here stays running until its lease lapses, when a
// sweeper (this instance or another) fails it.
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

// worker claims and runs jobs until ctx ends. It keeps draining while jobs are
// claimable for nodes it is not already working (the claim query excludes a
// node with a running job), and idles between polls otherwise. It waits for
// boot recovery (ready) before its first claim.
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

// idle waits for the next poll, an enqueue wakeup, or ctx cancellation, and
// reports whether the worker should keep going.
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

// sweeper reconciles stale jobs periodically: it fails jobs whose lease lapsed
// (a crashed instance's job), leaving live instances' jobs untouched.
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

// recoverOwned fails the jobs this instance claimed before a restart. It runs
// once at boot, before workers claim, so it never touches a live job.
func (s *Service) recoverOwned(ctx context.Context) {
	ids, err := s.store.FailOwnedToolchainJobs(ctx, s.instanceID)
	if err != nil {
		s.logger.Error("toolchain: recover owned jobs", "error", err)
		return
	}
	s.markInterrupted(ctx, ids)
}

// recoverStale fails running jobs whose lease lapsed.
func (s *Service) recoverStale(ctx context.Context) {
	ids, err := s.store.FailStaleToolchainJobs(ctx)
	if err != nil {
		s.logger.Error("toolchain: recover stale jobs", "error", err)
		return
	}
	s.markInterrupted(ctx, ids)
}

// markInterrupted records why each recovered job was failed.
func (s *Service) markInterrupted(ctx context.Context, ids []pgtype.UUID) {
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

// claimParams builds the claim's lease parameters.
func (s *Service) claimParams() sqlc.ClaimNextQueuedToolchainJobParams {
	return sqlc.ClaimNextQueuedToolchainJobParams{
		ClaimedBy:    s.instanceID,
		LeaseSeconds: int32(s.lease / time.Second),
	}
}

// run executes one claimed job: it dials the node, opens the install/uninstall
// stream, appends every output line and closes the job with the stream's
// outcome. It never returns an error: a failure is recorded on the job.
func (s *Service) run(ctx context.Context, job Job) {
	s.events.Job(ctx, job) // announce the running transition (status already set)

	log := &jobLog{svc: s, ctx: ctx, job: job}
	stopLease := s.startLeaseRenewal(ctx, job.ID)
	defer stopLease()

	client, err := s.dialClient(ctx, job.ServerID)
	if err != nil {
		log.line("stderr", "failed to reach the node agent: "+err.Error())
		s.finish(ctx, job, StatusFailed)
		return
	}
	defer func() { _ = client.Close() }()

	streamCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	req := &agentv1.InstallToolRequest{Id: job.ToolID}

	var stream grpc.ServerStreamingClient[agentv1.ToolEvent]
	if job.Action == ActionUninstall {
		stream, err = client.UninstallTool(streamCtx, req)
	} else {
		stream, err = client.InstallTool(streamCtx, req)
	}
	if err != nil {
		log.line("stderr", "failed to start "+string(job.Action)+": "+err.Error())
		s.finish(ctx, job, StatusFailed)
		return
	}

	// A stream that ends without an explicit success result is a failure: only
	// "succeeded" is success.
	outcome := StatusFailed
	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			// A cancelled worker context is a shutdown, not a job failure: leave
			// the job running so lease recovery records it as interrupted.
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
	s.finish(ctx, job, outcome)
}

// startLeaseRenewal keeps a running job's lease fresh so recovery never fails a
// live job; it returns a stop function that cancels the renewer and waits.
func (s *Service) startLeaseRenewal(ctx context.Context, jobID uuid.UUID) func() {
	leaseCtx, cancel := context.WithCancel(ctx)
	interval := s.lease / 3
	if interval < time.Second {
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
				if err := s.store.RenewToolchainJobLease(leaseCtx, sqlc.RenewToolchainJobLeaseParams{
					ID:           pgUUID(jobID),
					LeaseSeconds: int32(s.lease / time.Second),
				}); err != nil {
					s.logger.Warn("toolchain: renew job lease", "error", err, "job_id", jobID.String())
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}

// finish closes a job with its terminal status and publishes the transition.
// The write retries on a context that shutdown does not cancel, so a blip (or a
// shutdown racing the last line) cannot strand the job as running forever.
func (s *Service) finish(ctx context.Context, job Job, status Status) {
	finishCtx := context.WithoutCancel(ctx)
	var (
		row sqlc.ToolchainJob
		err error
	)
	for attempt := 0; attempt < 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(finishCtx, 5*time.Second)
		row, err = s.store.FinishToolchainJob(attemptCtx, finishParams(job.ID, status))
		cancel()
		if err == nil {
			break
		}
		s.logger.Warn("toolchain: finish job attempt failed",
			"error", err, "job_id", job.ID.String(), "attempt", attempt+1)
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	if err != nil {
		s.logger.Error("toolchain: finish job", "error", err, "job_id", job.ID.String(), "status", string(status))
		return
	}
	s.events.Job(finishCtx, jobFromRow(row))
}

// jobLog appends job output line by line, capping the persisted size: once the
// cap is reached it stops and records a single truncation marker.
type jobLog struct {
	svc       *Service
	ctx       context.Context
	job       Job
	seq       int64
	written   int
	truncated bool
}

// line appends one line of output (or the truncation marker once over the cap).
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
