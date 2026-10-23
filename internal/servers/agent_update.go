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

const rolloutTTL = time.Hour

const (
	agentVersionTTL        = 24 * time.Hour
	maxAgentVersionEntries = 10000
	maxAgentVersionLength  = 64
)

type AgentUpdateOfferer interface {
	Offer(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, error)
	TargetVersion(ctx context.Context) (string, error)
}

type AgentVersion struct {
	NodeID  string    `json:"node_id"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

type agentUpdateState struct {
	mu        sync.Mutex
	agents    map[string]AgentVersion
	rollout   string
	rolloutAt time.Time
}

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

func (s *ServerService) StartAgentRollout(version string) {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.agentUpdate.rollout = strings.TrimSpace(version)
	s.agentUpdate.rolloutAt = time.Now()
}

func (s *ServerService) AgentRolloutVersion() string {
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	if s.agentUpdate.rollout == "" || time.Since(s.agentUpdate.rolloutAt) > rolloutTTL {
		return ""
	}
	return s.agentUpdate.rollout
}

func (s *ServerService) AgentUpdateTarget(ctx context.Context) (string, error) {
	if s.updater == nil {
		return "", nil
	}
	return s.updater.TargetVersion(ctx)
}

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

func (s *ServerService) evictExpiredLocked(now time.Time) {
	for nodeID, entry := range s.agentUpdate.agents {
		if now.Sub(entry.At) > agentVersionTTL {
			delete(s.agentUpdate.agents, nodeID)
		}
	}
}

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
