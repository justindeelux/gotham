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
	container.Ports = portsFromInfo(info)
	if created := info.GetCreatedAt(); created != nil && created.IsValid() {
		timestamp := created.AsTime().UTC()
		container.Created = &timestamp
	}
	return container
}

// portsFromInfo renders the engine-reported port bindings as the same
// "host:container" / "ip:host:container" strings the gotham.ports label uses.
// Older agents report no bindings, so the label remains the fallback.
func portsFromInfo(info *agentv1.ContainerInfo) []string {
	ports := []string{}
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
		return ports
	}
	return portsFromLabels(info.GetLabels())
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
