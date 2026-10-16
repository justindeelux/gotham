package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// defaultToolTimeout bounds one install or uninstall. The long value is
	// deliberate: a package install and a Railpack/BuildKit download can take
	// minutes on a slow node, but a wedged command still cannot hold the node's
	// one install slot forever.
	defaultToolTimeout = 15 * time.Minute
	// defaultToolHelper is the fixed, root-owned privileged helper installed by
	// deploy/install-agent.sh and reached only through its verb-pinned sudoers
	// rule. The Go side passes a catalog id; the helper owns the executable argv.
	defaultToolHelper = "/usr/libexec/gotham/gotham-toolctl"
)

// Final stream statuses, matching the proto's ToolResult.status values.
const (
	toolStatusSucceeded = "succeeded"
	toolStatusFailed    = "failed"
)

// Helper verbs, one per direction. They are the only arguments the sudoers rule
// grants, so they must match deploy/install-toolctl-sudoers.sh exactly.
const (
	helperInstallVerb   = "install-tool"
	helperUninstallVerb = "uninstall-tool"
)

// ToolServer implements agentv1.ToolServiceServer. ListTools reports the host
// tool catalog with each tool's detected state; InstallTool and UninstallTool
// run the catalog installers on this node, streaming output and a final result.
//
// Every privileged command is the fixed gotham-toolctl helper with a catalog id
// (deploy/gotham-toolctl.sh): the agent never builds a root argv from request
// data, and the helper validates the id against its allow-list.
//
// The install/uninstall surface is guarded by a node-wide lock: only one runs
// at a time, so two deploys cannot install over each other, and a second call
// is refused with ResourceExhausted rather than queued.
type ToolServer struct {
	agentv1.UnimplementedToolServiceServer

	// runner executes the helper. It is a seam for tests.
	runner commandRunner
	// helper is the privileged helper path.
	helper string
	// installed reports whether a tool is present, used for idempotency and
	// dependency checks. It is a seam for tests.
	installed func(id string) bool
	// inUse reports whether a tool is busy, so an uninstall can refuse.
	inUse func(id string) (bool, string)
	// beginUninstall marks an uninstall in flight so a build cannot start into
	// its removal window; the returned function clears the mark.
	beginUninstall func(id string) func()
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
		runner:         execRunner{},
		helper:         defaultToolHelper,
		installed:      toolcatalog.Installed,
		inUse:          func(string) (bool, string) { return false, "" },
		beginUninstall: func(string) func() { return func() {} },
		timeout:        defaultToolTimeout,
		log:            slog.Default(),
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// WithToolUse wires the node's tool-use tracker so an uninstall refuses while a
// build is running and a build cannot start while an uninstall is running.
func WithToolUse(tracker *ToolUseTracker) ToolServerOption {
	return func(s *ToolServer) {
		if tracker != nil {
			s.inUse = tracker.InUse
			s.beginUninstall = tracker.BeginUninstall
		}
	}
}

// withToolRunner substitutes the command runner (tests).
func withToolRunner(runner commandRunner) ToolServerOption {
	return func(s *ToolServer) { s.runner = runner }
}

// withToolHelper substitutes the privileged helper path (tests).
func withToolHelper(path string) ToolServerOption {
	return func(s *ToolServer) { s.helper = path }
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

	ids, err := toolOrder(id, install)
	if err != nil {
		return toolPlanError(id, err)
	}

	if !install {
		// Mark the uninstall before the in-use check so a build cannot start
		// in the gap between reading "not in use" and pulling the tool out.
		release := s.beginUninstall(id)
		defer release()
		if reason := s.uninstallRefusal(id); reason != "" {
			return status.Errorf(codes.FailedPrecondition, "cannot uninstall %q: %s", id, reason)
		}
	}

	ctx, cancel := context.WithTimeout(stream.Context(), s.timeout)
	defer cancel()

	sender := &toolSender{stream: stream}
	verb := "installing"
	helperVerb := helperInstallVerb
	if !install {
		verb = "uninstalling"
		helperVerb = helperUninstallVerb
	}
	for _, toolID := range ids {
		switch present := s.installed(toolID); {
		case install && present:
			sender.note(toolID + " is already installed")
			continue
		case !install && !present:
			sender.note(toolID + " is not installed")
			continue
		}
		sender.note(fmt.Sprintf("%s %s", verb, toolID))
		code, runErr := s.runner.Run(ctx, []string{s.helper, helperVerb, toolID}, true, sender.emit)
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

// uninstallRefusal returns why a tool must not be removed, or "" when it is
// safe: nothing is using it and no installed tool depends on it. A tool a
// present tool depends on is refused so removing it never breaks a working
// build path (railpack needs buildkit, which needs docker).
func (s *ToolServer) uninstallRefusal(id string) string {
	if inUse, reason := s.inUse(id); inUse {
		if reason == "" {
			reason = "it is in use"
		}
		return reason
	}
	for _, dependent := range toolcatalog.Dependents(id) {
		if s.installed(dependent) {
			return fmt.Sprintf("%s is installed and depends on it", dependent)
		}
	}
	return ""
}

// toolOrder returns the ids to act on: dependency-first for an install, the
// single id for an uninstall.
func toolOrder(id string, install bool) ([]string, error) {
	if install {
		return toolcatalog.InstallOrder(id)
	}
	return toolcatalog.UninstallOrder(id)
}

// toolPlanError maps a catalog error onto a gRPC status: an unknown id is bad
// input, a refusal the node will never satisfy is a failed precondition, and
// anything else is internal.
func toolPlanError(id string, err error) error {
	switch {
	case errors.Is(err, toolcatalog.ErrUnknownTool):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, toolcatalog.ErrNotInstallable),
		errors.Is(err, toolcatalog.ErrNotUninstallable):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	default:
		return status.Errorf(codes.Internal, "plan %q: %v", id, err)
	}
}
