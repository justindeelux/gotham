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
	// ErrAgentRestarting: the root-mode toggle applied, but the agent has not
	// reconnected yet. The change is not lost; the next read shows the state.
	ErrAgentRestarting = errors.New("instance: the agent is restarting; reload when it reconnects")
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

// SSHSettings is the SSH server state read from one node.
type SSHSettings struct {
	Available              bool   `json:"available"`
	Port                   uint32 `json:"port"`
	PasswordAuthentication bool   `json:"password_authentication"`
	KeyAuthAvailable       bool   `json:"key_auth_available"`
}

// SSHInput is one desired SSH state.
type SSHInput struct {
	Port                   uint32 `json:"port"`
	PasswordAuthentication bool   `json:"password_authentication"`
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
	Reason    string
	Repair    bool
	Network   Network
	System    System
	SSH       SSHSettings
	AgentRoot bool
}

// NodeState is the per-node host settings response. Network and System are the
// host's live values (or the proposed change while one is pending).
type NodeState struct {
	Capability NodeCapability `json:"capability"`
	Reason     string         `json:"reason,omitempty"`
	Repair     bool           `json:"repair"`
	Network    Network        `json:"network"`
	System     System         `json:"system"`
	SSH        SSHSettings    `json:"ssh"`
	AgentRoot  bool           `json:"agent_root"`
	Pending    *Pending       `json:"pending"`
}

type State struct {
	PendingControlPlaneURL *PendingControlPlaneURL `json:"pending_control_plane_url"`
	General                General                 `json:"general"`
	Cleanup                Retention               `json:"cleanup"`
}

// Retention is how long Gotham keeps its own leftovers before the cleanup
// center may offer them. It is read at scan and clean time, so a change needs
// no restart.
type Retention struct {
	HistoryDays          int `json:"history_days"`
	BackupFileMinAgeDays int `json:"backup_file_min_age_days"`
}

type RetentionInput struct {
	HistoryDays          int `json:"history_days"`
	BackupFileMinAgeDays int `json:"backup_file_min_age_days"`
}

const (
	DefaultCleanupHistoryDays   = 90
	MinCleanupHistoryDays       = 7
	MaxCleanupHistoryDays       = 3650
	DefaultCleanupBackupFileAge = 7
	MinCleanupBackupFileAge     = 1
	MaxCleanupBackupFileAge     = 365
)

func defaultRetention() Retention {
	return Retention{
		HistoryDays:          DefaultCleanupHistoryDays,
		BackupFileMinAgeDays: DefaultCleanupBackupFileAge,
	}
}

func retentionFrom(historyDays int32, backupAge int32) Retention {
	return Retention{HistoryDays: int(historyDays), BackupFileMinAgeDays: int(backupAge)}
}

type Stored struct {
	PendingControlPlaneURL *PendingControlPlaneURL
	ControlPlaneURL        *string
	InstanceName           *string
	Timezone               *string
	Retention              Retention
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
