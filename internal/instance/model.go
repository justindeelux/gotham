package instance

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrUnsupported  = errors.New("instance: not supported on this host")
	ErrPending      = errors.New("instance: a network change is awaiting confirmation")
	ErrNoPending    = errors.New("instance: no network change is awaiting confirmation")
	ErrHost         = errors.New("instance: host helper failed")
	ErrRiskyNetwork = errors.New("instance: this change touches the active interface; confirm it explicitly")
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
	Timezone        Field `json:"timezone"`
}

type GeneralInput struct {
	ControlPlaneURL string `json:"control_plane_url"`
	InstanceName    string `json:"instance_name"`
	Timezone        string `json:"timezone"`
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

type Capabilities struct {
	Network bool `json:"network"`
	System  bool `json:"system"`
}

type HelperState string

const (
	HelperReady   HelperState = "ready"
	HelperMissing HelperState = "missing"
	HelperDenied  HelperState = "denied"
)

type Host struct {
	Helper       HelperState `json:"helper"`
	Repair       bool        `json:"repair"`
	NetworkStack bool        `json:"network_stack"`
	SystemStack  bool        `json:"system_stack"`
	Network      bool        `json:"network"`
	System       bool        `json:"system"`
}

func (h Host) Installable() bool {
	return h.Helper == HelperMissing && h.Repair && (h.NetworkStack || h.SystemStack)
}

type State struct {
	PendingControlPlaneURL *PendingControlPlaneURL `json:"pending_control_plane_url"`
	General                General                 `json:"general"`
	Network                Network                 `json:"network"`
	System                 System                  `json:"system"`
	Capabilities           Capabilities            `json:"capabilities"`
	Host                   Host                    `json:"host"`
	Pending                *Pending                `json:"pending"`
}

type Stored struct {
	PendingControlPlaneURL *PendingControlPlaneURL
	ControlPlaneURL        *string
	InstanceName           *string
	Timezone               *string
	Network                Network
	System                 System
	Pending                *Pending
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
