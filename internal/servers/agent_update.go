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
	maxAgentUpdateDetail   = 200

	// A rollout offer is refreshed on every agent poll, but the first offer is
	// the one that counts: if the node is still behind after this window, the
	// offer is reported as failed so the operator can retry it. The window must
	// cover a download, the wrapper restart and the heartbeat that reports the
	// new version.
	agentOfferStaleAfter = 10 * time.Minute
)

// Agent update states reported to the Servers page.
const (
	AgentUpdatePending  = "pending"
	AgentUpdateUpdating = "updating"
	AgentUpdateUpToDate = "up_to_date"
	AgentUpdateFailed   = "failed"
)

var failedAgentUpdateResults = map[string]bool{
	updatecore.StatusRolledBack:     true,
	updatecore.StatusRollbackFailed: true,
	updatecore.StatusNoBackup:       true,
	updatecore.StatusWrapperFailed:  true,
}

type AgentUpdateOfferer interface {
	Offer(ctx context.Context, agentVersion, goos, goarch string) (*updatecore.Release, error)
	TargetVersion(ctx context.Context) (string, error)
}

type AgentVersion struct {
	NodeID  string    `json:"node_id"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	State   string    `json:"state,omitempty"`
	Detail  string    `json:"detail,omitempty"`
}

type agentRollout struct {
	version string
	retry   bool
}

type agentOffer struct {
	version string
	at      time.Time
}

type agentFailure struct {
	version string
	detail  string
}

type agentUpdateState struct {
	mu           sync.Mutex
	agents       map[string]AgentVersion
	rollout      string
	rolloutAt    time.Time
	nodeRollouts map[string]agentRollout
	offers       map[string]agentOffer
	failures     map[string]agentFailure
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
	// The node reached the rollout target: there is nothing left to retry or
	// report for it.
	if entry, ok := s.agentUpdate.nodeRollouts[nodeID]; ok && sameAgentVersion(version, entry.version) {
		delete(s.agentUpdate.nodeRollouts, nodeID)
	}
	if entry, ok := s.agentUpdate.failures[nodeID]; ok && sameAgentVersion(version, entry.version) {
		delete(s.agentUpdate.failures, nodeID)
	}
	if entry, ok := s.agentUpdate.offers[nodeID]; ok && sameAgentVersion(version, entry.version) {
		delete(s.agentUpdate.offers, nodeID)
	}
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

// RecordAgentUpdateReport stores the wrapper outcome the agent sends on its
// update poll. Only a failed outcome is kept: it explains why a node is still
// behind. A manual retry clears it, and the agent stops reporting the attempt
// it already retried.
func (s *ServerService) RecordAgentUpdateReport(nodeID, result, detail, version string) {
	nodeID = strings.TrimSpace(nodeID)
	result = sanitizeAgentVersion(result)
	version = sanitizeAgentVersion(version)
	if nodeID == "" {
		return
	}
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	if !failedAgentUpdateResults[result] || version == "" {
		delete(s.agentUpdate.failures, nodeID)
		return
	}
	s.agentUpdate.failures[nodeID] = agentFailure{
		version: version,
		detail:  sanitizeAgentUpdateDetail(detail),
	}
}

func (s *ServerService) StartAgentRollout(version string) {
	stamp := time.Now()
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.agentUpdate.rollout = strings.TrimSpace(version)
	s.agentUpdate.rolloutAt = stamp
	// A manual rollout is also the operator's decision to retry: old failure
	// evidence must not describe the new attempt.
	for nodeID := range s.agentUpdate.agents {
		delete(s.agentUpdate.failures, nodeID)
		delete(s.agentUpdate.offers, nodeID)
	}
}

// StartAgentRolloutFor queues the target for one node and clears its failed
// state, which is what the "Update agent" button does.
func (s *ServerService) StartAgentRolloutFor(nodeID, version string) {
	nodeID = strings.TrimSpace(nodeID)
	version = strings.TrimSpace(version)
	if nodeID == "" || version == "" {
		return
	}
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	s.agentUpdate.nodeRollouts[nodeID] = agentRollout{version: version, retry: true}
	delete(s.agentUpdate.failures, nodeID)
	delete(s.agentUpdate.offers, nodeID)
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

// AgentUpdateStatus describes a node against the release the control plane can
// offer: pending (no poll since the target appeared), updating (offer sent),
// up_to_date, or failed with the wrapper's reason.
func (s *ServerService) AgentUpdateStatus(nodeID, target string) (string, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ""
	}
	s.agentUpdate.mu.Lock()
	defer s.agentUpdate.mu.Unlock()
	entry, ok := s.agentUpdate.agents[nodeID]
	if !ok {
		return "", ""
	}
	if sameAgentVersion(entry.Version, target) {
		return AgentUpdateUpToDate, ""
	}
	if failure, ok := s.agentUpdate.failures[nodeID]; ok && sameAgentVersion(failure.version, target) {
		return AgentUpdateFailed, failure.detail
	}
	if offer, ok := s.agentUpdate.offers[nodeID]; ok && sameAgentVersion(offer.version, target) {
		if time.Since(offer.at) > agentOfferStaleAfter {
			return AgentUpdateFailed, "agent did not apply the offer"
		}
		return AgentUpdateUpdating, ""
	}
	return AgentUpdatePending, ""
}

func (s *ServerService) OfferAgentUpdate(ctx context.Context, nodeID, agentVersion, goos, goarch string) (*updatecore.Release, bool, bool, error) {
	if s.updater == nil {
		return nil, false, false, nil
	}
	release, err := s.updater.Offer(ctx, agentVersion, goos, goarch)
	if err != nil || release == nil {
		return release, false, false, err
	}
	now := time.Now()
	s.agentUpdate.mu.Lock()
	rollout, retry := s.rolloutForLocked(nodeID, release.Version, now)
	if !rollout && s.releaseMatchesControlPlane(release.Version) {
		rollout = true
	}
	if rollout {
		if existing, ok := s.agentUpdate.offers[nodeID]; !ok || !sameAgentVersion(existing.version, release.Version) {
			s.agentUpdate.offers[nodeID] = agentOffer{version: release.Version, at: now}
		}
		if entry, ok := s.agentUpdate.nodeRollouts[nodeID]; ok && entry.retry {
			entry.retry = false
			s.agentUpdate.nodeRollouts[nodeID] = entry
			// The agent reported its previous failure in this same request; the
			// retry it is about to receive supersedes it.
			delete(s.agentUpdate.failures, nodeID)
		}
	}
	s.agentUpdate.mu.Unlock()
	return release, rollout, retry, nil
}

func (s *ServerService) rolloutForLocked(nodeID, offered string, now time.Time) (bool, bool) {
	if entry, ok := s.agentUpdate.nodeRollouts[nodeID]; ok && rolloutForces(entry.version, offered) {
		return true, entry.retry
	}
	if s.agentUpdate.rollout != "" && now.Sub(s.agentUpdate.rolloutAt) <= rolloutTTL &&
		rolloutForces(s.agentUpdate.rollout, offered) {
		return true, false
	}
	return false, false
}

// releaseMatchesControlPlane makes the control plane's own release the rollout
// target: when the offer is the release this control plane runs, every agent
// takes it without waiting for an operator. Offline nodes pick it up when they
// reconnect, because the condition is the running version, not a timer.
func (s *ServerService) releaseMatchesControlPlane(releaseVersion string) bool {
	version := strings.TrimSpace(s.version)
	if version == "" || version == "dev" {
		return false
	}
	return sameAgentVersion(releaseVersion, version)
}

func sameAgentVersion(a, b string) bool {
	av, aErr := updatecore.ParseVersion(a)
	bv, bErr := updatecore.ParseVersion(b)
	if aErr != nil || bErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return av.Compare(bv) == 0
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
	version = stripControl(version)
	version = strings.TrimSpace(version)
	if len(version) > maxAgentVersionLength {
		version = version[:maxAgentVersionLength]
	}
	return version
}

func sanitizeAgentUpdateDetail(detail string) string {
	detail = stripControl(detail)
	detail = strings.TrimSpace(detail)
	if len(detail) > maxAgentUpdateDetail {
		detail = detail[:maxAgentUpdateDetail]
	}
	return detail
}

func stripControl(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
