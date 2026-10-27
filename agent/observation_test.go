package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/agent/docker"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func observationAgent(t *testing.T, dockerClient docker.Client) *Agent {
	t.Helper()
	cfg := Config{
		CPAddr:            "passthrough:///bufnet",
		NodeID:            "node-observe",
		CertDir:           t.TempDir(),
		ManagedVolumeRoot: t.TempDir(),
		BackupRoot:        t.TempDir(),
	}
	return NewAgent(cfg, discardLogger(), dockerClient)
}

func TestObservationAndHeartbeatUseSeparateSamplers(t *testing.T) {
	runner := observationAgent(t, &fakeDockerClient{})
	if runner.sampler == nil || runner.observationSampler == nil {
		t.Fatal("both consumers need a sampler")
	}
	if runner.sampler == runner.observationSampler {
		t.Fatal("observation sampling would reset the heartbeat's delta baseline")
	}
}

func TestCollectObservationReportsManagedInventory(t *testing.T) {
	appID := uuid.NewString()
	deploymentID := uuid.NewString()
	dockerClient := &fakeDockerClient{
		dockerRootDir: "/var/lib/docker",
		containers: []*agentv1.ContainerInfo{
			{
				Id:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				State:  "running",
				Health: "healthy",
				Labels: map[string]string{
					"gotham.managed":       "true",
					"gotham.app_id":        appID,
					"gotham.deployment_id": deploymentID,
					"gotham.arbitrary":     "must-not-travel",
					"secret":               "must-not-travel",
				},
			},
			{
				Id:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Labels: map[string]string{"com.docker.compose.project": "gotham-123", "com.docker.compose.service": "web"},
			},
			{
				Id:     "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				Labels: map[string]string{"com.docker.compose.project": "someone-else"},
			},
		},
		detailsByID: map[string]docker.ContainerDetails{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {
				State:        "running",
				Health:       "healthy",
				RestartCount: 3,
				OOMKilled:    true,
				CreatedAt:    time.Unix(1_700_000_000, 0),
				StartedAt:    time.Unix(1_700_000_100, 0),
			},
		},
	}
	runner := observationAgent(t, dockerClient)
	report := runner.collectObservation(context.Background())

	if report.GetNodeId() != "node-observe" {
		t.Errorf("node_id = %q", report.GetNodeId())
	}
	if report.GetSessionId() == "" || report.GetSequence() != 1 {
		t.Errorf("session/sequence = %q/%d; want a session and sequence 1", report.GetSessionId(), report.GetSequence())
	}
	if report.GetSamplingIntervalMs() != defaultObservationInterval.Milliseconds() {
		t.Errorf("sampling interval = %d", report.GetSamplingIntervalMs())
	}
	if report.GetHost().GetCpuUsage().GetStatus() == agentv1.MetricStatus_METRIC_STATUS_OK {
		t.Error("first CPU sample was reported as measured")
	}
	for name, metric := range map[string]*agentv1.Metric{
		"mem":  report.GetHost().GetMemUsage(),
		"disk": report.GetHost().GetDiskUsage(),
	} {
		if metric.GetStatus() != agentv1.MetricStatus_METRIC_STATUS_OK {
			t.Errorf("%s status = %s; want measured", name, metric.GetStatus())
		}
	}
	filesystems := report.GetFilesystems()
	if len(filesystems) == 0 || filesystems[0].GetPurpose() != "root" || filesystems[0].GetStatus() != agentv1.MetricStatus_METRIC_STATUS_OK {
		t.Errorf("filesystems = %+v; want a measured root", filesystems)
	}
	inventory := report.GetInventory()
	if !inventory.GetComplete() || inventory.GetTruncated() {
		t.Errorf("inventory complete/truncated = %v/%v; want a complete generation", inventory.GetComplete(), inventory.GetTruncated())
	}
	if len(inventory.GetContainers()) != 2 {
		t.Fatalf("inventory = %+v; want the two managed containers", inventory.GetContainers())
	}
	observed := inventory.GetContainers()[0]
	if observed.GetId() != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("first container = %q", observed.GetId())
	}
	if observed.GetRestartCount() != 3 || !observed.GetOomKilled() || observed.GetHealth() != "healthy" {
		t.Errorf("container evidence = %+v; want restart/OOM/health", observed)
	}
	if observed.GetAppId() != appID || observed.GetDeploymentId() != deploymentID {
		t.Errorf("ownership = %q/%q; want %q/%q", observed.GetAppId(), observed.GetDeploymentId(), appID, deploymentID)
	}
	if observed.GetCreatedAt() == nil || observed.GetStartedAt() == nil {
		t.Error("lifecycle timestamps are missing")
	}
	if observed.GetFinishedAt() != nil {
		t.Error("a zero finished_at must stay absent")
	}
	second := inventory.GetContainers()[1]
	if second.GetComposeProject() != "gotham-123" || second.GetComposeService() != "web" {
		t.Errorf("compose ownership = %+v", second)
	}

	secondReport := runner.collectObservation(context.Background())
	if secondReport.GetSequence() != 2 {
		t.Errorf("sequence = %d; want 2", secondReport.GetSequence())
	}
	if secondReport.GetSessionId() != report.GetSessionId() {
		t.Error("session changed within one agent process")
	}
}

func TestCollectObservationIncompleteSourcesStayExplicit(t *testing.T) {
	dockerClient := &fakeDockerClient{err: errors.New("docker unavailable")}
	runner := observationAgent(t, dockerClient)
	report := runner.collectObservation(context.Background())
	if report.GetInventory().GetComplete() {
		t.Error("inventory is complete although Docker failed")
	}
	if report.GetInventory().GetReason() != "inventory_unavailable" {
		t.Errorf("inventory reason = %q", report.GetInventory().GetReason())
	}
	if len(report.GetInventory().GetContainers()) != 0 {
		t.Error("a failed inventory carried containers")
	}
	// Host and filesystem evidence still arrives beside the unavailable source.
	if report.GetHost().GetMemUsage().GetStatus() == agentv1.MetricStatus_METRIC_STATUS_UNSPECIFIED {
		t.Error("host evidence was dropped when Docker failed")
	}
	if len(report.GetFilesystems()) == 0 {
		t.Error("filesystem evidence was dropped when Docker failed")
	}
}

func TestCollectObservationPartialInspectIsIncomplete(t *testing.T) {
	first := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	second := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	dockerClient := &fakeDockerClient{
		containers: []*agentv1.ContainerInfo{
			{Id: first, Labels: map[string]string{"gotham.managed": "true"}},
			{Id: second, Labels: map[string]string{"gotham.managed": "true"}},
		},
		inspectErrByID: map[string]error{second: errors.New("inspect failed")},
	}
	runner := observationAgent(t, dockerClient)
	inventory := runner.collectObservation(context.Background()).GetInventory()
	if inventory.GetComplete() {
		t.Error("inventory is complete although one inspect failed")
	}
	if inventory.GetReason() != "container_inspect_failed" {
		t.Errorf("reason = %q", inventory.GetReason())
	}
	if len(inventory.GetContainers()) != 1 || inventory.GetContainers()[0].GetId() != first {
		t.Errorf("containers = %+v; want the inspected container only", inventory.GetContainers())
	}
}

func TestCollectObservationBoundsInventory(t *testing.T) {
	containers := make([]*agentv1.ContainerInfo, 0, maxInventoryContainers+5)
	for index := 0; index < maxInventoryContainers+5; index++ {
		containers = append(containers, &agentv1.ContainerInfo{
			Id:     fmt.Sprintf("%064x", index+1),
			Labels: map[string]string{"gotham.managed": "true"},
		})
	}
	runner := observationAgent(t, &fakeDockerClient{containers: containers})
	inventory := runner.collectObservation(context.Background()).GetInventory()
	if len(inventory.GetContainers()) != maxInventoryContainers {
		t.Errorf("containers = %d; want the cap %d", len(inventory.GetContainers()), maxInventoryContainers)
	}
	if !inventory.GetTruncated() || inventory.GetComplete() {
		t.Errorf("truncated/complete = %v/%v; a capped inventory is incomplete", inventory.GetTruncated(), inventory.GetComplete())
	}
}

func TestCollectObservationEmptyCompleteInventory(t *testing.T) {
	runner := observationAgent(t, &fakeDockerClient{})
	inventory := runner.collectObservation(context.Background()).GetInventory()
	if !inventory.GetComplete() || len(inventory.GetContainers()) != 0 {
		t.Errorf("inventory = %+v; an empty complete generation is valid evidence", inventory)
	}
}

func TestObservationStreamSendsReportsBesideHeartbeat(t *testing.T) {
	fake := &fakeAgentService{
		cpVersion:     "test",
		heartbeatCh:   make(chan *agentv1.HeartbeatRequest, 4),
		observationCh: make(chan *agentv1.ObservationReport, 4),
	}
	dialOptions := startFakeCP(t, fake)
	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-stream",
		CertDir: t.TempDir(),
	}, discardLogger(), &fakeDockerClient{}, WithDialOptions(dialOptions...),
		WithHeartbeatInterval(20*time.Millisecond),
		WithObservationInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx, nil) }()

	select {
	case report := <-fake.observationCh:
		if report.GetNodeId() != "node-stream" || report.GetSequence() == 0 {
			t.Errorf("report = %+v", report)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no observation report arrived")
	}
	select {
	case <-fake.heartbeatCh:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat stopped when observations started")
	}
}

func TestObservationStreamStopsWhenServerLacksSupport(t *testing.T) {
	fake := &fakeAgentService{observationUnimplemented: true}
	dialOptions := startFakeCP(t, fake)
	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-old-cp",
		CertDir: t.TempDir(),
	}, discardLogger(), &fakeDockerClient{}, WithDialOptions(dialOptions...))
	client := agentv1.NewAgentServiceClient(newAgentConn(t, runner))

	done := make(chan struct{})
	go func() {
		runner.pushObservations(context.Background(), client)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("observation push did not fall back after Unimplemented")
	}
}

func TestObservationFailureDoesNotEndHeartbeat(t *testing.T) {
	fake := &fakeAgentService{
		heartbeatCh:     make(chan *agentv1.HeartbeatRequest, 4),
		observationFail: true,
	}
	dialOptions := startFakeCP(t, fake)
	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-observe-fail",
		CertDir: t.TempDir(),
	}, discardLogger(), &fakeDockerClient{}, WithDialOptions(dialOptions...),
		WithHeartbeatInterval(20*time.Millisecond),
		WithObservationInterval(20*time.Millisecond),
		WithBackoff(5*time.Millisecond, 20*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx, nil) }()

	for index := 0; index < 3; index++ {
		select {
		case <-fake.heartbeatCh:
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeat stopped after an observation stream failure")
		}
	}
	if count := fake.registerCount(); count != 1 {
		t.Errorf("register calls = %d; observation failures must not re-register the node", count)
	}
}

func TestCommitObservationSequenceOnlyAdvances(t *testing.T) {
	runner := observationAgent(t, &fakeDockerClient{})
	first := runner.collectObservation(context.Background()).GetSequence()
	second := runner.collectObservation(context.Background()).GetSequence()
	if second <= first {
		t.Errorf("sequences = %d then %d; want monotonic", first, second)
	}
}
