package servers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Metric status values stored with the latest accepted observation. They mirror
// the proto enum but as stable JSON strings for consumers.
const (
	MetricStatusOK          = "ok"
	MetricStatusUnsupported = "unsupported"
	MetricStatusError       = "error"
	MetricStatusWarming     = "warming"
)

// Coverage values exposed to monitoring consumers.
const (
	CoverageFresh               = "fresh"
	CoveragePartial             = "partial"
	CoverageStale               = "stale"
	CoverageAgentUpdateRequired = "agent_update_required"
)

const (
	defaultObservationInterval  = 15 * time.Second
	observationFreshIntervals   = 3
	observationClockSkew        = 5 * time.Minute
	maxObservationFilesystems   = 32
	maxObservationContainers    = 512
	maxObservationSessionID     = 128
	maxObservationReason        = 64
	maxObservationPath          = 512
	maxObservationName          = 200
	minObservationInterval      = time.Second
	maxObservationInterval      = time.Hour
	expectedObservationInterval = defaultObservationInterval
)

type ObservedMetric struct {
	Status string  `json:"status"`
	Value  float64 `json:"value"`
	Reason string  `json:"reason,omitempty"`
}

type HostObservation struct {
	CPUUsage     ObservedMetric `json:"cpu_usage"`
	MemUsage     ObservedMetric `json:"mem_usage"`
	DiskUsage    ObservedMetric `json:"disk_usage"`
	NetRxBps     ObservedMetric `json:"net_rx_bps"`
	NetTxBps     ObservedMetric `json:"net_tx_bps"`
	DiskReadBps  ObservedMetric `json:"disk_read_bps"`
	DiskWriteBps ObservedMetric `json:"disk_write_bps"`
	TotalMem     uint64         `json:"total_mem"`
	TotalDisk    uint64         `json:"total_disk"`
}

type FilesystemObservation struct {
	Path           string `json:"path"`
	Purpose        string `json:"purpose"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	InodeStatus    string `json:"inode_status"`
	InodeReason    string `json:"inode_reason,omitempty"`
	TotalInodes    uint64 `json:"total_inodes"`
	FreeInodes     uint64 `json:"free_inodes"`
}

type ContainerObservation struct {
	ID             string     `json:"id"`
	State          string     `json:"state"`
	Health         string     `json:"health"`
	RestartCount   int64      `json:"restart_count"`
	OOMKilled      bool       `json:"oom_killed"`
	ExitCode       int32      `json:"exit_code"`
	RestartPolicy  string     `json:"restart_policy,omitempty"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Managed        bool       `json:"managed"`
	AppID          string     `json:"app_id,omitempty"`
	DeploymentID   string     `json:"deployment_id,omitempty"`
	DatabaseID     string     `json:"database_id,omitempty"`
	ComposeProject string     `json:"compose_project,omitempty"`
	ComposeService string     `json:"compose_service,omitempty"`
	Replica        string     `json:"replica,omitempty"`
	Role           string     `json:"role,omitempty"`
}

type ContainerInventory struct {
	Complete   bool                   `json:"complete"`
	Reason     string                 `json:"reason,omitempty"`
	Truncated  bool                   `json:"truncated"`
	Containers []ContainerObservation `json:"containers"`
}

type NodeObservation struct {
	ServerID         uuid.UUID
	SessionID        string
	Sequence         uint64
	ObservedAt       time.Time
	ReceivedAt       time.Time
	SamplingInterval time.Duration
	AgentVersion     string
	Host             HostObservation
	Filesystems      []FilesystemObservation
	Inventory        ContainerInventory
	InventoryAt      time.Time
}

type NodeObservationSnapshot struct {
	NodeObservation
	AgentUpdateRequired bool
	HostFresh           bool
	InventoryFresh      bool
	Coverage            string
}

func validateObservation(nodeID string, report *agentv1.ObservationReport, receivedAt time.Time) (NodeObservation, error) {
	if report == nil {
		return NodeObservation{}, fmt.Errorf("%w: observation report is required", ErrValidation)
	}
	if got := strings.TrimSpace(report.GetNodeId()); got == "" || got != nodeID {
		return NodeObservation{}, fmt.Errorf("%w: node_id does not match the authenticated node", ErrValidation)
	}
	sessionID := strings.TrimSpace(report.GetSessionId())
	if sessionID == "" || len(sessionID) > maxObservationSessionID {
		return NodeObservation{}, fmt.Errorf("%w: session_id is required and bounded", ErrValidation)
	}
	sequence := report.GetSequence()
	if sequence == 0 {
		return NodeObservation{}, fmt.Errorf("%w: sequence is required", ErrValidation)
	}
	pbObservedAt := report.GetObservedAt()
	if pbObservedAt == nil || pbObservedAt.AsTime().IsZero() {
		return NodeObservation{}, fmt.Errorf("%w: observed_at is required", ErrValidation)
	}
	observedAt := pbObservedAt.AsTime().UTC()
	if observedAt.After(receivedAt.Add(observationClockSkew)) {
		return NodeObservation{}, fmt.Errorf("%w: observed_at is in the future", ErrValidation)
	}
	// A fast clock may date a report up to the allowed skew ahead of receipt.
	// Clamp it to receipt time so a future date can never extend freshness or
	// block a later session restart.
	if observedAt.After(receivedAt) {
		observedAt = receivedAt.UTC()
	}
	interval := time.Duration(report.GetSamplingIntervalMs()) * time.Millisecond
	if interval < minObservationInterval || interval > maxObservationInterval {
		return NodeObservation{}, fmt.Errorf("%w: sampling_interval_ms is out of range", ErrValidation)
	}

	host, err := validateHost(report.GetHost())
	if err != nil {
		return NodeObservation{}, err
	}
	filesystems, err := validateFilesystems(report.GetFilesystems())
	if err != nil {
		return NodeObservation{}, err
	}
	inventory, err := validateInventory(report.GetInventory())
	if err != nil {
		return NodeObservation{}, err
	}

	return NodeObservation{
		SessionID:        sessionID,
		Sequence:         sequence,
		ObservedAt:       observedAt,
		ReceivedAt:       receivedAt.UTC(),
		SamplingInterval: interval,
		AgentVersion:     boundedText(report.GetAgentVersion(), maxObservationName),
		Host:             host,
		Filesystems:      filesystems,
		Inventory:        inventory,
		InventoryAt:      observedAt,
	}, nil
}

func validateHost(host *agentv1.HostObservation) (HostObservation, error) {
	if host == nil {
		return HostObservation{}, nil
	}
	values := map[string]struct {
		metric   *agentv1.Metric
		fraction bool
	}{
		"cpu_usage":      {host.GetCpuUsage(), true},
		"mem_usage":      {host.GetMemUsage(), true},
		"disk_usage":     {host.GetDiskUsage(), true},
		"net_rx_bps":     {host.GetNetRxBps(), false},
		"net_tx_bps":     {host.GetNetTxBps(), false},
		"disk_read_bps":  {host.GetDiskReadBps(), false},
		"disk_write_bps": {host.GetDiskWriteBps(), false},
	}
	validated := make(map[string]ObservedMetric, len(values))
	for name, value := range values {
		metric, err := validateMetric(name, value.metric, value.fraction)
		if err != nil {
			return HostObservation{}, err
		}
		validated[name] = metric
	}
	return HostObservation{
		CPUUsage:     validated["cpu_usage"],
		MemUsage:     validated["mem_usage"],
		DiskUsage:    validated["disk_usage"],
		NetRxBps:     validated["net_rx_bps"],
		NetTxBps:     validated["net_tx_bps"],
		DiskReadBps:  validated["disk_read_bps"],
		DiskWriteBps: validated["disk_write_bps"],
		TotalMem:     host.GetTotalMem(),
		TotalDisk:    host.GetTotalDisk(),
	}, nil
}

func validateMetric(name string, metric *agentv1.Metric, fraction bool) (ObservedMetric, error) {
	if metric == nil || metric.GetStatus() == agentv1.MetricStatus_METRIC_STATUS_UNSPECIFIED {
		return ObservedMetric{Status: MetricStatusError, Reason: "missing"}, nil
	}
	reason := boundedText(metric.GetReason(), maxObservationReason)
	switch metric.GetStatus() {
	case agentv1.MetricStatus_METRIC_STATUS_OK:
		value := metric.GetValue()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return ObservedMetric{}, fmt.Errorf("%w: %s is not finite", ErrValidation, name)
		}
		if fraction && (value < 0 || value > 1) {
			return ObservedMetric{}, fmt.Errorf("%w: %s must be a fraction in 0..1", ErrValidation, name)
		}
		if !fraction && value < 0 {
			return ObservedMetric{}, fmt.Errorf("%w: %s must not be negative", ErrValidation, name)
		}
		return ObservedMetric{Status: MetricStatusOK, Value: value, Reason: reason}, nil
	case agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED:
		return ObservedMetric{Status: MetricStatusUnsupported, Reason: reason}, nil
	case agentv1.MetricStatus_METRIC_STATUS_WARMING:
		return ObservedMetric{Status: MetricStatusWarming, Reason: reason}, nil
	default:
		return ObservedMetric{Status: MetricStatusError, Reason: reason}, nil
	}
}

func validateFilesystems(filesystems []*agentv1.FilesystemObservation) ([]FilesystemObservation, error) {
	if len(filesystems) > maxObservationFilesystems {
		return nil, fmt.Errorf("%w: too many filesystems", ErrValidation)
	}
	seen := make(map[string]bool, len(filesystems))
	out := make([]FilesystemObservation, 0, len(filesystems))
	for _, filesystem := range filesystems {
		if filesystem == nil {
			return nil, fmt.Errorf("%w: filesystem entry is required", ErrValidation)
		}
		path := strings.TrimSpace(filesystem.GetPath())
		if path == "" || len(path) > maxObservationPath {
			return nil, fmt.Errorf("%w: filesystem path is required and bounded", ErrValidation)
		}
		if seen[path] {
			return nil, fmt.Errorf("%w: duplicate filesystem path", ErrValidation)
		}
		seen[path] = true
		purpose := strings.TrimSpace(filesystem.GetPurpose())
		if !validFilesystemPurpose(purpose) {
			return nil, fmt.Errorf("%w: unknown filesystem purpose %q", ErrValidation, purpose)
		}
		status, err := validateFilesystemStatus(filesystem.GetStatus())
		if err != nil {
			return nil, err
		}
		inodeStatus, err := validateFilesystemStatus(filesystem.GetInodeStatus())
		if err != nil {
			return nil, err
		}
		total, available := filesystem.GetTotalBytes(), filesystem.GetAvailableBytes()
		if status == MetricStatusOK && available > total {
			return nil, fmt.Errorf("%w: available_bytes exceeds total_bytes", ErrValidation)
		}
		if status != MetricStatusOK {
			total, available = 0, 0
		}
		totalInodes, freeInodes := filesystem.GetTotalInodes(), filesystem.GetFreeInodes()
		if inodeStatus == MetricStatusOK && totalInodes == 0 {
			inodeStatus = MetricStatusUnsupported
		}
		if inodeStatus == MetricStatusOK && freeInodes > totalInodes {
			return nil, fmt.Errorf("%w: free_inodes exceeds total_inodes", ErrValidation)
		}
		if inodeStatus != MetricStatusOK {
			totalInodes, freeInodes = 0, 0
		}
		out = append(out, FilesystemObservation{
			Path:           path,
			Purpose:        purpose,
			Status:         status,
			Reason:         boundedText(filesystem.GetReason(), maxObservationReason),
			TotalBytes:     total,
			AvailableBytes: available,
			InodeStatus:    inodeStatus,
			InodeReason:    boundedText(filesystem.GetInodeReason(), maxObservationReason),
			TotalInodes:    totalInodes,
			FreeInodes:     freeInodes,
		})
	}
	return out, nil
}

func validFilesystemPurpose(purpose string) bool {
	switch purpose {
	case "root", "docker_data", "managed_storage", "backup":
		return true
	default:
		return false
	}
}

func validateFilesystemStatus(status agentv1.MetricStatus) (string, error) {
	switch status {
	case agentv1.MetricStatus_METRIC_STATUS_OK:
		return MetricStatusOK, nil
	case agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED:
		return MetricStatusUnsupported, nil
	case agentv1.MetricStatus_METRIC_STATUS_ERROR:
		return MetricStatusError, nil
	default:
		return "", fmt.Errorf("%w: invalid filesystem metric status", ErrValidation)
	}
}

func validateInventory(inventory *agentv1.ContainerInventory) (ContainerInventory, error) {
	if inventory == nil {
		return ContainerInventory{Reason: "missing", Containers: []ContainerObservation{}}, nil
	}
	if len(inventory.GetContainers()) > maxObservationContainers {
		return ContainerInventory{}, fmt.Errorf("%w: too many containers", ErrValidation)
	}
	complete := inventory.GetComplete() && !inventory.GetTruncated()
	seen := make(map[string]bool, len(inventory.GetContainers()))
	out := make([]ContainerObservation, 0, len(inventory.GetContainers()))
	for _, container := range inventory.GetContainers() {
		if container == nil {
			return ContainerInventory{}, fmt.Errorf("%w: container entry is required", ErrValidation)
		}
		id := strings.TrimSpace(container.GetId())
		if !validContainerID(id) {
			return ContainerInventory{}, fmt.Errorf("%w: invalid container id", ErrValidation)
		}
		if seen[id] {
			return ContainerInventory{}, fmt.Errorf("%w: duplicate container id", ErrValidation)
		}
		seen[id] = true
		if container.GetRestartCount() < 0 {
			return ContainerInventory{}, fmt.Errorf("%w: restart_count must not be negative", ErrValidation)
		}
		if err := validateOwnershipID(container.GetAppId()); err != nil {
			return ContainerInventory{}, err
		}
		if err := validateOwnershipID(container.GetDeploymentId()); err != nil {
			return ContainerInventory{}, err
		}
		if err := validateOwnershipID(container.GetDatabaseId()); err != nil {
			return ContainerInventory{}, err
		}
		project := strings.TrimSpace(container.GetComposeProject())
		if project != "" && !strings.HasPrefix(project, "gotham-") {
			return ContainerInventory{}, fmt.Errorf("%w: compose_project is not a Gotham project", ErrValidation)
		}
		out = append(out, ContainerObservation{
			ID:             id,
			State:          boundedText(container.GetState(), 32),
			Health:         boundedText(container.GetHealth(), 32),
			RestartCount:   container.GetRestartCount(),
			OOMKilled:      container.GetOomKilled(),
			ExitCode:       container.GetExitCode(),
			RestartPolicy:  boundedText(container.GetRestartPolicy(), 32),
			CreatedAt:      protoTime(container.GetCreatedAt()),
			StartedAt:      protoTime(container.GetStartedAt()),
			FinishedAt:     protoTime(container.GetFinishedAt()),
			Managed:        container.GetManaged(),
			AppID:          container.GetAppId(),
			DeploymentID:   container.GetDeploymentId(),
			DatabaseID:     container.GetDatabaseId(),
			ComposeProject: project,
			ComposeService: boundedText(container.GetComposeService(), maxObservationName),
			Replica:        boundedText(container.GetReplica(), 32),
			Role:           boundedText(container.GetRole(), maxObservationName),
		})
	}
	reason := boundedText(inventory.GetReason(), maxObservationReason)
	if !complete && reason == "" {
		reason = "incomplete"
	}
	if complete {
		reason = ""
	}
	return ContainerInventory{Complete: complete, Reason: reason, Truncated: inventory.GetTruncated(), Containers: out}, nil
}

func validContainerID(id string) bool {
	if len(id) < 12 || len(id) > 64 || len(id)%2 != 0 {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) >= 6
}

func validateOwnershipID(id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: ownership id is not a uuid", ErrValidation)
	}
	return nil
}

func protoTime(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	parsed := value.AsTime()
	if parsed.IsZero() {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	if !utf8.ValidString(value) {
		return ""
	}
	return value
}

func (s *ServerService) rejectForeignObservationOwnership(ctx context.Context, serverID pgtype.UUID, inventory ContainerInventory) error {
	seenApp := map[string]bool{}
	seenDatabase := map[string]bool{}
	applicationIDs := make([]pgtype.UUID, 0, len(inventory.Containers))
	databaseIDs := make([]pgtype.UUID, 0, len(inventory.Containers))
	for _, container := range inventory.Containers {
		if container.AppID != "" && !seenApp[container.AppID] {
			seenApp[container.AppID] = true
			if id, err := uuid.Parse(container.AppID); err == nil {
				applicationIDs = append(applicationIDs, pgUUID(id))
			}
		}
		if container.DatabaseID != "" && !seenDatabase[container.DatabaseID] {
			seenDatabase[container.DatabaseID] = true
			if id, err := uuid.Parse(container.DatabaseID); err == nil {
				databaseIDs = append(databaseIDs, pgUUID(id))
			}
		}
	}
	if len(applicationIDs) == 0 && len(databaseIDs) == 0 {
		return nil
	}
	foreign, err := s.store.ListForeignObservationOwnership(ctx, sqlc.ListForeignObservationOwnershipParams{
		ApplicationIds: applicationIDs,
		ServerID:       serverID,
		DatabaseIds:    databaseIDs,
	})
	if err != nil {
		return fmt.Errorf("check container ownership: %w", err)
	}
	if len(foreign) > 0 {
		return fmt.Errorf("%w: container ownership does not belong to this node", ErrValidation)
	}
	return nil
}

func (s *ServerService) RecordObservation(ctx context.Context, nodeID string, report *agentv1.ObservationReport, receivedAt time.Time) (*agentv1.ObservationAck, error) {
	if s.store == nil {
		return nil, errors.New("servers: store is not configured")
	}
	observation, err := validateObservation(nodeID, report, receivedAt)
	if err != nil {
		return nil, err
	}
	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lookup server by node id: %w", err)
	}
	observation.ServerID = uuid.UUID(row.ID.Bytes)

	if err := s.rejectForeignObservationOwnership(ctx, row.ID, observation.Inventory); err != nil {
		return nil, err
	}

	hostJSON, err := json.Marshal(observation.Host)
	if err != nil {
		return nil, fmt.Errorf("encode host observation: %w", err)
	}
	filesystemsJSON, err := json.Marshal(observation.Filesystems)
	if err != nil {
		return nil, fmt.Errorf("encode filesystem observation: %w", err)
	}
	inventoryJSON, err := json.Marshal(observation.Inventory)
	if err != nil {
		return nil, fmt.Errorf("encode inventory observation: %w", err)
	}

	stored, err := s.store.UpsertNodeObservation(ctx, sqlc.UpsertNodeObservationParams{
		ServerID:            row.ID,
		SessionID:           observation.SessionID,
		Sequence:            int64(observation.Sequence),
		ObservedAt:          pgtype.Timestamptz{Time: observation.ObservedAt, Valid: true},
		ReceivedAt:          pgtype.Timestamptz{Time: observation.ReceivedAt, Valid: true},
		SamplingIntervalMs:  observation.SamplingInterval.Milliseconds(),
		AgentVersion:        observation.AgentVersion,
		Host:                hostJSON,
		Filesystems:         filesystemsJSON,
		Inventory:           inventoryJSON,
		InventoryComplete:   observation.Inventory.Complete,
		InventoryReason:     observation.Inventory.Reason,
		InventoryObservedAt: pgtype.Timestamptz{Time: observation.InventoryAt, Valid: true},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("record observation: %w", err)
		}
		// A duplicate or obsolete report is acknowledged as not accepted; it
		// must never look like a fresh observation to the agent.
		current, lookupErr := s.store.GetNodeObservation(ctx, row.ID)
		if lookupErr != nil {
			return nil, fmt.Errorf("lookup current observation: %w", lookupErr)
		}
		return &agentv1.ObservationAck{
			ReceivedAt:         timestamppb.New(receivedAt.UTC()),
			AcceptedSequence:   uint64(current.Sequence),
			Accepted:           false,
			ExpectedIntervalMs: expectedObservationInterval.Milliseconds(),
		}, nil
	}

	s.logger.Debug("servers: observation accepted",
		"node_id", nodeID,
		"server_id", stored.ServerID.String(),
		"sequence", stored.Sequence,
		"inventory_complete", stored.InventoryComplete,
	)
	if s.pressure != nil {
		pressureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		s.pressure.RecordHostPressure(pressureCtx, uuid.UUID(row.ID.Bytes), row.Name, observation)
		cancel()
	}
	if s.runtime != nil {
		// Runtime detection follows the accepted generation. It gets its own
		// timeout and cannot turn a stored observation into a failed push.
		runtimeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		s.runtime.Observe(runtimeCtx, observation)
		cancel()
	}
	return &agentv1.ObservationAck{
		ReceivedAt:         timestamppb.New(receivedAt.UTC()),
		AcceptedSequence:   uint64(stored.Sequence),
		Accepted:           true,
		ExpectedIntervalMs: expectedObservationInterval.Milliseconds(),
	}, nil
}

func (s *ServerService) LatestObservation(ctx context.Context, serverID uuid.UUID) (NodeObservationSnapshot, error) {
	if s.store == nil {
		return NodeObservationSnapshot{}, errors.New("servers: store is not configured")
	}
	row, err := s.store.GetNodeObservation(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NodeObservationSnapshot{
				NodeObservation:     NodeObservation{ServerID: serverID},
				AgentUpdateRequired: true,
				Coverage:            CoverageAgentUpdateRequired,
			}, nil
		}
		return NodeObservationSnapshot{}, fmt.Errorf("get node observation: %w", err)
	}
	var host HostObservation
	if err := json.Unmarshal(row.Host, &host); err != nil {
		return NodeObservationSnapshot{}, fmt.Errorf("decode host observation: %w", err)
	}
	var filesystems []FilesystemObservation
	if err := json.Unmarshal(row.Filesystems, &filesystems); err != nil {
		return NodeObservationSnapshot{}, fmt.Errorf("decode filesystem observation: %w", err)
	}
	var inventory ContainerInventory
	if err := json.Unmarshal(row.Inventory, &inventory); err != nil {
		return NodeObservationSnapshot{}, fmt.Errorf("decode inventory observation: %w", err)
	}
	if inventory.Containers == nil {
		inventory.Containers = []ContainerObservation{}
	}

	interval := time.Duration(row.SamplingIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = expectedObservationInterval
	}
	// Freshness is bounded by the control plane's expected interval: a report
	// claiming a slower cadence cannot keep stale evidence fresh longer.
	freshInterval := interval
	if freshInterval > expectedObservationInterval {
		freshInterval = expectedObservationInterval
	}
	now := s.now()
	hostFresh := observationFresh(now, row.ObservedAt.Time, freshInterval)
	inventoryFresh := observationFresh(now, row.InventoryObservedAt.Time, freshInterval)
	coverage := CoverageStale
	switch {
	case hostFresh && inventoryFresh:
		coverage = CoverageFresh
	case hostFresh || inventoryFresh:
		coverage = CoveragePartial
	}
	snapshot := NodeObservationSnapshot{
		NodeObservation: NodeObservation{
			ServerID:         row.ServerID.Bytes,
			SessionID:        row.SessionID,
			Sequence:         uint64(row.Sequence),
			ObservedAt:       row.ObservedAt.Time.UTC(),
			ReceivedAt:       row.ReceivedAt.Time.UTC(),
			SamplingInterval: interval,
			AgentVersion:     row.AgentVersion,
			Host:             host,
			Filesystems:      filesystems,
			Inventory:        inventory,
			InventoryAt:      row.InventoryObservedAt.Time.UTC(),
		},
		HostFresh:      hostFresh,
		InventoryFresh: inventoryFresh,
		Coverage:       coverage,
	}
	return snapshot, nil
}

func observationFresh(now, observedAt time.Time, interval time.Duration) bool {
	if observedAt.IsZero() || interval <= 0 {
		return false
	}
	age := now.Sub(observedAt)
	if age < 0 || age > observationFreshIntervals*interval {
		return false
	}
	return true
}
