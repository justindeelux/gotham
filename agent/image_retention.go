package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// defaultImageHistory is how many images of one application the node retains
// (newest first) after a successful build, in addition to the images its
// containers currently use. Older images are removed so node disk does not grow
// without bound across deploys.
const defaultImageHistory = 5

// appImage is one built image of an application, as seen by retention.
type appImage struct {
	// ID is the Docker image id. Removal is by id so every tag of the image
	// (the plain gotham/<app> tag and the registry-qualified one) goes at once.
	ID string
	// Ref is the tag retention matched against the app, used for logging and
	// the keep-current decision.
	Ref string
	// Created is the image creation time, Unix seconds.
	Created int64
}

// dockerImageSummary is the subset of GET /images/json retention consumes.
type dockerImageSummary struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Created  int64    `json:"Created"`
}

// ListImages returns the images known to the engine.
func (c *DockerClient) listImages(ctx context.Context, all bool) ([]dockerImageSummary, error) {
	path := "/images/json?all=0"
	if all {
		path = "/images/json?all=1"
	}
	var images []dockerImageSummary
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &images); err != nil {
		return nil, err
	}
	return images, nil
}

// RemoveImage deletes an image by id or tag, forcing the removal when it has
// multiple tags. An image that is already gone (404) is reported as success so
// a retried cleanup is idempotent.
func (c *DockerClient) removeImage(ctx context.Context, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("docker: image reference is required")
	}
	path := "/images/" + ref + "?force=1&noprune=1"
	response, err := c.doRaw(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodDelete, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// PruneAppImages removes the stale images of one application after a build. It
// keeps the image being deployed (the plain gotham/<appID>:<keepDeploy> tag),
// every image a container still references, and the newest defaultImageHistory
// images. Removal is by image id, so the registry-qualified copy of an image is
// removed with its local tag.
//
// ponytail: node-local Docker images are reclaimed; the node registry's blob
// store still grows until registry delete+GC is wired (the registry runs
// without REGISTRY_STORAGE_DELETE_ENABLED and GC must run while it is stopped).
// Add that when registry disk pressure is observed.
func (c *DockerClient) PruneAppImages(ctx context.Context, appID, keepDeploy string) error {
	appRepo := imageRepoPrefix + appID
	keepTag := appRepo + ":" + keepDeploy

	images, err := c.listImages(ctx, true)
	if err != nil {
		return err
	}
	active, err := c.activeImageRefs(ctx)
	if err != nil {
		return err
	}

	var candidates []appImage
	for _, image := range images {
		ref, ok := appImageTag(image.RepoTags, appRepo)
		if !ok {
			continue
		}
		candidates = append(candidates, appImage{ID: image.ID, Ref: ref, Created: image.Created})
	}

	for _, stale := range selectStaleImages(candidates, keepTag, active, defaultImageHistory) {
		ref := stale.ID
		if ref == "" {
			ref = stale.Ref
		}
		if err := c.removeImage(ctx, ref); err != nil {
			return fmt.Errorf("docker: remove stale image %s: %w", ref, err)
		}
	}
	return nil
}

// activeImageRefs returns the ids and names of the images referenced by any
// container (running or stopped). An image in this set is never pruned: it is
// the active deployment or a resource the user still runs.
func (c *DockerClient) activeImageRefs(ctx context.Context) (map[string]struct{}, error) {
	var summaries []dockerContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?all=1", nil, &summaries); err != nil {
		return nil, err
	}
	active := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary.ImageID != "" {
			active[summary.ImageID] = struct{}{}
		}
		if summary.Image != "" {
			active[summary.Image] = struct{}{}
		}
	}
	return active, nil
}

// selectStaleImages returns the images to remove. The image being deployed
// (keepTag) and every active image are always kept; of the rest, the newest
// history images are kept and everything older is removed. The input is copied,
// so the caller's slice is not reordered.
func selectStaleImages(images []appImage, keepTag string, active map[string]struct{}, history int) []appImage {
	sorted := append([]appImage(nil), images...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Created != sorted[j].Created {
			return sorted[i].Created > sorted[j].Created
		}
		return sorted[i].Ref < sorted[j].Ref
	})

	kept := 0
	var stale []appImage
	for _, image := range sorted {
		if image.Ref == keepTag || imageActive(image, active) {
			kept++
			continue
		}
		if kept < history {
			kept++
			continue
		}
		stale = append(stale, image)
	}
	return stale
}

// imageActive reports whether any container references the image, by id or by
// the image name a container summary records.
func imageActive(image appImage, active map[string]struct{}) bool {
	if _, ok := active[image.ID]; ok {
		return true
	}
	if image.Ref == "" {
		return false
	}
	_, ok := active[image.Ref]
	return ok
}

// appImageTag returns the tag by which an image belongs to an application. The
// plain local tag gotham/<appID>:<deploy> is preferred; the
// registry-qualified tag (host:port/gotham/<appID>:<deploy>) is accepted so an
// image tagged only for the node registry is still retained and pruned.
func appImageTag(tags []string, appRepo string) (string, bool) {
	qualified := ""
	for _, tag := range tags {
		if _, ok := appDeployID(tag, appRepo); !ok {
			continue
		}
		if strings.HasPrefix(tag, appRepo+":") {
			return tag, true
		}
		if qualified == "" {
			qualified = tag
		}
	}
	return qualified, qualified != ""
}

// appDeployID extracts the deploy id from an image tag belonging to appRepo,
// rejecting a tag whose repository merely shares appRepo as a prefix (so
// gotham/web2 is not read as an image of gotham/web).
func appDeployID(tag, appRepo string) (string, bool) {
	idx := strings.LastIndex(tag, appRepo+":")
	if idx < 0 {
		return "", false
	}
	if idx > 0 && tag[idx-1] != '/' {
		return "", false
	}
	deploy := tag[idx+len(appRepo)+1:]
	if deploy == "" {
		return "", false
	}
	return deploy, true
}
