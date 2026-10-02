package deploy

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestMapRPCError guards the agent-error taxonomy on the deploy client,
// including the NotFound mapping that routes a missing Docker resource to a
// 404 instead of a 500.
func TestMapRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"unavailable", status.Error(codes.Unavailable, "down"), ErrAgentUnavailable},
		{"deadline", status.Error(codes.DeadlineExceeded, "slow"), ErrAgentUnavailable},
		{"canceled", status.Error(codes.Canceled, "gone"), ErrAgentUnavailable},
		{"invalid argument", status.Error(codes.InvalidArgument, "bad"), ErrValidation},
		{"not found", status.Error(codes.NotFound, "missing"), ErrNotFound},
		{"internal passthrough", status.Error(codes.Internal, "boom"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapRPCError(tt.err)
			if tt.want == nil {
				if err == nil || errors.Is(err, ErrAgentUnavailable) ||
					errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
					t.Fatalf("mapRPCError(%v) = %v; want passthrough", tt.err, err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("mapRPCError(%v) = %v; want %v", tt.err, err, tt.want)
			}
		})
	}
	if mapRPCError(nil) != nil {
		t.Error("mapRPCError(nil) != nil")
	}
}
