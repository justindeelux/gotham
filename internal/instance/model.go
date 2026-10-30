package instance

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrUnsupported    = errors.New("instance: not supported on this host")
	ErrPending        = errors.New("instance: a network change is awaiting confirmation")
	ErrNoPending      = errors.New("instance: no network change is awaiting confirmation")
	ErrHost           = errors.New("instance: host helper failed")
	ErrRiskyNetwork   = errors.New("instance: this change touches the active interface; confirm it explicitly")
	ErrNodeNotFound   = errors.New("instance: server not found")
	ErrAgentOutdated  = errors.New("instance: the node agent does not support host settings")
	ErrNodeOffline    = errors.New("instance: the node agent is unreachable")
	ErrHelperOutdated = errors.New("instance: the host helper does not support this operation")
)

type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	keys := make([]string, 0, len(e))
	for key := range e {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+": "+e[key])
	}
	return "instance: validation: " + strings.Join(parts, "; ")
}

type Source string

const (
	SourceEnv     Source = "env"
	SourceDB      Source = "db"
	SourceDefault Source = "default"
)

type Field struct {
	Value  string `json:"value"`
	Source Source `json:"source"`
	Locked bool   `json:"locked"`
	EnvVar string `json:"env_var,omitempty"`
}

type General struct {
	ControlPlaneURL Field `json:"control_plane_url"`
	InstanceName    Field `json:"instance_name"`
}

type GeneralInput struct {
	ControlPlaneURL string `json:"control_plane_url"`
	InstanceName    string `json:"instance_name"`
}

const (
	ModeDHCP   = "dhcp"
	ModeStatic = "static"
)

type IPConfig struct {
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

type IPv6Config struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

type Network struct {
	DNSServers []string   `json:"dns_servers"`
	IPv4       IPConfig   `json:"ipv4"`
	IPv6       IPv6Config `json:"ipv6"`
}

type NetworkInput struct {
	Network
	ConfirmInterfaceChange bool `json:"confirm_interface_change"`
}

type System struct {
	Hostname   string   `json:"hostname"`
	NTPEnabled bool     `json:"ntp_enabled"`
	NTPServers []string `json:"ntp_servers"`
	Timezone   string   `json:"timezone"`
}

type Pending struct {
	Previous Network   `json:"previous"`
	Proposed Network   `json:"proposed"`
	Deadline time.Time `json:"deadline"`
}

type PendingControlPlaneURL struct {
	Previous *string   `json:"previous"`
	Proposed string    `json:"proposed"`
	Deadline time.Time `json:"deadline"`
	Synced   bool      `json:"synced"`
}

var (
	ErrDomainPending     = errors.New("instance: a control-plane URL change is awaiting confirmation")
	ErrNoDomainPending   = errors.New("instance: no control-plane URL change is awaiting confirmation")
	ErrDomainNotSynced   = errors.New("instance: the new control-plane router has not synced")
	ErrConfirmDomainHost = errors.New("instance: confirm through the new control-plane host")
)

const ControlPlaneConfirmWindow = 120 * time.Second

// NodeCapability is what one node can do with its own host settings. Anything
// but ready means the settings forms are read-only, with Reason saying why.
type NodeCapability string

const (
	CapabilityReady            NodeCapability = "ready"
	CapabilityUnsupportedStack NodeCapability = "unsupported_stack"
	CapabilityHelperOutdated   NodeCapability = "helper_outdated"
	CapabilityHelperMissing    NodeCapability = "helper_missing"
	CapabilityHelperDenied     NodeCapability = "helper_denied"
	CapabilityAgentOutdated    NodeCapability = "agent_outdated"
	CapabilityOffline          NodeCapability = "offline"
)

// HostStatus is the live truth of one node's host plus what it supports. It is
// read on every request, never copied into the database.
type HostStatus struct {
	Capability NodeCapability
	// Reason overrides the generic capability text when the host has a more
	// specific story (for example an outdated helper that can be repaired).
	Reason  string
	Repair  bool
	Network Network
	System  System
}

// NodeState is the per-node host settings response. Network and System are the
// host's live values (or the proposed change while one is pending).
type NodeState struct {
	Capability NodeCapability `json:"capability"`
	Reason     string         `json:"reason,omitempty"`
	Repair     bool           `json:"repair"`
	Network    Network        `json:"network"`
	System     System         `json:"system"`
	Pending    *Pending       `json:"pending"`
}

type State struct {
	PendingControlPlaneURL *PendingControlPlaneURL `json:"pending_control_plane_url"`
	General                General                 `json:"general"`
}

type Stored struct {
	PendingControlPlaneURL *PendingControlPlaneURL
	ControlPlaneURL        *string
	InstanceName           *string
	Timezone               *string
}

const (
	DefaultInstanceName  = "gotham"
	DefaultTimezone      = "UTC"
	NetworkConfirmWindow = 120 * time.Second
)

const (
	EnvPublicURL    = "GOTHAM_PUBLIC_URL"
	EnvInstanceName = "GOTHAM_INSTANCE_NAME"
	EnvTimezone     = "GOTHAM_TIMEZONE"
)
