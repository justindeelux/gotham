package files

import (
	"github.com/justindeelux/gotham/agent/proxy"
)

const (
	DefaultMaxUpload            = 1 << 30
	DefaultMaxTransfers         = 8
	DefaultArchiveMaxBytes      = 4 << 30
	DefaultArchiveMaxEntries    = 100_000
	DefaultArchiveMaxInputBytes = 1 << 30
	DefaultTrashMaxAgeDays      = 30
	// MaxTrashMaxAgeDays caps the retention age (100 years) so an operator who
	// types a huge number cannot overflow the cutoff arithmetic and purge
	// everything.
	MaxTrashMaxAgeDays = 36_500
)

const defaultStateRoot = "/var/lib/gotham-agent"

const (
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

type Config struct {
	FileFullAccess    bool
	ManagedVolumeRoot string
	ComposeRoot       string
	FileRoots         string
	CertDir           string
	KeyFile           string
	CA                string
	BinaryPath        string
	AgentStateRoot    string
	ProxyRoot         string

	UpdateScript      string
	UpdateStatusPath  string
	UpdatePendingPath string
	UpdateLockPath    string
	UpdateRetryPath   string
	UpdateBackoffPath string

	FileMaxUpload            int64
	FileMaxTransfers         int
	FileArchiveMaxBytes      int64
	FileArchiveMaxEntries    int64
	FileArchiveMaxInputBytes int64
	// FileTrashMaxAgeDays expires recycle-bin items older than this many days.
	// Zero keeps them forever, and the agent supplies DefaultTrashMaxAgeDays.
	FileTrashMaxAgeDays int
	// FileTrashMaxBytes caps one root's recycle bin in bytes, removing the
	// oldest items first. Zero keeps today's unbounded behaviour.
	FileTrashMaxBytes int64
}

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
	if c.FileTrashMaxAgeDays < 0 {
		c.FileTrashMaxAgeDays = 0
	}
	if c.FileTrashMaxAgeDays > MaxTrashMaxAgeDays {
		c.FileTrashMaxAgeDays = MaxTrashMaxAgeDays
	}
	if c.FileTrashMaxBytes < 0 {
		c.FileTrashMaxBytes = 0
	}
}
