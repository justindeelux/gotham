package updates

import (
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// Release channels accepted by the agent. A stable node refuses beta offers; a
// beta node accepts stable and beta offers (the offer carries the release's own
// channel). An unknown configured value behaves like stable.
const (
	DefaultChannel = "stable"
	BetaChannel    = "beta"
)

// Config is the self-update subset of the agent configuration.
type Config struct {
	// AutoUpdate applies any verified offer on the poll instead of waiting for
	// an operator-triggered rollout.
	AutoUpdate bool
	// Interval is how often the agent polls the control plane.
	Interval time.Duration
	// Channel is the release channel this agent accepts.
	Channel string
	// BinaryPath is the fixed agent executable the updater swaps.
	BinaryPath string
	// UpdateScript is the root-owned restart/healthcheck wrapper.
	UpdateScript string
	// StatusPath is the root-owned authoritative update status.
	StatusPath string
	// PendingPath is the agent-owned staged gate marker.
	PendingPath string
	// LockPath is the lock shared with the wrapper.
	LockPath string
	// RetryPath is the agent-owned retry marker an operator reset writes to
	// clear the failed-update backoff of a running agent.
	RetryPath string
	// BackoffPath persists the failed-attempt count so the seeded backoff
	// escalates across wrapper restarts.
	BackoffPath string
	// Version is the running agent build version.
	Version string
	// Restart overrides the restart wrapper (tests). When nil the fixed
	// root-owned wrapper is run through sudo.
	Restart updatecore.RestartFunc
}
