package servers

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRecordObservationFutureDateDoesNotExtendFreshness(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)

	base := time.Now().UTC().Truncate(time.Second)
	current := base
	service.now = func() time.Time { return current }
	report := validObservationReport(nodeID)
	report.ObservedAt = timestamppb.New(base.Add(observationClockSkew))
	if _, err := service.RecordObservation(ctx, nodeID, report, base); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	snapshot, err := service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation: %v", err)
	}
	if !snapshot.ObservedAt.Equal(base) {
		t.Errorf("observed_at = %s; want it clamped to receipt time %s", snapshot.ObservedAt, base)
	}
	current = base.Add(3*defaultObservationInterval + time.Second)
	snapshot, err = service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation after expiry: %v", err)
	}
	if snapshot.HostFresh || snapshot.InventoryFresh {
		t.Errorf("freshness = host %v inventory %v; a future date extended it", snapshot.HostFresh, snapshot.InventoryFresh)
	}
}
