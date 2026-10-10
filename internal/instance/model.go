package instance

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrUnsupported — the host helper is missing or the host stack is not
	// supported for this section (409).
	ErrUnsupported = errors.New("instance: not supported on this host")
	// ErrPending — a network change awaits confirmation (409).
	ErrPending = errors.New("instance: a network change is awaiting confirmation")
	// ErrNoPending — there is no unconfirmed network change (409).
	ErrNoPending = errors.New("instance: no network change is awaiting confirmation")
	// ErrHost — the host helper failed; the change was not applied (502).
	ErrHost = errors.New("instance: host helper failed")
	// ErrRiskyNetwork — the change would disturb the active interface's
	// addresses (removing its address/gateway or switching it from static
	// to DHCP) without an explicit confirmation (409).
	ErrRiskyNetwork = errors.New("instance: this change touches the active interface; confirm it explicitly")
)

// FieldErrors maps a field path to a user-facing message. It is the error
// returned for invalid input (400).
type FieldErrors map[string]string

// Error implements error with a stable, sorted summary.
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

// Source says which precedence layer a general value came from.
type Source string

// Precedence layers, strongest first.
const (
	SourceEnv     Source = "env"
	SourceDB      Source = "db"
	SourceDefault Source = "default"
)

// Field is one effective general value.
type Field struct {
	Value  string `json:"value"`
	Source Source `json:"source"`
	// Locked is true when an environment variable decides the value.
	Locked bool   `json:"locked"`
	EnvVar string `json:"env_var,omitempty"`
}

// General is the effective general section.
type General struct {
	ControlPlaneURL Field `json:"control_plane_url"`
	InstanceName    Field `json:"instance_name"`
	Timezone        Field `json:"timezone"`
}

// GeneralInput is the writable general section.
type GeneralInput struct {
	ControlPlaneURL string `json:"control_plane_url"`
	InstanceName    string `json:"instance_name"`
	Timezone        string `json:"timezone"`
}

// Address modes.
const (
	ModeDHCP   = "dhcp"
	ModeStatic = "static"
)

// IPConfig is one address family's configuration. Address is CIDR notation.
type IPConfig struct {
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

// IPv6Config adds the enable switch.
type IPv6Config struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

// Network is the host network section.
type Network struct {
	DNSServers []string   `json:"dns_servers"`
	IPv4       IPConfig   `json:"ipv4"`
	IPv6       IPv6Config `json:"ipv6"`
}

// NetworkInput is the writable network section: the desired configuration
// plus the explicit confirmation for a change that would disturb the active
// interface (static-to-DHCP or a different address/gateway). The flag is
// never stored; it only gates the apply.
type NetworkInput struct {
	Network
	ConfirmInterfaceChange bool `json:"confirm_interface_change"`
}

// System is the Linux system section.
type System struct {
	Hostname   string   `json:"hostname"`
	NTPEnabled bool     `json:"ntp_enabled"`
	NTPServers []string `json:"ntp_servers"`
}

// Pending is an unconfirmed network change.
type Pending struct {
	Previous Network   `json:"previous"`
	Proposed Network   `json:"proposed"`
	Deadline time.Time `json:"deadline"`
}

// Capabilities report what the host helper can apply.
type Capabilities struct {
	Network bool `json:"network"`
	System  bool `json:"system"`
}

// State is the full read model.
type State struct {
	General      General      `json:"general"`
	Network      Network      `json:"network"`
	System       System       `json:"system"`
	Capabilities Capabilities `json:"capabilities"`
	Pending      *Pending     `json:"pending"`
}

// Stored is the persisted record. General values are nil when unset.
type Stored struct {
	ControlPlaneURL *string
	InstanceName    *string
	Timezone        *string
	Network         Network
	System          System
	Pending         *Pending
}

// Built-in defaults and the confirmation window of a network change.
const (
	DefaultInstanceName  = "gotham"
	DefaultTimezone      = "UTC"
	NetworkConfirmWindow = 120 * time.Second
)

// Environment variables that lock the general fields.
const (
	EnvPublicURL    = "GOTHAM_PUBLIC_URL"
	EnvInstanceName = "GOTHAM_INSTANCE_NAME"
	EnvTimezone     = "GOTHAM_TIMEZONE"
)
