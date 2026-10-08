package deploy

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
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

// dockerHubHost is the canonical scope of a bare reference (nginx, team/app):
// the credential scope comparison uses it so a bare name never matches an
// explicit host.
const dockerHubHost = "docker.io"

// RegistryHost returns the canonical registry host a validated reference
// pulls from: the lowercased explicit host (with port when present), or the
// Docker Hub marker for bare names. "" means the reference did not validate.
// Two references share a credential scope exactly when their hosts are equal:
// the orchestrator sends the stored credential only to that host.
func RegistryHost(ref string) string {
	parsed, err := ParseImageReference(ref)
	if err != nil {
		return ""
	}
	if host, _, ok := splitImageHost(parsed.Name); ok {
		return strings.ToLower(host)
	}
	return dockerHubHost
}

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
// a well-formed registry host when present, and never a loopback or
// unspecified host (the node-local scope pulls resolve to).
func checkImageName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: image reference has no repository name", ErrValidation)
	}
	rest := name
	if host, tail, hasHost := splitImageHost(name); hasHost {
		if err := checkRegistryHost(host); err != nil {
			return err
		}
		rest = tail
	}
	if rest == "" {
		return fmt.Errorf("%w: image reference has no repository path", ErrValidation)
	}
	for _, component := range strings.Split(rest, "/") {
		if !imageComponentPattern.MatchString(component) {
			return fmt.Errorf("%w: image path component %q must be lowercase alphanumeric with . _ - separators", ErrValidation, component)
		}
	}
	return nil
}

// splitImageHost splits a leading registry host off name: the first component
// is a host when it holds a ".", a ":" (a port) or is "localhost". A bare
// name (nginx, library/nginx) has no host and pulls from Docker Hub.
func splitImageHost(name string) (host, rest string, ok bool) {
	head, tail, found := strings.Cut(name, "/")
	if !found {
		return "", name, false
	}
	if strings.Contains(head, ".") || strings.Contains(head, ":") || strings.EqualFold(head, "localhost") {
		return head, tail, true
	}
	return "", name, false
}

// imageLabelPattern is one RFC 1035/1123 DNS label: alphanumerics with
// interior hyphens, never empty and never starting or ending with "-".
var imageLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkRegistryHost validates a registry host and refuses loopback and
// unspecified addresses: 127.0.0.0/8 (including inet_aton shorthand like
// 127.1), ::1, 0.0.0.0, [::] and localhost in any spelling. A reference
// pointing there would aim pulls (and the user's credential) at node-local
// services instead of the registry the user means. Ports are 1-65535.
func checkRegistryHost(host string) error {
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end < 0 {
			return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, host)
		}
		ip := net.ParseIP(host[1:end])
		if ip == nil {
			return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, host)
		}
		if ip.IsLoopback() || ip.IsUnspecified() {
			return fmt.Errorf("%w: registry host %q is a loopback or unspecified address", ErrValidation, host)
		}
		rest := host[end+1:]
		if rest == "" {
			return nil
		}
		port, ok := strings.CutPrefix(rest, ":")
		if !ok || !validRegistryPort(port) {
			return fmt.Errorf("%w: registry host %q has an invalid port", ErrValidation, host)
		}
		return nil
	}
	if strings.Contains(host, ":") {
		// Unbracketed IPv6 is not a valid registry host; a second colon is a
		// malformed host:port.
		if strings.Count(host, ":") != 1 {
			return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, host)
		}
		bare, port, _ := strings.Cut(host, ":")
		if !validRegistryPort(port) {
			return fmt.Errorf("%w: registry host %q has an invalid port", ErrValidation, host)
		}
		return checkRegistryName(bare)
	}
	return checkRegistryName(host)
}

// validRegistryPort accepts the Docker registry port range 1-65535.
func validRegistryPort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	value := 0
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
		value = value*10 + int(r-'0')
	}
	return value >= 1 && value <= 65535
}

// checkRegistryName validates a registry host without a port: an IP address
// (loopback and unspecified refused, including inet_aton shorthand), the
// localhost spellings, or dot-separated RFC 1123 labels.
func checkRegistryName(bare string) error {
	// The localhost spellings are checked after one trailing dot is ignored,
	// so "localhost." names the loopback problem instead of a generic one.
	if bare == "" {
		return fmt.Errorf("%w: registry host is empty", ErrValidation)
	}
	if ip := net.ParseIP(bare); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return fmt.Errorf("%w: registry host %q is a loopback or unspecified address", ErrValidation, bare)
		}
		return nil
	}
	if isNumericDots(bare) {
		value, ok := inetAton(bare)
		if !ok {
			return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, bare)
		}
		if value>>24 == 0x7f || value == 0 {
			return fmt.Errorf("%w: registry host %q is a loopback or unspecified address", ErrValidation, bare)
		}
		return nil
	}
	lower := strings.ToLower(bare)
	dotted := strings.TrimSuffix(lower, ".")
	if dotted == "localhost" || strings.HasSuffix(dotted, ".localhost") {
		return fmt.Errorf("%w: registry host %q is a loopback address", ErrValidation, bare)
	}
	if strings.HasSuffix(bare, ".") {
		return fmt.Errorf("%w: registry host %q is invalid", ErrValidation, bare)
	}
	for _, label := range strings.Split(lower, ".") {
		if !imageLabelPattern.MatchString(label) {
			return fmt.Errorf("%w: registry host label %q is invalid", ErrValidation, label)
		}
	}
	return nil
}

// isNumericDots reports an all-digits-and-dots host: either dotted-quad IPv4
// (handled by ParseIP above) or inet_aton shorthand (127.1, 0x7f.1 style is
// NOT accepted — hex and octal prefixes stay invalid, only decimal).
func isNumericDots(host string) bool {
	if !strings.Contains(host, ".") {
		return false
	}
	for _, r := range host {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// inetAton parses classic dotted-decimal shorthand (a, a.b, a.b.c, a.b.c.d)
// into a 32-bit value. Only decimal parts are accepted.
func inetAton(host string) (uint32, bool) {
	parts := strings.Split(host, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return 0, false
	}
	nums := make([]uint64, 0, len(parts))
	for _, part := range parts {
		if part == "" || len(part) > 10 {
			return 0, false
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return 0, false
		}
		nums = append(nums, value)
	}
	var value uint64
	switch len(nums) {
	case 1:
		value = nums[0]
	case 2:
		if nums[0] > 0xff || nums[1] > 0xffffff {
			return 0, false
		}
		value = nums[0]<<24 | nums[1]
	case 3:
		if nums[0] > 0xff || nums[1] > 0xff || nums[2] > 0xffff {
			return 0, false
		}
		value = nums[0]<<24 | nums[1]<<16 | nums[2]
	case 4:
		for _, n := range nums {
			if n > 0xff {
				return 0, false
			}
		}
		value = nums[0]<<24 | nums[1]<<16 | nums[2]<<8 | nums[3]
	}
	return uint32(value), true
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
