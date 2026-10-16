package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// defaultToolTimeout bounds one install or uninstall. The long value is
// deliberate: a package install and a Railpack/BuildKit download can take
// minutes on a slow node, but a wedged command still cannot hold the node's
// one install slot forever.
const defaultToolTimeout = 15 * time.Minute

// Final stream statuses, matching the proto's ToolResult.status values.
const (
	toolStatusSucceeded = "succeeded"
	toolStatusFailed    = "failed"
)

// ToolServer implements agentv1.ToolServiceServer. ListTools reports the host
// tool catalog with each tool's detected state; InstallTool and UninstallTool
// run the catalog installers on this node, streaming output and a final result.
//
// The install/uninstall surface is guarded by a node-wide lock: only one runs
// at a time, so two deploys cannot install over each other, and a second call
// is refused with ResourceExhausted rather than queued.
type ToolServer struct {
	agentv1.UnimplementedToolServiceServer

	// runner executes one install step. It is a seam for tests.
	runner commandRunner
	// env is the host install environment (arch, distro, directories).
	env toolcatalog.Env
	// installed reports whether a tool is present, used for idempotency and
	// dependency checks. It is a seam for tests.
	installed func(id string) bool
	// inUse reports whether a tool is busy, so an uninstall can refuse.
	inUse func(id string) (bool, string)
	// timeout bounds one install or uninstall.
	timeout time.Duration
	log     *slog.Logger

	// lock serialises installs and uninstalls on this node.
	lock sync.Mutex
}

// ToolServerOption tunes a ToolServer.
type ToolServerOption func(*ToolServer)

// NewToolServer returns a ToolService implementation over the host catalog.
func NewToolServer(options ...ToolServerOption) *ToolServer {
	server := &ToolServer{
		runner:    execRunner{},
		env:       toolcatalog.DefaultEnv(),
		installed: toolcatalog.Installed,
		inUse:     func(string) (bool, string) { return false, "" },
		timeout:   defaultToolTimeout,
		log:       slog.Default(),
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// WithToolUse wires the node's tool-use tracker so an uninstall refuses while a
// build is running.
func WithToolUse(tracker *ToolUseTracker) ToolServerOption {
	return func(s *ToolServer) {
		if tracker != nil {
			s.inUse = tracker.InUse
		}
	}
}

// withToolRunner substitutes the command runner (tests).
func withToolRunner(runner commandRunner) ToolServerOption {
	return func(s *ToolServer) { s.runner = runner }
}

// withToolEnv substitutes the install environment (tests).
func withToolEnv(env toolcatalog.Env) ToolServerOption {
	return func(s *ToolServer) { s.env = env }
}

// withToolInstalled substitutes the presence check (tests).
func withToolInstalled(fn func(string) bool) ToolServerOption {
	return func(s *ToolServer) { s.installed = fn }
}

// withToolInUse substitutes the in-use check (tests).
func withToolInUse(fn func(string) (bool, string)) ToolServerOption {
	return func(s *ToolServer) { s.inUse = fn }
}

// withToolTimeout substitutes the install timeout (tests).
func withToolTimeout(timeout time.Duration) ToolServerOption {
	return func(s *ToolServer) { s.timeout = timeout }
}

// ListTools returns every catalog entry with its detected state on this node.
func (s *ToolServer) ListTools(_ context.Context, _ *agentv1.ListToolsRequest) (*agentv1.ListToolsResponse, error) {
	detections := toolcatalog.DetectAll()
	tools := make([]*agentv1.ToolInfo, 0, len(detections))
	for _, detection := range detections {
		tool, ok := toolcatalog.Lookup(detection.ID)
		if !ok {
			continue
		}
		tools = append(tools, &agentv1.ToolInfo{
			Id:            tool.ID,
			Name:          tool.Name,
			Description:   tool.Description,
			Category:      string(tool.Category),
			Installed:     detection.Installed,
			Version:       detection.Version,
			Path:          detection.Path,
			Installable:   tool.Installable,
			Uninstallable: tool.Uninstallable,
			NeedsRoot:     tool.NeedsRoot,
			Deps:          tool.Deps,
			MinVersion:    tool.MinVersion,
			VersionOk:     detection.VersionOK,
		})
	}
	return &agentv1.ListToolsResponse{Tools: tools}, nil
}

// InstallTool installs one catalog tool and its missing dependencies, streaming
// each command's output and a final result.
func (s *ToolServer) InstallTool(request *agentv1.InstallToolRequest, stream grpc.ServerStreamingServer[agentv1.ToolEvent]) error {
	return s.execute(request.GetId(), true, stream)
}

// UninstallTool removes one catalog tool the same way.
func (s *ToolServer) UninstallTool(request *agentv1.InstallToolRequest, stream grpc.ServerStreamingServer[agentv1.ToolEvent]) error {
	return s.execute(request.GetId(), false, stream)
}

// execute runs one install or uninstall. A request the node will never satisfy
// (unknown id, a detect-only tool, a tool in use, another install running) is
// refused with a gRPC status error before any command runs, so the control
// plane can tell a refusal from a failed install. A command that ran and failed
// is reported in-band as a ToolResult with status "failed", so the stream still
// carries the actionable exit code.
func (s *ToolServer) execute(id string, install bool, stream grpc.ServerStreamingServer[agentv1.ToolEvent]) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return status.Error(codes.InvalidArgument, "tool id is required")
	}
	if !s.lock.TryLock() {
		return status.Error(codes.ResourceExhausted, "another tool install or uninstall is already running on this node")
	}
	defer s.lock.Unlock()

	tool, ok := toolcatalog.Lookup(id)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "%v: %q", toolcatalog.ErrUnknownTool, id)
	}
	if install && !tool.Installable {
		return status.Errorf(codes.FailedPrecondition, "tool %q cannot be installed on a node", id)
	}
	if !install {
		if !tool.Uninstallable {
			return status.Errorf(codes.FailedPrecondition, "tool %q cannot be uninstalled", id)
		}
		if reason := s.uninstallRefusal(tool); reason != "" {
			return status.Errorf(codes.FailedPrecondition, "cannot uninstall %q: %s", id, reason)
		}
	}

	items, err := plan(id, install, s.env)
	if err != nil {
		return toolPlanError(id, err)
	}

	ctx, cancel := context.WithTimeout(stream.Context(), s.timeout)
	defer cancel()

	sender := &toolSender{stream: stream}
	verb := "installing"
	if !install {
		verb = "uninstalling"
	}
	for _, item := range items {
		switch present := s.installed(item.Tool.ID); {
		case install && present:
			sender.note(item.Tool.ID + " is already installed")
			continue
		case !install && !present:
			sender.note(item.Tool.ID + " is not installed")
			continue
		}
		sender.note(fmt.Sprintf("%s %s", verb, item.Tool.ID))
		code, runErr := s.runSteps(ctx, item, sender.emit)
		if runErr == nil && code == 0 {
			continue
		}
		if runErr != nil {
			// A cancelled client is a cancelled RPC, not a failed install:
			// report the transport error instead of a result nobody reads.
			if parentErr := stream.Context().Err(); parentErr != nil {
				return status.FromContextError(parentErr).Err()
			}
			if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				sender.note(fmt.Sprintf("timed out after %s", s.timeout))
				return sender.result(toolStatusFailed, -1)
			}
			if code <= 0 {
				sender.note("could not run: " + runErr.Error())
				return sender.result(toolStatusFailed, -1)
			}
			// A non-zero exit is a normal failure: fall through and report it
			// with its own exit code.
		}
		return sender.result(toolStatusFailed, code)
	}

	s.log.Info("tool "+verb+" finished", slog.String("tool", id))
	return sender.result(toolStatusSucceeded, 0)
}

// runSteps runs every step of one plan item, stopping at the first non-zero
// exit. It returns that step's exit code alongside the command error.
func (s *ToolServer) runSteps(ctx context.Context, item toolcatalog.PlanItem, emit func(stream, line string) error) (int, error) {
	for _, step := range item.Steps {
		code, err := s.runner.Run(ctx, step, emit)
		if err != nil || code != 0 {
			return code, err
		}
	}
	return 0, nil
}

// uninstallRefusal returns why a tool must not be removed, or "" when it is
// safe: nothing is using it and no installed tool depends on it. A tool a
// present tool depends on is refused so removing it never breaks a working
// build path (railpack needs buildkit, which needs docker).
func (s *ToolServer) uninstallRefusal(tool toolcatalog.Tool) string {
	if inUse, reason := s.inUse(tool.ID); inUse {
		if reason == "" {
			reason = "it is in use"
		}
		return reason
	}
	for _, other := range toolcatalog.List() {
		if other.ID == tool.ID || !s.installed(other.ID) {
			continue
		}
		if slices.Contains(other.Deps, tool.ID) {
			return fmt.Sprintf("%s is installed and depends on it", other.ID)
		}
	}
	return ""
}

// plan builds the install or uninstall plan for id.
func plan(id string, install bool, env toolcatalog.Env) ([]toolcatalog.PlanItem, error) {
	if install {
		return toolcatalog.InstallPlan(id, env)
	}
	return toolcatalog.UninstallPlan(id, env)
}

// toolPlanError maps a plan error onto a gRPC status: an unknown id is bad
// input, a refusal the node will never satisfy is a failed precondition, and
// anything else is internal.
func toolPlanError(id string, err error) error {
	switch {
	case errors.Is(err, toolcatalog.ErrUnknownTool):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, toolcatalog.ErrNotInstallable),
		errors.Is(err, toolcatalog.ErrNotUninstallable),
		errors.Is(err, toolcatalog.ErrNoInstaller):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	default:
		return status.Errorf(codes.Internal, "plan %q: %v", id, err)
	}
}
