package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Service) evaluateHost(ctx context.Context, serverID uuid.UUID, nodeName string, obs servers.NodeObservation, rules Rules) {
	s.evaluateHostMetric(ctx, serverID, nodeName, obs, DetectorCPU, rules.CPU, obs.Host.CPUUsage)
	s.evaluateHostMetric(ctx, serverID, nodeName, obs, DetectorMemory, rules.Memory, obs.Host.MemUsage)
}

func (s *Service) evaluateHostMetric(ctx context.Context, serverID uuid.UUID, nodeName string, obs servers.NodeObservation, detector string, rule Rule, metric servers.ObservedMetric) {
	identity := incidents.Identity(detector, ResourceNode, serverID)
	if !rule.Enabled {
		s.closeRuleDisabled(ctx, identity)
		return
	}
	at := obs.ObservedAt
	if metric.Status != servers.MetricStatusOK {
		s.reportUnknown(ctx, incidents.Observation{
			Identity: identity, Detector: detector, ScopeKind: incidents.ScopeNode,
			ScopeID: serverID, NodeID: serverID, ResourceType: ResourceNode,
			ResourceID: serverID, ResourceName: nodeName, At: at,
			Evidence: "counter " + metric.Status + reasonSuffix(metric.Reason),
		})
		return
	}
	confirm, recover := rule.timings()
	value := metric.Value * 100
	_, _, err := s.incidents.Report(ctx, incidents.Observation{
		Identity:         identity,
		Detector:         detector,
		ScopeKind:        incidents.ScopeNode,
		ScopeID:          serverID,
		NodeID:           serverID,
		ResourceType:     ResourceNode,
		ResourceID:       serverID,
		ResourceName:     nodeName,
		At:               at,
		Valid:            true,
		Severity:         incidents.SeverityWarning,
		Value:            &value,
		TriggerThreshold: &rule.Warning,
		ClearThreshold:   &rule.Clear,
		ConfirmFor:       confirm,
		RecoverFor:       recover,
		MaxGap:           staleAfter,
		Evidence:         fmt.Sprintf("%.1f%% (warning %.0f%%, clear %.0f%%)", value, rule.Warning, rule.Clear),
	})
	s.logReport(detector, serverID, err)
}

func (s *Service) evaluateFilesystems(ctx context.Context, serverID uuid.UUID, nodeName string, obs servers.NodeObservation, rules Rules) {
	filesystems := mergeAliases(obs.Filesystems)
	seen := make(map[string]bool, len(filesystems))
	for _, fs := range filesystems {
		seen[fs.Path] = true
		identity, err := s.observeFilesystem(ctx, serverID, fs, obs.ObservedAt)
		if err != nil {
			s.logger.Warn("monitoring: record filesystem", "server_id", serverID.String(), "path", fs.Path, "error", err)
			continue
		}
		s.evaluateFilesystemCounter(ctx, serverID, nodeName, fs, identity, rules.Space, DetectorSpace, obs.ObservedAt)
		s.evaluateFilesystemCounter(ctx, serverID, nodeName, fs, identity, rules.Inode, DetectorInodes, obs.ObservedAt)
	}
	s.markMissingFilesystems(ctx, serverID, seen, obs.ObservedAt, rules)
}

func (s *Service) evaluateFilesystemCounter(ctx context.Context, serverID uuid.UUID, nodeName string, fs servers.FilesystemObservation, identity sqlc.FilesystemIdentity, rule FilesystemRule, detector string, at time.Time) {
	key := filesystemIdentity(detector, serverID, identity.FilesystemKey)
	if !rule.Enabled {
		s.closeRuleDisabled(ctx, key)
		return
	}
	base := incidents.Observation{
		Identity: key, Detector: detector, ScopeKind: incidents.ScopeNode,
		ScopeID: serverID, NodeID: serverID, ResourceType: ResourceFilesystem,
		ResourceID: uuid.UUID(identity.ID.Bytes), ResourceName: identity.Path, At: at,
	}
	total, used, available, known := fsBytes(fs)
	unit := "bytes"
	if detector == DetectorInodes {
		total, used, available, known = fsInodes(fs)
		unit = "inodes"
	}
	if !known || total == 0 {
		base.Valid = false
		base.Evidence = "counter unavailable"
		if detector == DetectorInodes {
			base.Evidence = "inodes " + fs.InodeStatus + reasonSuffix(fs.InodeReason)
		} else {
			base.Evidence = "space " + fs.Status + reasonSuffix(fs.Reason)
		}
		s.reportUnknown(ctx, base)
		return
	}
	value := float64(used) / float64(total) * 100
	desired, severityFor := filesystemSeverity(value, rule)
	confirm, _, downgrade, recover := rule.timings()
	if desired == incidents.SeverityCritical {
		severityFor = criticalFor(rule)
	} else if desired == incidents.SeverityWarning && severityFor == 0 {
		severityFor = downgrade
	}
	base.Valid = true
	base.Severity = desired
	base.SeverityFor = severityFor
	base.Value = &value
	base.TriggerThreshold = &rule.Warning
	base.ClearThreshold = &rule.Clear
	base.ConfirmFor = confirm
	base.RecoverFor = recover
	base.MaxGap = staleAfter
	base.Evidence = fmt.Sprintf("%s %s used, %s available of %s, purpose=%s",
		identity.Path, formatPercent(value), formatUnits(available, unit), formatUnits(total, unit), identity.Purpose)
	if nodeName != "" {
		base.Evidence += ", node=" + nodeName
	}
	_, _, err := s.incidents.Report(ctx, base)
	s.logReport(detector, serverID, err)
}

// filesystemSeverity returns the desired severity and its sustained window.
// Between downgrade and critical the severity is unchanged, so a warning does
// not flap critical on a single sample.
func filesystemSeverity(value float64, rule FilesystemRule) (incidents.Severity, time.Duration) {
	switch {
	case value >= rule.Critical:
		return incidents.SeverityCritical, criticalFor(rule)
	case value <= rule.Downgrade:
		return incidents.SeverityWarning, time.Duration(rule.DowngradeSeconds) * time.Second
	default:
		return "", 0
	}
}

func criticalFor(rule FilesystemRule) time.Duration {
	return time.Duration(rule.CriticalSeconds) * time.Second
}

func fsBytes(fs servers.FilesystemObservation) (total, used, available int64, ok bool) {
	if fs.Status != servers.MetricStatusOK || fs.TotalBytes == 0 {
		return 0, 0, 0, false
	}
	total = clampInt64(fs.TotalBytes)
	available = clampInt64(fs.AvailableBytes)
	used = total - available
	if used < 0 {
		used = 0
	}
	return total, used, available, true
}

func fsInodes(fs servers.FilesystemObservation) (total, used, available int64, ok bool) {
	if fs.InodeStatus != servers.MetricStatusOK || fs.TotalInodes == 0 {
		return 0, 0, 0, false
	}
	total = clampInt64(fs.TotalInodes)
	available = clampInt64(fs.FreeInodes)
	used = total - available
	if used < 0 {
		used = 0
	}
	return total, used, available, true
}

func (s *Service) observeFilesystem(ctx context.Context, serverID uuid.UUID, fs servers.FilesystemObservation, at time.Time) (sqlc.FilesystemIdentity, error) {
	row, err := s.store.GetFilesystemIdentity(ctx, sqlc.GetFilesystemIdentityParams{
		NodeID:        pgUUID(serverID),
		FilesystemKey: fs.Path,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		totalBytes, totalInodes := knownCapacity(fs)
		created, createErr := s.store.CreateFilesystemIdentity(ctx, sqlc.CreateFilesystemIdentityParams{
			ID:            pgUUID(uuid.New()),
			NodeID:        pgUUID(serverID),
			FilesystemKey: fs.Path,
			Path:          fs.Path,
			Purpose:       fs.Purpose,
			TotalBytes:    totalBytes,
			TotalInodes:   totalInodes,
			FirstSeenAt:   pgtype.Timestamptz{Time: at, Valid: true},
		})
		if createErr != nil {
			return sqlc.FilesystemIdentity{}, createErr
		}
		if err := s.recordFilesystemBucket(ctx, created, fs, at); err != nil {
			s.logger.Warn("monitoring: record filesystem history", "path", fs.Path, "error", err)
		}
		return created, nil
	}
	if err != nil {
		return sqlc.FilesystemIdentity{}, err
	}
	totalBytes, totalInodes := row.TotalBytes, row.TotalInodes
	segment := row.Segment
	if fs.Status == servers.MetricStatusOK && fs.TotalBytes > 0 && row.TotalBytes > 0 && clampInt64(fs.TotalBytes) != row.TotalBytes {
		segment++
	}
	if fs.InodeStatus == servers.MetricStatusOK && fs.TotalInodes > 0 && row.TotalInodes > 0 && clampInt64(fs.TotalInodes) != row.TotalInodes {
		segment++
	}
	// The segment bounds only the history. Incident identity stays on the
	// stable filesystem key, so a resize keeps the same active incident.
	if fs.Status == servers.MetricStatusOK && fs.TotalBytes > 0 {
		totalBytes = clampInt64(fs.TotalBytes)
	}
	if fs.InodeStatus == servers.MetricStatusOK && fs.TotalInodes > 0 {
		totalInodes = clampInt64(fs.TotalInodes)
	}
	updated, err := s.store.UpdateFilesystemIdentity(ctx, sqlc.UpdateFilesystemIdentityParams{
		Path:        fs.Path,
		Purpose:     fs.Purpose,
		Segment:     segment,
		TotalBytes:  totalBytes,
		TotalInodes: totalInodes,
		LastSeenAt:  pgtype.Timestamptz{Time: at, Valid: true},
		ID:          row.ID,
	})
	if err != nil {
		return sqlc.FilesystemIdentity{}, err
	}
	if err := s.recordFilesystemBucket(ctx, updated, fs, at); err != nil {
		s.logger.Warn("monitoring: record filesystem history", "path", fs.Path, "error", err)
	}
	return updated, nil
}

func knownCapacity(fs servers.FilesystemObservation) (int64, int64) {
	var bytes, inodes int64
	if fs.Status == servers.MetricStatusOK {
		bytes = clampInt64(fs.TotalBytes)
	}
	if fs.InodeStatus == servers.MetricStatusOK {
		inodes = clampInt64(fs.TotalInodes)
	}
	return bytes, inodes
}

// recordFilesystemBucket aggregates one observation into an hourly bucket.
// Unknown counters increment only the quality counters and leave min/max null,
// so a failed read is never stored as a zero sample.
func (s *Service) recordFilesystemBucket(ctx context.Context, identity sqlc.FilesystemIdentity, fs servers.FilesystemObservation, at time.Time) error {
	_, usedBytes, _, bytesOK := fsBytes(fs)
	_, usedInodes, _, inodesOK := fsInodes(fs)
	var minBytes, maxBytes, lastBytes *int64
	var minInodes, maxInodes, lastInodes *int64
	if bytesOK {
		value := usedBytes
		minBytes, maxBytes, lastBytes = &value, &value, &value
	}
	if inodesOK {
		value := usedInodes
		minInodes, maxInodes, lastInodes = &value, &value, &value
	}
	fresh, partial, unknown := 0, 0, 1
	switch {
	case bytesOK && inodesOK:
		fresh, partial, unknown = 1, 0, 0
	case bytesOK || inodesOK:
		fresh, partial, unknown = 0, 1, 0
	}
	return s.store.UpsertFilesystemUsageBucket(ctx, sqlc.UpsertFilesystemUsageBucketParams{
		IdentityID:     identity.ID,
		Segment:        identity.Segment,
		BucketAt:       pgtype.Timestamptz{Time: at.UTC().Truncate(time.Hour), Valid: true},
		Samples:        1,
		FreshSamples:   int32(fresh),
		PartialSamples: int32(partial),
		UnknownSamples: int32(unknown),
		MinUsedBytes:   minBytes,
		MaxUsedBytes:   maxBytes,
		LastUsedBytes:  lastBytes,
		MinUsedInodes:  minInodes,
		MaxUsedInodes:  maxInodes,
		LastUsedInodes: lastInodes,
		TotalBytes:     identity.TotalBytes,
		TotalInodes:    identity.TotalInodes,
		UpdatedAt:      pgtype.Timestamptz{Time: at.UTC(), Valid: true},
	})
}

// markMissingFilesystems keeps a filesystem that is absent from an accepted
// report active with unknown coverage. A disappeared mount is not a recovery.
func (s *Service) markMissingFilesystems(ctx context.Context, serverID uuid.UUID, seen map[string]bool, at time.Time, rules Rules) {
	rows, err := s.store.ListFilesystemIdentitiesByNode(ctx, pgUUID(serverID))
	if err != nil {
		s.logger.Warn("monitoring: list filesystems", "server_id", serverID.String(), "error", err)
		return
	}
	for _, row := range rows {
		if seen[row.FilesystemKey] {
			continue
		}
		spaceIdentity := filesystemIdentity(DetectorSpace, serverID, row.FilesystemKey)
		inodeIdentity := filesystemIdentity(DetectorInodes, serverID, row.FilesystemKey)
		if !rules.Space.Enabled {
			s.closeRuleDisabled(ctx, spaceIdentity)
		} else {
			s.reportUnknown(ctx, incidents.Observation{
				Identity: spaceIdentity, Detector: DetectorSpace, ScopeKind: incidents.ScopeNode,
				ScopeID: serverID, NodeID: serverID, ResourceType: ResourceFilesystem,
				ResourceID: uuid.UUID(row.ID.Bytes), ResourceName: row.Path, At: at,
				Evidence: "filesystem not reported",
			})
		}
		if !rules.Inode.Enabled {
			s.closeRuleDisabled(ctx, inodeIdentity)
		} else {
			s.reportUnknown(ctx, incidents.Observation{
				Identity: inodeIdentity, Detector: DetectorInodes, ScopeKind: incidents.ScopeNode,
				ScopeID: serverID, NodeID: serverID, ResourceType: ResourceFilesystem,
				ResourceID: uuid.UUID(row.ID.Bytes), ResourceName: row.Path, At: at,
				Evidence: "filesystem not reported",
			})
		}
	}
}

func (s *Service) reportUnknown(ctx context.Context, obs incidents.Observation) {
	obs.Severity = ""
	_, _, err := s.incidents.Report(ctx, obs)
	s.logReport(obs.Detector, obs.NodeID, err)
}

func (s *Service) closeRuleDisabled(ctx context.Context, identity string) {
	if err := s.incidents.Close(ctx, identity, "rule_disabled"); err != nil {
		s.logger.Warn("monitoring: close incident", "identity", identity, "reason", "rule_disabled", "error", err)
	}
}

func (s *Service) logReport(detector string, serverID uuid.UUID, err error) {
	if err != nil {
		s.logger.Warn("monitoring: report", "detector", detector, "server_id", serverID.String(), "error", err)
	}
}

func filesystemIdentity(detector string, serverID uuid.UUID, key string) string {
	return incidents.Identity(detector, ResourceFilesystem, serverID, key)
}

// mergeAliases collapses paths that describe the same filesystem within one
// report. The agent already deduplicates by device; this is defensive for
// nested bind mounts, where no device id is carried in the observation.
func mergeAliases(items []servers.FilesystemObservation) []servers.FilesystemObservation {
	out := make([]servers.FilesystemObservation, 0, len(items))
	for _, fs := range items {
		merged := false
		for i := range out {
			if sameFilesystem(out[i], fs) {
				out[i].Purpose = mergePurpose(out[i].Purpose, fs.Purpose)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, fs)
		}
	}
	return out
}

func sameFilesystem(a, b servers.FilesystemObservation) bool {
	if a.Path == b.Path {
		return true
	}
	if a.Status != servers.MetricStatusOK || b.Status != servers.MetricStatusOK {
		return false
	}
	if a.TotalBytes != b.TotalBytes || a.AvailableBytes != b.AvailableBytes {
		return false
	}
	if a.TotalInodes != b.TotalInodes || a.FreeInodes != b.FreeInodes {
		return false
	}
	return nestedPath(a.Path, b.Path) || nestedPath(b.Path, a.Path)
}

func nestedPath(parent, child string) bool {
	parent = strings.TrimSuffix(parent, "/")
	if parent == "" || parent == "/" {
		// Root is an ancestor of everything; capacity equality alone cannot
		// prove two mounts are the same filesystem.
		return false
	}
	return strings.HasPrefix(child, parent+"/")
}

func mergePurpose(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || a == b || strings.Contains(a, b) {
		return a
	}
	return a + "+" + b
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

func clampInt64(value uint64) int64 {
	if value > uint64(1<<63-1) {
		return 1<<63 - 1
	}
	return int64(value)
}

func formatPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}

func formatUnits(value int64, unit string) string {
	if unit == "inodes" {
		return strconv.FormatInt(value, 10) + " inodes"
	}
	return strconv.FormatInt(value, 10) + " bytes"
}

// NodeStatus is the read model behind the server-details pressure card.
type NodeStatus struct {
	NodeID      uuid.UUID          `json:"node_id"`
	ObservedAt  *time.Time         `json:"observed_at,omitempty"`
	Coverage    string             `json:"coverage"`
	Active      []ActivePressure   `json:"active"`
	Filesystems []FilesystemStatus `json:"filesystems"`
}

type ActivePressure struct {
	Detector        string     `json:"detector"`
	Severity        string     `json:"severity"`
	Lifecycle       string     `json:"lifecycle"`
	ResourceName    string     `json:"resource_name"`
	Evidence        string     `json:"evidence"`
	FirstObservedAt time.Time  `json:"first_observed_at"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
}

type FilesystemStatus struct {
	Path           string  `json:"path"`
	Purpose        string  `json:"purpose"`
	Status         string  `json:"status"`
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	InodeStatus    string  `json:"inode_status"`
	TotalInodes    uint64  `json:"total_inodes"`
	FreeInodes     uint64  `json:"free_inodes"`
	InodeUsedPct   float64 `json:"inode_used_percent"`
}

func (s *Service) NodeStatus(ctx context.Context, nodeID uuid.UUID) (NodeStatus, error) {
	if s == nil || s.store == nil || s.incidents == nil {
		return NodeStatus{}, errors.New("monitoring: store and incidents are required")
	}
	status := NodeStatus{NodeID: nodeID, Active: []ActivePressure{}, Filesystems: []FilesystemStatus{}, Coverage: "unknown"}
	row, err := s.store.GetNodeObservation(ctx, pgUUID(nodeID))
	switch {
	case err == nil:
		observed := row.ObservedAt.Time.UTC()
		status.ObservedAt = &observed
		status.Coverage = s.coverage(row, s.now().UTC())
		var filesystems []servers.FilesystemObservation
		if err := json.Unmarshal(row.Filesystems, &filesystems); err == nil {
			status.Filesystems = filesystemStatuses(mergeAliases(filesystems))
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return NodeStatus{}, fmt.Errorf("monitoring: node status: %w", err)
	}
	rows, err := s.incidents.ActiveNodeIncidents(ctx)
	if err != nil {
		return NodeStatus{}, err
	}
	for _, inc := range rows {
		if inc.NodeID != nodeID || !isPressureDetector(inc.Detector) {
			continue
		}
		status.Active = append(status.Active, ActivePressure{
			Detector: inc.Detector, Severity: string(inc.Severity), Lifecycle: string(inc.Lifecycle),
			ResourceName: inc.ResourceName, Evidence: inc.Evidence,
			FirstObservedAt: inc.FirstObservedAt.UTC(), ConfirmedAt: inc.ConfirmedAt,
		})
	}
	return status, nil
}

func (s *Service) coverage(row sqlc.NodeObservation, now time.Time) string {
	age := now.Sub(row.ObservedAt.Time.UTC())
	if age < 0 {
		return "unknown"
	}
	interval := time.Duration(row.SamplingIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if age <= 3*interval {
		return "fresh"
	}
	return "stale"
}

func filesystemStatuses(items []servers.FilesystemObservation) []FilesystemStatus {
	out := make([]FilesystemStatus, 0, len(items))
	for _, fs := range items {
		entry := FilesystemStatus{
			Path: fs.Path, Purpose: fs.Purpose, Status: fs.Status,
			TotalBytes: fs.TotalBytes, AvailableBytes: fs.AvailableBytes,
			InodeStatus: fs.InodeStatus, TotalInodes: fs.TotalInodes, FreeInodes: fs.FreeInodes,
		}
		if fs.Status == servers.MetricStatusOK && fs.TotalBytes > 0 {
			entry.UsedPercent = float64(fs.TotalBytes-fs.AvailableBytes) / float64(fs.TotalBytes) * 100
		}
		if fs.InodeStatus == servers.MetricStatusOK && fs.TotalInodes > 0 {
			entry.InodeUsedPct = float64(fs.TotalInodes-fs.FreeInodes) / float64(fs.TotalInodes) * 100
		}
		out = append(out, entry)
	}
	return out
}
