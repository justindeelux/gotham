package updates

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// DefaultUpdateScript is the installed, root-owned restart/healthcheck wrapper
// the Applier invokes after a successful swap. It lives outside every path the
// service user can write and takes no arguments: its binary, service and health
// targets come from a root-owned configuration file.
const DefaultUpdateScript = "/usr/libexec/gotham/gotham-update"

// ApplyResult reports the outcome of an apply attempt.
type ApplyResult struct {
	// Applied is false when the running version is already current.
	Applied bool   `json:"applied"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	// Staged reports that the swap succeeded and a restart/healthcheck is
	// pending; the durable outcome is recorded by the wrapper and surfaced by
	// LastStatus.
	Staged bool `json:"staged"`
	// Restart is an alias of Staged kept for the response shape.
	Restart bool `json:"restart"`
}

// Service is the self-update surface the HTTP layer depends on.
type Service interface {
	// Current returns the running version.
	Current() string
	// Check resolves the newest available release, or nil when up to date.
	Check(ctx context.Context) (*Release, error)
	// Apply checks the channel and applies the resolved release.
	Apply(ctx context.Context, channel Channel) (*ApplyResult, error)
	// Rollback restores the retained previous binary.
	Rollback() error
	// LastStatus returns the durable outcome of the most recent update attempt.
	LastStatus() (*Status, error)
	// StartAuto launches the scheduled check/apply loop when enabled.
	StartAuto(ctx context.Context)
}

// Config wires a self-update Service.
type Config struct {
	Current      string
	Repo         string
	BaseURL      string
	Channel      Channel
	PublicKey    ed25519.PublicKey
	BinaryPath   string
	OldPath      string
	LockPath     string
	UpdateScript string
	StatusPath   string
	Timeout      time.Duration
	MaxBytes     int64
	Logger       *slog.Logger
	Auto         bool
	AutoInterval time.Duration
	// GOARCH pins the asset architecture (tests); empty means the running one.
	GOARCH string

	// Client overrides the HTTP client for the checker and applier (tests).
	Client *http.Client
	// Restart overrides the restart wrapper (tests). When nil a default that
	// runs the fixed root-owned wrapper through sudo is used.
	Restart func(ctx context.Context) error
}

type service struct {
	current  string
	checker  *Checker
	applier  *Applier
	status   *StatusStore
	logger   *slog.Logger
	auto     bool
	interval time.Duration
}

// NewService builds a self-update Service. It does not fail when no public key
// is configured: Check still works, Apply stays disabled (fail closed). It
// recovers an interrupted swap at startup and logs the last recorded outcome.
func NewService(cfg Config) (Service, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	checker := &Checker{
		BaseURL: cfg.BaseURL,
		Repo:    cfg.Repo,
		Channel: cfg.Channel,
		Client:  cfg.Client,
		Timeout: cfg.Timeout,
		GOARCH:  cfg.GOARCH,
	}
	status := NewStatusStore(defaultString(cfg.StatusPath, ""))
	applier := &Applier{
		Client:     cfg.Client,
		BinaryPath: cfg.BinaryPath,
		OldPath:    cfg.OldPath,
		LockPath:   cfg.LockPath,
		Status:     status,
		Timeout:    cfg.Timeout,
		MaxBytes:   cfg.MaxBytes,
	}
	if cfg.PublicKey != nil {
		verifier, err := NewVerifier(cfg.PublicKey)
		if err != nil {
			return nil, err
		}
		applier.Verifier = verifier
	}
	applier.Restart = cfg.Restart
	if applier.Restart == nil {
		applier.Restart = defaultRestart(cfg.UpdateScript)
	}

	if restored, err := applier.Recover(); err != nil {
		logger.Warn("updates: startup recovery failed", "error", err)
	} else if restored {
		logger.Warn("updates: restored the previous binary after an interrupted update")
	}
	if last, err := status.Read(); err != nil {
		logger.Warn("updates: could not read the update status", "error", err)
	} else if last != nil {
		if last.Result == StatusOK {
			logger.Info("updates: last update succeeded", "version", last.Version, "at", last.At)
		} else {
			logger.Warn("updates: last update did not complete cleanly",
				"result", last.Result, "version", last.Version, "detail", last.Detail, "at", last.At)
		}
	}

	interval := cfg.AutoInterval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &service{
		current:  defaultString(cfg.Current, "dev"),
		checker:  checker,
		applier:  applier,
		status:   status,
		logger:   logger,
		auto:     cfg.Auto,
		interval: interval,
	}, nil
}

// Current returns the running version.
func (s *service) Current() string { return s.current }

// Check resolves the newest available release, or nil when up to date.
func (s *service) Check(ctx context.Context) (*Release, error) {
	return s.checker.Check(ctx, s.current)
}

// Apply checks the requested channel (defaulting to the configured one) and
// applies the resolved release.
func (s *service) Apply(ctx context.Context, channel Channel) (*ApplyResult, error) {
	checker := *s.checker
	if channel != "" {
		checker.Channel = channel
	}
	release, err := checker.Check(ctx, s.current)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return &ApplyResult{Applied: false, Version: s.current, Message: "already up to date"}, nil
	}
	outcome, err := s.applier.Apply(ctx, release)
	if err != nil {
		return nil, err
	}
	result := &ApplyResult{Applied: true, Version: outcome.Version}
	if outcome.Staged {
		result.Staged = true
		result.Restart = true
		result.Message = "staged; the restart wrapper will health-check and record the outcome"
	}
	return result, nil
}

// Rollback restores the retained previous binary.
func (s *service) Rollback() error { return s.applier.Rollback() }

// LastStatus returns the durable outcome of the most recent update attempt, or
// nil when no update has run.
func (s *service) LastStatus() (*Status, error) { return s.status.Read() }

// StartAuto runs the scheduled check/apply loop until ctx is cancelled. It is
// a no-op unless auto-update is enabled.
func (s *service) StartAuto(ctx context.Context) {
	if !s.auto || !AutoUpdateEnabled() {
		return
	}
	s.logger.Info("updates: auto-update enabled", "interval", s.interval.String())
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := s.Apply(ctx, "")
				switch {
				case err != nil:
					s.logger.Warn("updates: auto-update failed", "error", err)
				case result.Applied:
					s.logger.Info("updates: auto-update staged", "version", result.Version)
				default:
					s.logger.Debug("updates: no auto-update available", "current", result.Version)
				}
			}
		}
	}()
}

// defaultRestart runs the fixed, root-owned wrapper through sudo, detached, so
// it survives the restart of this process. The wrapper takes no arguments: it
// restarts the fixed service, health-checks it, rolls back on failure and
// records the outcome in its status file. sudoers grants exactly this command
// with no arguments.
//
// NoNewPrivileges must remain disabled in the unit because this call needs
// setuid sudo; the only privileged action granted is the fixed wrapper.
func defaultRestart(script string) func(ctx context.Context) error {
	return func(_ context.Context) error {
		path := defaultString(script, DefaultUpdateScript)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("updates: restart wrapper %s: %w", path, err)
		}
		cmd := exec.Command("sudo", "-n", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("updates: start restart wrapper: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}
