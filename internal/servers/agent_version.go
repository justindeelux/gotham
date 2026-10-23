package servers

import "time"

func (s *ServerService) AgentVersionForNode(nodeID string) (AgentVersion, bool) {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	entry, ok := s.agentUpdate.agents[nodeID]
	if !ok || time.Since(entry.At) > agentVersionTTL {
		return AgentVersion{}, false
	}
	return entry, true
}
