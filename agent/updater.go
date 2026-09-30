package agent

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

// Timeouts. The RPC is short; the download it triggers (a full binary over a
// possibly slow link) is bounded separately and much longer, so a slow rollout
// does not fail on every poll.
const (
	updateRPCTimeout      = 15 * time.Second
	updateDownloadTimeout = 10 * time.Minute
)

// Failed-update backoff (N5). After a failed attempt the agent does not
// re-download the same version every poll: it waits, doubling per consecutive
// failure up to max. The attempt count is persisted (see backoffCount) so it
// survives the wrapper restart that follows a rollback and the delay escalates
// instead of staying flat.
const (
	// updateFailedSeedBase is the first failed-attempt backoff (5m); each
	// further attempt doubles it, capped at updateBackoffMax.
	updateFailedSeedBase = 5 * time.Minute
	updateBackoffMax     = time.Hour
)

// statusFreshness bounds how old a healthy status may be before it is treated
// as unconfirmed (N2), so a stale `ok` for the same version from a previous
// install is never trusted.
const statusFreshness = 5 * time.Minute

// updater polls the control plane for a verified agent update and applies it.
//
// It only ever builds a release from the authenticated CP response: the URL
// and digest come from the server-authenticated TLS gRPC channel, and
// updatecore re-validates the URL (https-only, bounded redirects, no
// link-local dials), verifies the Ed25519 signature over the signed manifest
// with the key embedded in this binary, binds version/arch/file/digest and
// swaps atomically with rollback.
type updater struct {
	cfg        Config
	log        *slog.Logger
	applier    *updatecore.Applier
	version    func() string
	setVersion func(string)
	// backoffCount persists the per-version failed-attempt count (agent-owned)
	// so the seeded backoff escalates across wrapper restarts.
	backoffCount *updatecore.StatusStore

	// backoffMu guards backoff, the per-version failed-attempt backoff (N5).
	backoffMu sync.Mutex
	backoff   map[string]*backoffState
}

// backoffState is the exponential backoff for one offered version.
type backoffState struct {
	failures int
	until    time.Time
}

// newUpdater builds an updater. It returns (nil, nil) when no release public
// key is configured: without a key the agent cannot verify an offer, so it must
// not apply one. An empty BinaryPath also disables it (tests and dev runs).
func newUpdater(cfg Config, log *slog.Logger, version func() string, setVersion func(string)) (*updater, error) {
	if cfg.BinaryPath == "" {
		return nil, nil
	}
	publicKey, err := updatecore.LoadPublicKey()
	if err != nil {
		return nil, nil
	}
	verifier, err := updatecore.NewVerifier(publicKey)
	if err != nil {
		return nil, err
	}
	applier := &updatecore.Applier{
		Verifier:   verifier,
		BinaryPath: cfg.BinaryPath,
		OldPath:    cfg.BinaryPath + updatecore.OldSuffix,
		LockPath:   cfg.UpdateLockPath,
		Pending:    updatecore.NewStatusStore(cfg.UpdatePendingPath),
		Status:     updatecore.NewStatusStore(cfg.UpdateStatusPath),
		Restart:    cfg.Restart,
	}
	if applier.Restart == nil {
		applier.Restart = agentRestart(cfg.UpdateScript)
	}
	if restored, err := applier.Recover(); err != nil {
		log.Warn("agent: startup update recovery failed", "error", err)
	} else if restored {
		log.Warn("agent: restored the previous binary after an interrupted update")
	}
	// Resume a staged update left behind by a crash, reboot or OOM during the
	// wrapper's health window, exactly as the control plane does at startup. A
	// staged marker otherwise leaves the unproven binary running and refuses
	// every later update with ErrUpdatePending forever. ResumeStaged uses the
	// non-blocking lock, so it never waits for a wrapper that already owns the
	// outcome, and it rewrites the marker so it never loops. The health endpoint
	// is already listening by now (main starts it before NewAgent).
	if err := applier.ResumeStaged(context.Background()); err != nil {
		log.Warn("agent: could not resume a staged update", "error", err)
	}
	u := &updater{
		cfg:          cfg,
		log:          log,
		applier:      applier,
		version:      version,
		setVersion:   setVersion,
		backoffCount: updatecore.NewStatusStore(cfg.UpdateBackoffPath),
		backoff:      map[string]*backoffState{},
	}
	// A wrapper restart clears this process's in-memory backoff, so seed it from
	// the durable status: a release that just rolled back must not be re-applied
	// immediately (the rollout target may be unchanged).
	u.seedBackoffFromStatus()
	return u, nil
}

// run polls immediately and then once per interval until ctx is cancelled.
func (u *updater) run(ctx context.Context, client agentv1.UpdateServiceClient) {
	u.checkOnce(ctx, client)
	ticker := time.NewTicker(u.cfg.UpdateInterval)
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

// checkOnce asks the CP whether a newer agent version is available and, when
// one is offered, applies it. A plain offer is only applied when unattended
// auto-update is enabled; an operator-triggered rollout is always applied.
func (u *updater) checkOnce(ctx context.Context, client agentv1.UpdateServiceClient) {
	// An operator reset (or a durable-status change) clears a backoff so a fixed
	// release can be retried.
	u.consumeRetry()

	rpcCtx, cancel := context.WithTimeout(ctx, updateRPCTimeout)
	resp, err := client.RequestUpdate(rpcCtx, &agentv1.UpdateRequest{
		AgentVersion: u.version(),
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	})
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
	if !u.cfg.AutoUpdate && !resp.GetRollout() {
		u.log.Debug("agent: update available; unattended auto-update is off",
			"version", resp.GetLatestVersion())
		return
	}

	release, err := releaseFromOffer(resp)
	if err != nil {
		u.log.Warn("agent: refused an incomplete update offer", "error", err)
		return
	}
	// Monotonic guard: a compromised or buggy CP must not roll the fleet back to
	// an older, still validly signed release. `dev`/unparsable versions fail
	// closed. The local `.old` rollback stays the supported path.
	if !isNewerVersion(release.Version, u.version()) {
		u.log.Warn("agent: refusing a non-newer update offer",
			"offered", release.Version, "running", u.version())
		return
	}
	// A persistently failing version is backed off so the full binary is not
	// re-downloaded on every poll (N5).
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
	// Do not trust the launch: adopt the new version only when the durable,
	// root-owned status proves the new binary healthy. On a real restart this
	// process is replaced and the new process reports its own build version; on
	// rolled_back/rollback_failed/wrapper_failed/still-staged the running
	// version is kept, so a failed update never makes the node claim the new
	// version and never suppresses the retry.
	if u.statusHealthy(outcome.Version) {
		u.clearBackoff(outcome.Version)
		u.setVersion(outcome.Version)
		return
	}
	if outcome.Staged {
		// The wrapper owns the outcome and normally replaces this process before
		// it can observe it. Only arm a backoff when the durable status already
		// proves the rollback (for example a synchronous test hook); otherwise
		// the next startup seeds it from the status, so a healthy staged update
		// never leaves a spurious backoff or attempt-count file behind.
		if u.statusFailed(release.Version) {
			u.recordFailure(release.Version)
		}
		return
	}
	u.recordFailure(release.Version)
}

// statusHealthy reports whether the authoritative status records the new binary
// as healthy for version within the freshness window. A stale `ok` for the same
// version (for example from a previous install) is treated as unconfirmed.
func (u *updater) statusHealthy(version string) bool {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil || status.Result != updatecore.StatusOK || status.Version != version {
		return false
	}
	return time.Since(status.At) <= statusFreshness
}

// inBackoff reports whether version is currently backed off.
func (u *updater) inBackoff(version string) bool {
	u.backoffMu.Lock()
	defer u.backoffMu.Unlock()
	state, ok := u.backoff[version]
	return ok && time.Now().Before(state.until)
}

// recordFailure increments the persisted, exponential backoff for version after
// a failed attempt.
func (u *updater) recordFailure(version string) {
	u.applyBackoff(version, time.Now())
}

// applyBackoff increments the persisted failed-attempt count for version, records
// the failure time, and arms the exponential in-memory backoff.
func (u *updater) applyBackoff(version string, failureAt time.Time) {
	rec := u.readBackoff(version)
	rec.count++
	u.writeBackoff(version, rec.count, failureAt)
	u.armBackoff(version, rec.count)
}

// armBackoff arms the exponential in-memory backoff for version without
// changing the persisted count.
func (u *updater) armBackoff(version string, count int) {
	delay := seedDelay(count)
	u.backoffMu.Lock()
	u.backoff[version] = &backoffState{failures: count, until: time.Now().Add(delay)}
	u.backoffMu.Unlock()
	u.log.Info("agent: backing off a failed update",
		"version", version, "failures", count, "retry_in", delay.String())
}

// clearBackoff drops the backoff for version after a successful update and
// resets the persisted attempt count.
func (u *updater) clearBackoff(version string) {
	u.backoffMu.Lock()
	delete(u.backoff, version)
	u.backoffMu.Unlock()
	u.clearBackoffCount()
}

// statusFailed reports whether the durable status records a rollback for
// version (rolled_back or rollback_failed).
func (u *updater) statusFailed(version string) bool {
	status, err := u.applier.Status.Read()
	if err != nil || status == nil || status.Version != version {
		return false
	}
	return status.Result == updatecore.StatusRolledBack || status.Result == updatecore.StatusRollbackFailed
}

// seedBackoffFromStatus seeds the backoff for the version a durable
// rolled_back/rollback_failed status names when it is newer than the running
// version, so a wrapper restart does not immediately re-apply a release that
// just failed. The persisted attempt count is only bumped for a *new* failure
// (the status `at` changed), so an unrelated restart (reboot, OOM, container
// restart) does not escalate it; the delay still escalates across genuine
// attempts and is capped.
func (u *updater) seedBackoffFromStatus() {
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
		// First observation of this failure, or a new failure time.
		u.applyBackoff(status.Version, status.At)
		u.log.Warn("agent: seeded a backoff from a durable rollback",
			"version", status.Version, "result", status.Result, "failures", rec.count+1)
		return
	}
	// The same failure is still on disk (a restart, not a new attempt): arm the
	// existing backoff without counting it again.
	u.armBackoff(status.Version, rec.count)
	u.log.Warn("agent: re-armed the backoff for an unchanged rollback",
		"version", status.Version, "failures", rec.count)
}

// consumeRetry clears the whole backoff when an operator reset left the retry
// marker, and removes the marker and the persisted count. It is a cheap stat on
// the common path.
func (u *updater) consumeRetry() {
	path := strings.TrimSpace(u.cfg.UpdateRetryPath)
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

// seedDelay is the exponential failed-attempt delay: base * 2^(count-1), capped.
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

// backoffRecord is the persisted failed-attempt state for one version.
type backoffRecord struct {
	count int
	at    time.Time
}

// readBackoff returns the persisted failed-attempt state for version, or a zero
// record when none matches.
func (u *updater) readBackoff(version string) backoffRecord {
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

// writeBackoff persists the failed-attempt count and the failure time for
// version using the hardened StatusStore write (temp file + rename, no path
// chmod, no symlink follow).
func (u *updater) writeBackoff(version string, count int, at time.Time) {
	if u.backoffCount == nil {
		return
	}
	if err := u.backoffCount.Write(updatecore.Status{
		Result: "backoff", Version: version, Detail: strconv.Itoa(count), At: at,
	}); err != nil {
		u.log.Warn("agent: could not persist the failed-update count", "error", err)
	}
}

// clearBackoffCount removes the persisted failed-attempt state.
func (u *updater) clearBackoffCount() {
	if u.backoffCount == nil {
		return
	}
	_ = u.backoffCount.Remove()
}

// ResetUpdateState is the operator retry path (`gotham-agent update reset`). It
// clears the agent-owned pending marker, removes the authoritative status when
// permitted (it is root-owned; a non-root reset still works through the retry
// marker) and writes the retry marker a running agent consumes to clear its
// in-memory backoff.
func ResetUpdateState(cfg Config) error {
	if err := updatecore.NewStatusStore(cfg.UpdatePendingPath).Remove(); err != nil {
		return err
	}
	if path := strings.TrimSpace(cfg.UpdateStatusPath); path != "" {
		// Best effort: the status directory is root-owned, so a non-root reset
		// cannot remove it; the retry marker below still applies.
		_ = os.Remove(path)
	}
	path := strings.TrimSpace(cfg.UpdateRetryPath)
	if path == "" {
		return nil
	}
	return writeRetryMarker(path)
}

// writeRetryMarker writes the operator retry marker atomically. It never
// follows a symlink and never opens a non-regular file: it stages a temp file
// (O_EXCL, no-follow) and renames it over the path, so a symlink or FIFO that
// the service user planted in its own directory cannot redirect or block a root
// run. os.Rename replaces the path itself; it does not open it.
//
// The mode is set on the open file descriptor, never by path: a path-based chmod
// in the agent-owned directory could be redirected to a symlink target swapped
// in by the directory owner (R4-M1).
func writeRetryMarker(path string) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		// Refuse a symlinked parent: a nested retry path under the state dir
		// could otherwise let the service user redirect the write (LOW).
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
	// fchmod on the descriptor, before any path-based operation: the temp name
	// can be swapped for a symlink by the directory owner at any moment.
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

// isNewerVersion reports whether offered is strictly newer than current. An
// unparsable version on either side fails closed (not newer).
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

// releaseFromOffer turns a CP offer into the updatecore release the applier
// verifies. Every URL and the digest are re-checked by the applier; this only
// assembles the candidate and derives the asset file name from the asset URL
// so the signed manifest's file field can be bound.
func releaseFromOffer(resp *agentv1.UpdateResponse) (*updatecore.Release, error) {
	if resp.GetLatestVersion() == "" || resp.GetAssetUrl() == "" ||
		resp.GetManifestUrl() == "" || resp.GetManifestSignatureUrl() == "" {
		return nil, errors.New("agent: update offer is missing release material")
	}
	assetName := ""
	if parsed, err := url.Parse(resp.GetAssetUrl()); err == nil {
		assetName = path.Base(parsed.Path)
	}
	if assetName == "" || assetName == "." || assetName == "/" {
		return nil, errors.New("agent: update offer has no asset name")
	}
	return &updatecore.Release{
		Version:              resp.GetLatestVersion(),
		Channel:              resp.GetChannel(),
		Arch:                 runtime.GOARCH,
		AssetName:            assetName,
		AssetURL:             resp.GetAssetUrl(),
		ManifestURL:          resp.GetManifestUrl(),
		ManifestSignatureURL: resp.GetManifestSignatureUrl(),
		SHA256:               resp.GetSha256(),
	}, nil
}

// agentRestart runs the fixed, root-owned restart/healthcheck wrapper through
// sudo and returns a waiter for its exit. The wrapper takes no arguments: its
// binary, service, health URL, status, pending marker and lock come from a
// root-owned configuration file. The unit must keep KillMode=process so the
// wrapper survives the restart cgroup, and must not set NoNewPrivileges because
// this call needs setuid sudo (the only privileged action granted is the fixed
// wrapper).
func agentRestart(script string) updatecore.RestartFunc {
	return func(_ context.Context) (func() error, error) {
		path := script
		if path == "" {
			path = defaultAgentUpdateScript
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
