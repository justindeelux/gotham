package updates

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/updatecore"
)

// readRepoFile reads a repository-relative file from a test in this package.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestReleasePublicKeyConsistency pins the trust anchor: the public key in
// deploy/gotham-signing-key.pub must be the exact base64 value embedded in both
// installers (and, by the release workflow's preflight, in the binaries). A
// drift here would make the installer reject every release.
func TestReleasePublicKeyConsistency(t *testing.T) {
	pubPEM := readRepoFile(t, "deploy/gotham-signing-key.pub")
	key, err := updatecore.ParsePublicKey(pubPEM)
	if err != nil {
		t.Fatalf("parse deploy/gotham-signing-key.pub: %v", err)
	}
	want := base64.StdEncoding.EncodeToString(key)

	re := regexp.MustCompile(`GOTHAM_RELEASE_PUBLIC_KEY_B64="([^"]+)"`)
	for _, file := range []string{"deploy/install.sh", "deploy/install-agent.sh"} {
		match := re.FindStringSubmatch(readRepoFile(t, file))
		if match == nil {
			t.Fatalf("%s: embedded release public key not found", file)
		}
		if match[1] != want {
			t.Errorf("%s: embedded public key %q != deploy/gotham-signing-key.pub %q", file, match[1], want)
		}
	}
}

// TestReleaseAssetNamingContract pins the asset names GoReleaser produces to the
// names the update checkers resolve and the installers verify. Renaming any one
// of these without the others breaks self-update silently.
func TestReleaseAssetNamingContract(t *testing.T) {
	cfg := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		`name_template: "gotham-linux-{{ .Arch }}"`,
		`name_template: "gotham-agent-linux-{{ .Arch }}"`,
		`updatecore.PublicKey={{ .Env.GOTHAM_UPDATE_PUBLIC_KEY }}`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf(".goreleaser.yaml is missing %q", want)
		}
	}

	// The control-plane checker builds "gotham-linux-<arch>" and
	// updatecore.ManifestName builds "gotham-manifest-<arch>.txt".
	if got := updatecore.ManifestName("amd64"); got != "gotham-manifest-amd64.txt" {
		t.Errorf("ManifestName(amd64) = %q", got)
	}
	if AgentAssetPrefix != "gotham-agent-linux-" {
		t.Errorf("AgentAssetPrefix = %q", AgentAssetPrefix)
	}
	if AgentManifestPrefix != "gotham-agent-manifest-" {
		t.Errorf("AgentManifestPrefix = %q", AgentManifestPrefix)
	}

	verify := readRepoFile(t, "deploy/release-verify.sh")
	for _, want := range []string{
		`asset="gotham-linux-${arch}"`,
		`asset="gotham-agent-linux-${arch}"`,
		`manifest_prefix="gotham-manifest-"`,
		`manifest_prefix="gotham-agent-manifest-"`,
	} {
		if !strings.Contains(verify, want) {
			t.Errorf("deploy/release-verify.sh is missing %q", want)
		}
	}
}
