package updates

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/justindeelux/gotham/updatecore"
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
	// Reset clears a stale pending marker (operator reset path).
	Reset() error
	// Resume relaunches a staged update's wrapper at startup so a crash during
	// the health window is health-checked or rolled back.
	Resume(ctx context.Context) error
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
	PendingPath  string
	Timeout      time.Duration
	MaxBytes     int64
	// DownloadTimeout bounds the release-artifact body (kept separate from
	// Timeout, which bounds the short metadata/API requests).
	DownloadTimeout time.Duration
	// BackoffPath is the control-plane failed-attempt file for the AUTO_UPDATE
	// loop; empty disables the persisted backoff.
	BackoffPath  string
	Logger       *slog.Logger
	Auto         bool
	AutoInterval time.Duration
	// GOARCH pins the asset architecture (tests); empty means the running one.
	GOARCH string

	// Client overrides the HTTP client for the checker and applier (tests).
	Client *http.Client
	// Restart overrides the restart wrapper (tests). When nil a default that
	// runs the fixed root-owned wrapper through sudo is used.
	Restart RestartFunc
}

type service struct {
	current  string
	checker  *Checker
	applier  *Applier
	status   *StatusStore
	pending  *StatusStore
	backoff  *updateBackoff
	logger   *slog.Logger
	auto     bool
	interval time.Duration
}

// NewService builds a self-update Service. It does not fail when no public key
// is configured: Check still works, Apply stays disabled (fail closed). It
// resolves the fixed binary path once, recovers an interrupted swap at startup
// and logs the last recorded outcome.
func NewService(cfg Config) (Service, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	binPath := strings.TrimSpace(cfg.BinaryPath)
	if binPath == "" {
		binPath = BinaryPathFromEnv()
	}
	if !filepath.IsAbs(binPath) {
		return nil, fmt.Errorf("updates: binary path must be absolute: %s", binPath)
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
	pending := NewStatusStore(defaultString(cfg.PendingPath, ""))
	applier := &Applier{
		Client:          cfg.Client,
		BinaryPath:      binPath,
		OldPath:         cfg.OldPath,
		LockPath:        defaultString(cfg.LockPath, ""),
		Pending:         pending,
		Status:          status,
		Timeout:         cfg.Timeout,
		ArtifactTimeout: cfg.DownloadTimeout,
		MaxBytes:        cfg.MaxBytes,
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
	last, err := lastStatusOf(pending, status)
	if err != nil {
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
	backoffStore := updatecore.NewStatusStore(defaultString(cfg.BackoffPath, ""))
	svc := &service{
		current:  defaultString(cfg.Current, "dev"),
		checker:  checker,
		applier:  applier,
		status:   status,
		pending:  pending,
		backoff:  newUpdateBackoff(backoffStore, logger),
		logger:   logger,
		auto:     cfg.Auto,
		interval: interval,
	}
	// Seed the persisted backoff from the durable status so a restart after a
	// rollback does not immediately re-apply the same release (LOW-2).
	svc.backoff.seedFromStatus(last, svc.current)
	return svc, nil
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
	return s.applyRelease(ctx, release)
}

// applyRelease installs an already-resolved release and shapes the result. It is
// shared by the operator Apply and the AUTO_UPDATE loop.
func (s *service) applyRelease(ctx context.Context, release *Release) (*ApplyResult, error) {
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

// applyAutoUpdate runs one scheduled check/apply: it resolves the newest
// release, skips a version still inside its failed-attempt backoff, and applies
// otherwise. A failure is persisted so the next tick backs off (LOW-2). It
// returns the offered version (for logging — the staged version, not the
// running one) and whether an update was actually applied.
func (s *service) applyAutoUpdate(ctx context.Context) (string, bool, error) {
	release, err := s.Check(ctx)
	if err != nil {
		return "", false, err
	}
	if release == nil {
		return s.current, false, nil
	}
	if s.backoff.blocked(release.Version) {
		s.logger.Debug("updates: auto-update backoff active", "version", release.Version)
		return release.Version, false, nil
	}
	result, err := s.applyRelease(ctx, release)
	if err != nil {
		s.backoff.record(release.Version, time.Now())
		return release.Version, false, err
	}
	return result.Version, result.Applied, nil
}

// Rollback restores the retained previous binary and clears the pending marker.
func (s *service) Rollback() error { return s.applier.Rollback() }

// Reset clears a stale pending marker so a new apply can proceed and drops the
// failed-attempt backoff, so the operator can retry the same version. It does
// not touch the binary; an operator uses it only after confirming no wrapper is
// running.
func (s *service) Reset() error {
	err := s.pending.Remove()
	s.backoff.clear()
	return err
}

// Resume relaunches the wrapper for a staged update left over from a crash,
// reboot or OOM during the health window (M2). It never loops: the marker is
// rewritten to resuming before the launch.
func (s *service) Resume(ctx context.Context) error { return s.applier.ResumeStaged(ctx) }

// LastStatus returns the current durable outcome: the pending marker while an
// update is staged or its launcher failed, otherwise the authoritative
// root-owned status.
func (s *service) LastStatus() (*Status, error) {
	return lastStatusOf(s.pending, s.status)
}

// lastStatusOf prefers the pending marker (staged/in-progress or a
// control-plane launch failure) over the authoritative status.
func lastStatusOf(pending, status *StatusStore) (*Status, error) {
	if pending != nil {
		if p, err := pending.Read(); err != nil {
			return nil, err
		} else if p != nil {
			return p, nil
		}
	}
	return status.Read()
}

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
				version, applied, err := s.applyAutoUpdate(ctx)
				switch {
				case err != nil:
					s.logger.Warn("updates: auto-update failed", "error", err)
				case applied:
					s.logger.Info("updates: auto-update staged", "version", version)
				default:
					s.logger.Debug("updates: no auto-update available", "current", s.current)
				}
			}
		}
	}()
}

// defaultRestart runs the fixed, root-owned wrapper through sudo and returns a
// waiter for its exit. The wrapper takes no arguments: it restarts the fixed
// service, health-checks it, rolls back on failure and records the outcome.
// sudoers grants exactly this command with no arguments.
//
// The unit must keep KillMode=process so the wrapper is not killed when
// `systemctl restart gotham` tears down the service cgroup.
//
// NoNewPrivileges must remain disabled in the unit because this call needs
// setuid sudo; the only privileged action granted is the fixed wrapper.
func defaultRestart(script string) RestartFunc {
	return func(_ context.Context) (func() error, error) {
		path := defaultString(script, DefaultUpdateScript)
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("updates: restart wrapper %s: %w", path, err)
		}
		cmd := exec.Command("sudo", "-n", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("updates: start restart wrapper: %w", err)
		}
		return cmd.Wait, nil
	}
}
