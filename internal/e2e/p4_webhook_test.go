package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type p4Delivery struct {
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	DeploymentID string `json:"deployment_id"`
	Host         string `json:"host"`
}

func TestP4WebhookAutoDeploy(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newP4Fixture(t, "e2e/p4-hook-"+suffix, "gotham-p4-hook-"+suffix)

	app := h.createApplication(t, p4CreateApplication{
		EnvironmentID: h.envID.String(),
		Name:          "p4-hook-" + suffix,
		Provider:      "github",
		Repo:          fixture.repo,
		CloneURL:      fixture.dir,
		Branch:        "main",
		BuildPack:     "dockerfile",
		ServerID:      h.serverID.String(),
	})
	h.addDomain(t, app.ID, "p4-hook-"+suffix+".example.test", p4ContainerPort)

	hookSecret := "p4-e2e-secret-" + suffix
	h.seedWebhook(t, app, hookSecret)
	commit := p4CommitSHA(suffix)

	status, queued, raw := h.deliverGitHub(t, hookSecret, "delivery-1-"+suffix, fixture.repo, commit)
	if status != http.StatusAccepted {
		t.Fatalf("first delivery: status %d, want %d: %s", status, http.StatusAccepted, raw)
	}
	if queued.Status != "queued" || queued.DeploymentID == "" {
		t.Fatalf("first delivery = %+v, want a queued deployment (%s)", queued, raw)
	}

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
	waitForHTTPBody(t, h.appURL(t, app.ID), fixture.marker)

	if again := h.deployments(t, app.ID); len(again) != 1 {
		t.Errorf("application has %d deployments after the run, want 1", len(again))
	}

	t.Logf("webhook ok: app=%s deployment=%s commit=%s", app.ID, queued.DeploymentID, commit)
}

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
	mac.Write(payload)

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

func p4CommitSHA(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:40]
}
