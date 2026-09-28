package services

import (
	"context"
	"errors"
	"fmt"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ComposeAgent is the node agent's ComposeService as the control plane uses
// it, with the agent's protobuf types translated at the seam. The production
// implementation is *GRPCComposeAgent; tests substitute a fake.
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
	// Logs streams the project's (or one service's) merged logs until the
	// context is canceled or the stream ends.
	Logs(ctx context.Context, projectName, service string, tail int64, follow bool) (<-chan []byte, error)
}

// GRPCComposeAgent adapts the generated ComposeService client to ComposeAgent.
// It is built by the HTTP wiring from the servers package's dialed client, so
// this package never dials an agent itself.
type GRPCComposeAgent struct {
	client agentv1.ComposeServiceClient
}

// Compile-time guarantee.
var _ ComposeAgent = (*GRPCComposeAgent)(nil)

// NewGRPCComposeAgent wraps a generated ComposeService client.
func NewGRPCComposeAgent(client agentv1.ComposeServiceClient) *GRPCComposeAgent {
	return &GRPCComposeAgent{client: client}
}

// Validate implements ComposeAgent.
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

// Up implements ComposeAgent.
func (a *GRPCComposeAgent) Up(ctx context.Context, projectName string, composeYAML []byte, restart bool) error {
	_, err := a.client.ComposeUp(ctx, &agentv1.ComposeUpRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Restart:     restart,
	})
	return mapAgentError("up", err)
}

// Down implements ComposeAgent.
func (a *GRPCComposeAgent) Down(ctx context.Context, projectName string, composeYAML []byte) error {
	_, err := a.client.ComposeDown(ctx, &agentv1.ComposeDownRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
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

// Logs implements ComposeAgent by draining the server stream into a channel.
func (a *GRPCComposeAgent) Logs(ctx context.Context, projectName, service string, tail int64, follow bool) (<-chan []byte, error) {
	stream, err := a.client.ComposeLogs(ctx, &agentv1.ComposeLogsRequest{
		ProjectName: projectName,
		Service:     service,
		Tail:        tail,
		Follow:      follow,
	})
	if err != nil {
		return nil, mapAgentError("logs", err)
	}
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		for {
			chunk, err := stream.Recv()
			if err != nil {
				return
			}
			select {
			case chunks <- chunk.GetData():
			case <-ctx.Done():
				return
			}
		}
	}()
	return chunks, nil
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
