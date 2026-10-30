package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	cleanupCategoryBuildCache        = "build_cache"
	cleanupCategoryDanglingImages    = "dangling_images"
	cleanupCategoryStoppedContainers = "stopped_containers"
	cleanupCategoryUnusedImages      = "unused_images"

	cleanupIDBuildCache      = "build-cache"
	cleanupIDImagePrefix     = "image:"
	cleanupIDContainerPrefix = "container:"

	cleanupStatusRemoved = "removed"
	cleanupStatusSkipped = "skipped"
	cleanupStatusFailed  = "failed"

	cleanupManagedLabel = "gotham.managed"

	// rollbackImageKeep is the number of newest Gotham app images a manual or
	// scheduled cleanup never offers: those are the rollback targets.
	rollbackImageKeep = 3
)

type cleanupCandidate struct {
	item *agentv1.CleanupItem
	// ref is what the engine removes: an image ID, a container ID or the
	// aggregate build-cache handle.
	ref string
}

type cleanupScan struct {
	items      []*agentv1.CleanupItem
	candidates map[string]cleanupCandidate
	// excluded remembers why a still-present object was not listed, so Clean
	// can report "in use" or "too new" instead of a generic skip.
	excluded map[string]string
}

func newCleanupScan() *cleanupScan {
	return &cleanupScan{
		candidates: map[string]cleanupCandidate{},
		excluded:   map[string]string{},
	}
}

// ScanCleanup lists reclaimable Docker objects without removing anything.
func (c *DockerClient) ScanCleanup(ctx context.Context, minAge time.Duration) ([]*agentv1.CleanupItem, error) {
	scan, err := c.scanCleanup(ctx, minAge)
	if err != nil {
		return nil, err
	}
	return scan.items, nil
}

// Clean removes exactly the selected ids. Every id is re-checked against the
// node state first: an object that became in use or too new is skipped.
func (c *DockerClient) Clean(ctx context.Context, ids []string, minAge time.Duration) ([]*agentv1.CleanupItemResult, error) {
	scan, err := c.scanCleanup(ctx, minAge)
	if err != nil {
		return nil, err
	}
	cutoff := minAgeCutoff(minAge)
	results := make([]*agentv1.CleanupItemResult, 0, len(ids))
	for _, id := range ids {
		results = append(results, c.cleanOne(ctx, id, scan, cutoff))
	}
	return results, nil
}

func (c *DockerClient) cleanOne(ctx context.Context, id string, scan *cleanupScan, cutoff int64) *agentv1.CleanupItemResult {
	result := &agentv1.CleanupItemResult{Id: id}
	candidate, ok := scan.candidates[id]
	if !ok {
		if reason, present := scan.excluded[id]; present {
			result.Status = cleanupStatusSkipped
			result.Message = reason
			return result
		}
		switch {
		case strings.HasPrefix(id, cleanupIDImagePrefix),
			strings.HasPrefix(id, cleanupIDContainerPrefix),
			id == cleanupIDBuildCache:
			result.Status = cleanupStatusSkipped
			result.Message = "no longer present"
		default:
			result.Status = cleanupStatusFailed
			result.Message = "unknown item"
		}
		return result
	}

	result.Status = cleanupStatusRemoved
	result.ReclaimedBytes = candidate.item.GetSizeBytes()
	switch candidate.item.GetCategory() {
	case cleanupCategoryBuildCache:
		reclaimed, err := c.pruneBuildCache(ctx, cutoff)
		if err != nil {
			result.Status = cleanupStatusFailed
			result.Message = err.Error()
			result.ReclaimedBytes = 0
			return result
		}
		result.ReclaimedBytes = reclaimed
	case cleanupCategoryDanglingImages, cleanupCategoryUnusedImages:
		if err := c.removeImage(ctx, candidate.ref); err != nil {
			result.Status = cleanupStatusFailed
			result.Message = err.Error()
			result.ReclaimedBytes = 0
		}
	case cleanupCategoryStoppedContainers:
		// A cleanup never removes anonymous volumes: the container's data may
		// still be the only copy a user has.
		if err := c.removeContainer(ctx, candidate.ref, false); err != nil {
			result.Status = cleanupStatusFailed
			result.Message = err.Error()
			result.ReclaimedBytes = 0
		}
	default:
		result.Status = cleanupStatusFailed
		result.Message = "unknown item"
		result.ReclaimedBytes = 0
	}
	return result
}

func (c *DockerClient) scanCleanup(ctx context.Context, minAge time.Duration) (*cleanupScan, error) {
	cutoff := minAgeCutoff(minAge)
	scan := newCleanupScan()

	var containers []dockerContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?all=1&size=1", nil, &containers); err != nil {
		return nil, err
	}
	scanContainers(scan, containers, cutoff)

	if err := c.scanBuildCache(ctx, scan, cutoff); err != nil {
		return nil, err
	}
	if err := c.scanDanglingImages(ctx, scan, cutoff); err != nil {
		return nil, err
	}
	if err := c.scanUnusedImages(ctx, scan, containers, cutoff); err != nil {
		return nil, err
	}
	return scan, nil
}

func scanContainers(scan *cleanupScan, containers []dockerContainerSummary, cutoff int64) {
	for _, summary := range containers {
		if summary.ID == "" {
			continue
		}
		id := cleanupIDContainerPrefix + summary.ID
		switch {
		case summary.Labels[cleanupManagedLabel] == "true":
			// The owner stopped that app or database on purpose; Gotham removes
			// those containers through the resource lifecycle.
			scan.excluded[id] = "in use"
		case summary.State != "exited" && summary.State != "created":
			scan.excluded[id] = "in use"
		case cutoff > 0 && summary.Created > cutoff:
			scan.excluded[id] = "too new"
		default:
			name := containerName(summary.Names)
			if name == "" {
				name = shortID(summary.ID)
			}
			item := &agentv1.CleanupItem{
				Id:          id,
				Category:    cleanupCategoryStoppedContainers,
				Name:        name,
				SizeBytes:   summary.SizeRw,
				CreatedUnix: summary.Created,
				Reason:      stoppedReason(summary.Created),
			}
			scan.items = append(scan.items, item)
			scan.candidates[id] = cleanupCandidate{item: item, ref: summary.ID}
		}
	}
}

type dockerBuildCacheRecord struct {
	ID        string `json:"ID"`
	InUse     bool   `json:"InUse"`
	Size      int64  `json:"Size"`
	CreatedAt int64  `json:"CreatedAt"`
}

func (c *DockerClient) scanBuildCache(ctx context.Context, scan *cleanupScan, cutoff int64) error {
	var out struct {
		BuildCache []dockerBuildCacheRecord `json:"BuildCache"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/system/df?type=build-cache", nil, &out); err != nil {
		return err
	}
	var size int64
	records := 0
	for _, record := range out.BuildCache {
		if record.InUse {
			continue
		}
		if cutoff > 0 && record.CreatedAt > 0 && record.CreatedAt > cutoff {
			continue
		}
		size += record.Size
		records++
	}
	if records == 0 {
		return nil
	}
	item := &agentv1.CleanupItem{
		Id:        cleanupIDBuildCache,
		Category:  cleanupCategoryBuildCache,
		Name:      "Build cache",
		SizeBytes: size,
		Reason:    "unused build cache",
	}
	scan.items = append(scan.items, item)
	scan.candidates[item.GetId()] = cleanupCandidate{item: item, ref: cleanupIDBuildCache}
	return nil
}

func (c *DockerClient) scanDanglingImages(ctx context.Context, scan *cleanupScan, cutoff int64) error {
	filters, err := json.Marshal(map[string][]string{"dangling": {"true"}})
	if err != nil {
		return fmt.Errorf("docker: encode image filter: %w", err)
	}
	path := "/images/json?filters=" + url.QueryEscape(string(filters))
	var images []dockerImageSummary
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &images); err != nil {
		return err
	}
	for _, image := range images {
		id := cleanupIDImagePrefix + image.ID
		if cutoff > 0 && image.Created > cutoff {
			scan.excluded[id] = "too new"
			continue
		}
		item := &agentv1.CleanupItem{
			Id:          id,
			Category:    cleanupCategoryDanglingImages,
			Name:        shortID(image.ID),
			SizeBytes:   image.Size,
			CreatedUnix: image.Created,
			Reason:      "dangling",
		}
		scan.items = append(scan.items, item)
		scan.candidates[id] = cleanupCandidate{item: item, ref: image.ID}
	}
	return nil
}

func (c *DockerClient) scanUnusedImages(
	ctx context.Context, scan *cleanupScan, containers []dockerContainerSummary, cutoff int64,
) error {
	images, err := c.listImages(ctx, true)
	if err != nil {
		return err
	}
	active := activeImageRefsFrom(containers)

	var appGroups map[string][]appImage
	external := make([]appImage, 0, len(images))
	for _, image := range images {
		if image.ID == "" || len(image.RepoTags) == 0 {
			continue
		}
		appID, ref := appImageRef(image.RepoTags)
		if appID == "" {
			external = append(external, appImage{ID: image.ID, Ref: ref, Created: image.Created})
			continue
		}
		if appGroups == nil {
			appGroups = map[string][]appImage{}
		}
		appGroups[appID] = append(appGroups[appID], appImage{ID: image.ID, Ref: ref, Created: image.Created})
	}

	sizes := make(map[string]int64, len(images))
	for _, image := range images {
		sizes[image.ID] = image.Size
	}

	for _, group := range appGroups {
		for _, stale := range selectStaleImages(group, "", active, rollbackImageKeep) {
			addUnusedImage(scan, stale, sizes, cutoff)
		}
		recordUnlistedImages(scan, group, active)
	}
	for _, stale := range selectStaleImages(external, "", active, 0) {
		addUnusedImage(scan, stale, sizes, cutoff)
	}
	recordUnlistedImages(scan, external, active)
	return nil
}

// recordUnlistedImages remembers why an image was not offered: a running
// container uses it, or it is one of the newest rollback targets.
func recordUnlistedImages(scan *cleanupScan, images []appImage, active map[string]struct{}) {
	for _, image := range images {
		id := cleanupIDImagePrefix + image.ID
		if _, listed := scan.candidates[id]; listed {
			continue
		}
		if imageActive(image, active) {
			scan.excluded[id] = "in use"
			continue
		}
		if _, already := scan.excluded[id]; !already {
			scan.excluded[id] = "too new"
		}
	}
}

func addUnusedImage(scan *cleanupScan, image appImage, sizes map[string]int64, cutoff int64) {
	id := cleanupIDImagePrefix + image.ID
	if _, exists := scan.candidates[id]; exists {
		return
	}
	if cutoff > 0 && image.Created > cutoff {
		scan.excluded[id] = "too new"
		return
	}
	item := &agentv1.CleanupItem{
		Id:          id,
		Category:    cleanupCategoryUnusedImages,
		Name:        image.Ref,
		SizeBytes:   sizes[image.ID],
		CreatedUnix: image.Created,
		Reason:      "not used by any container",
	}
	scan.items = append(scan.items, item)
	scan.candidates[id] = cleanupCandidate{item: item, ref: image.ID}
}

// appImageRef returns the Gotham app id and the tag to display for one image.
// A registry-qualified tag ("host:port/gotham/<app>:<deploy>") also matches.
func appImageRef(tags []string) (string, string) {
	for _, tag := range tags {
		idx := strings.LastIndex(tag, imageRepoPrefix)
		if idx < 0 || (idx > 0 && tag[idx-1] != '/') {
			continue
		}
		rest := tag[idx+len(imageRepoPrefix):]
		appID, _, ok := strings.Cut(rest, ":")
		if !ok || appID == "" {
			continue
		}
		if ref, ok := appImageTag(tags, imageRepoPrefix+appID); ok {
			return appID, ref
		}
	}
	return "", tags[0]
}

func (c *DockerClient) pruneBuildCache(ctx context.Context, cutoff int64) (int64, error) {
	path := "/build/prune"
	if cutoff > 0 {
		path += "?until=" + strconv.FormatInt(cutoff, 10)
	}
	var out struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &out); err != nil {
		return 0, err
	}
	return out.SpaceReclaimed, nil
}

func minAgeCutoff(minAge time.Duration) int64 {
	if minAge <= 0 {
		return 0
	}
	return time.Now().Add(-minAge).Unix()
}

func stoppedReason(created int64) string {
	if created <= 0 {
		return "stopped"
	}
	hours := int(time.Since(time.Unix(created, 0)).Hours())
	switch {
	case hours >= 48:
		return fmt.Sprintf("stopped %d days", hours/24)
	case hours >= 1:
		return fmt.Sprintf("stopped %d hours", hours)
	default:
		return "stopped recently"
	}
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
