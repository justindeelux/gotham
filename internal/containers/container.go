package containers

import (
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// portsLabel carries operator-declared port mappings ("host:container",
// comma-separated) on a container. The agent ListContainers contract does not
// yet transport Docker port bindings, so the control plane surfaces ports from
// this label until the contract gains a ports field.
const portsLabel = "gotham.ports"

// Docker health statuses, as reported in a container's health field.
const (
	HealthStarting  = "starting"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

// Container is the shared wire representation of a container running on a
// managed node.
type Container struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Image   string     `json:"image"`
	State   string     `json:"state"`
	Status  string     `json:"status"`
	Ports   []string   `json:"ports"`
	Created *time.Time `json:"created,omitempty"`
	// PortsReported marks engine-reported bindings (true) versus the legacy
	// gotham.ports label fallback (false). Routing decisions must not trust
	// declared-only ports (R1), so it is internal and never serialised.
	PortsReported bool `json:"-"`
	// Mounts are the engine-reported bind mounts; RestartPolicy is the
	// container's native Docker restart policy (empty when unknown). Both are
	// internal evidence for managed-container convergence (R5).
	Labels        map[string]string `json:"-"`
	Mounts        []ContainerMount  `json:"-"`
	RestartPolicy string            `json:"-"`
	// Health is the container's Docker health status ("starting", "healthy",
	// "unhealthy") when it declares a healthcheck; empty when it does not.
	// Database readiness is gated on it.
	Health string `json:"-"`
}

// ContainerMount is one engine-reported bind mount of a container.
type ContainerMount struct {
	Source      string
	Destination string
	ReadOnly    bool
}

// RunOptions describes a raw container to create and start immediately via
// the agent's RunImage RPC.
type RunOptions struct {
	Image         string            `json:"image"`
	Name          string            `json:"name,omitempty"`
	Env           []string          `json:"env,omitempty"`
	Command       []string          `json:"command,omitempty"`
	Entrypoint    []string          `json:"entrypoint,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Ports         []string          `json:"ports,omitempty"`
	Volumes       []string          `json:"volumes,omitempty"`
	Networks      []string          `json:"networks,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	// Healthcheck optionally configures the container's native Docker
	// healthcheck. A nil value leaves the image's own healthcheck untouched.
	Healthcheck *Healthcheck `json:"healthcheck,omitempty"`
}

// Healthcheck is a container's native Docker healthcheck: Test is the
// exec-form command run inside the container (no shell), and the durations
// bound how Docker schedules and judges it.
type Healthcheck struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

// validate rejects run requests without an image.
func (o RunOptions) validate() error {
	if strings.TrimSpace(o.Image) == "" {
		return ErrValidation
	}
	return nil
}

// toProto maps run options onto the agent contract.
func (o RunOptions) toProto() *agentv1.CreateContainerRequest {
	return &agentv1.CreateContainerRequest{
		Image:         strings.TrimSpace(o.Image),
		Name:          o.Name,
		Env:           o.Env,
		Command:       o.Command,
		Entrypoint:    o.Entrypoint,
		Labels:        o.Labels,
		Ports:         o.Ports,
		Volumes:       o.Volumes,
		Networks:      o.Networks,
		RestartPolicy: o.RestartPolicy,
		Healthcheck:   o.Healthcheck.toProto(),
	}
}

// toProto maps the healthcheck onto the agent contract. A nil healthcheck or
// an empty test renders nil so the agent leaves the image's own healthcheck
// (or its absence) untouched.
func (h *Healthcheck) toProto() *agentv1.ContainerHealthcheck {
	if h == nil || len(h.Test) == 0 {
		return nil
	}
	return &agentv1.ContainerHealthcheck{
		Test:               h.Test,
		IntervalSeconds:    int64(h.Interval / time.Second),
		TimeoutSeconds:     int64(h.Timeout / time.Second),
		Retries:            int32(h.Retries),
		StartPeriodSeconds: int64(h.StartPeriod / time.Second),
	}
}

// newContainer maps one agent ContainerInfo onto the shared DTO. Ports is
// never nil so the API renders [] rather than null.
func newContainer(info *agentv1.ContainerInfo) Container {
	container := Container{
		Ports: []string{},
	}
	if info == nil {
		return container
	}
	container.ID = info.GetId()
	container.Name = info.GetName()
	container.Image = info.GetImage()
	container.State = info.GetState()
	container.Status = info.GetStatus()
	container.Ports, container.PortsReported = portsFromInfo(info)
	container.Labels = info.GetLabels()
	container.Mounts = mountsFromInfo(info)
	container.RestartPolicy = info.GetRestartPolicy()
	container.Health = info.GetHealth()
	if created := info.GetCreatedAt(); created != nil && created.IsValid() {
		timestamp := created.AsTime().UTC()
		container.Created = &timestamp
	}
	return container
}

// mountsFromInfo maps the engine-reported bind mounts.
func mountsFromInfo(info *agentv1.ContainerInfo) []ContainerMount {
	mounts := make([]ContainerMount, 0, len(info.GetMounts()))
	for _, mount := range info.GetMounts() {
		mounts = append(mounts, ContainerMount{
			Source:      mount.GetSource(),
			Destination: mount.GetDestination(),
			ReadOnly:    mount.GetReadOnly(),
		})
	}
	return mounts
}

// portsFromInfo renders the engine-reported port bindings as the same
// "host:container" / "ip:host:container" strings the gotham.ports label uses
// and reports whether the data came from the engine. Older agents report no
// bindings at all, so the label remains a display fallback only.
func portsFromInfo(info *agentv1.ContainerInfo) (ports []string, reported bool) {
	ports = []string{}
	for _, binding := range info.GetPorts() {
		if binding.GetPublicPort() <= 0 || binding.GetPrivatePort() <= 0 {
			continue
		}
		hostPort := strconv.Itoa(int(binding.GetPublicPort()))
		containerPort := strconv.Itoa(int(binding.GetPrivatePort()))
		switch ip := strings.TrimSpace(binding.GetIp()); ip {
		case "", "0.0.0.0", "::":
			ports = append(ports, hostPort+":"+containerPort)
		default:
			ports = append(ports, ip+":"+hostPort+":"+containerPort)
		}
	}
	if len(ports) > 0 {
		return ports, true
	}
	return portsFromLabels(info.GetLabels()), false
}

// portsFromLabels extracts operator-declared port mappings from the
// gotham.ports label. It always returns a non-nil slice.
func portsFromLabels(labels map[string]string) []string {
	ports := []string{}
	raw, ok := labels[portsLabel]
	if !ok {
		return ports
	}
	for _, spec := range strings.Split(raw, ",") {
		if spec = strings.TrimSpace(spec); spec != "" {
			ports = append(ports, spec)
		}
	}
	return ports
}
