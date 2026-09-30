package updates

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// Agent release asset family. The node agent binary is published as
// gotham-agent-linux-<arch> with its own signed manifest
// gotham-agent-manifest-<arch>.txt, distinct from the control-plane binary so a
// single release can carry both.
const (
	AgentAssetPrefix    = "gotham-agent-linux-"
	AgentManifestPrefix = "gotham-agent-manifest-"
)

// Bounds for the small signed-manifest fetches the offer verification performs.
const (
	maxAgentManifestBytes  = 64 << 10
	maxAgentSignatureBytes = 4 << 10
	// defaultOfferCacheTTL bounds how long a verified offer is reused, so a
	// fleet polling RequestUpdate does not re-hit the release API and the
	// manifest on every poll.
	defaultOfferCacheTTL = 2 * time.Minute
)

// SupportedAgentArches are the architectures the agent build publishes.
var SupportedAgentArches = []string{"amd64", "arm64"}

// AgentUpdaterConfig wires the control-plane side of the agent update flow.
type AgentUpdaterConfig struct {
	Repo      string
	BaseURL   string
	Channel   Channel
	PublicKey ed25519.PublicKey
	// Client overrides the HTTP client used for the release API and manifest
	// fetches (tests).
	Client  *http.Client
	Timeout time.Duration
	Logger  *slog.Logger
	// CacheTTL bounds offer caching; zero uses the default.
	CacheTTL time.Duration
}

// AgentUpdater resolves and verifies agent update offers. It owns the trust
// chain on the control-plane side: an offer is only ever returned after the
// release API has been queried and the signed manifest has been fetched and
// verified with the release public key. It never returns an unverified offer.
type AgentUpdater struct {
	checker  Checker
	verifier *Verifier
	client   *http.Client
	logger   *slog.Logger
	ttl      time.Duration

	mu    sync.Mutex
	cache map[string]cachedOffer
}

// cachedOffer is one verified offer keyed by arch|version.
type cachedOffer struct {
	release *Release
	expires time.Time
}

// NewAgentUpdater builds an updater. It returns (nil, nil) when no public key
// is configured: without a key the control plane cannot prove an offer is
// signed, so it must not make one (the agent embeds its own key and fails
// closed too).
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
		client: cfg.Client,
		logger: logger,
		ttl:    defaultDuration(cfg.CacheTTL, defaultOfferCacheTTL),
		cache:  map[string]cachedOffer{},
	}
	if cfg.PublicKey == nil {
		return nil, nil
	}
	verifier, err := NewVerifier(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	updater.verifier = verifier
	return updater, nil
}

// AgentUpdaterFromEnv assembles an AgentUpdater from the environment. It
// returns (nil, nil) when FEATURE_UPDATES=false or no public key is embedded:
// the update surface is then off.
func AgentUpdaterFromEnv(logger *slog.Logger) (*AgentUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !Enabled() {
		return nil, nil
	}
	publicKey, err := LoadPublicKey()
	if err != nil {
		logger.Info("updates: agent update offers disabled; no release public key", "reason", err)
		return nil, nil
	}
	return NewAgentUpdater(AgentUpdaterConfig{
		Repo:      RepoFromEnv(),
		BaseURL:   BaseURLFromEnv(),
		Channel:   ChannelFromEnv(),
		PublicKey: publicKey,
		Timeout:   defaultTimeout,
		Logger:    logger,
	})
}

// Offer resolves the newest agent release above agentVersion for the given
// platform. It returns (nil, nil) when the agent is current, the platform is
// unsupported, or no offer can be verified. A release-API or manifest failure
// is returned as an error so the caller can log it, but it is never turned into
// a partial offer.
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

	checker := a.checker
	checker.GOARCH = arch
	release, err := checker.Check(ctx, agentVersion)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	if cached := a.cached(arch, release.Version); cached != nil {
		return cached, nil
	}
	manifest, err := a.verifyManifest(ctx, release)
	if err != nil {
		return nil, err
	}
	release.SHA256 = manifest.SHA256
	a.store(arch, release.Version, release)
	return release, nil
}

// verifyManifest downloads the signed manifest and its detached signature,
// verifies the Ed25519 signature and binds the manifest to the resolved
// release (version, arch, file, channel).
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

// get performs a bounded GET against a verified destination.
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

// cached returns a copy of a live verified offer, or nil.
func (a *AgentUpdater) cached(arch, version string) *Release {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[arch+"|"+version]
	if !ok || time.Now().After(entry.expires) {
		return nil
	}
	clone := *entry.release
	return &clone
}

// store caches a verified offer.
func (a *AgentUpdater) store(arch, version string, release *Release) {
	clone := *release
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[arch+"|"+version] = cachedOffer{release: &clone, expires: time.Now().Add(a.ttl)}
}

// supportedAgentArch reports whether arch is a published agent architecture.
func supportedAgentArch(arch string) bool {
	for _, candidate := range SupportedAgentArches {
		if arch == candidate {
			return true
		}
	}
	return false
}
