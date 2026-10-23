package updates

import (
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const (
	DefaultChannel = "stable"
	BetaChannel    = "beta"
)

type Config struct {
	AutoUpdate   bool
	Interval     time.Duration
	Channel      string
	BinaryPath   string
	UpdateScript string
	StatusPath   string
	PendingPath  string
	LockPath     string
	RetryPath    string
	BackoffPath  string
	Version      string
	Restart      updatecore.RestartFunc
}
