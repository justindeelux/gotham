package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// p4Delivery is the webhook route's response body: why a delivery did or did
// not start a build, plus the preview host a pull request delivery acted on.
type p4Delivery struct {
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	DeploymentID string `json:"deployment_id"`
	Host         string `json:"host"`
}

// TestP4WebhookAutoDeploy proves the push → deploy path without touching a
// provider API: the application and its signing secret are seeded as rows, a
// signed GitHub-style push delivery is replayed against the public route, and
// the anti-spam ledger must swallow a second delivery of the same commit.
func TestP4WebhookAutoDeploy(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newP4Fixture(t, "e2e/p4-hook-"+suffix, "gotham-p4-hook-"+suffix)
	hostPort := freeHostPort(t)

	app := h.createApplication(t, p4CreateApplication{
		Name:      "p4-hook-" + suffix,
		Provider:  "github",
		Repo:      fixture.repo,
		CloneURL:  fixture.dir,
		Branch:    "main",
		BuildPack: "dockerfile",
		Port:      p4ContainerPort,
		HostPort:  hostPort,
		ServerID:  h.serverID.String(),
	})

	hookSecret := "p4-e2e-secret-" + suffix
	h.seedWebhook(t, app, hookSecret)
	commit := p4CommitSHA(suffix)

	// 1. A signed push queues a deployment.
	status, queued, raw := h.deliverGitHub(t, hookSecret, "delivery-1-"+suffix, fixture.repo, commit)
	if status != http.StatusAccepted {
		t.Fatalf("first delivery: status %d, want %d: %s", status, http.StatusAccepted, raw)
	}
	if queued.Status != "queued" || queued.DeploymentID == "" {
		t.Fatalf("first delivery = %+v, want a queued deployment (%s)", queued, raw)
	}

	// 2. The same commit delivered again must not queue a second deployment,
	// even under a different delivery id.
	status, duplicate, raw := h.deliverGitHub(t, hookSecret, "delivery-2-"+suffix, fixture.repo, commit)
	if status != http.StatusOK {
		t.Fatalf("second delivery: status %d, want %d: %s", status, http.StatusOK, raw)
	}
	if duplicate.Status != "duplicate" || duplicate.Reason != "commit already handled" {
		t.Errorf("second delivery = %+v, want duplicate \"commit already handled\"", duplicate)
	}
	if duplicate.DeploymentID != "" {
		t.Errorf("second delivery returned deployment %q, want none", duplicate.DeploymentID)
	}

	// 3. Exactly one deployment exists, and it runs.
	deployments := h.deployments(t, app.ID)
	if len(deployments) != 1 {
		t.Fatalf("application has %d deployments, want 1: %+v", len(deployments), deployments)
	}
	if deployments[0].ID != queued.DeploymentID {
		t.Errorf("deployment id = %q, want the queued one %q", deployments[0].ID, queued.DeploymentID)
	}
	running := h.waitForTerminal(t, app.ID, queued.DeploymentID)
	if running.State != "running" {
		t.Fatalf("webhook deployment state = %q, want running (error: %s)", running.State, running.Error)
	}
	waitForHTTPBody(t, "http://127.0.0.1:"+strconv.Itoa(int(hostPort))+"/index.html", fixture.marker)

	// 4. The replay queued nothing extra after the run finished either.
	if again := h.deployments(t, app.ID); len(again) != 1 {
		t.Errorf("application has %d deployments after the run, want 1", len(again))
	}

	t.Logf("webhook ok: app=%s deployment=%s commit=%s", app.ID, queued.DeploymentID, commit)
}

// deliverGitHub replays one signed GitHub push delivery against the harness
// control plane and returns the route's status, its decoded body and the raw
// payload for failure messages.
func (h *p4Harness) deliverGitHub(t *testing.T, secret, delivery, repo, commit string) (int, p4Delivery, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"ref":     "refs/heads/main",
		"after":   commit,
		"created": true,
		"repository": map[string]any{
			"full_name": repo,
			"name":      repo[strings.LastIndex(repo, "/")+1:],
			"html_url":  "https://example.test/" + repo,
		},
		"head_commit": map[string]any{
			"id":      commit,
			"message": "e2e push",
		},
	})
	if err != nil {
		t.Fatalf("marshal push payload: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload) // hash.Hash never fails

	request, err := http.NewRequest(http.MethodPost, h.baseURL+"/v1/webhooks/github", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build delivery request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-GitHub-Delivery", delivery)
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))

	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("post delivery: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read delivery response: %v", err)
	}
	var result p4Delivery
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode delivery response (%d): %v: %s", response.StatusCode, err, raw)
	}
	return response.StatusCode, result, string(raw)
}

// p4CommitSHA derives the 40-hex commit id GitHub would send for a push.
func p4CommitSHA(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:40]
}
