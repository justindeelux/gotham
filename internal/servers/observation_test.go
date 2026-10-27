package servers

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func observationMetric(value float64) *agentv1.Metric {
	return &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_OK, Value: value}
}

func validObservationReport(nodeID string) *agentv1.ObservationReport {
	containerID := strings.Repeat("a", 64)
	return &agentv1.ObservationReport{
		NodeId:             nodeID,
		SessionId:          "session-1",
		Sequence:           1,
		ObservedAt:         timestamppb.New(time.Unix(1_700_000_000, 0)),
		SamplingIntervalMs: 15_000,
		Host: &agentv1.HostObservation{
			CpuUsage:     observationMetric(0.25),
			MemUsage:     observationMetric(0.5),
			DiskUsage:    observationMetric(0.75),
			NetRxBps:     observationMetric(1024),
			NetTxBps:     observationMetric(2048),
			DiskReadBps:  observationMetric(4096),
			DiskWriteBps: observationMetric(8192),
			TotalMem:     8 << 30,
			TotalDisk:    100 << 30,
		},
		Filesystems: []*agentv1.FilesystemObservation{{
			Path:           "/",
			Purpose:        "root",
			Status:         agentv1.MetricStatus_METRIC_STATUS_OK,
			TotalBytes:     100,
			AvailableBytes: 40,
			InodeStatus:    agentv1.MetricStatus_METRIC_STATUS_OK,
			TotalInodes:    10,
			FreeInodes:     4,
		}},
		Inventory: &agentv1.ContainerInventory{
			Complete: true,
			Containers: []*agentv1.ContainerObservation{{
				Id:           containerID,
				State:        "running",
				Health:       "healthy",
				AppId:        uuid.NewString(),
				OomKilled:    true,
				CreatedAt:    timestamppb.New(time.Unix(1_700_000_000, 0)),
				StartedAt:    timestamppb.New(time.Unix(1_700_000_100, 0)),
				RestartCount: 2,
			}},
		},
	}
}

func TestValidateObservationAcceptsValidReport(t *testing.T) {
	received := time.Unix(1_700_000_010, 0)
	observation, err := validateObservation("node-1", validObservationReport("node-1"), received)
	if err != nil {
		t.Fatalf("validateObservation: %v", err)
	}
	if observation.SessionID != "session-1" || observation.Sequence != 1 {
		t.Errorf("session/sequence = %q/%d", observation.SessionID, observation.Sequence)
	}
	if observation.Host.CPUUsage.Status != MetricStatusOK || observation.Host.CPUUsage.Value != 0.25 {
		t.Errorf("cpu = %+v; want ok 0.25", observation.Host.CPUUsage)
	}
	if len(observation.Filesystems) != 1 || observation.Filesystems[0].Purpose != "root" {
		t.Errorf("filesystems = %+v", observation.Filesystems)
	}
	if !observation.Inventory.Complete || len(observation.Inventory.Containers) != 1 {
		t.Errorf("inventory = %+v", observation.Inventory)
	}
	if !observation.Inventory.Containers[0].OOMKilled || observation.Inventory.Containers[0].RestartCount != 2 {
		t.Errorf("container evidence = %+v", observation.Inventory.Containers[0])
	}
}

func TestValidateObservationRejectsInvalidReports(t *testing.T) {
	received := time.Unix(1_700_000_010, 0)
	tests := map[string]func(*agentv1.ObservationReport){
		"cross-node claim": func(report *agentv1.ObservationReport) { report.NodeId = "node-2" },
		"missing node":     func(report *agentv1.ObservationReport) { report.NodeId = "" },
		"missing session":  func(report *agentv1.ObservationReport) { report.SessionId = "" },
		"missing sequence": func(report *agentv1.ObservationReport) { report.Sequence = 0 },
		"missing time":     func(report *agentv1.ObservationReport) { report.ObservedAt = nil },
		"future time": func(report *agentv1.ObservationReport) {
			report.ObservedAt = timestamppb.New(received.Add(observationClockSkew + time.Second))
		},
		"short interval": func(report *agentv1.ObservationReport) { report.SamplingIntervalMs = 1 },
		"long interval":  func(report *agentv1.ObservationReport) { report.SamplingIntervalMs = 3_600_001 },
		"cpu above one": func(report *agentv1.ObservationReport) {
			report.Host.CpuUsage = observationMetric(1.5)
		},
		"negative rate": func(report *agentv1.ObservationReport) {
			report.Host.NetRxBps = observationMetric(-1)
		},
		"not finite": func(report *agentv1.ObservationReport) {
			report.Host.MemUsage = observationMetric(math.NaN())
		},
		"duplicate filesystem": func(report *agentv1.ObservationReport) {
			report.Filesystems = append(report.Filesystems, report.Filesystems[0])
		},
		"unknown purpose": func(report *agentv1.ObservationReport) { report.Filesystems[0].Purpose = "elsewhere" },
		"available above total": func(report *agentv1.ObservationReport) {
			report.Filesystems[0].AvailableBytes = report.Filesystems[0].TotalBytes + 1
		},
		"duplicate container": func(report *agentv1.ObservationReport) {
			report.Inventory.Containers = append(report.Inventory.Containers, report.Inventory.Containers[0])
		},
		"invalid container id": func(report *agentv1.ObservationReport) { report.Inventory.Containers[0].Id = "short" },
		"invalid ownership id": func(report *agentv1.ObservationReport) { report.Inventory.Containers[0].AppId = "not-a-uuid" },
		"foreign compose project": func(report *agentv1.ObservationReport) {
			report.Inventory.Containers[0].ComposeProject = "someone-else"
		},
		"negative restarts": func(report *agentv1.ObservationReport) { report.Inventory.Containers[0].RestartCount = -1 },
		"too many filesystems": func(report *agentv1.ObservationReport) {
			for len(report.Filesystems) <= maxObservationFilesystems {
				report.Filesystems = append(report.Filesystems, &agentv1.FilesystemObservation{
					Path:    "/mnt/" + uuid.NewString(),
					Purpose: "backup",
					Status:  agentv1.MetricStatus_METRIC_STATUS_ERROR,
				})
			}
		},
		"too many containers": func(report *agentv1.ObservationReport) {
			for len(report.Inventory.Containers) <= maxObservationContainers {
				report.Inventory.Containers = append(report.Inventory.Containers, &agentv1.ContainerObservation{
					Id: strings.Repeat("b", 64),
				})
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			report := validObservationReport("node-1")
			mutate(report)
			if _, err := validateObservation("node-1", report, received); !errors.Is(err, ErrValidation) {
				t.Errorf("validateObservation = %v; want ErrValidation", err)
			}
		})
	}
}

func TestValidateObservationKeepsUnavailableSourcesExplicit(t *testing.T) {
	report := validObservationReport("node-1")
	report.Host.CpuUsage = &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_ERROR, Reason: "read_failed"}
	report.Host.MemUsage = &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED}
	report.Filesystems[0].InodeStatus = agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED
	observation, err := validateObservation("node-1", report, time.Unix(1_700_000_010, 0))
	if err != nil {
		t.Fatalf("validateObservation: %v", err)
	}
	if observation.Host.CPUUsage.Status != MetricStatusError || observation.Host.CPUUsage.Reason != "read_failed" {
		t.Errorf("cpu = %+v; want an explicit error", observation.Host.CPUUsage)
	}
	if observation.Host.MemUsage.Status != MetricStatusUnsupported {
		t.Errorf("mem = %+v; want unsupported", observation.Host.MemUsage)
	}
	if observation.Host.DiskUsage.Status != MetricStatusOK {
		t.Errorf("disk = %+v; a working source must stay measured", observation.Host.DiskUsage)
	}
	if observation.Filesystems[0].InodeStatus != MetricStatusUnsupported {
		t.Errorf("inodes = %+v; want unknown, not zero", observation.Filesystems[0])
	}
}

func TestValidateObservationNonOKValuesAreIgnored(t *testing.T) {
	report := validObservationReport("node-1")
	report.Host.CpuUsage = &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_ERROR, Value: 99}
	observation, err := validateObservation("node-1", report, time.Unix(1_700_000_010, 0))
	if err != nil {
		t.Fatalf("validateObservation: %v", err)
	}
	if observation.Host.CPUUsage.Value != 0 {
		t.Errorf("non-OK value = %v; want 0", observation.Host.CPUUsage.Value)
	}
}

func TestValidateObservationTruncatedInventoryIsIncomplete(t *testing.T) {
	report := validObservationReport("node-1")
	report.Inventory.Complete = true
	report.Inventory.Truncated = true
	observation, err := validateObservation("node-1", report, time.Unix(1_700_000_010, 0))
	if err != nil {
		t.Fatalf("validateObservation: %v", err)
	}
	if observation.Inventory.Complete {
		t.Error("truncated inventory was accepted as complete")
	}
	if observation.Inventory.Reason == "" {
		t.Error("incomplete inventory has no reason")
	}
}

func TestObservationFreshnessBoundary(t *testing.T) {
	observed := time.Unix(1_700_000_000, 0)
	interval := 15 * time.Second
	if !observationFresh(observed.Add(3*interval), observed, interval) {
		t.Error("a report at exactly three intervals is fresh")
	}
	if observationFresh(observed.Add(3*interval+time.Nanosecond), observed, interval) {
		t.Error("a report older than three intervals is stale")
	}
	if observationFresh(observed, observed, 0) {
		t.Error("zero interval must never prove freshness")
	}
	if observationFresh(observed.Add(-observationClockSkew-time.Second), observed, interval) {
		t.Error("a far future observation must not be fresh")
	}
	if observationFresh(observed, time.Time{}, interval) {
		t.Error("a zero observation time must not be fresh")
	}
}
