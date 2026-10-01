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

// TestReleaseKeyRingContract pins the embedded key-ring contract: both release
// binaries embed the current key plus the optional pre-positioned next key, and
// the workflow validates the next key (parses as 32 raw bytes, differs from the
// current) and asserts its embed in every binary before publishing. A release
// that embedded a partial ring would leave nodes unable to verify the promoted
// key without a fleet reinstall.
func TestReleaseKeyRingContract(t *testing.T) {
	cfg := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"updatecore.PublicKey={{ .Env.GOTHAM_UPDATE_PUBLIC_KEY }}",
		"updatecore.NextPublicKey={{ .Env.GOTHAM_UPDATE_NEXT_PUBLIC_KEY }}",
	} {
		if got := strings.Count(cfg, want); got != 2 {
			t.Errorf(".goreleaser.yaml embeds %q %d times, want one per binary (2)", want, got)
		}
	}

	workflow := readRepoFile(t, ".github/workflows/release.yml")
	// The optional secret must reach every step that consumes it: the
	// validation step, the GoReleaser build environment, and the per-binary
	// embed assertion. Pinning the exact count means dropping any one of them
	// (which would let a ring-less release ship while the secret is set, since
	// the assert step's `if [ -n ... ]` guard would see an unset variable)
	// fails this test.
	secretEnv := "GOTHAM_UPDATE_NEXT_PUBLIC_KEY: ${{ secrets.GOTHAM_UPDATE_NEXT_PUBLIC_KEY }}"
	if got := strings.Count(workflow, secretEnv); got != 3 {
		t.Errorf(".github/workflows/release.yml passes the next-key secret %d times, want 3 (validate, GoReleaser build, embed assert)", got)
	}
	for _, want := range []string{
		// The next key must decode to 32 raw bytes and differ from the current.
		`[ "$decoded" = "32" ]`,
		`[ "$next" != "$GOTHAM_UPDATE_PUBLIC_KEY" ]`,
		// Every built binary must embed the configured next key.
		`grep -aqF "$GOTHAM_UPDATE_NEXT_PUBLIC_KEY" "$bin"`,
	} {
		if got := strings.Count(workflow, want); got != 1 {
			t.Errorf(".github/workflows/release.yml contains %q %d times, want exactly 1", want, got)
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
