package updates

import (
	"errors"
	"strings"
	"testing"
)

// TestManifestRoundtrip covers build, marshal and parse.
func TestManifestRoundtrip(t *testing.T) {
	artifact := []byte("gotham binary")
	manifest := BuildManifest("v1.2.0", "stable", "amd64", "gotham-linux-amd64", artifact)
	if len(manifest.SHA256) != 64 {
		t.Fatalf("sha256 length = %d, want 64", len(manifest.SHA256))
	}

	parsed, err := ParseManifest(manifest.Marshal())
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if parsed != manifest {
		t.Fatalf("roundtrip = %+v, want %+v", parsed, manifest)
	}
	if err := parsed.Verify(artifact); err != nil {
		t.Fatalf("Verify matching artifact: %v", err)
	}
	if err := parsed.Verify([]byte("other")); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Verify other artifact = %v, want ErrChecksumMismatch", err)
	}
}

// TestParseManifestRejectsInvalid covers missing and malformed fields.
func TestParseManifestRejectsInvalid(t *testing.T) {
	valid := "version=v1.2.0\nchannel=stable\narch=amd64\nfile=gotham-linux-amd64\nsha256=" +
		strings.Repeat("a", 64) + "\n"
	if _, err := ParseManifest([]byte(valid)); err != nil {
		t.Fatalf("ParseManifest(valid): %v", err)
	}

	cases := map[string]string{
		"missing version": "channel=stable\narch=amd64\nfile=f\nsha256=" + strings.Repeat("a", 64),
		"missing channel": "version=v1.2.0\narch=amd64\nfile=f\nsha256=" + strings.Repeat("a", 64),
		"missing arch":    "version=v1.2.0\nchannel=stable\nfile=f\nsha256=" + strings.Repeat("a", 64),
		"missing file":    "version=v1.2.0\nchannel=stable\narch=amd64\nsha256=" + strings.Repeat("a", 64),
		"missing sha":     "version=v1.2.0\nchannel=stable\narch=amd64\nfile=f",
		"short sha":       "version=v1.2.0\nchannel=stable\narch=amd64\nfile=f\nsha256=abc",
		"non-hex sha":     "version=v1.2.0\nchannel=stable\narch=amd64\nfile=f\nsha256=" + strings.Repeat("z", 64),
	}
	for name, body := range cases {
		if _, err := ParseManifest([]byte(body)); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: err = %v, want ErrManifest", name, err)
		}
	}
}

// TestBindManifest covers the identity binding.
func TestBindManifest(t *testing.T) {
	rel := &Release{Version: "v1.2.0", Channel: "stable", Arch: "amd64", AssetName: "gotham-linux-amd64"}
	good := Manifest{Version: "v1.2.0", Channel: "stable", Arch: "amd64", File: "gotham-linux-amd64", SHA256: strings.Repeat("a", 64)}
	if err := bindManifest(good, rel); err != nil {
		t.Fatalf("bindManifest(good): %v", err)
	}

	bad := map[string]Manifest{
		"version": {Version: "v9.9.9", Channel: "stable", Arch: "amd64", File: "gotham-linux-amd64"},
		"channel": {Version: "v1.2.0", Channel: "beta", Arch: "amd64", File: "gotham-linux-amd64"},
		"arch":    {Version: "v1.2.0", Channel: "stable", Arch: "arm64", File: "gotham-linux-amd64"},
		"file":    {Version: "v1.2.0", Channel: "stable", Arch: "amd64", File: "gotham-linux-arm64"},
	}
	for name, manifest := range bad {
		if err := bindManifest(manifest, rel); !errors.Is(err, ErrManifest) {
			t.Errorf("bindManifest(%s) = %v, want ErrManifest", name, err)
		}
	}
}

// TestManifestNames pins the asset naming contract INFRA-9.1 must produce.
func TestManifestNames(t *testing.T) {
	if got := ManifestName("arm64"); got != "gotham-manifest-arm64.txt" {
		t.Errorf("ManifestName = %q", got)
	}
}
