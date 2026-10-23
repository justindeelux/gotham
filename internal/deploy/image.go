package deploy

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type ImageReference struct {
	Raw    string
	Name   string
	Tag    string
	Digest string
}

func (r ImageReference) Pinned() bool { return r.Digest != "" }

var (
	imageComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	imageTagPattern       = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)
	imageDigestPattern    = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)
)

const maxImageRefLen = 255

const dockerHubHost = "docker.io"

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

func ValidateImageReference(raw string) error {
	_, err := ParseImageReference(raw)
	return err
}

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

func splitImageTag(repo string) (name, tag string, hadTag bool) {
	if index := strings.LastIndex(repo, ":"); index >= 0 && index > strings.LastIndex(repo, "/") {
		return repo[:index], repo[index+1:], true
	}
	return repo, "", false
}

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

var imageLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

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

func checkRegistryName(bare string) error {
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
