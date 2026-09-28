package services

import (
	"context"
	"errors"
	"io"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeComposeClient is a generated ComposeService client whose log stream is
// scripted. The other RPCs come from the embedded nil interface; the tests
// only exercise Logs.
type fakeComposeClient struct {
	agentv1.ComposeServiceClient
	stream    grpc.ServerStreamingClient[agentv1.ComposeLogChunk]
	streamErr error
	closed    int
}

// ComposeLogs implements agentv1.ComposeServiceClient.
func (c *fakeComposeClient) ComposeLogs(context.Context, *agentv1.ComposeLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ComposeLogChunk], error) {
	if c.streamErr != nil {
		return nil, c.streamErr
	}
	return c.stream, nil
}

// Close implements the connection lifecycle.
func (c *fakeComposeClient) Close() error {
	c.closed++
	return nil
}

// scriptedComposeStream returns the scripted receive results.
type scriptedComposeStream struct {
	grpc.ClientStream
	chunks [][]byte
	errs   []error
	index  int
}

// Recv returns the next scripted chunk or error.
func (s *scriptedComposeStream) Recv() (*agentv1.ComposeLogChunk, error) {
	index := s.index
	s.index++
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
	return nil, io.EOF
}

// TestGRPCComposeAgentFirstReadFailure proves a stream the node refuses fails
// the opening call with its own code (so the HTTP layer can still answer a
// correct status) instead of becoming a successful empty stream, and the
// connection is closed.
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

	// A dial-level failure is agent-unavailable too, and closes the client.
	client = &fakeComposeClient{streamErr: errors.New("dial failed")}
	agent = NewGRPCComposeAgent(client)
	if _, err := agent.Logs(context.Background(), "gotham-x", "", 10, false); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("dial error = %v, want ErrAgentUnavailable", err)
	}
	if client.closed != 1 {
		t.Fatalf("client closed %d times, want 1", client.closed)
	}
}

// TestGRPCComposeAgentStreamLifecycle proves an empty stream ends cleanly, a
// mid-stream failure is reported by Err after the chunks drain, and the
// connection closes exactly once in each case.
func TestGRPCComposeAgentStreamLifecycle(t *testing.T) {
	// Empty stream: the node accepted it and had nothing to send.
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

	// Mid-stream failure: chunks are delivered, then Err reports the status.
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

	// A client cancellation ends the stream without an error.
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
