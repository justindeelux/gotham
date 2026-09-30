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
	"runtime"
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
// re-download the same version every poll: it waits base, doubling per
// consecutive failure up to max.
const (
	updateBackoffBase = time.Minute
	updateBackoffMax  = time.Hour
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
	return &updater{
		cfg:        cfg,
		log:        log,
		applier:    applier,
		version:    version,
		setVersion: setVersion,
		backoff:    map[string]*backoffState{},
	}, nil
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

// recordFailure extends the exponential backoff for version.
func (u *updater) recordFailure(version string) {
	u.backoffMu.Lock()
	defer u.backoffMu.Unlock()
	state, ok := u.backoff[version]
	if !ok {
		state = &backoffState{}
		u.backoff[version] = state
	}
	state.failures++
	delay := updateBackoffBase
	for i := 1; i < state.failures && delay < updateBackoffMax; i++ {
		delay *= 2
	}
	if delay > updateBackoffMax {
		delay = updateBackoffMax
	}
	state.until = time.Now().Add(delay)
	u.log.Info("agent: backing off after a failed update",
		"version", version, "failures", state.failures, "retry_in", delay.String())
}

// clearBackoff drops the backoff for version after a successful update.
func (u *updater) clearBackoff(version string) {
	u.backoffMu.Lock()
	defer u.backoffMu.Unlock()
	delete(u.backoff, version)
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
