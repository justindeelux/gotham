package servers

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func okMetric(value float64) *agentv1.Metric {
	return &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_OK, Value: value}
}

func warmingMetric() *agentv1.Metric {
	return &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_WARMING}
}

func containerID(seed byte) string {
	id := make([]byte, 24)
	for index := range id {
		id[index] = "0123456789abcdef"[int(seed+byte(index))%16]
	}
	return string(id)
}

func TestParseResourceRange(t *testing.T) {
	for _, name := range []string{ResourceRange1h, ResourceRange24h, ResourceRange7d, ResourceRange30d} {
		spec, err := ParseResourceRange(name)
		if err != nil {
			t.Fatalf("range %s: %v", name, err)
		}
		if spec.window <= 0 || spec.bucket <= 0 || spec.step == "" {
			t.Errorf("range %s has an incomplete spec: %+v", name, spec)
		}
	}
	if _, err := ParseResourceRange("2h"); !errors.Is(err, ErrValidation) {
		t.Errorf("unknown range error = %v; want validation", err)
	}
}

func TestValidateContainerSamplesKeepsUnknownMetricsUnknown(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	samples, err := validateContainerSamples([]*agentv1.ContainerResourceSample{{
		Id:                 containerID(1),
		ObservedAt:         timestamppb.New(now),
		CpuCores:           warmingMetric(),
		MemWorkingSetBytes: okMetric(1024),
		MemLimitBytes:      okMetric(4096),
		NetRxBps:           warmingMetric(),
		NetTxBps:           warmingMetric(),
		DiskReadBps:        warmingMetric(),
		DiskWriteBps:       warmingMetric(),
	}}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d; want 1", len(samples))
	}
	got := samples[0]
	if got.CPUCores != nil || got.NetRxBps != nil || got.NetTxBps != nil || got.DiskReadBps != nil || got.DiskWriteBps != nil {
		t.Error("warming metrics must stay unknown, not zero")
	}
	if got.MemWorkingSet == nil || *got.MemWorkingSet != 1024 {
		t.Errorf("working set = %v; want 1024", got.MemWorkingSet)
	}
	if got.MemLimit == nil || *got.MemLimit != 4096 {
		t.Errorf("limit = %v; want 4096", got.MemLimit)
	}
}

func TestValidateContainerSamplesRejectsNegativeAndDuplicate(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	if _, err := validateContainerSamples([]*agentv1.ContainerResourceSample{{
		Id:       containerID(2),
		CpuCores: okMetric(-1),
	}}, now, now); !errors.Is(err, ErrValidation) {
		t.Errorf("negative CPU error = %v; want validation", err)
	}
	duplicate := containerID(3)
	if _, err := validateContainerSamples([]*agentv1.ContainerResourceSample{
		{Id: duplicate},
		{Id: duplicate},
	}, now, now); !errors.Is(err, ErrValidation) {
		t.Errorf("duplicate sample error = %v; want validation", err)
	}
	if _, err := validateContainerSamples([]*agentv1.ContainerResourceSample{{
		Id:         containerID(4),
		ObservedAt: timestamppb.New(now.Add(time.Hour)),
	}}, now, now); !errors.Is(err, ErrValidation) {
		t.Errorf("future sample error = %v; want validation", err)
	}
}

func TestBuildResourceSampleRowsMarksPartialReplicaCoverage(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	at := time.Unix(1000, 0).UTC()
	first := containerID(5)
	second := containerID(6)
	observation := NodeObservation{
		Inventory: ContainerInventory{
			Complete: true,
			Containers: []ContainerObservation{
				{ID: first, Managed: true, State: "running", AppID: appID.String(), ComposeService: "web", DeploymentID: "d1", Replica: "1"},
				{ID: second, Managed: true, State: "running", AppID: appID.String(), ComposeService: "web", DeploymentID: "d1", Replica: "2"},
			},
		},
	}
	rows := buildResourceSampleRows(serverID, observation, []containerResourceSample{{
		ContainerID:   first,
		ObservedAt:    at,
		MemWorkingSet: int64Ptr(512),
		hasAnyValue:   true,
	}})
	if len(rows) != 1 {
		t.Fatalf("rows = %d; want 1", len(rows))
	}
	if rows[0].ExpectedReplicas != 2 || rows[0].ObservedReplicas != 1 {
		t.Errorf("coverage = %d/%d; want 1/2", rows[0].ObservedReplicas, rows[0].ExpectedReplicas)
	}
	if !rows[0].CoveragePartial {
		t.Error("missing replica must be marked partial")
	}
	if rows[0].ResourceType != ResourceTypeApplication || rows[0].ApplicationID.Bytes != appID {
		t.Errorf("ownership = %s %v", rows[0].ResourceType, rows[0].ApplicationID)
	}
}

func TestBuildResourceSampleRowsSumsCompleteReplicas(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	at := time.Unix(1000, 0).UTC()
	containers := make([]ContainerObservation, 0, 2)
	samples := make([]containerResourceSample, 0, 2)
	for index := 0; index < 2; index++ {
		id := containerID(byte(10 + index))
		containers = append(containers, ContainerObservation{
			ID: id, Managed: true, State: "running", AppID: appID.String(),
			ComposeService: "web", DeploymentID: "d1", Replica: string(rune('1' + index)),
		})
		samples = append(samples, containerResourceSample{
			ContainerID: id, ObservedAt: at, CPUCores: floatPtr(0.2), hasAnyValue: true,
		})
	}
	rows := buildResourceSampleRows(serverID, NodeObservation{
		Inventory: ContainerInventory{Complete: true, Containers: containers},
	}, samples)
	if len(rows) != 2 {
		t.Fatalf("rows = %d; want 2", len(rows))
	}
	for _, row := range rows {
		if row.CoveragePartial {
			t.Error("complete replica coverage marked partial")
		}
	}
}

func TestResourceSeriesSumsReplicasAndKeepsGaps(t *testing.T) {
	series := make(map[string]*ResourceHistorySeries)
	at := time.Unix(1000, 0).UTC().Truncate(30 * time.Second)
	cpuA := 0.2
	cpuB := 0.3
	memA := int64(1000)
	memB := int64(2000)
	rows := []sqlc.ListResourceSamplesRow{
		{ObservedAt: pgTime(at), Service: "web", DeploymentID: "d1", Replica: "1", CpuCores: &cpuA, MemWorkingSetBytes: &memA},
		{ObservedAt: pgTime(at), Service: "web", DeploymentID: "d1", Replica: "2", CpuCores: &cpuB, MemWorkingSetBytes: &memB},
	}
	buildRawResourceSeries(series, rows, 30*time.Second)
	points := series["web|d1"].Points
	if len(points) != 1 {
		t.Fatalf("points = %d; want 1", len(points))
	}
	if points[0].CPUCores == nil || !almostEquals(*points[0].CPUCores, 0.5) {
		t.Errorf("CPU = %v; want the 0.5-core replica total", points[0].CPUCores)
	}
	if points[0].MemWorkingSetBytes == nil || !almostEquals(*points[0].MemWorkingSetBytes, 3000) {
		t.Errorf("memory = %v; want the 3000-byte replica total", points[0].MemWorkingSetBytes)
	}
	if points[0].NetRxBps != nil {
		t.Errorf("missing metric = %v; want a gap", *points[0].NetRxBps)
	}
}

func TestResourceRollupScalesByReplicas(t *testing.T) {
	series := make(map[string]*ResourceHistorySeries)
	at := time.Unix(1000, 0).UTC().Truncate(5 * time.Minute)
	rows := []sqlc.ListResourceRollupPointsRow{{
		Bucket:           pgTime(at),
		Service:          "web",
		DeploymentID:     "d1",
		Metric:           "cpu_cores",
		Samples:          4,
		Replicas:         2,
		ExpectedReplicas: 2,
		ValueSum:         1.0,
		ValueMin:         0.2,
		ValueMax:         0.3,
		LatestAt:         pgTime(at.Add(20 * time.Second)),
	}}
	buildRollupResourceSeries(series, rows, 5*time.Minute)
	points := series["web|d1"].Points
	if len(points) != 1 {
		t.Fatalf("points = %d; want 1", len(points))
	}
	// 4 samples summing 1.0 core = 0.25 per sample; two replicas = 0.5 cores.
	if points[0].CPUCores == nil || !almostEquals(*points[0].CPUCores, 0.5) {
		t.Errorf("CPU = %v; want 0.5", points[0].CPUCores)
	}
	if points[0].Coverage != ResourceCoverageComplete {
		t.Errorf("coverage = %s; want complete", points[0].Coverage)
	}
}

func TestDeploymentMarkersUseFirstBucket(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	series := []ResourceHistorySeries{
		{Service: "web", DeploymentID: "old", Points: []ResourceHistoryPoint{{Bucket: base}}},
		{Service: "web", DeploymentID: "new", Points: []ResourceHistoryPoint{{Bucket: base.Add(time.Hour)}}},
		{Service: "api", DeploymentID: "old", Points: []ResourceHistoryPoint{{Bucket: base.Add(30 * time.Minute)}}},
	}
	markers := deploymentMarkers(series)
	if len(markers) != 2 {
		t.Fatalf("markers = %d; want one per deployment", len(markers))
	}
	if markers[0].DeploymentID != "old" || !markers[0].At.Equal(base) {
		t.Errorf("first marker = %+v", markers[0])
	}
}

func pgTime(value time.Time) (out pgtype.Timestamptz) {
	out.Time = value
	out.Valid = true
	return out
}

func floatPtr(value float64) *float64 { return &value }

func int64Ptr(value int64) *int64 { return &value }

func almostEquals(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestFilterExistingResourceRowsDropsStaleLabels(t *testing.T) {
	valid := uuid.New()
	stale := uuid.New()
	rows := []sqlc.InsertResourceSamplesParams{
		{ResourceType: ResourceTypeApplication, ResourceID: pgUUID(valid), ApplicationID: pgUUID(valid)},
		{ResourceType: ResourceTypeApplication, ResourceID: pgUUID(stale), ApplicationID: pgUUID(stale)},
	}
	filtered := filterExistingResourceRows(rows, map[string]map[uuid.UUID]bool{
		ResourceTypeApplication: {valid: true},
	})
	if len(filtered) != 1 || filtered[0].ResourceID.Bytes != valid {
		t.Fatalf("filtered = %+v; want only the live application", filtered)
	}
}

func TestResourcePointMarksPerMetricReplicaGapsPartial(t *testing.T) {
	series := make(map[string]*ResourceHistorySeries)
	at := time.Unix(1000, 0).UTC().Truncate(30 * time.Second)
	cpu := 0.2
	memA := int64(1000)
	memB := int64(2000)
	rows := []sqlc.ListResourceSamplesRow{
		{ObservedAt: pgTime(at), Service: "web", DeploymentID: "d1", Replica: "1", CpuCores: &cpu, MemWorkingSetBytes: &memA},
		{ObservedAt: pgTime(at), Service: "web", DeploymentID: "d1", Replica: "2", MemWorkingSetBytes: &memB},
	}
	buildRawResourceSeries(series, rows, 30*time.Second)
	point := series["web|d1"].Points[0]
	if point.Coverage != ResourceCoveragePartial {
		t.Errorf("coverage = %s; want partial because CPU has only one replica", point.Coverage)
	}
	// The CPU value is one replica's measurement, not scaled to the two-replica
	// point, even though coverage says the metric is partial.
	if point.CPUCores == nil || !almostEquals(*point.CPUCores, 0.2) {
		t.Errorf("CPU = %v; want the one measured replica value", point.CPUCores)
	}
}

func TestRollupDoesNotScaleOneReplicaByThePointReplicaCount(t *testing.T) {
	series := make(map[string]*ResourceHistorySeries)
	at := time.Unix(1000, 0).UTC().Truncate(5 * time.Minute)
	rows := []sqlc.ListResourceRollupPointsRow{
		{Bucket: pgTime(at), Service: "web", DeploymentID: "d1", Metric: "cpu_cores", Samples: 1, Replicas: 1, ExpectedReplicas: 2, ValueSum: 0.2, ValueMin: 0.2, ValueMax: 0.2, LatestAt: pgTime(at)},
		{Bucket: pgTime(at), Service: "web", DeploymentID: "d1", Metric: "mem_working_set_bytes", Samples: 2, Replicas: 2, ExpectedReplicas: 2, ValueSum: 3000, ValueMin: 1000, ValueMax: 2000, LatestAt: pgTime(at)},
	}
	buildRollupResourceSeries(series, rows, 5*time.Minute)
	point := series["web|d1"].Points[0]
	if point.CPUCores == nil || !almostEquals(*point.CPUCores, 0.2) {
		t.Errorf("CPU = %v; want 0.2 from the single covered replica", point.CPUCores)
	}
	if point.Coverage != ResourceCoveragePartial {
		t.Errorf("coverage = %s; want partial", point.Coverage)
	}
}
