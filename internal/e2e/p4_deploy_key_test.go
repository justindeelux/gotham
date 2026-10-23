package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const privateRepoSSHStub = `#!/bin/sh
record="$GOTHAM_E2E_SSH_RECORD"
key=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-i" ]; then key="$arg"; fi
  prev="$arg"
done
if [ -n "$key" ] && [ -f "$key" ]; then
  cat "$key" > "$GOTHAM_E2E_SSH_KEY_COPY"
fi
echo "ssh stand-in served a clone" >> "$record"
for last in "$@"; do :; done
exec /bin/sh -c "$last"
`

func (h *p4Harness) seedPrivateRepoDeployKey(t *testing.T, appID, repo string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate deploy key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "gotham:e2e")
	if err != nil {
		t.Fatalf("marshal deploy key: %v", err)
	}
	privatePEM := string(pem.EncodeToMemory(block))
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal deploy public key: %v", err)
	}
	sealed, err := providers.SealSecret(h.secret, privatePEM)
	if err != nil {
		t.Fatalf("seal deploy key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := h.st.CreateApplicationDeployKey(ctx, sqlc.CreateApplicationDeployKeyParams{
		ApplicationID: pgUUID(uuid.MustParse(appID)),
		Provider:      "github",
		Repo:          repo,
		ProviderKeyID: "p4-e2e-deploy-key",
		Fingerprint:   ssh.FingerprintSHA256(sshPub),
		PublicKey:     strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
	}, "deploy-key:"+appID, sealed); err != nil {
		t.Fatalf("seed deploy key row: %v", err)
	}
}

func TestP4DeployPrivateRepoOverSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ssh stand-in is a POSIX shell script")
	}
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newP4Fixture(t, "e2e/p4-private-"+suffix, "gotham-p4-private-v1-"+suffix)

	app := h.createApplication(t, p4CreateApplication{
		EnvironmentID: h.envID.String(),
		Name:          "p4-private-" + suffix,
		Provider:      "github",
		Repo:          fixture.repo,
		CloneURL:      "ssh://git@127.0.0.1" + filepath.ToSlash(fixture.dir),
		Branch:        "main",
		BuildPack:     "dockerfile",
		ServerID:      h.serverID.String(),
	})
	h.addDomain(t, app.ID, "p4-private-"+suffix+".example.test", p4ContainerPort)
	hookCtx, hookCancel := context.WithTimeout(context.Background(), 15*time.Second)
	hookRow, err := h.st.GetApplicationWebhookByApp(hookCtx, pgUUID(uuid.MustParse(app.ID)))
	hookCancel()
	if err != nil {
		t.Fatalf("auto-installed webhook row: %v", err)
	}
	if hookRow.Provider != "github" || !strings.Contains(hookRow.Url, "/api/v1/webhooks/github") {
		t.Errorf("auto-installed hook = provider %q url %q, want the github delivery route",
			hookRow.Provider, hookRow.Url)
	}
	h.seedPrivateRepoDeployKey(t, app.ID, fixture.repo)

	stubDir := t.TempDir()
	recordPath := filepath.Join(stubDir, "record")
	keyCopyPath := filepath.Join(stubDir, "key.pem")
	if err := os.WriteFile(filepath.Join(stubDir, "ssh"), []byte(privateRepoSSHStub), 0o755); err != nil {
		t.Fatalf("write ssh stand-in: %v", err)
	}
	t.Setenv("GOTHAM_E2E_SSH_RECORD", recordPath)
	t.Setenv("GOTHAM_E2E_SSH_KEY_COPY", keyCopyPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	queued := h.deploy(t, app.ID)
	if queued.State != "queued" {
		t.Errorf("queued deployment state = %q, want queued", queued.State)
	}
	h.waitForDeployLog(t, queued.ID, "using the application deploy key")
	running := h.waitForTerminal(t, app.ID, queued.ID)
	if running.State != "running" {
		t.Fatalf("deployment state = %q, want running (error: %s)", running.State, running.Error)
	}
	waitForHTTPBody(t, h.appURL(t, app.ID), fixture.marker)

	if _, err := os.Stat(keyCopyPath); err != nil {
		t.Errorf("the ssh stand-in saw no deploy key: %v", err)
	}
	t.Logf("private repo deploy ok: app=%s deployment=%s port=%d", app.ID, running.ID, h.publishedHostPort(t, app.ID, p4ContainerPort))
}
