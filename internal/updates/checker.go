package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const (
	DefaultRepo         = "justindeelux/gotham"
	DefaultBaseURL      = "https://api.github.com"
	DefaultChannel      = ChannelStable
	defaultTimeout      = 10 * time.Second
	maxReleases         = 200
	maxChangelogEntries = 20
	maxNoteBytes        = 32 << 10
)

type Channel string

const (
	ChannelStable Channel = "stable"
	ChannelBeta   Channel = "beta"
)

type Release = updatecore.Release

type Checker struct {
	BaseURL        string
	Repo           string
	Channel        Channel
	Client         *http.Client
	Timeout        time.Duration
	GOOS           string
	GOARCH         string
	AssetPrefix    string
	ManifestPrefix string
}

var (
	ErrNoRelease     = errors.New("updates: no releases found")
	ErrAssetNotFound = errors.New("updates: platform asset not found")
	ErrHTTP          = errors.New("updates: releases API request failed")
)

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ChangelogEntry struct {
	Version     string
	Tag         string
	Channel     string
	Prerelease  bool
	Notes       string
	PublishedAt time.Time
	HTMLURL     string
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func (c *Checker) Check(ctx context.Context, current string) (*Release, error) {
	currentVersion, err := updatecore.ParseVersion(current)
	if err != nil {
		return nil, err
	}

	releases, err := c.fetchReleases(ctx)
	if err != nil {
		return nil, err
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
	candidate, err := c.resolve(release, bestVersion, arch)
	if err != nil {
		return nil, err
	}
	return candidate, nil
}

func (c *Checker) fetchReleases(ctx context.Context) ([]ghRelease, error) {
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
	return releases, nil
}

func (c *Checker) Changelog(ctx context.Context, current string) ([]ChangelogEntry, error) {
	releases, err := c.fetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	currentVersion, err := updatecore.ParseVersion(current)
	if err != nil {
		return nil, nil
	}
	channel := c.channel()
	var rankedList []rankedChangelog
	for _, release := range releases {
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
		releaseChannel := ChannelStable
		if release.Prerelease {
			releaseChannel = ChannelBeta
		}
		rankedList = append(rankedList, rankedChangelog{
			entry: ChangelogEntry{
				Version:     version.String(),
				Tag:         release.TagName,
				Channel:     string(releaseChannel),
				Prerelease:  release.Prerelease,
				Notes:       truncateNotes(release.Body),
				PublishedAt: release.PublishedAt,
				HTMLURL:     httpsLink(release.HTMLURL),
			},
			version: version,
		})
	}
	sortChangelog(rankedList)
	entries := make([]ChangelogEntry, 0, min(len(rankedList), maxChangelogEntries))
	for i := range rankedList {
		if len(entries) >= maxChangelogEntries {
			break
		}
		entries = append(entries, rankedList[i].entry)
	}
	return entries, nil
}

func (c *Checker) CurrentChangelog(ctx context.Context, current string) (*ChangelogEntry, error) {
	currentVersion, err := updatecore.ParseVersion(current)
	if err != nil {
		return nil, nil
	}
	releases, err := c.fetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	for _, release := range releases {
		if release.Draft || release.TagName == "" {
			continue
		}
		version, parseErr := updatecore.ParseVersion(release.TagName)
		if parseErr != nil || version.Compare(currentVersion) != 0 {
			continue
		}
		releaseChannel := ChannelStable
		if release.Prerelease {
			releaseChannel = ChannelBeta
		}
		entry := &ChangelogEntry{
			Version:     version.String(),
			Tag:         release.TagName,
			Channel:     string(releaseChannel),
			Prerelease:  release.Prerelease,
			Notes:       truncateNotes(release.Body),
			PublishedAt: release.PublishedAt,
			HTMLURL:     httpsLink(release.HTMLURL),
		}
		return entry, nil
	}
	return nil, nil
}

type rankedChangelog struct {
	entry   ChangelogEntry
	version updatecore.Version
}

func sortChangelog(rankedList []rankedChangelog) {
	sort.Slice(rankedList, func(i, j int) bool {
		return rankedList[i].version.Compare(rankedList[j].version) > 0
	})
}

func truncateNotes(body string) string {
	if len(body) <= maxNoteBytes {
		return body
	}
	return body[:maxNoteBytes] + "\n\n…(truncated)"
}

func httpsLink(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func (c *Checker) resolve(release ghRelease, version updatecore.Version, arch string) (*Release, error) {
	assetName := c.assetPrefix() + arch
	manifestName := updatecore.ManifestNameWithPrefix(c.ManifestPrefix, arch)
	manifestSigName := manifestName + updatecore.ManifestSigSuffix

	releaseChannel := ChannelStable
	if release.Prerelease {
		releaseChannel = ChannelBeta
	}
	candidate := &Release{
		Version:     version.String(),
		Tag:         release.TagName,
		Channel:     string(releaseChannel),
		Prerelease:  release.Prerelease,
		Notes:       release.Body,
		HTMLURL:     httpsLink(release.HTMLURL),
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

func (c *Checker) channel() Channel {
	if c.Channel == ChannelBeta {
		return ChannelBeta
	}
	return ChannelStable
}

func (c *Checker) arch() string {
	if c.GOARCH != "" {
		return c.GOARCH
	}
	return runtime.GOARCH
}

func (c *Checker) assetPrefix() string {
	if c.AssetPrefix != "" {
		return c.AssetPrefix
	}
	return "gotham-linux-"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
