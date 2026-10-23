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
	"sync"
	"syscall"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const DefaultUpdateScript = "/usr/libexec/gotham/gotham-update"

type ApplyResult struct {
	Applied bool   `json:"applied"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	Staged  bool   `json:"staged"`
	Restart bool   `json:"restart"`
}

type Service interface {
	Current() string
	Check(ctx context.Context) (*Release, error)
	Changelog(ctx context.Context) ([]ChangelogEntry, error)
	Apply(ctx context.Context, channel Channel) (*ApplyResult, error)
	Rollback() error
	Reset() error
	Resume(ctx context.Context) error
	LastStatus() (*Status, error)
	StartAuto(ctx context.Context)
}

type Config struct {
	Current         string
	Repo            string
	BaseURL         string
	Channel         Channel
	PublicKeys      []ed25519.PublicKey
	BinaryPath      string
	OldPath         string
	LockPath        string
	UpdateScript    string
	StatusPath      string
	PendingPath     string
	Timeout         time.Duration
	MaxBytes        int64
	DownloadTimeout time.Duration
	BackoffPath     string
	Logger          *slog.Logger
	Auto            bool
	AutoInterval    time.Duration
	Schedules       ScheduleStore
	Timezone        func() string
	GOARCH          string

	Client  *http.Client
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
	interval time.Duration

	schedules        ScheduleStore
	timezone         func() string
	reload           chan struct{}
	mu               sync.Mutex
	sched            Schedule
	nextAt           time.Time
	checkedAt        time.Time
	checkErr         string
	changelogKey     string
	changelogEntries []ChangelogEntry
	changelogUntil   time.Time
}

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
	if len(cfg.PublicKeys) > 0 {
		verifier, err := NewVerifierSet(cfg.PublicKeys...)
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
		interval: interval,

		schedules: cfg.Schedules,
		timezone:  cfg.Timezone,
		reload:    make(chan struct{}, 1),
		sched:     defaultSchedule(cfg, interval),
	}
	svc.loadSchedule()
	svc.backoff.seedFromStatus(last, svc.current)
	return svc, nil
}

func (s *service) Current() string { return s.current }

func (s *service) Check(ctx context.Context) (*Release, error) {
	return s.checkOn(ctx, "")
}

func (s *service) checkOn(ctx context.Context, channel Channel) (*Release, error) {
	checker := *s.checker
	checker.Channel = channel
	if channel == "" {
		checker.Channel = s.schedule().Channel
	}
	release, err := checker.Check(ctx, s.current)
	s.recordCheck(err)
	return release, err
}

func (s *service) Changelog(ctx context.Context) ([]ChangelogEntry, error) {
	channel := s.schedule().Channel
	key := string(channel) + "\x00" + s.current
	s.mu.Lock()
	if key == s.changelogKey && time.Now().Before(s.changelogUntil) {
		cached := append([]ChangelogEntry(nil), s.changelogEntries...)
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	checker := *s.checker
	checker.Channel = channel
	entries, err := checker.Changelog(ctx, s.current)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		if current, cerr := checker.CurrentChangelog(ctx, s.current); cerr != nil {
			return nil, cerr
		} else if current != nil {
			entries = []ChangelogEntry{*current}
		}
	}

	s.mu.Lock()
	s.changelogKey = key
	s.changelogEntries = append([]ChangelogEntry(nil), entries...)
	s.changelogUntil = time.Now().Add(OfferCacheTTLFromEnv())
	s.mu.Unlock()
	return entries, nil
}

func (s *service) Apply(ctx context.Context, channel Channel) (*ApplyResult, error) {
	release, err := s.checkOn(ctx, channel)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return &ApplyResult{Applied: false, Version: s.current, Message: "already up to date"}, nil
	}
	return s.applyRelease(ctx, release)
}

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

func (s *service) Rollback() error { return s.applier.Rollback() }

func (s *service) Reset() error {
	err := s.pending.Remove()
	s.backoff.clear()
	return err
}

func (s *service) Resume(ctx context.Context) error { return s.applier.ResumeStaged(ctx) }

func (s *service) LastStatus() (*Status, error) {
	return lastStatusOf(s.pending, s.status)
}

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
