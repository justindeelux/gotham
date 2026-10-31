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

// HostCleaner scans and clears the root-only categories (the systemd journal
// and the package cache) on one node. The local control-plane node and every
// agent node answer through the same fixed host helper. nil means the feature
// is absent, so no host category is offered.
type HostCleaner interface {
	CleanupStatus(ctx context.Context, serverID uuid.UUID) (HostCleanupStatus, error)
	CleanupHost(ctx context.Context, serverID uuid.UUID, category string, minAge time.Duration) (int64, error)
}

// HostCleanupCategory is one root-only category's state on a node.
type HostCleanupCategory struct {
	Supported bool
	Manager   string
	Bytes     int64
}

// HostCleanupStatus is the journal and package-cache state of one node.
type HostCleanupStatus struct {
	Journal      HostCleanupCategory
	PackageCache HostCleanupCategory
}

// LeftoverSource scans and cleans Gotham's own leftovers on the control-plane
// node. nil means the feature is absent, so no Gotham category is offered.
type LeftoverSource interface {
	Scan(ctx context.Context) ([]Item, error)
	Clean(ctx context.Context, id string, minAge time.Duration) ItemResult
}

// Registry resolves a recorded node.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

// Store persists cleanup history and answers the busy check.
type Store interface {
	CountActiveDeploymentsByServer(ctx context.Context, serverID uuid.UUID) (int64, error)
	RecordCleanupRun(ctx context.Context, run store.CleanupRun, items []store.CleanupRunItem) error
	ListCleanupExclusions(ctx context.Context) ([]store.CleanupExclusion, error)
}

// Config wires the cleanup service.
type Config struct {
	Registry  Registry
	Dial      DialFunc
	Volumes   containers.VolumeService
	Host      HostCleaner
	Leftovers LeftoverSource
	Store     Store
	// Center persists schedules, exclusions and history reads. When nil the
	// control-plane store given as Store is used.
	Center CenterStore
	// Notifier receives the outcome of a scheduled run. Manual runs never
	// notify.
	Notifier ScheduleNotifier
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

// CategoryNotice says why one category could not be scanned on a node. The
// node itself stays ok: only that category is unavailable.
type CategoryNotice struct {
	Category string
	Reason   string
}

// NodeScan is one node's scan outcome.
type NodeScan struct {
	ServerID string
	Status   string
	Reason   string
	Items    []Item
	Notices  []CategoryNotice
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
	RunID    string
	Items    []ItemResult
}

// Service scans nodes and removes selected objects.
type Service struct {
	registry  Registry
	dial      DialFunc
	volumes   containers.VolumeService
	host      HostCleaner
	leftovers LeftoverSource
	store     Store
	center    CenterStore
	notifier  ScheduleNotifier
	logger    *slog.Logger
	timeout   time.Duration
	now       func() time.Time
	loc       *time.Location

	mu       sync.Mutex
	locks    map[string]*sync.Mutex
	inflight map[uuid.UUID]bool
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
	center := cfg.Center
	if center == nil {
		center, _ = cfg.Store.(CenterStore)
	}
	return &Service{
		registry:  cfg.Registry,
		dial:      cfg.Dial,
		volumes:   cfg.Volumes,
		host:      cfg.Host,
		leftovers: cfg.Leftovers,
		store:     cfg.Store,
		center:    center,
		notifier:  cfg.Notifier,
		logger:    logger,
		timeout:   timeout,
		now:       time.Now,
		loc:       time.Local,
		locks:     map[string]*sync.Mutex{},
		inflight:  map[uuid.UUID]bool{},
	}
}

// Scan fans out over the requested nodes, bounded to a small parallelism, and
// returns one outcome per input id in input order.
func (s *Service) Scan(ctx context.Context, serverIDs []string, minAge time.Duration) []NodeScan {
	exclusions, err := s.loadExclusions(ctx)
	if err != nil {
		// An exclusion list that cannot be read must not offer protected items
		// for deletion.
		scans := make([]NodeScan, len(serverIDs))
		for i, raw := range serverIDs {
			scans[i] = NodeScan{ServerID: raw, Status: StatusFailed, Reason: err.Error()}
		}
		return scans
	}
	scans := make([]NodeScan, len(serverIDs))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxScanParallel)
	for i, raw := range serverIDs {
		i, raw := i, raw
		group.Go(func() error {
			scans[i] = s.scanNode(groupCtx, raw, minAge, exclusions)
			return nil
		})
	}
	_ = group.Wait()
	return scans
}

func (s *Service) scanNode(ctx context.Context, raw string, minAge time.Duration, exclusions exclusionIndex) NodeScan {
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
	hostItems, notices := s.scanHost(ctx, serverID)
	scan.Items = append(scan.Items, hostItems...)
	scan.Notices = notices
	scan.Items = append(scan.Items, s.scanLeftovers(ctx, serverID)...)
	scan.Items = exclusions.filter(serverID, scan.Items)
	return scan
}

// scanHost adds one item per supported root-only category that holds bytes,
// and a notice for each one this host cannot scan. A failure here never makes
// the node itself failed: the Docker scan above already succeeded.
func (s *Service) scanHost(ctx context.Context, serverID uuid.UUID) ([]Item, []CategoryNotice) {
	if s.host == nil {
		return nil, nil
	}
	status, err := s.host.CleanupStatus(ctx, serverID)
	if err != nil {
		s.logger.Debug("cleanup: host cleanup status failed", "server_id", serverID.String(), "error", err)
		reason := hostNoticeReason(err)
		return nil, []CategoryNotice{
			{Category: CategoryJournal, Reason: reason},
			{Category: CategoryPackageCache, Reason: reason},
		}
	}
	var items []Item
	var notices []CategoryNotice
	if status.Journal.Supported {
		if status.Journal.Bytes > 0 {
			items = append(items, Item{
				ID:        idHostPrefix + CategoryJournal,
				Category:  CategoryJournal,
				Name:      hostCategoryName(CategoryJournal),
				SizeBytes: status.Journal.Bytes,
				Reason:    ReasonJournalArchived,
			})
		}
	} else {
		notices = append(notices, CategoryNotice{Category: CategoryJournal, Reason: ReasonUnsupported})
	}
	if status.PackageCache.Supported {
		if status.PackageCache.Bytes > 0 {
			items = append(items, Item{
				ID:        idHostPrefix + CategoryPackageCache,
				Category:  CategoryPackageCache,
				Name:      hostCategoryName(CategoryPackageCache),
				SizeBytes: status.PackageCache.Bytes,
				Reason:    ReasonPackageCache,
			})
		}
	} else {
		notices = append(notices, CategoryNotice{Category: CategoryPackageCache, Reason: ReasonUnsupported})
	}
	return items, notices
}

func hostNoticeReason(err error) string {
	switch {
	case errors.Is(err, ErrHostHelperOutdated):
		return ReasonHelperOutdated
	case errors.Is(err, ErrHostAgentOutdated):
		return ReasonAgentOutdated
	default:
		return ReasonUnsupported
	}
}

func hostCategoryName(category string) string {
	if category == CategoryPackageCache {
		return "Package cache"
	}
	return "Journal"
}

// scanLeftovers adds the control-plane leftovers only when this node is the
// local Gotham node: another node's agent has no access to the control plane's
// database or data directory.
func (s *Service) scanLeftovers(ctx context.Context, serverID uuid.UUID) []Item {
	if s.leftovers == nil || !s.controlPlaneNode(ctx, serverID) {
		return nil
	}
	items, err := s.leftovers.Scan(ctx)
	if err != nil {
		s.logger.Warn("cleanup: gotham leftovers scan failed", "server_id", serverID.String(), "error", err)
		return nil
	}
	return items
}

func (s *Service) controlPlaneNode(ctx context.Context, serverID uuid.UUID) bool {
	if s.registry == nil {
		return false
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		return false
	}
	return server.NodeID != nil && *server.NodeID == "localhost"
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
	return s.clean(ctx, actor, trigger, nil, rawServerID, ids, minAge)
}

// clean is the shared path for manual, scheduled and threshold runs.
func (s *Service) clean(
	ctx context.Context, actor uuid.UUID, trigger string, scheduleID *uuid.UUID,
	rawServerID string, ids []string, minAge time.Duration,
) (result CleanResult) {
	startedAt := s.now()
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
	// Exclusions protect every trigger; a list that cannot be read fails the
	// clean instead of risking a protected removal.
	exclusions, err := s.loadExclusions(ctx)
	if err != nil {
		result.Status = StatusFailed
		result.Reason = err.Error()
		return result
	}
	// Every accepted Clean writes one history run, including a skipped attempt:
	// the audit shows what was asked of the node, not only what was removed.
	defer func() {
		if runID := s.record(ctx, actor, trigger, scheduleID, serverID, startedAt, result); runID != uuid.Nil {
			result.RunID = runID.String()
		}
	}()
	ids = dedupeIDs(ids)

	// ponytail: in-process lock, single control plane. A second Clean on the
	// same node waits; a scheduled run tries once and reports busy instead of
	// stacking work. A second control plane would need a distributed lock.
	var unlock func()
	if trigger == TriggerManual {
		unlock = s.lockNode(rawServerID)
	} else {
		var locked bool
		unlock, locked = s.tryLockNode(rawServerID)
		if !locked {
			result.Status = StatusSkipped
			result.Reason = ReasonBusy
			return result
		}
	}
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

	agentIDs, volumeIDs, hostIDs, leftoverIDs := splitIDs(ids)
	var excluded []ItemResult
	known := map[string]Item{}
	if len(agentIDs) > 0 {
		items, err := s.scanAgent(ctx, serverID, minAge)
		if err != nil {
			return s.cleanFailure(result, err)
		}
		for _, item := range items {
			known[item.GetId()] = itemFromProto(item)
		}
		kept := make([]string, 0, len(agentIDs))
		for _, id := range agentIDs {
			info, ok := known[id]
			if !ok {
				category, name := describeID(id)
				info = Item{ID: id, Category: category, Name: name}
			}
			if exclusions.matches(serverID, info.Category, info.Name, id) {
				excluded = append(excluded, excludedResult(info))
				continue
			}
			kept = append(kept, id)
		}
		agentIDs = kept
		if len(agentIDs) > 0 {
			response, err := s.cleanAgent(ctx, serverID, agentIDs, minAge)
			if err != nil {
				return s.cleanFailure(result, err)
			}
			for _, item := range response.GetResults() {
				result.Items = append(result.Items, resultFromProto(item, known))
			}
		}
	}
	if len(volumeIDs) > 0 {
		cleaned, skipped := s.cleanVolumes(ctx, serverID, trigger, volumeIDs, minAge, exclusions)
		result.Items = append(result.Items, cleaned...)
		excluded = append(excluded, skipped...)
	}
	if len(hostIDs) > 0 {
		cleaned, skipped := s.cleanHost(ctx, serverID, hostIDs, minAge, exclusions)
		result.Items = append(result.Items, cleaned...)
		excluded = append(excluded, skipped...)
	}
	if len(leftoverIDs) > 0 {
		kept := make([]string, 0, len(leftoverIDs))
		for _, id := range leftoverIDs {
			category, name := describeID(id)
			if exclusions.matches(serverID, category, name, id) {
				excluded = append(excluded, excludedResult(Item{ID: id, Category: category, Name: name}))
				continue
			}
			kept = append(kept, id)
		}
		if len(kept) > 0 {
			result.Items = append(result.Items, s.cleanLeftovers(ctx, serverID, kept, minAge)...)
		}
	}

	result.Items = append(result.Items, excluded...)
	result.Items = orderResults(ids, result.Items)
	return result
}

// excludedResult reports an item the owner protected as skipped, never removed.
func excludedResult(item Item) ItemResult {
	return ItemResult{
		ID:       item.ID,
		Category: item.Category,
		Name:     item.Name,
		Status:   StatusSkipped,
		Message:  "excluded",
	}
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

// cleanHost clears the selected root-only categories. They are safe at every
// trigger, so unlike orphaned volumes there is no manual-only refusal.
func (s *Service) cleanHost(
	ctx context.Context, serverID uuid.UUID, ids []string, minAge time.Duration, exclusions exclusionIndex,
) (results, excluded []ItemResult) {
	results = make([]ItemResult, 0, len(ids))
	for _, id := range ids {
		category := strings.TrimPrefix(id, idHostPrefix)
		result := ItemResult{
			ID:       id,
			Category: category,
			Name:     hostCategoryName(category),
			Status:   statusRemoved,
		}
		if exclusions.matches(serverID, result.Category, result.Name, id) {
			excluded = append(excluded, excludedResult(Item{ID: id, Category: result.Category, Name: result.Name}))
			continue
		}
		if s.host == nil {
			result.Status, result.Message = StatusSkipped, ReasonUnsupported
			results = append(results, result)
			continue
		}
		reclaimed, err := s.host.CleanupHost(ctx, serverID, category, minAge)
		if err != nil {
			status, message := hostSkipReason(err)
			if status == "" {
				status, message = StatusFailed, err.Error()
			}
			result.Status, result.Message = status, message
			results = append(results, result)
			continue
		}
		result.ReclaimedBytes = reclaimed
		results = append(results, result)
	}
	return results, excluded
}

// cleanLeftovers removes the selected control-plane leftovers. They are safe at
// every trigger as long as each item re-checks its own preconditions.
func (s *Service) cleanLeftovers(
	ctx context.Context, serverID uuid.UUID, ids []string, minAge time.Duration,
) []ItemResult {
	results := make([]ItemResult, 0, len(ids))
	for _, id := range ids {
		category, name := describeID(id)
		result := ItemResult{ID: id, Category: category, Name: name}
		switch {
		case s.leftovers == nil:
			result.Status, result.Message = StatusSkipped, "unsupported"
		case !s.controlPlaneNode(ctx, serverID):
			result.Status, result.Message = StatusSkipped, "control-plane node only"
		default:
			result = s.leftovers.Clean(ctx, id, minAge)
		}
		results = append(results, result)
	}
	return results
}

// hostSkipReason reports the categories a clean must skip rather than fail:
// the feature is absent on that host, not broken.
func hostSkipReason(err error) (string, string) {
	switch {
	case errors.Is(err, ErrHostUnsupported):
		return StatusSkipped, "unsupported"
	case errors.Is(err, ErrHostHelperOutdated):
		return StatusSkipped, "helper outdated"
	case errors.Is(err, ErrHostAgentOutdated):
		return StatusSkipped, "agent outdated"
	}
	return "", ""
}

func (s *Service) cleanVolumes(
	ctx context.Context, serverID uuid.UUID, trigger string, ids []string, minAge time.Duration,
	exclusions exclusionIndex,
) (results, excluded []ItemResult) {
	results = make([]ItemResult, 0, len(ids))
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
		return results, nil
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
		return results, nil
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
		return results, nil
	}
	known := make(map[string]containers.OrphanedVolume, len(volumes))
	for _, volume := range volumes {
		known[volume.Name] = volume
	}
	cutoff := minAgeCutoff(minAge)
	for _, id := range ids {
		name := strings.TrimPrefix(id, idVolumePrefix)
		item := ItemResult{ID: id, Category: CategoryOrphanedVolumes, Name: name, Status: statusRemoved}
		if exclusions.matches(serverID, item.Category, item.Name, id) {
			excluded = append(excluded, excludedResult(Item{ID: id, Category: item.Category, Name: item.Name}))
			continue
		}
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
	return results, excluded
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

func splitIDs(ids []string) (agentIDs, volumeIDs, hostIDs, leftoverIDs []string) {
	for _, id := range ids {
		switch {
		case strings.HasPrefix(id, idVolumePrefix):
			volumeIDs = append(volumeIDs, id)
		case strings.HasPrefix(id, idHostPrefix):
			hostIDs = append(hostIDs, id)
		case strings.HasPrefix(id, idGothamPrefix):
			leftoverIDs = append(leftoverIDs, id)
		default:
			agentIDs = append(agentIDs, id)
		}
	}
	return agentIDs, volumeIDs, hostIDs, leftoverIDs
}

func (s *Service) record(
	ctx context.Context, actor uuid.UUID, trigger string, scheduleID *uuid.UUID, serverID uuid.UUID,
	startedAt time.Time, result CleanResult,
) uuid.UUID {
	if s.store == nil {
		return uuid.Nil
	}
	runID := uuid.New()
	reclaimed := int64(0)
	for _, item := range result.Items {
		reclaimed += item.ReclaimedBytes
	}
	run := store.CleanupRun{
		ID:             runID,
		ServerID:       serverID,
		Trigger:        trigger,
		StartedAt:      startedAt,
		FinishedAt:     s.now(),
		ReclaimedBytes: reclaimed,
		ScheduleID:     scheduleID,
		Status:         result.Status,
		Reason:         result.Reason,
	}
	if actor != uuid.Nil {
		run.ActorUserID = &actor
	}
	history := make([]store.CleanupRunItem, 0, len(result.Items))
	for _, item := range result.Items {
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
		return uuid.Nil
	}
	return runID
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
	lock := s.lockFor(serverID)
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// tryLockNode takes the node lock only when it is free, so a scheduled run
// reports busy instead of queuing behind a manual clean.
func (s *Service) tryLockNode(serverID string) (func(), bool) {
	s.mu.Lock()
	lock := s.lockFor(serverID)
	s.mu.Unlock()
	if !lock.TryLock() {
		return func() {}, false
	}
	return lock.Unlock, true
}

func (s *Service) lockFor(serverID string) *sync.Mutex {
	lock := s.locks[serverID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[serverID] = lock
	}
	return lock
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
	case strings.HasPrefix(id, idHostPrefix):
		category := strings.TrimPrefix(id, idHostPrefix)
		return category, hostCategoryName(category)
	case strings.HasPrefix(id, idGothamPrefix+"backup_file:"):
		return CategoryBackupFiles, strings.TrimPrefix(id, idGothamPrefix+"backup_file:")
	case strings.HasPrefix(id, idGothamPrefix+"history:"):
		return CategoryHistory, historyLabel(strings.TrimPrefix(id, idGothamPrefix+"history:"))
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
