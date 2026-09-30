package servers

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/justindeelux/gotham/updatecore"
)

// rolloutTTL bounds how long an operator-triggered "update all agents" rollout
// stays active. Agents poll, so a rollout is a pull signal, not a push; after
// the TTL it lapses so a node that rejoins much later is not force-updated by a
// stale trigger.
const rolloutTTL = time.Hour

// Version-map bounds. The map is populated only from validated heartbeats (an
// unknown node is refused before the write), so these guard against a
// compromised but registered node reporting a huge or churning version.
const (
	// agentVersionTTL drops an entry not refreshed within the window; agents
	// re-report every heartbeat, so this only removes dead nodes.
	agentVersionTTL = 24 * time.Hour
	// maxAgentVersionEntries caps the map size.
	maxAgentVersionEntries = 10000
	// maxAgentVersionLength bounds a reported version string.
	maxAgentVersionLength = 64
)

// AgentUpdateOfferer resolves a verified, signed agent update offer for a
// reporting node and reports the newest agent release version. It is
// implemented by internal/updates.AgentUpdater; the interface lives here so the
// gRPC gateway does not depend on the HTTP/code surface.
type AgentUpdateOfferer interface {
	Offer(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, error)
	// TargetVersion returns the newest agent release version, or "" when the
	// updater is disabled.
	TargetVersion(ctx context.Context) (string, error)
}

// AgentVersion is the last agent version one node reported from a heartbeat.
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

// RecordAgentVersion records the version a validated node reported. It is
// called only from the heartbeat path, which already resolved the node in the
// registry, so an unknown node can never be inserted here. The version is
// sanitized and length-bounded, expired entries are evicted, and the map is
// capped.
func (s *ServerService) RecordAgentVersion(nodeID, version string) {
	nodeID = strings.TrimSpace(nodeID)
	version = sanitizeAgentVersion(version)
	if nodeID == "" || version == "" {
		return
	}
	now := time.Now().UTC()
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.evictExpiredLocked(now)
	if _, exists := s.agentUpdate.agents[nodeID]; !exists && len(s.agentUpdate.agents) >= maxAgentVersionEntries {
		s.evictOldestLocked()
	}
	s.agentUpdate.agents[nodeID] = AgentVersion{NodeID: nodeID, Version: version, At: now}
}

// KnownAgentVersions returns the live version map, sorted by node id.
func (s *ServerService) KnownAgentVersions() []AgentVersion {
	now := time.Now().UTC()
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.evictExpiredLocked(now)
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

// AgentUpdateTarget returns the newest agent release version from the agent
// release family, or "" when the updater is disabled. It is deliberately
// independent of the control plane's own version: the CP is normally updated
// first, and "update all agents" must still roll out when the CP is current.
func (s *ServerService) AgentUpdateTarget(ctx context.Context) (string, error) {
	if s.updater == nil {
		return "", nil
	}
	return s.updater.TargetVersion(ctx)
}

// OfferAgentUpdate resolves an offer for the reporting node and reports whether
// an active rollout covers it. A rollout forces an update for any offered
// release at or above the rollout target, so a fleet that is partway through a
// rollout converges even if a newer release was published within the TTL.
func (s *ServerService) OfferAgentUpdate(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, bool, error) {
	if s.updater == nil {
		return nil, false, nil
	}
	release, err := s.updater.Offer(ctx, agentVersion, goos, goarch)
	if err != nil || release == nil {
		return release, false, err
	}
	rollout := s.AgentRolloutVersion()
	return release, rolloutForces(rollout, release.Version), nil
}

// rolloutForces reports whether an active rollout target covers an offered
// version (offered >= target). Either version failing to parse fails closed
// (no forced update).
func rolloutForces(target, offered string) bool {
	if target == "" {
		return false
	}
	targetVersion, err := updatecore.ParseVersion(target)
	if err != nil {
		return false
	}
	offeredVersion, err := updatecore.ParseVersion(offered)
	if err != nil {
		return false
	}
	return offeredVersion.Compare(targetVersion) >= 0
}

// evictExpiredLocked drops entries not refreshed within agentVersionTTL.
func (s *ServerService) evictExpiredLocked(now time.Time) {
	for nodeID, entry := range s.agentUpdate.agents {
		if now.Sub(entry.At) > agentVersionTTL {
			delete(s.agentUpdate.agents, nodeID)
		}
	}
}

// evictOldestLocked drops the least recently reported entry.
func (s *ServerService) evictOldestLocked() {
	var (
		oldestID string
		oldest   time.Time
	)
	for nodeID, entry := range s.agentUpdate.agents {
		if oldestID == "" || entry.At.Before(oldest) {
			oldestID, oldest = nodeID, entry.At
		}
	}
	if oldestID != "" {
		delete(s.agentUpdate.agents, oldestID)
	}
}

// sanitizeAgentVersion keeps a version on one line, strips control characters
// and bounds its length. An empty result is refused by the caller.
func sanitizeAgentVersion(version string) string {
	version = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, version)
	version = strings.TrimSpace(version)
	if len(version) > maxAgentVersionLength {
		version = version[:maxAgentVersionLength]
	}
	return version
}
