package services

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeComposeClient struct {
	agentv1.ComposeServiceClient
	stream    grpc.ServerStreamingClient[agentv1.ComposeLogChunk]
	streamErr error
	closed    int
}

func (c *fakeComposeClient) ComposeLogs(context.Context, *agentv1.ComposeLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ComposeLogChunk], error) {
	if c.streamErr != nil {
		return nil, c.streamErr
	}
	return c.stream, nil
}

func (c *fakeComposeClient) Close() error {
	c.closed++
	return nil
}

type scriptedComposeStream struct {
	grpc.ClientStream
	chunks [][]byte
	ready  bool
	errs   []error
	block  <-chan struct{}
	index  int
}

func (s *scriptedComposeStream) Recv() (*agentv1.ComposeLogChunk, error) {
	index := s.index
	s.index++
	if index == 0 && s.ready {
		return &agentv1.ComposeLogChunk{Ready: true}, nil
	}
	if s.ready {
		index--
	}
	if index < len(s.chunks) {
		return &agentv1.ComposeLogChunk{Data: s.chunks[index]}, nil
	}
	if index-len(s.chunks) < len(s.errs) {
		err := s.errs[index-len(s.chunks)]
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	if s.block != nil {
		<-s.block
	}
	return nil, io.EOF
}

func TestGRPCComposeAgentQuietFollowOpensOnAcceptance(t *testing.T) {
	block := make(chan struct{})
	client := &fakeComposeClient{stream: &scriptedComposeStream{ready: true, block: block}}
	agent := NewGRPCComposeAgent(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan LogStream, 1)
	openErr := make(chan error, 1)
	go func() {
		stream, err := agent.Logs(ctx, "gotham-x", "web", 10, true)
		if err != nil {
			openErr <- err
			return
		}
		opened <- stream
	}()
	select {
	case err := <-openErr:
		t.Fatalf("Logs: %v", err)
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("a quiet follow stream did not open without its first log line")
	}
	close(block)
}

func TestGRPCComposeAgentRefusalFailsBeforeAnyOutput(t *testing.T) {
	client := &fakeComposeClient{stream: &scriptedComposeStream{
		errs: []error{status.Error(codes.InvalidArgument, "no such compose service \"missing\" in this project")},
	}}
	agent := NewGRPCComposeAgent(client)
	stream, err := agent.Logs(context.Background(), "gotham-x", "missing", 10, false)
	if stream != nil {
		t.Fatalf("stream = %v, want nil for a refused command", stream)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestGRPCComposeAgentCloseUnblocksBackpressuredStream(t *testing.T) {
	chunks := make([][]byte, 200)
	for i := range chunks {
		chunks[i] = []byte("line\n")
	}
	client := &fakeComposeClient{stream: &scriptedComposeStream{ready: true, chunks: chunks}}
	agent := NewGRPCComposeAgent(client)
	stream, err := agent.Logs(context.Background(), "gotham-x", "", 0, true)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if _, ok := <-stream.Chunks(); !ok {
		t.Fatal("stream closed before any chunk")
	}
	time.Sleep(200 * time.Millisecond)
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-stream.Chunks():
			if !ok {
				if err := stream.Err(); err != nil {
					t.Fatalf("Err after a caller Close = %v, want nil", err)
				}
				return
			}
		case <-deadline:
			t.Fatal("Close did not unblock the backpressured drain")
		}
	}
}

func TestGRPCComposeAgentFirstReadFailure(t *testing.T) {
	client := &fakeComposeClient{
		stream: &scriptedComposeStream{errs: []error{status.Error(codes.Unavailable, "node down")}},
	}
	agent := NewGRPCComposeAgent(client)
	stream, err := agent.Logs(context.Background(), "gotham-x", "", 10, false)
	if stream != nil {
		t.Fatalf("stream = %v, want nil on a first-read failure", stream)
	}
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("error = %v, want ErrAgentUnavailable", err)
	}
	if client.closed != 1 {
		t.Fatalf("client closed %d times, want 1", client.closed)
	}

	client = &fakeComposeClient{streamErr: errors.New("dial failed")}
	agent = NewGRPCComposeAgent(client)
	if _, err := agent.Logs(context.Background(), "gotham-x", "", 10, false); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("dial error = %v, want ErrAgentUnavailable", err)
	}
	if client.closed != 1 {
		t.Fatalf("client closed %d times, want 1", client.closed)
	}
}

func TestGRPCComposeAgentStreamLifecycle(t *testing.T) {
	client := &fakeComposeClient{stream: &scriptedComposeStream{}}
	agent := NewGRPCComposeAgent(client)
	stream, err := agent.Logs(context.Background(), "gotham-x", "", 10, false)
	if err != nil {
		t.Fatalf("Logs(empty): %v", err)
	}
	for range stream.Chunks() {
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("empty stream Err = %v, want nil", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if client.closed < 1 {
		t.Fatalf("the empty stream did not close its client")
	}

	client = &fakeComposeClient{stream: &scriptedComposeStream{
		chunks: [][]byte{[]byte("line one\n")},
		errs:   []error{status.Error(codes.Internal, "compose died")},
	}}
	agent = NewGRPCComposeAgent(client)
	stream, err = agent.Logs(context.Background(), "gotham-x", "", 10, true)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	var received []byte
	for chunk := range stream.Chunks() {
		received = append(received, chunk...)
	}
	if string(received) != "line one\n" {
		t.Fatalf("received = %q", received)
	}
	if err := stream.Err(); !errors.Is(err, ErrDeployFailed) {
		t.Fatalf("terminal error = %v, want ErrDeployFailed", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if client.closed < 1 {
		t.Fatalf("the failed stream did not close its client")
	}

	ctx, cancel := context.WithCancel(context.Background())
	client = &fakeComposeClient{stream: &scriptedComposeStream{
		chunks: [][]byte{[]byte("line one\n")},
		errs:   []error{status.Error(codes.Canceled, "canceled")},
	}}
	agent = NewGRPCComposeAgent(client)
	stream, err = agent.Logs(ctx, "gotham-x", "", 10, true)
	if err != nil {
		t.Fatalf("Logs(cancel): %v", err)
	}
	cancel()
	for range stream.Chunks() {
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("cancelled stream Err = %v, want nil", err)
	}
}
