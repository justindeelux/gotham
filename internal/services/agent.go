package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ComposeAgent is the node agent's ComposeService as the control plane uses
// it, with the agent's protobuf types translated at the seam. The production
// implementation is *GRPCComposeAgent; tests substitute a fake.
//
// Connection ownership: a dial returns a fresh agent for one operation and its
// Close must be called when the unary operation completes, so repeated
// operations and UI polling do not accumulate gRPC connections. Logs hands
// connection ownership to the returned LogStream, which closes it when the
// stream ends or the caller stops reading.
type ComposeAgent interface {
	// Validate writes the document and validates it, returning the declared
	// compose service names.
	Validate(ctx context.Context, projectName string, composeYAML []byte) ([]string, error)
	// Up starts the project (or restarts its containers when restart is set).
	Up(ctx context.Context, projectName string, composeYAML []byte, restart bool) error
	// Down stops and removes the project's containers; named volumes stay.
	Down(ctx context.Context, projectName string, composeYAML []byte) error
	// Ps lists the project's containers.
	Ps(ctx context.Context, projectName string) ([]ComposeContainer, error)
	// Logs opens the project's (or one service's) merged log stream. On
	// success the returned stream owns the agent's connection.
	Logs(ctx context.Context, projectName, service string, tail int64, follow bool) (LogStream, error)
	// Close releases the agent's connection. It is safe to call more than
	// once; later calls return nil.
	Close() error
}

// LogStream is one node log stream. A first-read failure is reported by the
// opening call, so the HTTP layer can still answer a correct status; Err
// carries a failure that happened after the stream had started (nil for a
// clean end or a client cancellation, where no status can be corrected
// anymore).
type LogStream interface {
	// Chunks delivers raw output until the stream ends.
	Chunks() <-chan []byte
	// Err reports a terminal stream failure after Chunks is closed.
	Err() error
	// Close releases the stream's connection. It is safe to call more than
	// once.
	Close() error
}

// composeClient is the generated ComposeService client plus the connection
// lifecycle. *servers.ComposeClient satisfies it; the interface keeps this
// package free of the dialing layer.
type composeClient interface {
	agentv1.ComposeServiceClient
	Close() error
}

// GRPCComposeAgent adapts the generated ComposeService client to ComposeAgent.
// It is built by the HTTP wiring from the servers package's dialed client, so
// this package never dials an agent itself.
type GRPCComposeAgent struct {
	client    composeClient
	closeOnce sync.Once
}

// Compile-time guarantee.
var _ ComposeAgent = (*GRPCComposeAgent)(nil)

// NewGRPCComposeAgent wraps a generated ComposeService client.
func NewGRPCComposeAgent(client composeClient) *GRPCComposeAgent {
	return &GRPCComposeAgent{client: client}
}

// Close releases the underlying connection once.
func (a *GRPCComposeAgent) Close() error {
	var err error
	a.closeOnce.Do(func() {
		if a.client != nil {
			err = a.client.Close()
		}
	})
	return err
}

// Validate implements ComposeAgent. Services projects opt out of the node's
// application-scope confinement allowlist, which applies unless the caller
// opts out.
func (a *GRPCComposeAgent) Validate(ctx context.Context, projectName string, composeYAML []byte) ([]string, error) {
	response, err := a.client.ComposeValidate(ctx, &agentv1.ComposeValidateRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Unconfined:  true,
	})
	if err != nil {
		return nil, mapAgentError("validate", err)
	}
	return response.GetServices(), nil
}

// Up implements ComposeAgent.
func (a *GRPCComposeAgent) Up(ctx context.Context, projectName string, composeYAML []byte, restart bool) error {
	_, err := a.client.ComposeUp(ctx, &agentv1.ComposeUpRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Restart:     restart,
		Unconfined:  true,
	})
	return mapAgentError("up", err)
}

// Down implements ComposeAgent.
func (a *GRPCComposeAgent) Down(ctx context.Context, projectName string, composeYAML []byte) error {
	_, err := a.client.ComposeDown(ctx, &agentv1.ComposeDownRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Unconfined:  true,
	})
	return mapAgentError("down", err)
}

// Ps implements ComposeAgent.
func (a *GRPCComposeAgent) Ps(ctx context.Context, projectName string) ([]ComposeContainer, error) {
	response, err := a.client.ComposePs(ctx, &agentv1.ComposePsRequest{ProjectName: projectName})
	if err != nil {
		return nil, mapAgentError("ps", err)
	}
	containers := make([]ComposeContainer, 0, len(response.GetContainers()))
	for _, container := range response.GetContainers() {
		containers = append(containers, ComposeContainer{
			Service:     container.GetService(),
			ContainerID: container.GetContainerId(),
			Name:        container.GetName(),
			Image:       container.GetImage(),
			State:       container.GetState(),
			Status:      container.GetStatus(),
			Health:      container.GetHealth(),
			Ports:       container.GetPorts(),
		})
	}
	return containers, nil
}

// Logs implements ComposeAgent. The first receive happens synchronously and
// must be the agent's acceptance frame: a node that refuses the request
// (unavailable, missing project, unknown compose service) fails the opening
// call with its own code, while an accepted stream — even one that stays quiet
// for a long time — opens immediately. Older agents that send data first are
// still supported: the first frame is treated as output.
func (a *GRPCComposeAgent) Logs(ctx context.Context, projectName, service string, tail int64, follow bool) (LogStream, error) {
	stream, err := a.client.ComposeLogs(ctx, &agentv1.ComposeLogsRequest{
		ProjectName: projectName,
		Service:     service,
		Tail:        tail,
		Follow:      follow,
	})
	if err != nil {
		_ = a.Close()
		return nil, mapAgentError("logs", err)
	}
	first, err := stream.Recv()
	if err != nil {
		_ = a.Close()
		if errors.Is(err, io.EOF) {
			closed := &grpcLogStream{chunks: make(chan []byte)}
			close(closed.chunks)
			return closed, nil
		}
		return nil, mapAgentError("logs", err)
	}
	firstChunk := first.GetData()
	if first.GetReady() {
		// Acceptance only: no output exists yet.
		firstChunk = nil
	}
	streamCtx, cancel := context.WithCancel(ctx)
	logStream := &grpcLogStream{
		agent:      a,
		chunks:     make(chan []byte, 64),
		firstChunk: firstChunk,
		ctx:        streamCtx,
		cancel:     cancel,
	}
	go logStream.drain(stream)
	return logStream, nil
}

// grpcLogStream delivers one agent log stream. drain owns the receive loop and
// the agent's connection; Err is safe to read after Chunks is closed (the
// channel close happens after the error is stored).
//
// The stream owns a cancellation context so Close can unblock both the receive
// and the channel-send paths: a caller that stops reading and closes the
// stream must not leave the drain goroutine retained on a full chunk channel.
type grpcLogStream struct {
	agent      *GRPCComposeAgent
	chunks     chan []byte
	firstChunk []byte
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	err        error
}

// Compile-time guarantee.
var _ LogStream = (*grpcLogStream)(nil)

// Chunks implements LogStream.
func (s *grpcLogStream) Chunks() <-chan []byte { return s.chunks }

// Err implements LogStream.
func (s *grpcLogStream) Err() error { return s.err }

// Close implements LogStream. It cancels the stream (unblocking the drain) and
// releases the connection; it is safe to call more than once.
func (s *grpcLogStream) Close() error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
	if s.agent == nil {
		return nil
	}
	return s.agent.Close()
}

// drain receives chunks until the stream ends, then closes the channel and the
// connection. A terminal failure is stored unless the stream ended because the
// caller canceled it or closed it (neither needs an error).
func (s *grpcLogStream) drain(stream grpc.ServerStreamingClient[agentv1.ComposeLogChunk]) {
	defer func() {
		close(s.chunks)
		_ = s.Close()
	}()
	send := func(chunk []byte) bool {
		select {
		case s.chunks <- chunk:
			return true
		case <-s.ctx.Done():
			return false
		}
	}
	if len(s.firstChunk) > 0 && !send(s.firstChunk) {
		return
	}
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) && s.ctx.Err() == nil {
				s.err = mapAgentError("logs", err)
			}
			return
		}
		if chunk.GetReady() {
			// A duplicate acceptance frame carries no output.
			continue
		}
		if !send(chunk.GetData()) {
			return
		}
	}
}

// mapAgentError translates a gRPC status into this package's sentinels so the
// routes layer maps it to an HTTP status. A dial or transport error (no gRPC
// status) is always agent-unavailable.
func mapAgentError(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s: %v", ErrAgentUnavailable, action, err)
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%w: %s: %v", ErrAgentUnavailable, action, err)
	}
	switch grpcStatus.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition:
		return fmt.Errorf("%w: %s: %s", ErrValidation, action, grpcStatus.Message())
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %s: %s", ErrAgentUnavailable, action, grpcStatus.Message())
	default:
		return fmt.Errorf("%w: %s: %s", ErrDeployFailed, action, grpcStatus.Message())
	}
}
