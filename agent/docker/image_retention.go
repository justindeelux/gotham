package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const defaultImageHistory = 5

type appImage struct {
	ID      string
	Ref     string
	Created int64
}

type dockerImageSummary struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Created  int64    `json:"Created"`
	// Size is the image's uncompressed size in bytes.
	Size int64 `json:"Size"`
}

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

func (c *DockerClient) removeImage(ctx context.Context, ref string) error {
	return c.removeImageWithForce(ctx, ref, true)
}

func (c *DockerClient) removeImageUnforced(ctx context.Context, ref string) error {
	return c.removeImageWithForce(ctx, ref, false)
}

func (c *DockerClient) removeImageWithForce(ctx context.Context, ref string, force bool) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("docker: image reference is required")
	}
	path := "/images/" + ref + "?noprune=1"
	if force {
		path += "&force=1"
	}
	response, err := c.doRaw(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if response.StatusCode == http.StatusConflict {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("%w: %s", ErrDockerImageInUse, strings.TrimSpace(string(raw)))
	}
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodDelete, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

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

	var errs []error
	for _, stale := range selectStaleImages(candidates, keepTag, active, defaultImageHistory) {
		ref := stale.ID
		if ref == "" {
			ref = stale.Ref
		}
		if err := c.removeImage(ctx, ref); err != nil {
			errs = append(errs, fmt.Errorf("docker: remove stale image %s: %w", ref, err))
		}
	}
	return errors.Join(errs...)
}

func (c *DockerClient) activeImageRefs(ctx context.Context) (map[string]struct{}, error) {
	var summaries []dockerContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?all=1", nil, &summaries); err != nil {
		return nil, err
	}
	return activeImageRefsFrom(summaries), nil
}

// activeImageRefsFrom indexes the images a container references by id and tag.
func activeImageRefsFrom(summaries []dockerContainerSummary) map[string]struct{} {
	active := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary.ImageID != "" {
			active[summary.ImageID] = struct{}{}
		}
		if summary.Image != "" {
			active[summary.Image] = struct{}{}
		}
	}
	return active
}

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
