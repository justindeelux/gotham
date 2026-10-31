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

type ComposeAgent interface {
	Validate(ctx context.Context, projectName string, composeYAML []byte) ([]string, error)
	Up(ctx context.Context, projectName string, composeYAML []byte, restart bool) error
	Down(ctx context.Context, projectName string, composeYAML []byte, removeVolumes bool) ([]string, error)
	Ps(ctx context.Context, projectName string) ([]ComposeContainer, error)
	Logs(ctx context.Context, projectName, service string, tail int64, follow bool) (LogStream, error)
	Close() error
}

type LogStream interface {
	Chunks() <-chan []byte
	Err() error
	Close() error
}

type composeClient interface {
	agentv1.ComposeServiceClient
	Close() error
}

type GRPCComposeAgent struct {
	client    composeClient
	closeOnce sync.Once
}

var _ ComposeAgent = (*GRPCComposeAgent)(nil)

func NewGRPCComposeAgent(client composeClient) *GRPCComposeAgent {
	return &GRPCComposeAgent{client: client}
}

func (a *GRPCComposeAgent) Close() error {
	var err error
	a.closeOnce.Do(func() {
		if a.client != nil {
			err = a.client.Close()
		}
	})
	return err
}

func (a *GRPCComposeAgent) Validate(ctx context.Context, projectName string, composeYAML []byte) ([]string, error) {
	response, err := a.client.ComposeValidate(ctx, &agentv1.ComposeValidateRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
	})
	if err != nil {
		return nil, mapAgentError("validate", err)
	}
	return response.GetServices(), nil
}

func (a *GRPCComposeAgent) Up(ctx context.Context, projectName string, composeYAML []byte, restart bool) error {
	_, err := a.client.ComposeUp(ctx, &agentv1.ComposeUpRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Restart:     restart,
	})
	return mapAgentError("up", err)
}

func (a *GRPCComposeAgent) Down(ctx context.Context, projectName string, composeYAML []byte, removeVolumes bool) ([]string, error) {
	response, err := a.client.ComposeDown(ctx, &agentv1.ComposeDownRequest{
		ProjectName:   projectName,
		ComposeYaml:   composeYAML,
		RemoveVolumes: removeVolumes,
	})
	if err != nil {
		return nil, mapAgentError("down", err)
	}
	return response.GetVolumeWarnings(), nil
}

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

type grpcLogStream struct {
	agent      *GRPCComposeAgent
	chunks     chan []byte
	firstChunk []byte
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	err        error
}

var _ LogStream = (*grpcLogStream)(nil)

func (s *grpcLogStream) Chunks() <-chan []byte { return s.chunks }

func (s *grpcLogStream) Err() error { return s.err }

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
			continue
		}
		if !send(chunk.GetData()) {
			return
		}
	}
}

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
