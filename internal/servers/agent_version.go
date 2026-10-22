package servers

import "time"

// AgentVersionForNode returns only the requested heartbeat entry. The caller
// must authorize the stored node before looking up its version.
func (s *ServerService) AgentVersionForNode(nodeID string) (AgentVersion, bool) {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	entry, ok := s.agentUpdate.agents[nodeID]
	if !ok || time.Since(entry.At) > agentVersionTTL {
		return AgentVersion{}, false
	}
	return entry, true
}
