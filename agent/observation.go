package agent

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/agent/docker"
	"github.com/justindeelux/gotham/agent/stats"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultObservationInterval = 15 * time.Second
	observationCollectTimeout  = 10 * time.Second
	inspectConcurrency         = 4
	maxInventoryContainers     = 256
	maxObservedFilesystems     = 32
	containerSampleInterval    = 30 * time.Second
	containerSampleConcurrency = 4
	maxContainerSamples        = 256
)

func WithObservationInterval(interval time.Duration) Option {
	return func(a *Agent) {
		if interval > 0 {
			a.observationInterval = interval
		}
	}
}

// pushObservations sends one observation generation per collection interval,
// each on its own client stream so the control plane's acknowledgement (or an
// Unimplemented from an older server) is known before the next interval. It
// runs beside the heartbeat: its failures retry on their own and never end the
// heartbeat stream. An older control plane answers Unimplemented, and the agent
// simply stays on heartbeat-only monitoring.
func (a *Agent) pushObservations(ctx context.Context, client agentv1.AgentServiceClient) {
	backoff := a.minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := a.publishObservation(ctx, client)
		if ctx.Err() != nil {
			return
		}
		if status.Code(err) == codes.Unimplemented {
			a.log.Info("agent: control plane does not support observations; using heartbeat-only monitoring")
			return
		}
		delay := a.observationInterval
		if err != nil {
			delay = a.backoffDelay(backoff)
			a.log.Debug("agent: observation push failed; retrying", "error", err, "backoff", delay.String())
			backoff = nextBackoff(backoff, a.maxBackoff)
		} else {
			backoff = a.minBackoff
			if elapsed := time.Since(started); elapsed < a.observationInterval {
				delay = a.observationInterval - elapsed
			}
		}
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func (a *Agent) publishObservation(ctx context.Context, client agentv1.AgentServiceClient) error {
	streamCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, a.cfg.NodeID), observationCollectTimeout)
	defer cancel()
	stream, err := client.PushObservations(streamCtx)
	if err != nil {
		return err
	}
	if err := a.sendObservation(ctx, stream); err != nil {
		_, closeErr := stream.CloseAndRecv()
		if closeErr != nil {
			return closeErr
		}
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
}

func (a *Agent) sendObservation(ctx context.Context, stream agentv1.AgentService_PushObservationsClient) error {
	collectCtx, cancel := context.WithTimeout(ctx, observationCollectTimeout)
	defer cancel()
	return stream.Send(a.collectObservation(collectCtx))
}

func (a *Agent) collectObservation(ctx context.Context) *agentv1.ObservationReport {
	usage, _ := a.observationSampler.Sample()
	observedAt := usage.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	} else {
		observedAt = observedAt.UTC()
	}
	inventory := a.collectInventory(ctx)
	return &agentv1.ObservationReport{
		NodeId:             a.cfg.NodeID,
		SessionId:          a.observationSession,
		Sequence:           a.nextObservationSequence(),
		ObservedAt:         timestamppb.New(observedAt),
		SamplingIntervalMs: a.observationInterval.Milliseconds(),
		Host:               hostObservation(usage),
		Filesystems:        a.observeFilesystems(ctx),
		Inventory:          inventory,
		ContainerSamples:   a.collectContainerSamples(ctx, inventory),
		AgentVersion:       a.Version(),
	}
}

// collectContainerSamples samples managed running containers at most every 30
// seconds. It runs on the observation stream, so a slow stats call never blocks
// the heartbeat stream. Concurrency and the per-call timeout are bounded.
func (a *Agent) collectContainerSamples(ctx context.Context, inventory *agentv1.ContainerInventory) []*agentv1.ContainerResourceSample {
	now := time.Now()
	if a.containerSamples == nil || a.docker == nil || inventory == nil {
		return nil
	}
	ids := make(map[string]struct{}, len(inventory.GetContainers()))
	candidates := make([]string, 0, len(inventory.GetContainers()))
	for _, container := range inventory.GetContainers() {
		ids[container.GetId()] = struct{}{}
		if container.GetState() == "running" {
			candidates = append(candidates, container.GetId())
		}
	}
	a.containerSamples.Retain(ids)
	if !a.containerSampleAt.IsZero() && now.Sub(a.containerSampleAt) < containerSampleInterval {
		return nil
	}
	a.containerSampleAt = now

	sort.Strings(candidates)
	if len(candidates) > maxContainerSamples {
		candidates = candidates[:maxContainerSamples]
	}
	type result struct {
		sample *agentv1.ContainerResourceSample
	}
	results := make([]result, len(candidates))
	semaphore := make(chan struct{}, containerSampleConcurrency)
	var wait sync.WaitGroup
	for index, id := range candidates {
		wait.Add(1)
		go func(index int, id string) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
			defer cancel()
			counters, err := a.docker.ContainerStats(callCtx, id)
			if err != nil {
				a.log.Debug("agent: container stats unavailable", "container_id", id, "error", err)
				return
			}
			sampleAt := time.Now().UTC()
			sample := a.containerSamples.Observe(id, stats.ContainerCounters{
				CPUTotal:    counters.CPUTotal,
				SystemCPU:   counters.SystemCPU,
				OnlineCPUs:  counters.OnlineCPUs,
				MemoryUsage: counters.MemoryUsage,
				MemoryCache: counters.MemoryCache,
				MemoryLimit: counters.MemoryLimit,
				NetRx:       counters.NetRx,
				NetTx:       counters.NetTx,
				DiskRead:    counters.DiskRead,
				DiskWrite:   counters.DiskWrite,
			}, sampleAt)
			results[index] = result{sample: containerResourceSampleProto(id, sampleAt, sample)}
		}(index, id)
	}
	wait.Wait()

	samples := make([]*agentv1.ContainerResourceSample, 0, len(candidates))
	for _, item := range results {
		if item.sample != nil {
			samples = append(samples, item.sample)
		}
	}
	return samples
}

func containerResourceSampleProto(id string, at time.Time, sample stats.ContainerSample) *agentv1.ContainerResourceSample {
	out := &agentv1.ContainerResourceSample{
		Id:                 id,
		ObservedAt:         timestamppb.New(at),
		CpuCores:           containerMetric(sample.CPUCores),
		MemWorkingSetBytes: &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_OK, Value: float64(sample.MemWorkingSetBytes)},
		MemUnlimited:       sample.MemUnlimited,
		NetRxBps:           containerMetric(sample.NetRxBps),
		NetTxBps:           containerMetric(sample.NetTxBps),
		DiskReadBps:        containerMetric(sample.DiskReadBps),
		DiskWriteBps:       containerMetric(sample.DiskWriteBps),
	}
	if sample.MemLimitBytes != nil {
		out.MemLimitBytes = &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_OK, Value: float64(*sample.MemLimitBytes)}
	}
	return out
}

func containerMetric(value *float64) *agentv1.Metric {
	if value == nil {
		return &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_WARMING, Reason: "warming"}
	}
	return &agentv1.Metric{Status: agentv1.MetricStatus_METRIC_STATUS_OK, Value: *value}
}

func (a *Agent) nextObservationSequence() uint64 {
	a.sequenceMu.Lock()
	defer a.sequenceMu.Unlock()
	a.observationSequence++
	return a.observationSequence
}

func hostObservation(usage stats.Usage) *agentv1.HostObservation {
	return &agentv1.HostObservation{
		CpuUsage:     toMetric(usage.CPUStatus, usage.CPU, usage.CPUReason),
		MemUsage:     toMetric(usage.MemStatus, usage.Mem, usage.MemReason),
		DiskUsage:    toMetric(usage.DiskStatus, usage.Disk, usage.DiskReason),
		NetRxBps:     toMetric(usage.NetStatus, usage.NetRxBps, usage.NetReason),
		NetTxBps:     toMetric(usage.NetStatus, usage.NetTxBps, usage.NetReason),
		DiskReadBps:  toMetric(usage.DiskIOStatus, usage.DiskReadBps, usage.DiskIOReason),
		DiskWriteBps: toMetric(usage.DiskIOStatus, usage.DiskWriteBps, usage.DiskIOReason),
		TotalMem:     usage.TotalMem,
		TotalDisk:    usage.TotalDisk,
	}
}

func toMetric(value stats.MetricStatus, number float64, reason string) *agentv1.Metric {
	metric := &agentv1.Metric{Reason: reason}
	switch value {
	case stats.MetricOK:
		metric.Status, metric.Value = agentv1.MetricStatus_METRIC_STATUS_OK, number
	case stats.MetricUnsupported:
		metric.Status = agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED
	case stats.MetricWarming:
		metric.Status = agentv1.MetricStatus_METRIC_STATUS_WARMING
	default:
		metric.Status = agentv1.MetricStatus_METRIC_STATUS_ERROR
	}
	return metric
}

func (a *Agent) observeFilesystems(ctx context.Context) []*agentv1.FilesystemObservation {
	candidates := []stats.FilesystemCandidate{{Path: "/", Purpose: stats.PurposeRoot}}
	if a.docker != nil {
		if root := a.dockerDataRoot(ctx); root != "" && root != "/" {
			candidates = append(candidates, stats.FilesystemCandidate{Path: root, Purpose: stats.PurposeDockerData})
		}
	}
	if root := strings.TrimSpace(a.cfg.ManagedVolumeRoot); root != "" {
		candidates = append(candidates, stats.FilesystemCandidate{Path: root, Purpose: stats.PurposeManagedStorage})
	}
	if root := strings.TrimSpace(a.cfg.BackupRoot); root != "" {
		candidates = append(candidates, stats.FilesystemCandidate{Path: root, Purpose: stats.PurposeBackup})
	}

	filesystems := a.filesystems.Collect(candidates)
	if len(filesystems) > maxObservedFilesystems {
		filesystems = filesystems[:maxObservedFilesystems]
	}
	out := make([]*agentv1.FilesystemObservation, 0, len(filesystems))
	for _, filesystem := range filesystems {
		out = append(out, &agentv1.FilesystemObservation{
			Path:           filesystem.Path,
			Purpose:        filesystem.Purpose,
			Status:         toMetricStatus(filesystem.Status),
			Reason:         filesystem.Reason,
			TotalBytes:     filesystem.TotalBytes,
			AvailableBytes: filesystem.AvailableBytes,
			InodeStatus:    toMetricStatus(filesystem.InodeStatus),
			InodeReason:    filesystem.InodeReason,
			TotalInodes:    filesystem.TotalInodes,
			FreeInodes:     filesystem.FreeInodes,
		})
	}
	return out
}

// dockerDataRoot caches the engine's data root for the topology window: the
// path is mount evidence, not a per-sample counter, so it is refreshed at most
// once a minute like the filesystem topology.
func (a *Agent) dockerDataRoot(ctx context.Context) string {
	a.dockerRootMu.Lock()
	if !a.dockerRootAt.IsZero() && time.Since(a.dockerRootAt) < time.Minute {
		root := a.dockerRoot
		a.dockerRootMu.Unlock()
		return root
	}
	a.dockerRootMu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	root, err := a.docker.DockerRootDir(callCtx)
	cancel()
	if err != nil {
		root = ""
	}
	a.dockerRootMu.Lock()
	a.dockerRoot = root
	a.dockerRootAt = time.Now()
	a.dockerRootMu.Unlock()
	return root
}

func toMetricStatus(value stats.MetricStatus) agentv1.MetricStatus {
	switch value {
	case stats.MetricOK:
		return agentv1.MetricStatus_METRIC_STATUS_OK
	case stats.MetricUnsupported:
		return agentv1.MetricStatus_METRIC_STATUS_UNSUPPORTED
	case stats.MetricWarming:
		return agentv1.MetricStatus_METRIC_STATUS_WARMING
	default:
		return agentv1.MetricStatus_METRIC_STATUS_ERROR
	}
}

func (a *Agent) collectInventory(ctx context.Context) *agentv1.ContainerInventory {
	inventory := &agentv1.ContainerInventory{}
	if a.docker == nil {
		inventory.Reason = "docker_unavailable"
		return inventory
	}
	containers, err := a.docker.ListContainers(ctx, true, nil, false)
	if err != nil {
		inventory.Reason = "inventory_unavailable"
		return inventory
	}
	managed := make([]*agentv1.ContainerInfo, 0, len(containers))
	for _, container := range containers {
		if managedContainer(container.GetLabels()) {
			managed = append(managed, container)
		}
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i].GetId() < managed[j].GetId() })
	if len(managed) > maxInventoryContainers {
		managed = managed[:maxInventoryContainers]
		inventory.Truncated = true
	}

	observed, failed := a.inspectContainers(ctx, managed)
	inventory.Containers = observed
	switch {
	case failed > 0:
		inventory.Reason = "container_inspect_failed"
	case inventory.Truncated:
		inventory.Reason = "truncated"
	default:
		inventory.Complete = true
	}
	return inventory
}

func (a *Agent) inspectContainers(ctx context.Context, containers []*agentv1.ContainerInfo) ([]*agentv1.ContainerObservation, int) {
	type result struct {
		index   int
		details docker.ContainerDetails
		err     error
	}
	results := make([]result, len(containers))
	semaphore := make(chan struct{}, inspectConcurrency)
	var wait sync.WaitGroup
	for index, container := range containers {
		wait.Add(1)
		go func(index int, container *agentv1.ContainerInfo) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
			defer cancel()
			details, err := a.docker.InspectContainer(callCtx, container.GetId())
			results[index] = result{index: index, details: details, err: err}
		}(index, container)
	}
	wait.Wait()

	observed := make([]*agentv1.ContainerObservation, 0, len(containers))
	failed := 0
	for index, container := range containers {
		if results[index].err != nil {
			a.log.Debug("agent: container inspect unavailable", "container_id", container.GetId(), "error", results[index].err)
			failed++
			continue
		}
		observed = append(observed, containerObservation(container, results[index].details))
	}
	return observed, failed
}

func managedContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	return labels["gotham.managed"] == "true" ||
		labels["gotham.app_id"] != "" ||
		labels["gotham.deployment_id"] != "" ||
		labels["gotham.db_id"] != "" ||
		labels["gotham.role"] != "" ||
		labels["gotham.component"] == "proxy" ||
		strings.HasPrefix(labels["com.docker.compose.project"], "gotham-")
}

func containerObservation(container *agentv1.ContainerInfo, details docker.ContainerDetails) *agentv1.ContainerObservation {
	labels := container.GetLabels()
	observation := &agentv1.ContainerObservation{
		Id:             container.GetId(),
		State:          details.State,
		Health:         details.Health,
		RestartCount:   details.RestartCount,
		OomKilled:      details.OOMKilled,
		ExitCode:       details.ExitCode,
		RestartPolicy:  details.RestartPolicy,
		Managed:        labels["gotham.managed"] == "true",
		AppId:          labels["gotham.app_id"],
		DeploymentId:   labels["gotham.deployment_id"],
		DatabaseId:     labels["gotham.db_id"],
		ComposeProject: labels["com.docker.compose.project"],
		ComposeService: labels["com.docker.compose.service"],
		Replica:        labels["com.docker.compose.container-number"],
		Role:           labels["gotham.role"],
	}
	if observation.State == "" {
		observation.State = container.GetState()
	}
	if observation.Health == "" {
		observation.Health = container.GetHealth()
	}
	if createdAt := details.CreatedAt; !createdAt.IsZero() {
		observation.CreatedAt = timestamppb.New(createdAt.UTC())
	} else if container.GetCreatedAt() != nil {
		observation.CreatedAt = container.GetCreatedAt()
	}
	if !details.StartedAt.IsZero() {
		observation.StartedAt = timestamppb.New(details.StartedAt.UTC())
	}
	if !details.FinishedAt.IsZero() {
		observation.FinishedAt = timestamppb.New(details.FinishedAt.UTC())
	}
	return observation
}
