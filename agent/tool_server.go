package agent

import (
	"context"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
)

// ToolServer implements agentv1.ToolServiceServer. ListTools reports the host
// tool catalog with each tool's detected state. InstallTool and UninstallTool
// are defined in the contract but not implemented yet: they are inherited from
// the embedded UnimplementedToolServiceServer and answer codes.Unimplemented
// until the installer lands, so later packages never touch the proto again.
type ToolServer struct {
	agentv1.UnimplementedToolServiceServer
}

// NewToolServer returns a ToolService implementation over the host catalog.
func NewToolServer() *ToolServer { return &ToolServer{} }

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
		})
	}
	return &agentv1.ListToolsResponse{Tools: tools}, nil
}
