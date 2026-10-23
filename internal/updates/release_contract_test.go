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

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

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
	secretEnv := "GOTHAM_UPDATE_NEXT_PUBLIC_KEY: ${{ secrets.GOTHAM_UPDATE_NEXT_PUBLIC_KEY }}"
	if got := strings.Count(workflow, secretEnv); got != 3 {
		t.Errorf(".github/workflows/release.yml passes the next-key secret %d times, want 3 (validate, GoReleaser build, embed assert)", got)
	}
	for _, want := range []string{
		`[ "$decoded" = "32" ]`,
		`[ "$next" != "$GOTHAM_UPDATE_PUBLIC_KEY" ]`,
		`grep -aqF "$GOTHAM_UPDATE_NEXT_PUBLIC_KEY" "$bin"`,
	} {
		if got := strings.Count(workflow, want); got != 1 {
			t.Errorf(".github/workflows/release.yml contains %q %d times, want exactly 1", want, got)
		}
	}
}

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
