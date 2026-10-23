package updatecore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	ManifestPrefix    = "gotham-manifest-"
	ManifestSuffix    = ".txt"
	ManifestSigSuffix = ".sig"
)

type Manifest struct {
	Version      string
	Channel      string
	Arch         string
	File         string
	SHA256       string
	HelperFile   string
	HelperSHA256 string
}

func ManifestName(arch string) string {
	return ManifestPrefix + arch + ManifestSuffix
}

func ManifestNameWithPrefix(prefix, arch string) string {
	if prefix == "" {
		prefix = ManifestPrefix
	}
	return prefix + arch + ManifestSuffix
}

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

func (m Manifest) WithHelper(name string, data []byte) Manifest {
	sum := sha256.Sum256(data)
	m.HelperFile = name
	m.HelperSHA256 = hex.EncodeToString(sum[:])
	return m
}

func (m Manifest) Marshal() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "version=%s\n", m.Version)
	fmt.Fprintf(&b, "channel=%s\n", m.Channel)
	fmt.Fprintf(&b, "arch=%s\n", m.Arch)
	fmt.Fprintf(&b, "file=%s\n", m.File)
	fmt.Fprintf(&b, "sha256=%s\n", m.SHA256)
	if m.HelperFile != "" && m.HelperSHA256 != "" {
		fmt.Fprintf(&b, "helper_file=%s\n", m.HelperFile)
		fmt.Fprintf(&b, "helper_sha256=%s\n", m.HelperSHA256)
	}
	return []byte(b.String())
}

var ErrManifest = errors.New("updates: release manifest invalid")

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

func (m Manifest) Verify(data []byte) error {
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), m.SHA256) {
		return ErrChecksumMismatch
	}
	return nil
}
