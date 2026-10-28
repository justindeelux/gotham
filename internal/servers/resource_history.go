package servers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Resource history ranges. Each range is served from one storage tier and has
// a bounded number of chart points.
const (
	ResourceRange1h  = "1h"
	ResourceRange24h = "24h"
	ResourceRange7d  = "7d"
	ResourceRange30d = "30d"

	ResourceTypeApplication = "application"
	ResourceTypeDatabase    = "database"

	ResourceCapabilityAvailable   = "available"
	ResourceCapabilityUnavailable = "unavailable"

	ResourceCoverageComplete = "complete"
	ResourceCoveragePartial  = "partial"
	ResourceCoverageUnknown  = "unknown"

	ResourceTierMinute = "minute"
	ResourceTierHour   = "hour"
)

const maxResourceSamplesPerReport = 512

type resourceRangeSpec struct {
	window time.Duration
	step   string
	tier   string
	bucket time.Duration
}

var resourceRanges = map[string]resourceRangeSpec{
	ResourceRange1h:  {window: time.Hour, step: "30s", tier: "raw", bucket: 30 * time.Second},
	ResourceRange24h: {window: 24 * time.Hour, step: "5m", tier: ResourceTierMinute, bucket: 5 * time.Minute},
	ResourceRange7d:  {window: 7 * 24 * time.Hour, step: "1h", tier: ResourceTierHour, bucket: time.Hour},
	ResourceRange30d: {window: 30 * 24 * time.Hour, step: "1h", tier: ResourceTierHour, bucket: time.Hour},
}

func ParseResourceRange(value string) (resourceRangeSpec, error) {
	spec, ok := resourceRanges[strings.TrimSpace(value)]
	if !ok {
		return resourceRangeSpec{}, fmt.Errorf("%w: range must be one of 1h, 24h, 7d or 30d", ErrValidation)
	}
	return spec, nil
}

// containerResourceSample is one validated numeric sample from the agent.
// A nil metric is unknown; it is never a zero measurement.
type containerResourceSample struct {
	ContainerID   string
	ObservedAt    time.Time
	CPUCores      *float64
	MemWorkingSet *int64
	MemLimit      *int64
	MemUnlimited  bool
	NetRxBps      *float64
	NetTxBps      *float64
	DiskReadBps   *float64
	DiskWriteBps  *float64
	hasAnyValue   bool
}

func validateContainerSamples(samples []*agentv1.ContainerResourceSample, observedAt, receivedAt time.Time) ([]containerResourceSample, error) {
	if len(samples) > maxResourceSamplesPerReport {
		return nil, fmt.Errorf("%w: too many container samples", ErrValidation)
	}
	seen := make(map[string]bool, len(samples))
	out := make([]containerResourceSample, 0, len(samples))
	for _, sample := range samples {
		if sample == nil {
			return nil, fmt.Errorf("%w: container sample is required", ErrValidation)
		}
		id := strings.TrimSpace(sample.GetId())
		if !validContainerID(id) {
			return nil, fmt.Errorf("%w: invalid container sample id", ErrValidation)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: duplicate container sample id", ErrValidation)
		}
		seen[id] = true

		sampleAt := observedAt
		if pb := sample.GetObservedAt(); pb != nil {
			at := pb.AsTime().UTC()
			if at.After(receivedAt.Add(observationClockSkew)) {
				return nil, fmt.Errorf("%w: container sample observed_at is in the future", ErrValidation)
			}
			if at.After(receivedAt) {
				at = receivedAt.UTC()
			}
			sampleAt = at
		}

		cpu, err := resourceMetric("cpu_cores", sample.GetCpuCores())
		if err != nil {
			return nil, err
		}
		mem, err := resourceMetric("mem_working_set_bytes", sample.GetMemWorkingSetBytes())
		if err != nil {
			return nil, err
		}
		limit, err := resourceMetric("mem_limit_bytes", sample.GetMemLimitBytes())
		if err != nil {
			return nil, err
		}
		netRx, err := resourceMetric("net_rx_bps", sample.GetNetRxBps())
		if err != nil {
			return nil, err
		}
		netTx, err := resourceMetric("net_tx_bps", sample.GetNetTxBps())
		if err != nil {
			return nil, err
		}
		diskRead, err := resourceMetric("disk_read_bps", sample.GetDiskReadBps())
		if err != nil {
			return nil, err
		}
		diskWrite, err := resourceMetric("disk_write_bps", sample.GetDiskWriteBps())
		if err != nil {
			return nil, err
		}

		validated := containerResourceSample{
			ContainerID:   id,
			ObservedAt:    sampleAt,
			CPUCores:      cpu,
			NetRxBps:      netRx,
			NetTxBps:      netTx,
			DiskReadBps:   diskRead,
			DiskWriteBps:  diskWrite,
			MemUnlimited:  sample.GetMemUnlimited(),
			MemWorkingSet: floatToInt64(mem),
		}
		// An unlimited container has no configured limit; the limit stays
		// unknown rather than becoming a 0-byte limit.
		if !validated.MemUnlimited {
			validated.MemLimit = floatToInt64(limit)
		}
		validated.hasAnyValue = validated.CPUCores != nil || validated.MemWorkingSet != nil ||
			validated.NetRxBps != nil || validated.NetTxBps != nil ||
			validated.DiskReadBps != nil || validated.DiskWriteBps != nil
		out = append(out, validated)
	}
	return out, nil
}

func resourceMetric(name string, metric *agentv1.Metric) (*float64, error) {
	if metric == nil || metric.GetStatus() != agentv1.MetricStatus_METRIC_STATUS_OK {
		return nil, nil
	}
	value := metric.GetValue()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("%w: %s is not finite", ErrValidation, name)
	}
	if value < 0 {
		return nil, fmt.Errorf("%w: %s must not be negative", ErrValidation, name)
	}
	return &value, nil
}

func floatToInt64(value *float64) *int64 {
	if value == nil {
		return nil
	}
	rounded := int64(math.Round(*value))
	return &rounded
}

type resourceOwnership struct {
	resourceType string
	resourceID   uuid.UUID
	service      string
	deploymentID string
	replica      string
}

func (s *ServerService) recordResourceSamples(ctx context.Context, serverID uuid.UUID, observation NodeObservation, samples []containerResourceSample) {
	if len(samples) == 0 {
		return
	}
	rows := buildResourceSampleRows(serverID, observation, samples)
	if len(rows) == 0 {
		return
	}
	rows = s.filterExistingResourceRows(ctx, rows)
	if len(rows) == 0 {
		return
	}
	if err := s.store.InsertResourceSamples(ctx, rows); err != nil {
		s.logger.Warn("servers: recording resource samples failed",
			"server_id", serverID.String(), "samples", len(rows), "error", err)
	}
}

func buildResourceSampleRows(serverID uuid.UUID, observation NodeObservation, samples []containerResourceSample) []sqlc.InsertResourceSamplesParams {
	byID := make(map[string]ContainerObservation, len(observation.Inventory.Containers))
	expected := make(map[string]int)
	for _, container := range observation.Inventory.Containers {
		byID[container.ID] = container
		if !container.Managed || container.State != "running" {
			continue
		}
		if key, ok := resourceGroupKey(container); ok {
			expected[key]++
		}
	}

	type candidate struct {
		sample    containerResourceSample
		ownership resourceOwnership
	}
	candidates := make([]candidate, 0, len(samples))
	observed := make(map[string]map[string]bool)
	for _, sample := range samples {
		container, ok := byID[sample.ContainerID]
		if !ok || !container.Managed || container.State != "running" {
			continue
		}
		if !sample.hasAnyValue {
			continue
		}
		ownership, ok := resourceOwnershipFor(container)
		if !ok {
			continue
		}
		key := resourceGroupKeyOf(ownership)
		if observed[key] == nil {
			observed[key] = make(map[string]bool)
		}
		observed[key][ownership.replica] = true
		candidates = append(candidates, candidate{sample: sample, ownership: ownership})
	}

	rows := make([]sqlc.InsertResourceSamplesParams, 0, len(candidates))
	for _, item := range candidates {
		key := resourceGroupKeyOf(item.ownership)
		expectedReplicas := expected[key]
		observedReplicas := len(observed[key])
		coveragePartial := !observation.Inventory.Complete || expectedReplicas > observedReplicas
		row := sqlc.InsertResourceSamplesParams{
			ServerID:           pgUUID(serverID),
			ResourceType:       item.ownership.resourceType,
			ResourceID:         pgUUID(item.ownership.resourceID),
			Service:            item.ownership.service,
			DeploymentID:       item.ownership.deploymentID,
			Replica:            item.ownership.replica,
			ContainerID:        item.sample.ContainerID,
			ObservedAt:         pgtype.Timestamptz{Time: item.sample.ObservedAt.UTC(), Valid: true},
			CpuCores:           item.sample.CPUCores,
			MemWorkingSetBytes: item.sample.MemWorkingSet,
			MemLimitBytes:      item.sample.MemLimit,
			MemUnlimited:       item.sample.MemUnlimited,
			NetRxBps:           item.sample.NetRxBps,
			NetTxBps:           item.sample.NetTxBps,
			DiskReadBps:        item.sample.DiskReadBps,
			DiskWriteBps:       item.sample.DiskWriteBps,
			ExpectedReplicas:   int32(expectedReplicas),
			ObservedReplicas:   int32(observedReplicas),
			CoveragePartial:    coveragePartial,
		}
		if item.ownership.resourceType == ResourceTypeApplication {
			row.ApplicationID = pgUUID(item.ownership.resourceID)
		} else {
			row.DatabaseID = pgUUID(item.ownership.resourceID)
		}
		rows = append(rows, row)
	}
	return rows
}

// filterExistingResourceRows drops a stale label whose application/database
// row is already gone, so one orphan container cannot fail the node's whole
// COPY. A missing lookup is treated as no rows rather than as permission to
// insert a foreign key that will fail.
func (s *ServerService) filterExistingResourceRows(ctx context.Context, rows []sqlc.InsertResourceSamplesParams) []sqlc.InsertResourceSamplesParams {
	appIDs := make([]pgtype.UUID, 0, len(rows))
	dbIDs := make([]pgtype.UUID, 0, len(rows))
	seenApp := make(map[pgtype.UUID]bool, len(rows))
	seenDB := make(map[pgtype.UUID]bool, len(rows))
	for _, row := range rows {
		if row.ApplicationID.Valid && !seenApp[row.ApplicationID] {
			seenApp[row.ApplicationID] = true
			appIDs = append(appIDs, row.ApplicationID)
		}
		if row.DatabaseID.Valid && !seenDB[row.DatabaseID] {
			seenDB[row.DatabaseID] = true
			dbIDs = append(dbIDs, row.DatabaseID)
		}
	}
	existing, err := s.store.ListExistingResourceOwners(ctx, sqlc.ListExistingResourceOwnersParams{
		ApplicationIds: appIDs,
		DatabaseIds:    dbIDs,
	})
	if err != nil {
		s.logger.Warn("servers: checking resource owners failed", "error", err)
		return nil
	}
	allowed := make(map[string]map[uuid.UUID]bool, 2)
	for _, row := range existing {
		if allowed[row.Kind] == nil {
			allowed[row.Kind] = make(map[uuid.UUID]bool)
		}
		allowed[row.Kind][uuid.UUID(row.ID.Bytes)] = true
	}
	return filterExistingResourceRows(rows, allowed)
}

func filterExistingResourceRows(rows []sqlc.InsertResourceSamplesParams, allowed map[string]map[uuid.UUID]bool) []sqlc.InsertResourceSamplesParams {
	out := make([]sqlc.InsertResourceSamplesParams, 0, len(rows))
	for _, row := range rows {
		id := uuid.UUID(row.ResourceID.Bytes)
		if allowed[row.ResourceType][id] {
			out = append(out, row)
		}
	}
	return out
}

func resourceOwnershipFor(container ContainerObservation) (resourceOwnership, bool) {
	if id, err := uuid.Parse(container.AppID); err == nil {
		return resourceOwnership{
			resourceType: ResourceTypeApplication,
			resourceID:   id,
			service:      container.ComposeService,
			deploymentID: container.DeploymentID,
			replica:      container.Replica,
		}, true
	}
	if id, err := uuid.Parse(container.DatabaseID); err == nil {
		return resourceOwnership{
			resourceType: ResourceTypeDatabase,
			resourceID:   id,
			service:      container.ComposeService,
			deploymentID: container.DeploymentID,
			replica:      container.Replica,
		}, true
	}
	return resourceOwnership{}, false
}

func resourceGroupKey(container ContainerObservation) (string, bool) {
	ownership, ok := resourceOwnershipFor(container)
	if !ok {
		return "", false
	}
	return resourceGroupKeyOf(ownership), true
}

func resourceGroupKeyOf(ownership resourceOwnership) string {
	return ownership.resourceType + "|" + ownership.resourceID.String() + "|" + ownership.service + "|" + ownership.deploymentID
}

// ResourceHistoryPoint is one chart bucket. A nil metric is unknown; the chart
// draws a gap there instead of a zero.
type ResourceHistoryPoint struct {
	Bucket             time.Time `json:"bucket"`
	CPUCores           *float64  `json:"cpu_cores"`
	MemWorkingSetBytes *float64  `json:"mem_working_set_bytes"`
	MemLimitBytes      int64     `json:"mem_limit_bytes"`
	MemUnlimited       bool      `json:"mem_unlimited"`
	NetRxBps           *float64  `json:"net_rx_bps"`
	NetTxBps           *float64  `json:"net_tx_bps"`
	DiskReadBps        *float64  `json:"disk_read_bps"`
	DiskWriteBps       *float64  `json:"disk_write_bps"`
	LatestAt           time.Time `json:"latest_at"`
	Replicas           int       `json:"replicas"`
	ExpectedReplicas   int       `json:"expected_replicas"`
	Coverage           string    `json:"coverage"`
}

type ResourceHistorySeries struct {
	Service      string                 `json:"service"`
	DeploymentID string                 `json:"deployment_id"`
	Points       []ResourceHistoryPoint `json:"points"`
}

type ResourceDeploymentMarker struct {
	At           time.Time `json:"at"`
	DeploymentID string    `json:"deployment_id"`
	Service      string    `json:"service"`
}

type ResourceHistory struct {
	Range      string                     `json:"range"`
	Step       string                     `json:"step"`
	From       time.Time                  `json:"from"`
	To         time.Time                  `json:"to"`
	Capability string                     `json:"capability"`
	Coverage   string                     `json:"coverage"`
	Series     []ResourceHistorySeries    `json:"series"`
	Markers    []ResourceDeploymentMarker `json:"markers"`
}

func (s *ServerService) ResourceHistory(ctx context.Context, resourceType string, resourceID uuid.UUID, rangeName, service string) (ResourceHistory, error) {
	if s.store == nil {
		return ResourceHistory{}, errors.New("servers: store is not configured")
	}
	if resourceType != ResourceTypeApplication && resourceType != ResourceTypeDatabase {
		return ResourceHistory{}, fmt.Errorf("%w: unknown resource type", ErrValidation)
	}
	spec, err := ParseResourceRange(rangeName)
	if err != nil {
		return ResourceHistory{}, err
	}
	service = strings.TrimSpace(service)
	if len(service) > maxObservationName {
		return ResourceHistory{}, fmt.Errorf("%w: service is too long", ErrValidation)
	}

	to := s.now().UTC()
	from := to.Add(-spec.window)
	params := sqlc.ListResourceSamplesParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
		RangeStart:   pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:     pgtype.Timestamptz{Time: to, Valid: true},
	}
	if service != "" {
		params.ServiceFilter = &service
	}

	seriesMap := make(map[string]*ResourceHistorySeries)
	if spec.tier == "raw" {
		rows, err := s.store.ListResourceSamples(ctx, params)
		if err != nil {
			return ResourceHistory{}, fmt.Errorf("list resource samples: %w", err)
		}
		buildRawResourceSeries(seriesMap, rows, spec.bucket)
	} else {
		rollupParams := sqlc.ListResourceRollupPointsParams{
			ResourceType:  resourceType,
			ResourceID:    pgUUID(resourceID),
			Tier:          spec.tier,
			RangeStart:    pgtype.Timestamptz{Time: from, Valid: true},
			RangeEnd:      pgtype.Timestamptz{Time: to, Valid: true},
			ServiceFilter: params.ServiceFilter,
		}
		rows, err := s.store.ListResourceRollupPoints(ctx, rollupParams)
		if err != nil {
			return ResourceHistory{}, fmt.Errorf("list resource rollups: %w", err)
		}
		buildRollupResourceSeries(seriesMap, rows, spec.bucket)
	}

	history := ResourceHistory{
		Range:      rangeName,
		Step:       spec.step,
		From:       from,
		To:         to,
		Capability: ResourceCapabilityUnavailable,
		Coverage:   ResourceCoverageUnknown,
		Series:     make([]ResourceHistorySeries, 0, len(seriesMap)),
		Markers:    []ResourceDeploymentMarker{},
	}
	if len(seriesMap) > 0 {
		history.Capability = ResourceCapabilityAvailable
		history.Coverage = ResourceCoverageComplete
	} else {
		exists, err := s.store.ResourceHistoryExists(ctx, resourceType, pgUUID(resourceID))
		if err != nil {
			return ResourceHistory{}, fmt.Errorf("check resource history: %w", err)
		}
		if exists {
			history.Capability = ResourceCapabilityAvailable
		}
	}
	for _, series := range seriesMap {
		sort.Slice(series.Points, func(i, j int) bool {
			return series.Points[i].Bucket.Before(series.Points[j].Bucket)
		})
		for _, point := range series.Points {
			if point.Coverage == ResourceCoveragePartial {
				history.Coverage = ResourceCoveragePartial
			}
		}
		history.Series = append(history.Series, *series)
	}
	sort.Slice(history.Series, func(i, j int) bool {
		if history.Series[i].Service != history.Series[j].Service {
			return history.Series[i].Service < history.Series[j].Service
		}
		return history.Series[i].DeploymentID < history.Series[j].DeploymentID
	})
	history.Markers = deploymentMarkers(history.Series)
	return history, nil
}

type resourceBucketBuilder struct {
	latestAt         time.Time
	replicas         int
	replicaSet       map[string]bool
	metricReplicas   map[string]int
	expectedReplicas int
	coveragePartial  bool
	memLimit         int64
	memUnlimited     bool
	values           map[string]map[string]float64 // metric -> replica -> value
}

func seriesFor(seriesMap map[string]*ResourceHistorySeries, service, deploymentID string) *ResourceHistorySeries {
	key := service + "|" + deploymentID
	series, ok := seriesMap[key]
	if !ok {
		series = &ResourceHistorySeries{Service: service, DeploymentID: deploymentID, Points: []ResourceHistoryPoint{}}
		seriesMap[key] = series
	}
	return series
}

func buildRawResourceSeries(seriesMap map[string]*ResourceHistorySeries, rows []sqlc.ListResourceSamplesRow, bucket time.Duration) {
	type pointKey struct {
		series string
		bucket time.Time
	}
	builders := make(map[pointKey]*resourceBucketBuilder)
	for _, row := range rows {
		series := seriesFor(seriesMap, row.Service, row.DeploymentID)
		at := row.ObservedAt.Time.UTC().Truncate(bucket)
		key := pointKey{series: series.Service + "|" + series.DeploymentID, bucket: at}
		builder, ok := builders[key]
		if !ok {
			builder = &resourceBucketBuilder{
				values:     make(map[string]map[string]float64),
				replicaSet: make(map[string]bool),
			}
			builders[key] = builder
		}
		if row.ObservedAt.Valid && row.ObservedAt.Time.After(builder.latestAt) {
			builder.latestAt = row.ObservedAt.Time.UTC()
		}
		builder.replicaSet[row.Replica] = true
		if int(row.ExpectedReplicas) > builder.expectedReplicas {
			builder.expectedReplicas = int(row.ExpectedReplicas)
		}
		if row.CoveragePartial {
			builder.coveragePartial = true
		}
		if row.MemUnlimited {
			builder.memUnlimited = true
		}
		if row.MemLimitBytes != nil && *row.MemLimitBytes > builder.memLimit {
			builder.memLimit = *row.MemLimitBytes
		}
		replica := row.Replica
		addRawMetric(builder, "cpu_cores", replica, row.CpuCores)
		if row.MemWorkingSetBytes != nil {
			builderAdd(builder, "mem_working_set_bytes", replica, float64(*row.MemWorkingSetBytes))
		}
		addRawMetric(builder, "net_rx_bps", replica, row.NetRxBps)
		addRawMetric(builder, "net_tx_bps", replica, row.NetTxBps)
		addRawMetric(builder, "disk_read_bps", replica, row.DiskReadBps)
		addRawMetric(builder, "disk_write_bps", replica, row.DiskWriteBps)
	}
	for key, builder := range builders {
		builder.metricReplicas = make(map[string]int, len(builder.values))
		for metric, values := range builder.values {
			builder.metricReplicas[metric] = len(values)
		}
		series := seriesMap[key.series]
		series.Points = append(series.Points, resourcePoint(key.bucket, builder))
	}
}

func addRawMetric(builder *resourceBucketBuilder, metric, replica string, value *float64) {
	if value == nil {
		return
	}
	builderAdd(builder, metric, replica, *value)
}

func builderAdd(builder *resourceBucketBuilder, metric, replica string, value float64) {
	if builder.values[metric] == nil {
		builder.values[metric] = make(map[string]float64)
	}
	builder.values[metric][replica] = value
}

func resourcePoint(at time.Time, builder *resourceBucketBuilder) ResourceHistoryPoint {
	if len(builder.replicaSet) > builder.replicas {
		builder.replicas = len(builder.replicaSet)
	}
	point := ResourceHistoryPoint{
		Bucket:           at,
		LatestAt:         builder.latestAt,
		Replicas:         builder.replicas,
		ExpectedReplicas: builder.expectedReplicas,
		MemLimitBytes:    builder.memLimit,
		MemUnlimited:     builder.memUnlimited,
		Coverage:         ResourceCoverageComplete,
	}
	if builder.coveragePartial || builder.expectedReplicas > builder.replicas {
		point.Coverage = ResourceCoveragePartial
	}
	// A metric missing one replica's sample is a partial metric even when the
	// point as a whole has every replica (for example the first CPU sample
	// after a restart next to a measured memory value).
	for _, replicas := range builder.metricReplicas {
		if replicas > 0 && replicas < builder.replicas {
			point.Coverage = ResourceCoveragePartial
			break
		}
	}
	point.CPUCores = replicaSum(builder.values["cpu_cores"])
	point.NetRxBps = replicaSum(builder.values["net_rx_bps"])
	point.NetTxBps = replicaSum(builder.values["net_tx_bps"])
	point.DiskReadBps = replicaSum(builder.values["disk_read_bps"])
	point.DiskWriteBps = replicaSum(builder.values["disk_write_bps"])
	point.MemWorkingSetBytes = replicaSum(builder.values["mem_working_set_bytes"])
	return point
}

// replicaSum keeps a service's total meaningful: each replica contributes its
// own measurement, so two 0.2-core replicas report 0.4 cores.
func replicaSum(values map[string]float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return &total
}

func buildRollupResourceSeries(seriesMap map[string]*ResourceHistorySeries, rows []sqlc.ListResourceRollupPointsRow, bucket time.Duration) {
	type accumulator struct {
		samples          int64
		replicas         int
		expectedReplicas int
		coveragePartial  bool
		valueSum         float64
		memLimit         int64
		memUnlimited     bool
	}
	type pointKey struct {
		series string
		bucket time.Time
	}
	type metricKey struct {
		point  pointKey
		metric string
	}
	builders := make(map[pointKey]*resourceBucketBuilder)
	accumulators := make(map[metricKey]*accumulator)

	for _, row := range rows {
		series := seriesFor(seriesMap, row.Service, row.DeploymentID)
		at := row.Bucket.Time.UTC().Truncate(bucket)
		key := pointKey{series: series.Service + "|" + series.DeploymentID, bucket: at}
		builder, ok := builders[key]
		if !ok {
			builder = &resourceBucketBuilder{
				values:         make(map[string]map[string]float64),
				metricReplicas: make(map[string]int),
			}
			builders[key] = builder
		}
		if row.LatestAt.Valid && row.LatestAt.Time.After(builder.latestAt) {
			builder.latestAt = row.LatestAt.Time.UTC()
		}
		if int(row.Replicas) > builder.replicas {
			builder.replicas = int(row.Replicas)
		}
		if int(row.ExpectedReplicas) > builder.expectedReplicas {
			builder.expectedReplicas = int(row.ExpectedReplicas)
		}
		if row.CoveragePartial {
			builder.coveragePartial = true
		}
		if row.MemUnlimited {
			builder.memUnlimited = true
		}
		if row.MemLimitBytes > builder.memLimit {
			builder.memLimit = row.MemLimitBytes
		}

		ak := metricKey{point: key, metric: row.Metric}
		acc, ok := accumulators[ak]
		if !ok {
			acc = &accumulator{}
			accumulators[ak] = acc
		}
		acc.samples += row.Samples
		if int(row.Replicas) > acc.replicas {
			acc.replicas = int(row.Replicas)
		}
		if int(row.ExpectedReplicas) > acc.expectedReplicas {
			acc.expectedReplicas = int(row.ExpectedReplicas)
		}
		acc.coveragePartial = acc.coveragePartial || row.CoveragePartial
		acc.valueSum += row.ValueSum
		if row.MemLimitBytes > acc.memLimit {
			acc.memLimit = row.MemLimitBytes
		}
		acc.memUnlimited = acc.memUnlimited || row.MemUnlimited
	}

	for ak, acc := range accumulators {
		builder := builders[ak.point]
		if builder == nil || acc.replicas == 0 {
			continue
		}
		// value_sum covers every sample in the bucket; dividing by the sample
		// count gives the per-replica average, and scaling by replicas keeps
		// concurrent replicas additive. A partially covered bucket says so.
		value := acc.valueSum / float64(acc.samples) * float64(acc.replicas)
		builderAdd(builder, ak.metric, "rollup", value)
		if acc.replicas > builder.metricReplicas[ak.metric] {
			builder.metricReplicas[ak.metric] = acc.replicas
		}
	}
	for key, builder := range builders {
		series := seriesMap[key.series]
		series.Points = append(series.Points, resourcePoint(key.bucket, builder))
	}
}

func deploymentMarkers(series []ResourceHistorySeries) []ResourceDeploymentMarker {
	first := make(map[string]ResourceDeploymentMarker)
	for _, item := range series {
		if item.DeploymentID == "" || len(item.Points) == 0 {
			continue
		}
		at := item.Points[0].Bucket
		existing, ok := first[item.DeploymentID]
		if ok && !at.Before(existing.At) {
			continue
		}
		first[item.DeploymentID] = ResourceDeploymentMarker{
			At:           at,
			DeploymentID: item.DeploymentID,
			Service:      item.Service,
		}
	}
	markers := make([]ResourceDeploymentMarker, 0, len(first))
	for _, marker := range first {
		markers = append(markers, marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		if !markers[i].At.Equal(markers[j].At) {
			return markers[i].At.Before(markers[j].At)
		}
		return markers[i].DeploymentID < markers[j].DeploymentID
	})
	return markers
}
