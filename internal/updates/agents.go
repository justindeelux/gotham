package updates

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const (
	AgentAssetPrefix    = "gotham-agent-linux-"
	AgentManifestPrefix = "gotham-agent-manifest-"
)

const (
	maxAgentManifestBytes    = 64 << 10
	maxAgentSignatureBytes   = 4 << 10
	defaultOfferCacheTTL     = 10 * time.Minute
	minNegativeOfferCacheTTL = time.Second
	agentFloorVersion        = "v0.0.0"
)

var SupportedAgentArches = []string{"amd64", "arm64"}

type AgentUpdaterConfig struct {
	Repo       string
	BaseURL    string
	Channel    Channel
	PublicKeys []ed25519.PublicKey
	Client     *http.Client
	Timeout    time.Duration
	Logger     *slog.Logger
	CacheTTL   time.Duration
}

type AgentUpdater struct {
	checker     Checker
	verifier    *Verifier
	client      *http.Client
	logger      *slog.Logger
	ttl         time.Duration
	negativeTTL time.Duration

	mu      sync.Mutex
	cache   map[string]*cachedEntry
	fetchMu sync.Mutex
}

type cachedEntry struct {
	release    *Release
	freshUntil time.Time
	failure    error
	retryAt    time.Time
}

func NewAgentUpdater(cfg AgentUpdaterConfig) (*AgentUpdater, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	updater := &AgentUpdater{
		checker: Checker{
			BaseURL:        cfg.BaseURL,
			Repo:           cfg.Repo,
			Channel:        cfg.Channel,
			Client:         cfg.Client,
			Timeout:        cfg.Timeout,
			AssetPrefix:    AgentAssetPrefix,
			ManifestPrefix: AgentManifestPrefix,
		},
		client:      cfg.Client,
		logger:      logger,
		ttl:         defaultDuration(cfg.CacheTTL, defaultOfferCacheTTL),
		negativeTTL: negativeCacheTTL(cfg.CacheTTL),
		cache:       map[string]*cachedEntry{},
	}
	if len(cfg.PublicKeys) == 0 {
		return nil, nil
	}
	verifier, err := NewVerifierSet(cfg.PublicKeys...)
	if err != nil {
		return nil, err
	}
	updater.verifier = verifier
	return updater, nil
}

func AgentUpdaterFromEnv(logger *slog.Logger) (*AgentUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !Enabled() {
		return nil, nil
	}
	publicKeys, err := LoadPublicKeys()
	if err != nil {
		logger.Info("updates: agent update offers disabled; no release public key", "reason", err)
		return nil, nil
	}
	return NewAgentUpdater(AgentUpdaterConfig{
		Repo:       RepoFromEnv(),
		BaseURL:    BaseURLFromEnv(),
		Channel:    ChannelFromEnv(),
		PublicKeys: publicKeys,
		Timeout:    defaultTimeout,
		Logger:     logger,
		CacheTTL:   OfferCacheTTLFromEnv(),
	})
}

func (a *AgentUpdater) Offer(ctx context.Context, agentVersion, goos, goarch string) (*Release, error) {
	if a == nil || a.verifier == nil {
		return nil, nil
	}
	if goos != "" && !strings.EqualFold(goos, "linux") {
		return nil, nil
	}
	arch := strings.ToLower(strings.TrimSpace(goarch))
	if !supportedAgentArch(arch) {
		return nil, fmt.Errorf("%w: unsupported agent arch %q", ErrAssetNotFound, goarch)
	}

	release, err := a.resolve(ctx, arch)
	if err != nil || release == nil {
		return nil, err
	}

	current, err := updatecore.ParseVersion(agentVersion)
	if err != nil {
		return nil, nil
	}
	offered, err := updatecore.ParseVersion(release.Version)
	if err != nil {
		return nil, nil
	}
	if offered.Compare(current) <= 0 {
		return nil, nil
	}
	return release, nil
}

func (a *AgentUpdater) TargetVersion(ctx context.Context) (string, error) {
	if a == nil || a.verifier == nil {
		return "", nil
	}
	var lastErr error
	for _, arch := range SupportedAgentArches {
		release, err := a.resolve(ctx, arch)
		if err != nil {
			lastErr = err
			continue
		}
		if release != nil {
			return release.Version, nil
		}
	}
	return "", lastErr
}

func (a *AgentUpdater) resolve(ctx context.Context, arch string) (*Release, error) {
	if release, err, ok := a.cached(arch, time.Now()); ok {
		return release, err
	}

	a.fetchMu.Lock()
	defer a.fetchMu.Unlock()
	if release, err, ok := a.cached(arch, time.Now()); ok {
		return release, err
	}

	release, err := a.fetchRelease(ctx, arch)
	if err != nil {
		if errors.Is(err, ErrNoRelease) {
			a.storeEmpty(arch)
			return nil, nil
		}
		a.storeFailure(arch, err)
		if stale := a.stale(arch); stale != nil {
			a.logger.Warn("updates: serving a stale agent offer after a release lookup failure",
				"arch", arch, "error", err)
			return stale, nil
		}
		return nil, err
	}
	if release == nil {
		a.storeEmpty(arch)
		return nil, nil
	}
	a.store(arch, release)
	return release, nil
}

func (a *AgentUpdater) cached(arch string, now time.Time) (*Release, error, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[arch]
	if !ok {
		return nil, nil, false
	}
	if entry.release != nil && now.Before(entry.freshUntil) {
		clone := *entry.release
		return &clone, nil, true
	}
	if now.Before(entry.retryAt) {
		if entry.release != nil {
			clone := *entry.release
			return &clone, nil, true
		}
		return nil, entry.failure, true
	}
	return nil, nil, false
}

func (a *AgentUpdater) stale(arch string) *Release {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[arch]
	if !ok || entry.release == nil {
		return nil
	}
	clone := *entry.release
	return &clone
}

func (a *AgentUpdater) store(arch string, release *Release) {
	clone := *release
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[arch] = &cachedEntry{
		release:    &clone,
		freshUntil: now.Add(a.ttl),
		retryAt:    now.Add(a.ttl),
	}
}

func (a *AgentUpdater) storeFailure(arch string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[arch]
	if !ok {
		entry = &cachedEntry{}
		a.cache[arch] = entry
	}
	entry.failure = err
	entry.retryAt = time.Now().Add(a.negativeTTL)
}

func (a *AgentUpdater) storeEmpty(arch string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[arch]
	if !ok {
		entry = &cachedEntry{}
		a.cache[arch] = entry
	}
	entry.failure = nil
	entry.retryAt = time.Now().Add(a.negativeTTL)
}

func negativeCacheTTL(configured time.Duration) time.Duration {
	ttl := defaultDuration(configured, defaultOfferCacheTTL) / 5
	if ttl < minNegativeOfferCacheTTL {
		return minNegativeOfferCacheTTL
	}
	return ttl
}

func (a *AgentUpdater) fetchRelease(ctx context.Context, arch string) (*Release, error) {
	checker := a.checker
	checker.GOARCH = arch
	release, err := checker.Check(ctx, agentFloorVersion)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	manifest, err := a.verifyManifest(ctx, release)
	if err != nil {
		return nil, err
	}
	release.SHA256 = manifest.SHA256
	return release, nil
}

func (a *AgentUpdater) verifyManifest(ctx context.Context, release *Release) (updatecore.Manifest, error) {
	manifestBytes, err := a.get(ctx, release.ManifestURL, maxAgentManifestBytes)
	if err != nil {
		return updatecore.Manifest{}, err
	}
	signature, err := a.get(ctx, release.ManifestSignatureURL, maxAgentSignatureBytes)
	if err != nil {
		return updatecore.Manifest{}, err
	}
	if err := a.verifier.Verify(manifestBytes, signature); err != nil {
		return updatecore.Manifest{}, err
	}
	manifest, err := updatecore.ParseManifest(manifestBytes)
	if err != nil {
		return updatecore.Manifest{}, err
	}
	switch {
	case manifest.Version != release.Version:
		return updatecore.Manifest{}, fmt.Errorf("%w: manifest version %q disagrees with %q", ErrManifest, manifest.Version, release.Version)
	case manifest.Arch != release.Arch:
		return updatecore.Manifest{}, fmt.Errorf("%w: manifest arch %q disagrees with %q", ErrManifest, manifest.Arch, release.Arch)
	case manifest.File != release.AssetName:
		return updatecore.Manifest{}, fmt.Errorf("%w: manifest file %q disagrees with %q", ErrManifest, manifest.File, release.AssetName)
	case release.Channel != "" && manifest.Channel != release.Channel:
		return updatecore.Manifest{}, fmt.Errorf("%w: manifest channel %q disagrees with %q", ErrManifest, manifest.Channel, release.Channel)
	}
	return manifest, nil
}

func (a *AgentUpdater) get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	if err := updatecore.ValidateURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gotham-selfupdate")
	client := a.client
	if client == nil {
		client = updatecore.DefaultHTTPClient(defaultDuration(a.checker.Timeout, defaultTimeout))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", ErrDownload, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrDownload, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, rawURL, maxBytes)
	}
	return data, nil
}

func supportedAgentArch(arch string) bool {
	for _, candidate := range SupportedAgentArches {
		if arch == candidate {
			return true
		}
	}
	return false
}
