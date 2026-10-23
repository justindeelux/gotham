package tools

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
	defaultToolTimeout = 15 * time.Minute
	defaultToolHelper  = "/usr/libexec/gotham/gotham-toolctl"
)

const (
	toolStatusSucceeded = "succeeded"
	toolStatusFailed    = "failed"
)

const (
	helperInstallVerb   = "install-tool"
	helperUninstallVerb = "uninstall-tool"
)

type ToolServer struct {
	agentv1.UnimplementedToolServiceServer

	runner         commandRunner
	helper         string
	installed      func(id string) bool
	inUse          func(id string) (bool, string)
	beginUninstall func(id string) func()
	timeout        time.Duration
	log            *slog.Logger

	lock sync.Mutex
}

type ToolServerOption func(*ToolServer)

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

func WithToolUse(tracker *ToolUseTracker) ToolServerOption {
	return func(s *ToolServer) {
		if tracker != nil {
			s.inUse = tracker.InUse
			s.beginUninstall = tracker.BeginUninstall
		}
	}
}

func withToolRunner(runner commandRunner) ToolServerOption {
	return func(s *ToolServer) { s.runner = runner }
}

func withToolHelper(path string) ToolServerOption {
	return func(s *ToolServer) { s.helper = path }
}

func withToolInstalled(fn func(string) bool) ToolServerOption {
	return func(s *ToolServer) { s.installed = fn }
}

func withToolInUse(fn func(string) (bool, string)) ToolServerOption {
	return func(s *ToolServer) { s.inUse = fn }
}

func withToolTimeout(timeout time.Duration) ToolServerOption {
	return func(s *ToolServer) { s.timeout = timeout }
}

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

func (s *ToolServer) InstallTool(request *agentv1.InstallToolRequest, stream grpc.ServerStreamingServer[agentv1.ToolEvent]) error {
	return s.execute(request.GetId(), true, stream)
}

func (s *ToolServer) UninstallTool(request *agentv1.InstallToolRequest, stream grpc.ServerStreamingServer[agentv1.ToolEvent]) error {
	return s.execute(request.GetId(), false, stream)
}

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
			tool, _ := toolcatalog.Lookup(toolID)
			if tool.MinVersion == "" || toolcatalog.DetectTool(tool).VersionOK {
				sender.note(toolID + " is already installed")
				continue
			}
		case !install && !present:
			sender.note(toolID + " is not installed")
			continue
		}
		sender.note(fmt.Sprintf("%s %s", verb, toolID))
		code, runErr := s.runner.Run(ctx, []string{s.helper, helperVerb, toolID}, true, sender.emit)
		if runErr == nil && code == 0 {
			tool, _ := toolcatalog.Lookup(toolID)
			if install && tool.MinVersion != "" {
				detected := toolcatalog.DetectTool(tool)
				if !detected.Installed || !detected.VersionOK {
					version := detected.Version
					if version == "" {
						version = "unknown"
					}
					sender.note(fmt.Sprintf("%s %s detected after install/update; >= %s required — the distro repository may not provide a newer version; add a newer repository / PPA", toolID, version, tool.MinVersion))
					return sender.result(toolStatusFailed, -1)
				}
			}
			continue
		}
		if runErr != nil {
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
		}
		return sender.result(toolStatusFailed, code)
	}

	s.log.Info("tool "+verb+" finished", slog.String("tool", id))
	return sender.result(toolStatusSucceeded, 0)
}

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

func toolOrder(id string, install bool) ([]string, error) {
	if install {
		return toolcatalog.InstallOrder(id)
	}
	return toolcatalog.UninstallOrder(id)
}

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
