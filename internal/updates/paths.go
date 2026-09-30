package updates

// Control-plane deployment paths. The node agent has its own layout, so these
// stay out of updatecore.
const (
	// DefaultBinaryPath is the fixed target executable. It must match the
	// unit's ExecStart and is never resolved from the live inode.
	DefaultBinaryPath = "/var/lib/gotham/bin/gotham"
	// DefaultStatusPath is the authoritative status file, written by the
	// root-owned wrapper in a root-owned directory the control plane can only
	// read.
	DefaultStatusPath = "/var/lib/gotham-updater/update.status"
	// DefaultPendingPath is the control-plane-owned pending marker (in the
	// service StateDirectory) that gates a second apply while one is staged.
	DefaultPendingPath = "/var/lib/gotham/update.pending"
	// DefaultLockPath serializes Apply/Rollback between the control plane and
	// the privileged wrapper.
	DefaultLockPath = "/var/lib/gotham/update.lock"
	// DefaultUpdaterConf is the root-owned wrapper configuration.
	DefaultUpdaterConf = "/etc/gotham/updater.conf"
)
