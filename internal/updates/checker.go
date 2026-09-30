package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const (
	// DefaultRepo is the GitHub repository that publishes Gotham releases.
	DefaultRepo = "justindeelux/gotham"
	// DefaultBaseURL is the public GitHub Releases API. The configured base URL
	// can point at a GitHub Enterprise host or a local mirror.
	DefaultBaseURL = "https://api.github.com"
	// DefaultChannel is the release channel used unless overridden.
	DefaultChannel = ChannelStable
	// defaultTimeout bounds a single Releases API request.
	defaultTimeout = 10 * time.Second
	// maxReleases caps how many releases are decoded from the API response.
	maxReleases = 200
)

// Channel selects which releases are eligible.
type Channel string

const (
	// ChannelStable ignores prereleases.
	ChannelStable Channel = "stable"
	// ChannelBeta includes prereleases.
	ChannelBeta Channel = "beta"
)

// Release is an alias of the shared update candidate the checker resolves and
// the applier installs.
type Release = updatecore.Release

// Checker queries the Releases API for a repository and resolves an update.
type Checker struct {
	// BaseURL is the Releases API root (default DefaultBaseURL).
	BaseURL string
	// Repo is "owner/name".
	Repo string
	// Channel selects stable or beta releases.
	Channel Channel
	// Client is the HTTP client; a bounded default is used when nil.
	Client *http.Client
	// Timeout bounds a single request when Client is nil.
	Timeout time.Duration
	// GOOS/GOARCH select the asset, defaulting to the running platform.
	GOOS   string
	GOARCH string
	// AssetPrefix is the release asset family prefix; empty selects the
	// control-plane binary prefix "gotham-linux-". The node agent sets
	// "gotham-agent-linux-".
	AssetPrefix string
	// ManifestPrefix is the signed manifest family prefix; empty selects
	// "gotham-manifest-".
	ManifestPrefix string
}

// HTTP errors and resolution failures.
var (
	// ErrNoRelease is returned when the API response carries no releases.
	ErrNoRelease = errors.New("updates: no releases found")
	// ErrAssetNotFound is returned when a release has no asset for the
	// running platform.
	ErrAssetNotFound = errors.New("updates: platform asset not found")
	// ErrHTTP is returned for a non-2xx Releases API response.
	ErrHTTP = errors.New("updates: releases API request failed")
)

// ghRelease mirrors the subset of the GitHub Releases API we consume.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

// ghAsset mirrors a release asset.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Check returns the newest eligible release newer than current, or (nil, nil)
// when the running version is up to date. It fails closed: a malformed or
// unreachable API response is an error, never a silent "no update".
func (c *Checker) Check(ctx context.Context, current string) (*Release, error) {
	currentVersion, err := updatecore.ParseVersion(current)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(defaultString(c.BaseURL, DefaultBaseURL), "/")
	if err := updatecore.ValidateURL(base); err != nil {
		return nil, fmt.Errorf("%w: base url %q", err, base)
	}
	repo := strings.Trim(strings.TrimSpace(c.Repo), "/")
	if repo == "" {
		repo = DefaultRepo
	}

	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", base, repo, maxReleases)
	body, err := c.get(ctx, endpoint, 4<<20)
	if err != nil {
		return nil, err
	}

	var releases []ghRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("%w: decode releases: %v", ErrHTTP, err)
	}
	if len(releases) == 0 {
		return nil, ErrNoRelease
	}

	channel := c.channel()
	arch := c.arch()
	best := -1
	var bestVersion updatecore.Version

	for i, release := range releases {
		if release.Draft || release.TagName == "" {
			continue
		}
		if channel == ChannelStable && release.Prerelease {
			continue
		}
		version, parseErr := updatecore.ParseVersion(release.TagName)
		if parseErr != nil {
			continue
		}
		if version.Compare(currentVersion) <= 0 {
			continue
		}
		if best >= 0 && version.Compare(bestVersion) <= 0 {
			continue
		}
		best, bestVersion = i, version
	}
	if best < 0 {
		return nil, nil
	}

	release := releases[best]
	candidate, err := c.resolve(release, bestVersion, channel, arch)
	if err != nil {
		return nil, err
	}
	return candidate, nil
}

// resolve maps a chosen release onto the platform asset, its signed manifest
// and the manifest signature.
func (c *Checker) resolve(release ghRelease, version updatecore.Version, channel Channel, arch string) (*Release, error) {
	assetName := c.assetPrefix() + arch
	manifestName := updatecore.ManifestNameWithPrefix(c.ManifestPrefix, arch)
	manifestSigName := manifestName + updatecore.ManifestSigSuffix

	candidate := &Release{
		Version:     version.String(),
		Tag:         release.TagName,
		Channel:     string(channel),
		Prerelease:  release.Prerelease,
		Notes:       release.Body,
		PublishedAt: release.PublishedAt,
		Arch:        arch,
		AssetName:   assetName,
	}

	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			candidate.AssetURL = asset.BrowserDownloadURL
			candidate.AssetSize = asset.Size
		case manifestName:
			candidate.ManifestName = manifestName
			candidate.ManifestURL = asset.BrowserDownloadURL
		case manifestSigName:
			candidate.ManifestSignatureURL = asset.BrowserDownloadURL
		}
	}

	if candidate.AssetURL == "" {
		return nil, fmt.Errorf("%w: %s in release %s", ErrAssetNotFound, assetName, release.TagName)
	}
	if candidate.ManifestURL == "" {
		return nil, fmt.Errorf("%w: %s in release %s", ErrAssetNotFound, manifestName, release.TagName)
	}
	if candidate.ManifestSignatureURL == "" {
		return nil, fmt.Errorf("%w: %s in release %s", ErrAssetNotFound, manifestSigName, release.TagName)
	}
	for _, rawURL := range []string{candidate.AssetURL, candidate.ManifestURL, candidate.ManifestSignatureURL} {
		if err := updatecore.ValidateURL(rawURL); err != nil {
			return nil, err
		}
	}
	return candidate, nil
}

// get performs a bounded GET and returns the response body.
func (c *Checker) get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	if err := updatecore.ValidateURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gotham-selfupdate")

	client := c.Client
	if client == nil {
		client = updatecore.DefaultHTTPClient(defaultDuration(c.Timeout, defaultTimeout))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHTTP, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %s", ErrHTTP, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrHTTP, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: response too large", ErrHTTP)
	}
	return body, nil
}

// channel returns the configured channel, defaulting to stable.
func (c *Checker) channel() Channel {
	if c.Channel == ChannelBeta {
		return ChannelBeta
	}
	return ChannelStable
}

// arch returns the asset architecture, defaulting to the running platform.
func (c *Checker) arch() string {
	if c.GOARCH != "" {
		return c.GOARCH
	}
	return runtime.GOARCH
}

// assetPrefix returns the release asset family prefix.
func (c *Checker) assetPrefix() string {
	if c.AssetPrefix != "" {
		return c.AssetPrefix
	}
	return "gotham-linux-"
}

// defaultString returns value when non-empty, otherwise fallback.
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// defaultDuration returns value when positive, otherwise fallback.
func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
