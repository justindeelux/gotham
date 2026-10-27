package servers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func addObservationNode(t *testing.T, service *ServerService) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nodeID := uniqueNodeID("node-observation")
	if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	row, err := service.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = service.store.DeleteServer(cleanupCtx, row.ID)
	})
	return nodeID
}

func TestRecordObservationSequencingAndFreshness(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)

	base := time.Now().UTC().Truncate(time.Second)
	current := base
	service.now = func() time.Time { return current }

	first := validObservationReport(nodeID)
	first.ObservedAt = timestamppb.New(base)
	ack, err := service.RecordObservation(ctx, nodeID, first, base)
	if err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	if !ack.GetAccepted() || ack.GetAcceptedSequence() != 1 {
		t.Fatalf("ack = %+v; want sequence 1 accepted", ack)
	}

	snapshot, err := service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation: %v", err)
	}
	if snapshot.AgentUpdateRequired || snapshot.Coverage != CoverageFresh || !snapshot.HostFresh || !snapshot.InventoryFresh {
		t.Errorf("snapshot = %+v; want fresh host and inventory evidence", snapshot)
	}
	if snapshot.Host.CPUUsage.Value != 0.25 {
		t.Errorf("cpu = %+v", snapshot.Host.CPUUsage)
	}
	if len(snapshot.Inventory.Containers) != 1 {
		t.Errorf("containers = %d; want 1", len(snapshot.Inventory.Containers))
	}

	duplicate, err := service.RecordObservation(ctx, nodeID, first, base.Add(time.Second))
	if err != nil {
		t.Fatalf("duplicate RecordObservation: %v", err)
	}
	if duplicate.GetAccepted() || duplicate.GetAcceptedSequence() != 1 {
		t.Errorf("duplicate ack = %+v; want not accepted at sequence 1", duplicate)
	}

	olderSession := validObservationReport(nodeID)
	olderSession.SessionId = "session-2"
	olderSession.ObservedAt = timestamppb.New(base.Add(-time.Minute))
	olderAck, err := service.RecordObservation(ctx, nodeID, olderSession, base.Add(time.Second))
	if err != nil {
		t.Fatalf("older session RecordObservation: %v", err)
	}
	if olderAck.GetAccepted() {
		t.Error("an older reconnect session overwrote fresher state")
	}

	newerSession := validObservationReport(nodeID)
	newerSession.SessionId = "session-2"
	newerSession.ObservedAt = timestamppb.New(base.Add(time.Second))
	newerAck, err := service.RecordObservation(ctx, nodeID, newerSession, base.Add(2*time.Second))
	if err != nil {
		t.Fatalf("newer session RecordObservation: %v", err)
	}
	if !newerAck.GetAccepted() {
		t.Error("a newer reconnect session was not accepted")
	}

	current = base.Add(time.Second + 3*defaultObservationInterval + time.Nanosecond)
	snapshot, err = service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation after expiry: %v", err)
	}
	if snapshot.Coverage != CoverageStale || snapshot.HostFresh || snapshot.InventoryFresh {
		t.Errorf("snapshot = %+v; want stale after three intervals", snapshot)
	}
	if !snapshot.ObservedAt.Equal(base.Add(time.Second)) {
		t.Errorf("observed_at = %s; a restart/freshness read must not rewrite the observation time", snapshot.ObservedAt)
	}
}

func TestRecordObservationPartialKeepsCompleteInventory(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)

	base := time.Now().UTC().Truncate(time.Second)
	service.now = func() time.Time { return base }
	first := validObservationReport(nodeID)
	first.ObservedAt = timestamppb.New(base)
	if _, err := service.RecordObservation(ctx, nodeID, first, base); err != nil {
		t.Fatalf("complete RecordObservation: %v", err)
	}

	partial := validObservationReport(nodeID)
	partial.Sequence = 2
	partial.ObservedAt = timestamppb.New(base.Add(defaultObservationInterval))
	partial.Host.CpuUsage = observationMetric(0.9)
	partial.Inventory = &agentv1.ContainerInventory{
		Complete: false,
		Reason:   "container_inspect_failed",
		Containers: []*agentv1.ContainerObservation{{
			Id: strings.Repeat("f", 64),
		}},
	}
	if _, err := service.RecordObservation(ctx, nodeID, partial, base.Add(defaultObservationInterval)); err != nil {
		t.Fatalf("partial RecordObservation: %v", err)
	}
	snapshot, err := service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation: %v", err)
	}
	if snapshot.Host.CPUUsage.Value != 0.9 {
		t.Errorf("host evidence = %+v; a partial report must still update host metrics", snapshot.Host)
	}
	if !snapshot.Inventory.Complete || len(snapshot.Inventory.Containers) != 1 {
		t.Errorf("inventory = %+v; a partial inventory must not replace the last complete generation", snapshot.Inventory)
	}

	emptyComplete := validObservationReport(nodeID)
	emptyComplete.Sequence = 3
	emptyComplete.ObservedAt = timestamppb.New(base.Add(2 * defaultObservationInterval))
	emptyComplete.Inventory = &agentv1.ContainerInventory{Complete: true}
	if _, err := service.RecordObservation(ctx, nodeID, emptyComplete, base.Add(2*defaultObservationInterval)); err != nil {
		t.Fatalf("empty complete RecordObservation: %v", err)
	}
	snapshot, err = service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation after empty complete: %v", err)
	}
	if !snapshot.Inventory.Complete || len(snapshot.Inventory.Containers) != 0 {
		t.Errorf("inventory = %+v; a complete empty generation must prove no container remains", snapshot.Inventory)
	}
}

func TestOldAgentHeartbeatOnlyStaysUsable(t *testing.T) {
	service, st := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)

	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.2, SentAt: timestamppb.Now()}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	if row.Status != StatusReady {
		t.Errorf("status = %q; heartbeat must keep working for an old agent", row.Status)
	}
	snapshot, err := service.LatestObservation(ctx, uuid.UUID(row.ID.Bytes))
	if err != nil {
		t.Fatalf("LatestObservation: %v", err)
	}
	if !snapshot.AgentUpdateRequired || snapshot.Coverage != CoverageAgentUpdateRequired {
		t.Errorf("snapshot = %+v; evidence-free monitoring must say agent update required", snapshot)
	}
}

func TestRecordObservationRejectsCrossNodeClaim(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)

	report := validObservationReport("another-node")
	if _, err := service.RecordObservation(ctx, nodeID, report, time.Now().UTC()); !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordObservation = %v; want ErrValidation for a cross-node claim", err)
	}
}

func TestPushObservationsOverGateway(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)
	conn := startTestGateway(t, service)
	client := agentv1.NewAgentServiceClient(conn)

	stream, err := client.PushObservations(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatalf("PushObservations: %v", err)
	}
	report := validObservationReport(nodeID)
	report.ObservedAt = timestamppb.Now()
	if err := stream.Send(report); err != nil {
		t.Fatalf("send observation: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if !ack.GetAccepted() || ack.GetAcceptedSequence() != 1 {
		t.Errorf("ack = %+v; want accepted sequence 1", ack)
	}

	snapshot, err := service.LatestObservation(ctx, mustServerID(t, service, nodeID))
	if err != nil {
		t.Fatalf("LatestObservation: %v", err)
	}
	if snapshot.Coverage != CoverageFresh {
		t.Errorf("coverage = %q; want fresh after an accepted push", snapshot.Coverage)
	}
}

func TestPushObservationsRejectsCrossNodeMetadata(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := addObservationNode(t, service)
	conn := startTestGateway(t, service)
	client := agentv1.NewAgentServiceClient(conn)

	stream, err := client.PushObservations(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, nodeID))
	if err != nil {
		t.Fatalf("PushObservations: %v", err)
	}
	report := validObservationReport(nodeID)
	report.NodeId = "another-node"
	report.ObservedAt = timestamppb.Now()
	if err := stream.Send(report); err != nil {
		t.Fatalf("send observation: %v", err)
	}
	_ = stream.CloseSend()
	if _, err := stream.CloseAndRecv(); err == nil {
		t.Fatal("a cross-node observation was accepted")
	}
}

func mustServerID(t *testing.T, service *ServerService, nodeID string) uuid.UUID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, err := service.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	return uuid.UUID(row.ID.Bytes)
}
