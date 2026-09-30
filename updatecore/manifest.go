package updatecore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Release manifest naming. A release publishes
// gotham-manifest-<arch>.txt and its detached Ed25519 signature
// gotham-manifest-<arch>.txt.sig. The signature authenticates the manifest,
// and the manifest binds the release identity (version, channel, arch, file)
// to the artifact digest, closing the metadata/downgrade gap of a raw-bytes
// signature.
const (
	ManifestPrefix    = "gotham-manifest-"
	ManifestSuffix    = ".txt"
	ManifestSigSuffix = ".sig"
)

// Manifest is the signed release descriptor.
type Manifest struct {
	Version string
	Channel string
	Arch    string
	File    string
	SHA256  string
}

// ManifestName returns the manifest asset name for arch.
func ManifestName(arch string) string {
	return ManifestPrefix + arch + ManifestSuffix
}

// ManifestNameWithPrefix returns the manifest asset name for a release family,
// e.g. "gotham-agent-manifest-amd64.txt" for the node agent. An empty prefix
// uses the default control-plane prefix.
func ManifestNameWithPrefix(prefix, arch string) string {
	if prefix == "" {
		prefix = ManifestPrefix
	}
	return prefix + arch + ManifestSuffix
}

// BuildManifest computes the manifest for artifact.
func BuildManifest(version, channel, arch, file string, artifact []byte) Manifest {
	sum := sha256.Sum256(artifact)
	return Manifest{
		Version: version,
		Channel: channel,
		Arch:    arch,
		File:    file,
		SHA256:  hex.EncodeToString(sum[:]),
	}
}

// Marshal renders the manifest deterministically (one key=value per line).
func (m Manifest) Marshal() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "version=%s\n", m.Version)
	fmt.Fprintf(&b, "channel=%s\n", m.Channel)
	fmt.Fprintf(&b, "arch=%s\n", m.Arch)
	fmt.Fprintf(&b, "file=%s\n", m.File)
	fmt.Fprintf(&b, "sha256=%s\n", m.SHA256)
	return []byte(b.String())
}

// ErrManifest is returned when a manifest is missing or unusable.
var ErrManifest = errors.New("updates: release manifest invalid")

// ParseManifest parses the key=value manifest, rejecting missing or malformed
// required fields.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "version":
			m.Version = strings.TrimSpace(value)
		case "channel":
			m.Channel = strings.TrimSpace(value)
		case "arch":
			m.Arch = strings.TrimSpace(value)
		case "file":
			m.File = strings.TrimSpace(value)
		case "sha256":
			m.SHA256 = strings.TrimSpace(value)
		}
	}
	switch {
	case m.Version == "":
		return Manifest{}, fmt.Errorf("%w: missing version", ErrManifest)
	case m.Channel == "":
		return Manifest{}, fmt.Errorf("%w: missing channel", ErrManifest)
	case m.Arch == "":
		return Manifest{}, fmt.Errorf("%w: missing arch", ErrManifest)
	case m.File == "":
		return Manifest{}, fmt.Errorf("%w: missing file", ErrManifest)
	case len(m.SHA256) != 64:
		return Manifest{}, fmt.Errorf("%w: missing or malformed sha256", ErrManifest)
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return Manifest{}, fmt.Errorf("%w: malformed sha256", ErrManifest)
	}
	return m, nil
}

// Verify checks that data matches the manifest digest.
func (m Manifest) Verify(data []byte) error {
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), m.SHA256) {
		return ErrChecksumMismatch
	}
	return nil
}
