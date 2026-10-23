package server

import "github.com/justindeelux/gotham/internal/servers"

type nodeVersionReporter interface {
	AgentVersionForNode(nodeID string) (servers.AgentVersion, bool)
}

func (s *Server) observedServerDTO(node *servers.Server) serverDTO {
	dto := newServerDTO(node)
	if reporter, ok := s.servers.(nodeVersionReporter); ok && node.NodeID != nil {
		if entry, found := reporter.AgentVersionForNode(*node.NodeID); found {
			dto.AgentVersion = &entry.Version
			dto.AgentVersionReportedAt = &entry.At
		}
	}
	return dto
}
