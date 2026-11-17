package updates

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/updatecore"
)

const (
	defaultUpdateScript = "/usr/libexec/gotham/gotham-agent-update"

	updateRPCTimeout      = 15 * time.Second
	updateDownloadTimeout = 10 * time.Minute
)

const (
	agentAssetPrefix    = "gotham-agent-linux-"
	agentManifestPrefix = "gotham-agent-manifest-"
)

var errOffer = errors.New("agent: refused update offer")

const (
	updateFailedSeedBase = 5 * time.Minute
	updateBackoffMax     = time.Hour
)

const statusFreshness = 5 * time.Minute

type Updater struct {
	cfg          Config
	log          *slog.Logger
	applier      *updatecore.Applier
	version      func() string
	setVersion   func(string)
	backoffCount *updatecore.StatusStore

	backoffMu sync.Mutex
	backoff   map[string]*backoffState

	// reportFloor suppresses the pre-retry wrapper outcome after an operator
	// retry, so the control plane does not show the old failure again.
	reportFloor time.Time
}

type backoffState struct {
	failures int
	until    time.Time
}

func New(cfg Config, log *slog.Logger, version func() string, setVersion func(string)) (*Updater, error) {
	if cfg.BinaryPath == "" {
		return nil, nil
	}
	publicKeys, err := updatecore.LoadPublicKeys()
	if err != nil {
		return nil, nil
	}
	verifier, err := updatecore.NewVerifierSet(publicKeys...)
	if err != nil {
		return nil, err
	}
	applier := &updatecore.Applier{
		Verifier:        verifier,
		BinaryPath:      cfg.BinaryPath,
		OldPath:         cfg.BinaryPath + updatecore.OldSuffix,
		LockPath:        cfg.LockPath,
		Pending:         updatecore.NewStatusStore(cfg.PendingPath),
		Status:          updatecore.NewStatusStore(cfg.StatusPath),
		ArtifactTimeout: updateDownloadTimeout,
		Restart:         cfg.Restart,
	}
	if applier.Restart == nil {
		applier.Restart = agentRestart(cfg.UpdateScript)
	}
	if restored, err := applier.Recover(); err != nil {
		log.Warn("agent: startup update recovery failed", "error", err)
	} else if restored {
		log.Warn("agent: restored the previous binary after an interrupted update")
	}
	if err := applier.ResumeStaged(context.Background()); err != nil {
		log.Warn("agent: could not resume a staged update", "error", err)
	}
	u := &Updater{
		cfg:          cfg,
		log:          log,
		applier:      applier,
		version:      version,
		setVersion:   setVersion,
		backoffCount: updatecore.NewStatusStore(cfg.BackoffPath),
		backoff:      map[string]*backoffState{},
	}
	u.seedBackoffFromStatus()
	return u, nil
}

func (u *Updater) Run(ctx context.Context, client agentv1.UpdateServiceClient) {
	u.checkOnce(ctx, client)
	ticker := time.NewTicker(u.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.checkOnce(ctx, client)
		}
	}
}

func (u *Updater) checkOnce(ctx context.Context, client agentv1.UpdateServiceClient) {
	u.consumeRetry()

	request := &agentv1.UpdateRequest{
		AgentVersion: u.version(),
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	}
	if status := u.lastStatus(); status != nil {
		request.UpdateResult = status.Result
		request.UpdateDetail = status.Detail
		request.UpdateVersion = status.Version
	}

	rpcCtx, cancel := context.WithTimeout(ctx, updateRPCTimeout)
	resp, err := client.RequestUpdate(rpcCtx, request)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			u.log.Warn("agent: update check failed", "error", err)
		}
		return
	}
	if !resp.GetUpdateAvailable() {
		return
	}
	if resp.GetRetry() {
		u.resetFailedState()
	}
	if !u.cfg.AutoUpdate && !resp.GetRollout() {
		u.log.Debug("agent: update available; unattended auto-update is off",
			"version", resp.GetLatestVersion())
		return
	}

	release, err := releaseFromOffer(resp, u.cfg.Channel)
	if err != nil {
		u.log.Warn("agent: refused an incomplete update offer", "error", err)
		return
	}
	if !isNewerVersion(release.Version, u.version()) {
		u.log.Warn("agent: refusing a non-newer update offer",
			"offered", release.Version, "running", u.version())
		return
	}
	if u.inBackoff(release.Version) {
		u.log.Debug("agent: update backoff active", "version", release.Version)
		return
	}

	downloadCtx, cancelDownload := context.WithTimeout(ctx, updateDownloadTimeout)
	defer cancelDownload()
	outcome, err := u.applier.Apply(downloadCtx, release)
	if err != nil {
		u.log.Warn("agent: update failed; the previous binary is retained",
			"version", release.Version, "error", err)
		u.recordFailure(release.Version)
		return
	}
	u.log.Info("agent: update staged", "version", outcome.Version, "staged", outcome.Staged)
	if u.statusHealthy(outcome.Version) {
		u.clearBackoff(outcome.Version)
		u.setVersion(outcome.Version)
		return
	}
	if outcome.Staged {
		if u.statusFailed(release.Version) {
			u.recordFailure(release.Version)
		}
		return
	}
	u.recordFailure(release.Version)
}

func (u *Updater) statusHealthy(version string) bool {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil || status.Result != updatecore.StatusOK || status.Version != version {
		return false
	}
	return time.Since(status.At) <= statusFreshness
}

// lastStatus reports the wrapper's durable outcome to the control plane so a
// failed rollout is visible with its reason. The status file is root-owned and
// read-only for the agent; a missing or unreadable file is simply no evidence.
// A status at or below reportFloor is the attempt an operator already retried
// and is not reported again.
func (u *Updater) lastStatus() *updatecore.Status {
	status := u.rawStatus()
	if status == nil {
		return nil
	}
	if !status.At.IsZero() && !status.At.After(u.reportFloor) {
		return nil
	}
	return status
}

func (u *Updater) rawStatus() *updatecore.Status {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil {
		return nil
	}
	return status
}

// resetFailedState honours a control-plane retry (the "Update agent" button):
// it clears the local backoff and pending marker exactly like
// `gotham-agent update reset`, so the same version is attempted again. The
// root-owned status file is left for the next successful update to overwrite;
// the control plane ignores a report older than its rollout.
func (u *Updater) resetFailedState() {
	if status := u.rawStatus(); status != nil && !status.At.IsZero() {
		u.reportFloor = status.At
	}
	u.backoffMu.Lock()
	u.backoff = map[string]*backoffState{}
	u.backoffMu.Unlock()
	u.clearBackoffCount()
	_ = updatecore.NewStatusStore(u.cfg.PendingPath).Remove()
	u.log.Info("agent: retry requested by the control plane; cleared the failed-update state")
}

func (u *Updater) inBackoff(version string) bool {
	u.backoffMu.Lock()
	defer u.backoffMu.Unlock()
	state, ok := u.backoff[version]
	return ok && time.Now().Before(state.until)
}

func (u *Updater) recordFailure(version string) {
	u.applyBackoff(version, time.Now())
}

func (u *Updater) applyBackoff(version string, failureAt time.Time) {
	rec := u.readBackoff(version)
	rec.count++
	u.writeBackoff(version, rec.count, failureAt)
	u.armBackoff(version, rec.count)
}

func (u *Updater) armBackoff(version string, count int) {
	delay := seedDelay(count)
	u.backoffMu.Lock()
	u.backoff[version] = &backoffState{failures: count, until: time.Now().Add(delay)}
	u.backoffMu.Unlock()
	u.log.Info("agent: backing off a failed update",
		"version", version, "failures", count, "retry_in", delay.String())
}

func (u *Updater) clearBackoff(version string) {
	u.backoffMu.Lock()
	delete(u.backoff, version)
	u.backoffMu.Unlock()
	u.clearBackoffCount()
}

func (u *Updater) statusFailed(version string) bool {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil || status.Version != version {
		return false
	}
	return status.Result == updatecore.StatusRolledBack || status.Result == updatecore.StatusRollbackFailed
}

func (u *Updater) seedBackoffFromStatus() {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil {
		return
	}
	switch status.Result {
	case updatecore.StatusRolledBack, updatecore.StatusRollbackFailed:
	default:
		return
	}
	if !isNewerVersion(status.Version, u.version()) {
		return
	}
	rec := u.readBackoff(status.Version)
	if rec.count < 1 || rec.at.IsZero() || !rec.at.Equal(status.At) {
		u.applyBackoff(status.Version, status.At)
		u.log.Warn("agent: seeded a backoff from a durable rollback",
			"version", status.Version, "result", status.Result, "failures", rec.count+1)
		return
	}
	u.armBackoff(status.Version, rec.count)
	u.log.Warn("agent: re-armed the backoff for an unchanged rollback",
		"version", status.Version, "failures", rec.count)
}

func (u *Updater) consumeRetry() {
	path := strings.TrimSpace(u.cfg.RetryPath)
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	u.backoffMu.Lock()
	u.backoff = map[string]*backoffState{}
	u.backoffMu.Unlock()
	u.clearBackoffCount()
	_ = os.Remove(path)
	u.log.Info("agent: update retry requested; cleared the failed-update backoff")
}

func seedDelay(count int) time.Duration {
	delay := updateFailedSeedBase
	for i := 1; i < count && delay < updateBackoffMax; i++ {
		delay *= 2
	}
	if delay > updateBackoffMax {
		delay = updateBackoffMax
	}
	return delay
}

type backoffRecord struct {
	count int
	at    time.Time
}

func (u *Updater) readBackoff(version string) backoffRecord {
	if u.backoffCount == nil {
		return backoffRecord{}
	}
	status, err := u.backoffCount.Read()
	if err != nil || status == nil || status.Version != version {
		return backoffRecord{}
	}
	count, err := strconv.Atoi(strings.TrimSpace(status.Detail))
	if err != nil || count < 0 {
		count = 0
	}
	return backoffRecord{count: count, at: status.At}
}

func (u *Updater) writeBackoff(version string, count int, at time.Time) {
	if u.backoffCount == nil {
		return
	}
	if err := u.backoffCount.Write(updatecore.Status{
		Result: "backoff", Version: version, Detail: strconv.Itoa(count), At: at,
	}); err != nil {
		u.log.Warn("agent: could not persist the failed-update count", "error", err)
	}
}

func (u *Updater) clearBackoffCount() {
	if u.backoffCount == nil {
		return
	}
	_ = u.backoffCount.Remove()
}

func ResetState(cfg Config) error {
	if err := updatecore.NewStatusStore(cfg.PendingPath).Remove(); err != nil {
		return err
	}
	if path := strings.TrimSpace(cfg.StatusPath); path != "" {
		_ = os.Remove(path)
	}
	path := strings.TrimSpace(cfg.RetryPath)
	if path == "" {
		return nil
	}
	return writeRetryMarker(path)
}

func writeRetryMarker(path string) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("agent: refusing symlinked retry directory %s", dir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".update.retry.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString("retry\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func isNewerVersion(offered, current string) bool {
	offeredVersion, err := updatecore.ParseVersion(offered)
	if err != nil {
		return false
	}
	currentVersion, err := updatecore.ParseVersion(current)
	if err != nil {
		return false
	}
	return offeredVersion.Compare(currentVersion) > 0
}

func releaseFromOffer(resp *agentv1.UpdateResponse, channel string) (*updatecore.Release, error) {
	if resp.GetLatestVersion() == "" || resp.GetAssetUrl() == "" ||
		resp.GetManifestUrl() == "" || resp.GetManifestSignatureUrl() == "" {
		return nil, fmt.Errorf("%w: update offer is missing release material", errOffer)
	}

	arch := runtime.GOARCH
	wantAsset := agentAssetPrefix + arch
	assetName := ""
	if parsed, err := url.Parse(resp.GetAssetUrl()); err == nil {
		assetName = path.Base(parsed.Path)
	}
	if assetName != wantAsset {
		return nil, fmt.Errorf("%w: offered asset %q is not %q", errOffer, assetName, wantAsset)
	}

	wantManifest := agentManifestPrefix + arch + ".txt"
	manifestName := ""
	if parsed, err := url.Parse(resp.GetManifestUrl()); err == nil {
		manifestName = path.Base(parsed.Path)
	}
	if manifestName != wantManifest {
		return nil, fmt.Errorf("%w: offered manifest %q is not %q", errOffer, manifestName, wantManifest)
	}

	if channel == "" {
		channel = DefaultChannel
	}
	offered := strings.TrimSpace(resp.GetChannel())
	switch offered {
	case DefaultChannel:
	case BetaChannel:
		if channel != BetaChannel {
			return nil, fmt.Errorf("%w: offer channel %q is not the configured %q", errOffer, offered, channel)
		}
	default:
		return nil, fmt.Errorf("%w: offer has no usable channel", errOffer)
	}

	return &updatecore.Release{
		Version:              resp.GetLatestVersion(),
		Channel:              offered,
		Arch:                 arch,
		AssetName:            assetName,
		AssetURL:             resp.GetAssetUrl(),
		ManifestURL:          resp.GetManifestUrl(),
		ManifestSignatureURL: resp.GetManifestSignatureUrl(),
		SHA256:               resp.GetSha256(),
	}, nil
}

func agentRestart(script string) updatecore.RestartFunc {
	return func(_ context.Context) (func() error, error) {
		path := script
		if path == "" {
			path = defaultUpdateScript
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("agent: restart wrapper %s: %w", path, err)
		}
		cmd := exec.Command("sudo", "-n", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("agent: start restart wrapper: %w", err)
		}
		return cmd.Wait, nil
	}
}
