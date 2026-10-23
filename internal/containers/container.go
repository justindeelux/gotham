package containers

import (
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const portsLabel = "gotham.ports"

const (
	HealthStarting  = "starting"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

type Container struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	Status        string            `json:"status"`
	Ports         []string          `json:"ports"`
	Created       *time.Time        `json:"created,omitempty"`
	PortsReported bool              `json:"-"`
	Labels        map[string]string `json:"-"`
	Mounts        []ContainerMount  `json:"-"`
	RestartPolicy string            `json:"-"`
	Health        string            `json:"health,omitempty"`
}

type ContainerMount struct {
	Source      string
	Destination string
	ReadOnly    bool
}

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
	Healthcheck   *Healthcheck      `json:"healthcheck,omitempty"`
}

type Healthcheck struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

func (o RunOptions) validate() error {
	if strings.TrimSpace(o.Image) == "" {
		return ErrValidation
	}
	return nil
}

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
