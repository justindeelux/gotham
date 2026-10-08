package deploy

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"
)

// Docker image sources (GS-9) deploy a prebuilt reference: registry/repo:tag
// with optional @sha256 digest pinning and an optional private-registry
// credential. There is no build step: the orchestrator pulls on the target
// node and runs the image with the application's env/ports/domains/storage.

// ImageReference is a validated Docker image reference split into its parts.
// Name carries the registry host and repository path without tag or digest;
// Tag is "" when the reference relies on the registry default; Digest is ""
// unless pinned.
type ImageReference struct {
	// Raw is the trimmed reference as entered.
	Raw string
	// Name is the host + repository path (no tag, no digest).
	Name string
	// Tag is the tag without the leading ":", "" when absent.
	Tag string
	// Digest is the pinned digest ("sha256:..."), "" when absent.
	Digest string
}

// Pinned reports whether the reference carries a digest pin.
func (r ImageReference) Pinned() bool { return r.Digest != "" }

var (
	imageComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	imageTagPattern       = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)
	imageDigestPattern    = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)
)

// maxImageRefLen bounds the stored reference. The engine accepts longer
// values, but anything past this is a pasted manifest, not a reference.
const maxImageRefLen = 255

// ValidateImageReference accepts the image references an image source may
// carry: [host[:port]/]path[:tag][@digest]. It refuses whitespace (a second
// argv token smuggled into logs), malformed names/tags/digests, and hosts in
// the node's loopback scope — the node-local registry publishes on loopback,
// so a loopback reference would resolve to the internal credential scope,
// never to the registry the user means. A bare `latest` tag stays valid (the
// wizard warns, it does not refuse).
func ValidateImageReference(raw string) error {
	_, err := ParseImageReference(raw)
	return err
}

// ParseImageReference validates raw and splits it into its parts.
func ParseImageReference(raw string) (ImageReference, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ImageReference{}, fmt.Errorf("%w: image reference is required", ErrValidation)
	}
	if len(ref) > maxImageRefLen {
		return ImageReference{}, fmt.Errorf("%w: image reference is too long", ErrValidation)
	}
	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ImageReference{}, fmt.Errorf("%w: image reference must not contain whitespace", ErrValidation)
		}
	}

	repo, digest, _ := strings.Cut(ref, "@")
	if strings.Contains(digest, "@") {
		return ImageReference{}, fmt.Errorf("%w: image reference %q has more than one digest", ErrValidation, ref)
	}
	parsed := ImageReference{Raw: ref}
	if digest != "" {
		if !imageDigestPattern.MatchString(digest) {
			return ImageReference{}, fmt.Errorf("%w: image digest %q must be sha256:<64 hex chars>", ErrValidation, digest)
		}
		parsed.Digest = strings.ToLower(digest)
	}

	name, tag, hadTag := splitImageTag(repo)
	if hadTag && tag == "" {
		return ImageReference{}, fmt.Errorf("%w: image tag in %q is empty", ErrValidation, ref)
	}
	if tag != "" && !imageTagPattern.MatchString(tag) {
		return ImageReference{}, fmt.Errorf("%w: image tag %q is invalid", ErrValidation, tag)
	}
	parsed.Tag = tag
	if err := checkImageName(name); err != nil {
		return ImageReference{}, err
	}
	parsed.Name = name
	return parsed, nil
}

// splitImageTag cuts the tag off repo: the tag separator is the last ":"
// after the last "/" (a port colon in the registry host sits before it).
// hadTag reports whether the separator was present, so an empty tag
// (nginx:) is rejected instead of passing as untagged.
func splitImageTag(repo string) (name, tag string, hadTag bool) {
	if index := strings.LastIndex(repo, ":"); index >= 0 && index > strings.LastIndex(repo, "/") {
		return repo[:index], repo[index+1:], true
	}
	return repo, "", false
}

// checkImageName validates the host + repository path: non-empty components,
// a well-formed registry host when present, and never a loopback host (the
// internal node-registry scope).
func checkImageName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: image reference has no repository name", ErrValidation)
	}
	host, rest, hasHost := cutImageHost(name)
	if hasHost {
		if err := checkRegistryHost(host); err != nil {
			return err
		}
		name = rest
	}
	if name == "" {
		return fmt.Errorf("%w: image reference has no repository path", ErrValidation)
	}
	for _, component := range strings.Split(name, "/") {
		if !imageComponentPattern.MatchString(component) {
			return fmt.Errorf("%w: image path component %q must be lowercase alphanumeric with . _ - separators", ErrValidation, component)
		}
	}
	return nil
}

// cutImageHost splits a leading registry host off name: the first component
// is a host when it holds a ".", a ":" (a port) or is "localhost". A bare
// name (nginx, library/nginx) has no host and pulls from Docker Hub.
func cutImageHost(name string) (host, rest string, ok bool) {
	head, tail, found := strings.Cut(name, "/")
	if !found {
		return "", name, false
	}
	if strings.Contains(head, ".") || strings.Contains(head, ":") || strings.EqualFold(head, "localhost") {
		return head, tail, true
	}
	return "", name, false
}

// checkRegistryHost validates a registry host and refuses the loopback scope
// the node-local registry lives in: 127.0.0.0/8, ::1 and localhost (with or
// without a port). A reference pointing there would authenticate against — or
// miss — the internal registry instead of the registry the user means.
func checkRegistryHost(host string) error {
	bare := host
	if h, port, err := net.SplitHostPort(host); err == nil {
		bare = h
		if port == "" {
			return fmt.Errorf("%w: registry host %q has an empty port", ErrValidation, host)
		}
		for _, r := range port {
			if r < '0' || r > '9' {
				return fmt.Errorf("%w: registry host %q has an invalid port", ErrValidation, host)
			}
		}
	} else if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		// A bare IPv6 literal without a port and without brackets is not a
		// valid registry host.
		return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, host)
	}
	trimmed := strings.Trim(bare, "[]")
	if strings.EqualFold(trimmed, "localhost") || isLoopbackIP(trimmed) {
		return fmt.Errorf("%w: registry host %q is reserved for the node-local registry", ErrValidation, host)
	}
	if trimmed == "" {
		return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, host)
	}
	return nil
}

// isLoopbackIP reports whether host parses as a loopback IP.
func isLoopbackIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PinnedImageReference renders name pinned to digest (name@digest) for
// rollbacks: the tag may have moved since the release, the digest has not.
// An empty digest falls back to ref unchanged, so a release recorded before
// digest resolution still rolls back to its tag.
func PinnedImageReference(ref, digest string) (string, error) {
	parsed, err := ParseImageReference(ref)
	if err != nil {
		return "", err
	}
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return parsed.Raw, nil
	}
	if !imageDigestPattern.MatchString(digest) {
		return "", fmt.Errorf("%w: image digest %q must be sha256:<64 hex chars>", ErrValidation, digest)
	}
	return parsed.Name + "@" + strings.ToLower(digest), nil
}
