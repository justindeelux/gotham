package toolchain

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// interruptedLog is appended to any job still marked running on boot: the
// control plane that was driving it died mid-install, and the half-finished
// install is not safe to retry automatically.
const interruptedLog = "\ninterrupted: the control plane restarted while this job was running\n"

// Start launches the queue worker. It is idempotent: a second call while the
// worker runs does nothing. The worker reconciles interrupted jobs, then claims
// and runs queued jobs one at a time (globally, which is a superset of the
// required per-node sequential order) until stopped.
func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop(ctx)
}

// Close stops the worker and waits for the in-flight job, then releases the
// realtime publisher. It is safe to call on a service that never started, and
// safe to call more than once. A job interrupted by shutdown stays "running"
// and is reconciled to failed by the next Start (see interruptedLog).
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

// loop is the worker: reconcile once, then drain the queue on every wakeup or
// poll tick until the context ends.
func (s *Service) loop(ctx context.Context) {
	defer s.wg.Done()

	if ids, err := s.store.FailInterruptedToolchainJobs(ctx, interruptedLog); err != nil {
		s.logger.Error("toolchain: reconcile interrupted jobs", "error", err)
	} else if len(ids) > 0 {
		s.logger.Warn("toolchain: marked interrupted jobs failed on boot", "count", len(ids))
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		s.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

// drain claims and runs queued jobs until the queue is empty. It stops early if
// the context ends, leaving any in-flight job for boot reconciliation.
func (s *Service) drain(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		row, err := s.store.ClaimNextQueuedToolchainJob(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			s.logger.Error("toolchain: claim queued job", "error", err)
			return
		}
		s.run(ctx, jobFromRow(row))
	}
}

// run executes one claimed job: it dials the node, opens the install/uninstall
// stream, appends every output line and closes the job with the stream's
// outcome. It never returns an error: a failure is recorded on the job.
func (s *Service) run(ctx context.Context, job Job) {
	s.events.Job(ctx, job) // announce the running transition (status already set)

	client, err := s.dialClient(ctx, job.ServerID)
	if err != nil {
		s.finish(ctx, job, StatusFailed, "failed to reach the node agent: "+err.Error()+"\n")
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
		s.finish(ctx, job, StatusFailed, "failed to start "+string(job.Action)+": "+err.Error()+"\n")
		return
	}

	status := StatusSucceeded
	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			// A cancelled worker context is a shutdown, not a job failure: leave
			// the job running so boot reconciliation records it as interrupted.
			if ctx.Err() != nil {
				return
			}
			status = StatusFailed
			s.appendLog(ctx, job, "stderr", "stream error: "+recvErr.Error())
			break
		}
		switch ev := event.GetEvent().(type) {
		case *agentv1.ToolEvent_Output:
			s.appendLog(ctx, job, ev.Output.GetStream(), ev.Output.GetLine())
		case *agentv1.ToolEvent_Result:
			if ev.Result.GetStatus() == string(StatusFailed) {
				status = StatusFailed
			}
		}
	}
	s.finish(ctx, job, status, "")
}

// appendLog persists one output line and streams it. A DB error is logged, not
// fatal: losing a log line must never abort the job.
func (s *Service) appendLog(ctx context.Context, job Job, stream, line string) {
	if ctx.Err() != nil {
		return
	}
	if _, err := s.store.AppendToolchainJobLog(ctx, appendLogParams(job.ID, line+"\n")); err != nil {
		s.logger.Warn("toolchain: append job log", "error", err, "job_id", job.ID.String())
	}
	s.events.Log(ctx, job.ServerID, job.ID, stream, line)
}

// finish closes a job with its terminal status and an optional final message,
// then publishes the terminal transition (and the message as a log line).
func (s *Service) finish(ctx context.Context, job Job, status Status, tail string) {
	row, err := s.store.FinishToolchainJob(ctx, finishParams(job.ID, status, tail))
	if err != nil {
		s.logger.Error("toolchain: finish job", "error", err, "job_id", job.ID.String(), "status", string(status))
		return
	}
	finished := jobFromRow(row)
	if msg := strings.TrimRight(tail, "\n"); msg != "" {
		s.events.Log(ctx, job.ServerID, job.ID, "stderr", msg)
	}
	s.events.Job(ctx, finished)
}
