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
	// HelperFile and HelperSHA256 optionally bind a co-shipped host-helper
	// payload archive (the deploy/ helper scripts, tarred) to the same
	// signature. The control plane's root update wrapper verifies the payload
	// against HelperSHA256 before applying it (JUS-188); an empty pair means the
	// release ships no helper payload. Optional for every existing consumer:
	// the Go applier verifies only the binary SHA256 and ignores these fields.
	HelperFile   string
	HelperSHA256 string
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

// WithHelper binds a co-shipped helper payload archive (its file name and
// bytes) to the manifest, returning the updated manifest. The digest is what
// the root update wrapper checks before applying the payload.
func (m Manifest) WithHelper(name string, data []byte) Manifest {
	sum := sha256.Sum256(data)
	m.HelperFile = name
	m.HelperSHA256 = hex.EncodeToString(sum[:])
	return m
}

// Marshal renders the manifest deterministically (one key=value per line).
func (m Manifest) Marshal() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "version=%s\n", m.Version)
	fmt.Fprintf(&b, "channel=%s\n", m.Channel)
	fmt.Fprintf(&b, "arch=%s\n", m.Arch)
	fmt.Fprintf(&b, "file=%s\n", m.File)
	fmt.Fprintf(&b, "sha256=%s\n", m.SHA256)
	// Omitted entirely for a release with no helper payload, so older manifests
	// render byte-for-byte as before.
	if m.HelperFile != "" && m.HelperSHA256 != "" {
		fmt.Fprintf(&b, "helper_file=%s\n", m.HelperFile)
		fmt.Fprintf(&b, "helper_sha256=%s\n", m.HelperSHA256)
	}
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
		case "helper_file":
			m.HelperFile = strings.TrimSpace(value)
		case "helper_sha256":
			m.HelperSHA256 = strings.TrimSpace(value)
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
	// The helper binding is optional, but a manifest that names one without a
	// digest (or with a malformed digest) is rejected rather than silently
	// accepted: the root wrapper refuses a payload it cannot check.
	if m.HelperFile != "" || m.HelperSHA256 != "" {
		if m.HelperFile == "" {
			return Manifest{}, fmt.Errorf("%w: helper_sha256 without helper_file", ErrManifest)
		}
		if len(m.HelperSHA256) != 64 {
			return Manifest{}, fmt.Errorf("%w: missing or malformed helper_sha256", ErrManifest)
		}
		if _, err := hex.DecodeString(m.HelperSHA256); err != nil {
			return Manifest{}, fmt.Errorf("%w: malformed helper_sha256", ErrManifest)
		}
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
