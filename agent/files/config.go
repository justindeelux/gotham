package files

import (
	"github.com/justindeelux/gotham/agent/proxy"
)

// Defaults applied when the matching Config field is unset.
const (
	// DefaultMaxUpload bounds one file upload in bytes.
	DefaultMaxUpload = 1 << 30
	// DefaultMaxTransfers caps concurrent file transfers per node; a transfer
	// above the cap is refused as ResourceExhausted.
	DefaultMaxTransfers = 8
	// DefaultArchiveMaxBytes bounds the total uncompressed bytes an archive
	// operation may read (compress) or write (extract), guarding a zip bomb and
	// an oversized archive. Override with GOTHAM_AGENT_ARCHIVE_MAX_BYTES.
	DefaultArchiveMaxBytes = 4 << 30 // 4 GiB
	// DefaultArchiveMaxEntries bounds how many entries an archive operation may
	// walk (compress) or unpack (extract). Override with
	// GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES.
	DefaultArchiveMaxEntries = 100_000
	// DefaultArchiveMaxInputBytes bounds the on-disk size of an archive file
	// that extract will read, checked before any archive parser runs so a
	// hostile archive cannot make the agent allocate ahead of the entry/byte
	// caps. Override with GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES.
	DefaultArchiveMaxInputBytes = 1 << 30 // 1 GiB
)

// defaultStateRoot is the agent's state directory (agent.Config default
// GOTHAM_AGENT_CERT_DIR layout); a browsable root may not overlap it.
const defaultStateRoot = "/var/lib/gotham-agent"

// certFileName and keyFileName are the certificate and key files stored under
// CertDir; the file service protects them individually.
const (
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

// Config is the file-service subset of the agent configuration.
type Config struct {
	// FileFullAccess exposes the whole filesystem as one writable root "/" with
	// no protected paths (the operator accepted the risk).
	FileFullAccess bool
	// ManagedVolumeRoot is the writable managed-volumes root.
	ManagedVolumeRoot string
	// ComposeRoot is the read-only compose service root.
	ComposeRoot string
	// FileRoots lists extra browsable roots, colon-separated; every entry is
	// read-only.
	FileRoots string
	// CertDir stores the agent certificate and key.
	CertDir string
	// KeyFile optionally overrides the agent private key path.
	KeyFile string
	// CA is the control-plane CA bundle path.
	CA string
	// BinaryPath is the agent executable.
	BinaryPath string
	// AgentStateRoot is the agent state directory.
	AgentStateRoot string
	// ProxyRoot is the node's Traefik directory.
	ProxyRoot string

	// UpdateScript, UpdateStatusPath, UpdatePendingPath, UpdateLockPath,
	// UpdateRetryPath and UpdateBackoffPath are the self-update state paths the
	// file service must not expose.
	UpdateScript      string
	UpdateStatusPath  string
	UpdatePendingPath string
	UpdateLockPath    string
	UpdateRetryPath   string
	UpdateBackoffPath string

	// FileMaxUpload bounds one upload in bytes.
	FileMaxUpload int64
	// FileMaxTransfers caps concurrent transfers per node.
	FileMaxTransfers int
	// FileArchiveMaxBytes bounds the total uncompressed bytes one archive
	// operation may read or write.
	FileArchiveMaxBytes int64
	// FileArchiveMaxEntries bounds the entry count one archive operation may
	// walk or unpack.
	FileArchiveMaxEntries int64
	// FileArchiveMaxInputBytes bounds the on-disk size of an archive file that
	// extract will read.
	FileArchiveMaxInputBytes int64
}

// applyDefaults fills the unset values so a zero Config is usable.
func (c *Config) applyDefaults() {
	if c.AgentStateRoot == "" {
		c.AgentStateRoot = defaultStateRoot
	}
	if c.ProxyRoot == "" {
		c.ProxyRoot = proxy.DefaultRoot
	}
	if c.FileMaxUpload <= 0 {
		c.FileMaxUpload = DefaultMaxUpload
	}
	if c.FileMaxTransfers <= 0 {
		c.FileMaxTransfers = DefaultMaxTransfers
	}
	if c.FileArchiveMaxBytes <= 0 {
		c.FileArchiveMaxBytes = DefaultArchiveMaxBytes
	}
	if c.FileArchiveMaxEntries <= 0 {
		c.FileArchiveMaxEntries = DefaultArchiveMaxEntries
	}
	if c.FileArchiveMaxInputBytes <= 0 {
		c.FileArchiveMaxInputBytes = DefaultArchiveMaxInputBytes
	}
}
