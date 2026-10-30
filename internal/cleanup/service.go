package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	maxScanParallel = 4
	defaultTimeout  = 2 * time.Minute
)

// DockerClient is the slice of the node's DockerService the cleanup uses. An
// agent older than this contract lacks both methods and answers
// codes.Unimplemented.
type DockerClient interface {
	ScanCleanup(ctx context.Context, in *agentv1.CleanupScanRequest, opts ...grpc.CallOption) (*agentv1.CleanupScanResponse, error)
	Clean(ctx context.Context, in *agentv1.CleanupCleanRequest, opts ...grpc.CallOption) (*agentv1.CleanupCleanResponse, error)
}

// DialFunc opens a client to one node's agent.
type DialFunc func(ctx context.Context, server *servers.Server) (DockerClient, error)

// Registry resolves a recorded node.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

// Store persists cleanup history and answers the busy check.
type Store interface {
	CountActiveDeploymentsByServer(ctx context.Context, serverID uuid.UUID) (int64, error)
	RecordCleanupRun(ctx context.Context, run store.CleanupRun, items []store.CleanupRunItem) error
}

// Config wires the cleanup service.
type Config struct {
	Registry Registry
	Dial     DialFunc
	Volumes  containers.VolumeService
	Store    Store
	Logger   *slog.Logger
	Timeout  time.Duration
}

// Item is one reclaimable object.
type Item struct {
	ID          string
	Category    string
	Name        string
	SizeBytes   int64
	CreatedUnix int64
	Reason      string
}

// NodeScan is one node's scan outcome.
type NodeScan struct {
	ServerID string
	Status   string
	Reason   string
	Items    []Item
}

// ItemResult is one requested object's clean outcome.
type ItemResult struct {
	ID             string
	Category       string
	Name           string
	Status         string
	Message        string
	ReclaimedBytes int64
}

// CleanResult is one node's clean outcome.
type CleanResult struct {
	ServerID string
	Status   string
	Reason   string
	Items    []ItemResult
}

// Service scans nodes and removes selected objects.
type Service struct {
	registry Registry
	dial     DialFunc
	volumes  containers.VolumeService
	store    Store
	logger   *slog.Logger
	timeout  time.Duration

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Service{
		registry: cfg.Registry,
		dial:     cfg.Dial,
		volumes:  cfg.Volumes,
		store:    cfg.Store,
		logger:   logger,
		timeout:  timeout,
		locks:    map[string]*sync.Mutex{},
	}
}

// Scan fans out over the requested nodes, bounded to a small parallelism, and
// returns one outcome per input id in input order.
func (s *Service) Scan(ctx context.Context, serverIDs []string, minAge time.Duration) []NodeScan {
	scans := make([]NodeScan, len(serverIDs))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxScanParallel)
	for i, raw := range serverIDs {
		i, raw := i, raw
		group.Go(func() error {
			scans[i] = s.scanNode(groupCtx, raw, minAge)
			return nil
		})
	}
	_ = group.Wait()
	return scans
}

func (s *Service) scanNode(ctx context.Context, raw string, minAge time.Duration) NodeScan {
	scan := NodeScan{ServerID: raw, Status: StatusOK}
	serverID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return failedScan(raw, fmt.Errorf("%w: %q", ErrInvalidServerID, raw))
	}
	busy, err := s.nodeBusy(ctx, serverID)
	if err != nil {
		return failedScan(raw, err)
	}
	if busy {
		return NodeScan{ServerID: raw, Status: StatusSkipped, Reason: ReasonBusy}
	}

	items, err := s.scanAgent(ctx, serverID, minAge)
	if err != nil {
		scanStatus, reason, message := nodeFailure(err)
		scan.Status, scan.Reason = scanStatus, reason
		if scan.Status == StatusFailed {
			scan.Reason = message
		}
		return scan
	}
	for _, item := range items {
		scan.Items = append(scan.Items, itemFromProto(item))
	}
	scan.Items = append(scan.Items, s.scanVolumes(ctx, serverID, minAge)...)
	return scan
}

func failedScan(serverID string, err error) NodeScan {
	return NodeScan{ServerID: serverID, Status: StatusFailed, Reason: err.Error()}
}

func (s *Service) scanVolumes(ctx context.Context, serverID uuid.UUID, minAge time.Duration) []Item {
	if s.volumes == nil {
		return nil
	}
	volumes, err := s.volumes.ListOrphanedVolumes(ctx, serverID)
	if err != nil {
		s.logger.Debug("cleanup: orphaned volume listing failed", "server_id", serverID.String(), "error", err)
		return nil
	}
	cutoff := minAgeCutoff(minAge)
	items := make([]Item, 0, len(volumes))
	for _, volume := range volumes {
		if cutoff > 0 && volume.CreatedAt != nil && volume.CreatedAt.After(time.Unix(cutoff, 0)) {
			continue
		}
		item := Item{
			ID:        idVolumePrefix + volume.Name,
			Category:  CategoryOrphanedVolumes,
			Name:      volume.Name,
			SizeBytes: volume.SizeBytes,
			Reason:    "not owned by any recorded resource",
		}
		if volume.CreatedAt != nil {
			item.CreatedUnix = volume.CreatedAt.Unix()
		}
		items = append(items, item)
	}
	return items
}

// Clean removes the selected ids on one node. Volumes are only removed for a
// manual trigger; every call writes one run with its item results.
func (s *Service) Clean(
	ctx context.Context, actor uuid.UUID, trigger, rawServerID string, ids []string, minAge time.Duration,
) (result CleanResult) {
	startedAt := time.Now()
	result = CleanResult{ServerID: rawServerID, Status: StatusOK}
	if !validTrigger(trigger) {
		result.Status = StatusFailed
		result.Reason = fmt.Errorf("%w: %q", ErrInvalidTrigger, trigger).Error()
		return result
	}
	serverID, err := uuid.Parse(strings.TrimSpace(rawServerID))
	if err != nil {
		result.Status = StatusFailed
		result.Reason = fmt.Errorf("%w: %q", ErrInvalidServerID, rawServerID).Error()
		return result
	}
	// Every accepted Clean writes one history run, including a skipped attempt:
	// the audit shows what was asked of the node, not only what was removed.
	defer func() {
		s.record(ctx, actor, trigger, serverID, startedAt, result.Items)
	}()
	ids = dedupeIDs(ids)

	// ponytail: in-process lock, single control plane. A second Clean on the
	// same node waits; a second control plane would need a distributed lock.
	unlock := s.lockNode(rawServerID)
	defer unlock()

	busy, err := s.nodeBusy(ctx, serverID)
	if err != nil {
		result.Status = StatusFailed
		result.Reason = err.Error()
		return result
	}
	if busy {
		result.Status = StatusSkipped
		result.Reason = ReasonBusy
		return result
	}

	agentIDs, volumeIDs := splitIDs(ids)
	known := map[string]Item{}
	if len(agentIDs) > 0 {
		items, err := s.scanAgent(ctx, serverID, minAge)
		if err != nil {
			return s.cleanFailure(result, err)
		}
		for _, item := range items {
			known[item.GetId()] = itemFromProto(item)
		}
		response, err := s.cleanAgent(ctx, serverID, agentIDs, minAge)
		if err != nil {
			return s.cleanFailure(result, err)
		}
		for _, item := range response.GetResults() {
			result.Items = append(result.Items, resultFromProto(item, known))
		}
	}
	if len(volumeIDs) > 0 {
		result.Items = append(result.Items, s.cleanVolumes(ctx, serverID, trigger, volumeIDs, minAge)...)
	}

	result.Items = orderResults(ids, result.Items)
	return result
}

func (s *Service) cleanFailure(result CleanResult, err error) CleanResult {
	nodeStatus, reason, message := nodeFailure(err)
	result.Status = nodeStatus
	result.Reason = reason
	if nodeStatus == StatusFailed {
		result.Reason = message
	}
	return result
}

func (s *Service) cleanVolumes(
	ctx context.Context, serverID uuid.UUID, trigger string, ids []string, minAge time.Duration,
) []ItemResult {
	results := make([]ItemResult, 0, len(ids))
	if s.volumes == nil {
		for _, id := range ids {
			results = append(results, ItemResult{
				ID:       id,
				Category: CategoryOrphanedVolumes,
				Name:     strings.TrimPrefix(id, idVolumePrefix),
				Status:   StatusSkipped,
				Message:  "agent unavailable",
			})
		}
		return results
	}
	if trigger != TriggerManual {
		for _, id := range ids {
			results = append(results, ItemResult{
				ID:       id,
				Category: CategoryOrphanedVolumes,
				Name:     strings.TrimPrefix(id, idVolumePrefix),
				Status:   StatusSkipped,
				Message:  "manual only",
			})
		}
		return results
	}

	volumes, err := s.volumes.ListOrphanedVolumes(ctx, serverID)
	if err != nil {
		reason := ""
		switch {
		case errors.Is(err, containers.ErrVolumeListUnsupported):
			reason = "agent too old"
		case errors.Is(err, containers.ErrAgentUnavailable):
			reason = "agent unavailable"
		}
		for _, id := range ids {
			result := ItemResult{
				ID:       id,
				Category: CategoryOrphanedVolumes,
				Name:     strings.TrimPrefix(id, idVolumePrefix),
				Status:   StatusSkipped,
				Message:  reason,
			}
			if reason == "" {
				result.Status = StatusFailed
				result.Message = err.Error()
			}
			results = append(results, result)
		}
		return results
	}
	known := make(map[string]containers.OrphanedVolume, len(volumes))
	for _, volume := range volumes {
		known[volume.Name] = volume
	}
	cutoff := minAgeCutoff(minAge)
	for _, id := range ids {
		name := strings.TrimPrefix(id, idVolumePrefix)
		item := ItemResult{ID: id, Category: CategoryOrphanedVolumes, Name: name, Status: statusRemoved}
		volume, ok := known[name]
		switch {
		case !ok:
			item.Status = StatusSkipped
			item.Message = "no longer present"
		case cutoff > 0 && volume.CreatedAt != nil && volume.CreatedAt.After(time.Unix(cutoff, 0)):
			item.Status = StatusSkipped
			item.Message = "too new"
		default:
			if err := s.volumes.RemoveOrphanedVolume(ctx, serverID, name); err != nil {
				switch {
				case errors.Is(err, containers.ErrVolumeInUse):
					item.Status = StatusSkipped
					item.Message = "in use"
				default:
					item.Status = StatusFailed
					item.Message = err.Error()
				}
				break
			}
			item.ReclaimedBytes = volume.SizeBytes
		}
		results = append(results, item)
	}
	return results
}

func orderResults(ids []string, results []ItemResult) []ItemResult {
	byID := make(map[string]ItemResult, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}
	ordered := make([]ItemResult, 0, len(ids))
	for _, id := range ids {
		result, ok := byID[id]
		if !ok {
			result = ItemResult{ID: id, Status: StatusFailed, Message: "agent returned no result"}
			result.Category, result.Name = describeID(id)
		}
		ordered = append(ordered, result)
	}
	return ordered
}

func dedupeIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func splitIDs(ids []string) (agentIDs, volumeIDs []string) {
	for _, id := range ids {
		if strings.HasPrefix(id, idVolumePrefix) {
			volumeIDs = append(volumeIDs, id)
			continue
		}
		agentIDs = append(agentIDs, id)
	}
	return agentIDs, volumeIDs
}

func (s *Service) record(
	ctx context.Context, actor uuid.UUID, trigger string, serverID uuid.UUID,
	startedAt time.Time, items []ItemResult,
) {
	if s.store == nil {
		return
	}
	reclaimed := int64(0)
	for _, item := range items {
		reclaimed += item.ReclaimedBytes
	}
	run := store.CleanupRun{
		ID:             uuid.New(),
		ServerID:       serverID,
		Trigger:        trigger,
		StartedAt:      startedAt,
		FinishedAt:     time.Now(),
		ReclaimedBytes: reclaimed,
	}
	if actor != uuid.Nil {
		run.ActorUserID = &actor
	}
	history := make([]store.CleanupRunItem, 0, len(items))
	for _, item := range items {
		history = append(history, store.CleanupRunItem{
			ItemID:         item.ID,
			Category:       item.Category,
			Name:           item.Name,
			Status:         item.Status,
			Message:        item.Message,
			ReclaimedBytes: item.ReclaimedBytes,
		})
	}
	// A cancelled request must not leave a completed delete unaudited.
	if err := s.store.RecordCleanupRun(context.WithoutCancel(ctx), run, history); err != nil {
		s.logger.Warn("cleanup: recording the run failed", "server_id", serverID.String(), "error", err)
	}
}

func (s *Service) scanAgent(ctx context.Context, serverID uuid.UUID, minAge time.Duration) ([]*agentv1.CleanupItem, error) {
	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	response, err := client.ScanCleanup(ctx, &agentv1.CleanupScanRequest{
		MinAgeSeconds: minAgeSeconds(minAge),
	})
	if err != nil {
		return nil, err
	}
	return response.GetItems(), nil
}

func (s *Service) cleanAgent(
	ctx context.Context, serverID uuid.UUID, ids []string, minAge time.Duration,
) (*agentv1.CleanupCleanResponse, error) {
	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return client.Clean(ctx, &agentv1.CleanupCleanRequest{
		Ids:           ids,
		MinAgeSeconds: minAgeSeconds(minAge),
	})
}

var errAgentUnreachable = errors.New("cleanup: agent unreachable")

func (s *Service) client(ctx context.Context, serverID uuid.UUID) (DockerClient, context.CancelFunc, error) {
	if s.registry == nil || s.dial == nil {
		return nil, nil, fmt.Errorf("%w: agent dialer is not configured", errAgentUnreachable)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		cancel()
		if errors.Is(err, servers.ErrNotFound) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	client, err := s.dial(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	if client == nil {
		cancel()
		return nil, nil, fmt.Errorf("%w: dial returned no client", errAgentUnreachable)
	}
	if closer, ok := client.(io.Closer); ok {
		inner := cancel
		cancel = func() {
			_ = closer.Close()
			inner()
		}
	}
	return client, cancel, nil
}

func (s *Service) nodeBusy(ctx context.Context, serverID uuid.UUID) (bool, error) {
	if s.store == nil {
		return false, nil
	}
	count, err := s.store.CountActiveDeploymentsByServer(ctx, serverID)
	if err != nil {
		return false, fmt.Errorf("cleanup: check active deployments: %w", err)
	}
	return count > 0, nil
}

func (s *Service) lockNode(serverID string) func() {
	s.mu.Lock()
	lock := s.locks[serverID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[serverID] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func nodeFailure(err error) (nodeStatus, reason, message string) {
	if errors.Is(err, servers.ErrNotFound) {
		return StatusFailed, "", err.Error()
	}
	if errors.Is(err, errAgentUnreachable) {
		return StatusSkipped, ReasonOffline, ""
	}
	switch status.Code(err) {
	case codes.Unimplemented:
		return StatusSkipped, ReasonUnsupported, ""
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return StatusSkipped, ReasonOffline, ""
	default:
		return StatusFailed, "", err.Error()
	}
}

func itemFromProto(item *agentv1.CleanupItem) Item {
	return Item{
		ID:          item.GetId(),
		Category:    item.GetCategory(),
		Name:        item.GetName(),
		SizeBytes:   item.GetSizeBytes(),
		CreatedUnix: item.GetCreatedUnix(),
		Reason:      item.GetReason(),
	}
}

func resultFromProto(item *agentv1.CleanupItemResult, known map[string]Item) ItemResult {
	result := ItemResult{
		ID:             item.GetId(),
		Status:         item.GetStatus(),
		Message:        item.GetMessage(),
		ReclaimedBytes: item.GetReclaimedBytes(),
	}
	if info, ok := known[result.ID]; ok {
		result.Category = info.Category
		result.Name = info.Name
	} else {
		result.Category, result.Name = describeID(result.ID)
	}
	return result
}

// describeID is the fallback for an item the agent no longer lists; the scan
// before the clean normally provides the exact category and name.
func describeID(id string) (string, string) {
	switch {
	case strings.HasPrefix(id, idVolumePrefix):
		return CategoryOrphanedVolumes, strings.TrimPrefix(id, idVolumePrefix)
	case strings.HasPrefix(id, idImagePrefix):
		return CategoryUnusedImages, strings.TrimPrefix(id, idImagePrefix)
	case strings.HasPrefix(id, idContainerPrefix):
		return CategoryStoppedContainers, strings.TrimPrefix(id, idContainerPrefix)
	case id == idBuildCache:
		return CategoryBuildCache, "Build cache"
	default:
		return "", id
	}
}

func validTrigger(trigger string) bool {
	return trigger == TriggerManual || trigger == TriggerSchedule || trigger == TriggerThreshold
}

func minAgeCutoff(minAge time.Duration) int64 {
	if minAge <= 0 {
		return 0
	}
	return time.Now().Add(-minAge).Unix()
}

func minAgeSeconds(minAge time.Duration) int64 {
	if minAge <= 0 {
		return 0
	}
	return int64(minAge / time.Second)
}
