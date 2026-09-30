package servers

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// rolloutTTL bounds how long an operator-triggered "update all agents" rollout
// stays active. Agents poll, so a rollout is a pull signal, not a push; after
// the TTL it lapses so a node that rejoins much later is not force-updated by a
// stale trigger.
const rolloutTTL = time.Hour

// AgentUpdateOfferer resolves a verified, signed agent update offer for a
// reporting node. It is implemented by internal/updates.AgentUpdater; the
// interface lives here so the gRPC gateway does not depend on the HTTP/code
// surface.
type AgentUpdateOfferer interface {
	Offer(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, error)
}

// AgentVersion is the last agent version one node reported, from a heartbeat or
// an update request.
type AgentVersion struct {
	NodeID  string    `json:"node_id"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// agentUpdateState is the control-plane-side agent version map and rollout
// marker. It is in-memory on purpose: agents re-report every heartbeat (~10s),
// so the map self-heals after a control-plane restart and needs no migration.
type agentUpdateState struct {
	mu        sync.Mutex
	agents    map[string]AgentVersion
	rollout   string
	rolloutAt time.Time
}

// RecordAgentVersion records the version a node reported.
func (s *ServerService) RecordAgentVersion(nodeID, version string) {
	nodeID = strings.TrimSpace(nodeID)
	version = strings.TrimSpace(version)
	if nodeID == "" || version == "" {
		return
	}
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.agentUpdate.agents[nodeID] = AgentVersion{NodeID: nodeID, Version: version, At: time.Now().UTC()}
}

// KnownAgentVersions returns the version map, sorted by node id.
func (s *ServerService) KnownAgentVersions() []AgentVersion {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	out := make([]AgentVersion, 0, len(s.agentUpdate.agents))
	for _, entry := range s.agentUpdate.agents {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// StartAgentRollout marks version as the target of an operator-triggered
// "update all agents" rollout. It does not contact any agent: agents pick the
// rollout up on their next RequestUpdate poll. An empty version clears it.
func (s *ServerService) StartAgentRollout(version string) {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.agentUpdate.rollout = strings.TrimSpace(version)
	s.agentUpdate.rolloutAt = time.Now()
}

// AgentRolloutVersion returns the active rollout target, or "".
func (s *ServerService) AgentRolloutVersion() string {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	if s.agentUpdate.rollout == "" || time.Since(s.agentUpdate.rolloutAt) > rolloutTTL {
		return ""
	}
	return s.agentUpdate.rollout
}

// OfferAgentUpdate resolves an offer for the reporting node and reports whether
// an active rollout covers it.
func (s *ServerService) OfferAgentUpdate(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, bool, error) {
	if s.updater == nil {
		return nil, false, nil
	}
	release, err := s.updater.Offer(ctx, agentVersion, goos, goarch)
	if err != nil || release == nil {
		return release, false, err
	}
	rollout := s.AgentRolloutVersion()
	return release, rollout != "" && rollout == release.Version, nil
}
