package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestP8PreviewLifecycle is the gated end-to-end proof of BE-8.1: a signed
// pull_request delivery creates a sibling application cloned from the base
// configuration, deploys the PR head branch through the real Phase 4
// orchestrator, records the preview binding, and a closed PR deletes the
// sibling again. The Git host is never called: the hook is seeded as a row and
// deliveries are replayed by hand (the badge comment is the only provider
// call, and it is best effort by design).
func TestP8PreviewLifecycle(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newP4Fixture(t, "e2e/p8-preview-"+suffix, "gotham-p8-base-"+suffix)

	// A distinct head branch proves the preview builds the PR head, not the
	// base application's watched branch.
	git(t, fixture.dir, "checkout", "-b", "feature/preview")
	fixture.commit(t, "gotham-p8-head-"+suffix, p4Dockerfile)
	git(t, fixture.dir, "checkout", "main")

	baseDomain := "p8-" + suffix + ".apps.example.test"
	app := h.createApplication(t, p4CreateApplication{
		Name:       "p8-preview-" + suffix,
		Provider:   "github",
		Repo:       fixture.repo,
		CloneURL:   fixture.dir,
		Branch:     "main",
		BuildPack:  "dockerfile",
		BaseDomain: baseDomain,
		Port:       p4ContainerPort,
		HostPort:   freeHostPort(t),
		ServerID:   h.serverID.String(),
	})
	hookSecret := "p8-e2e-secret-" + suffix
	h.seedWebhook(t, app, hookSecret)

	// 1. PR opened: the delivery queues the preview and reports its host.
	headSHA := p4CommitSHA("p8-head-" + suffix)
	status, opened, raw := h.deliverGitHubPR(t, hookSecret, "p8-pr-open-"+suffix, map[string]any{
		"action": "opened",
		"number": 7,
		"pull_request": map[string]any{
			"number": 7,
			"head":   map[string]any{"ref": "feature/preview", "sha": headSHA, "repo": map[string]any{"full_name": fixture.repo, "fork": false}},
			"base":   map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"full_name": fixture.repo},
	})
	if status != http.StatusAccepted || opened.Status != "queued" {
		t.Fatalf("open delivery: status %d body %+v (%s)", status, opened, raw)
	}
	wantHost := "pr-7-p8-preview-" + suffix + "." + baseDomain
	if opened.Host != wantHost {
		t.Fatalf("preview host = %q, want %q", opened.Host, wantHost)
	}

	// 2. The sibling application is a real row cloned from the base config.
	sibling := h.applicationByName(t, app.Name+"-pr-7")
	if sibling.Branch != "feature/preview" || sibling.BaseDomain != wantHost {
		t.Fatalf("sibling = %+v, want the PR branch on %s", sibling, wantHost)
	}
	if sibling.CloneURL != fixture.dir || sibling.ServerID != h.serverID.String() {
		t.Fatalf("sibling did not inherit the base config: %+v", sibling)
	}

	// 3. The binding row is active and points at the sibling.
	preview := h.previewRow(t, app.ID, 7)
	if preview.State != "active" || preview.Host != wantHost ||
		preview.Branch != "feature/preview" || preview.HeadSHA != headSHA {
		t.Fatalf("preview row = %+v", preview)
	}
	if preview.PreviewApplicationID != sibling.ID {
		t.Fatalf("preview points at %s, want the sibling %s", preview.PreviewApplicationID, sibling.ID)
	}

	// 4. The preview actually deploys through the Phase 4 orchestrator.
	deployments := h.deployments(t, sibling.ID)
	if len(deployments) != 1 {
		t.Fatalf("sibling has %d deployments, want 1: %+v", len(deployments), deployments)
	}
	running := h.waitForTerminal(t, sibling.ID, deployments[0].ID)
	if running.State != "running" {
		t.Fatalf("preview deployment state = %q, want running (error: %s)", running.State, running.Error)
	}

	// 5. A redelivered synchronize of the same commit is an anti-spam no-op.
	status, duplicate, _ := h.deliverGitHubPR(t, hookSecret, "p8-pr-sync-"+suffix, map[string]any{
		"action": "synchronize",
		"number": 7,
		"pull_request": map[string]any{
			"number": 7,
			"head":   map[string]any{"ref": "feature/preview", "sha": headSHA, "repo": map[string]any{"full_name": fixture.repo, "fork": false}},
			"base":   map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"full_name": fixture.repo},
	})
	if status != http.StatusOK || duplicate.Status != "duplicate" {
		t.Fatalf("redelivery: status %d body %+v", status, duplicate)
	}
	if got := h.deployments(t, sibling.ID); len(got) != 1 {
		t.Fatalf("sibling has %d deployments after the redelivery, want 1", len(got))
	}

	// 6. Closing the PR tears the sibling down and marks the binding deleted.
	status, closed, raw := h.deliverGitHubPR(t, hookSecret, "p8-pr-close-"+suffix, map[string]any{
		"action": "closed",
		"number": 7,
		"pull_request": map[string]any{
			"number": 7,
			"head":   map[string]any{"ref": "feature/preview", "sha": headSHA, "repo": map[string]any{"full_name": fixture.repo, "fork": false}},
			"base":   map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"full_name": fixture.repo},
	})
	if status != http.StatusOK || closed.Status != "deleted" {
		t.Fatalf("close delivery: status %d body %+v (%s)", status, closed, raw)
	}
	if _, err := h.st.GetApplication(context.Background(), pgUUID(uuid.MustParse(sibling.ID))); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("sibling still exists after the close: %v", err)
	}
	preview = h.previewRow(t, app.ID, 7)
	if preview.State != "deleted" || preview.DeletedAt == nil {
		t.Fatalf("preview after close = %+v, want deleted", preview)
	}

	// 7. The base application is untouched.
	if _, err := h.st.GetApplication(context.Background(), pgUUID(uuid.MustParse(app.ID))); err != nil {
		t.Fatalf("base application after the close: %v", err)
	}

	t.Logf("preview ok: app=%s sibling=%s host=%s", app.ID, sibling.ID, wantHost)
}

// p8PreviewRow mirrors the preview_deploys row the test asserts on.
type p8PreviewRow struct {
	State                string
	Host                 string
	Branch               string
	HeadSHA              string
	PreviewApplicationID string
	DeletedAt            *time.Time
}

// previewRow reads one preview binding straight from the schema.
func (h *p4Harness) previewRow(t *testing.T, appID string, prNumber int) p8PreviewRow {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var (
		row          p8PreviewRow
		previewAppID *uuid.UUID
		deletedAt    *time.Time
	)
	err := h.st.DB.QueryRow(ctx,
		`SELECT state, host, branch, head_sha, preview_application_id, deleted_at
		 FROM preview_deploys WHERE application_id = $1 AND pr_number = $2`,
		uuid.MustParse(appID), prNumber,
	).Scan(&row.State, &row.Host, &row.Branch, &row.HeadSHA, &previewAppID, &deletedAt)
	if err != nil {
		t.Fatalf("read preview row: %v", err)
	}
	if previewAppID != nil {
		row.PreviewApplicationID = previewAppID.String()
	}
	row.DeletedAt = deletedAt
	return row
}

// applicationByName finds the caller's application with the given name.
func (h *p4Harness) applicationByName(t *testing.T, name string) p4Application {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := h.st.ListApplicationsByUser(ctx, pgUUID(h.userID))
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}
	for _, row := range rows {
		if row.Name != name {
			continue
		}
		serverID := ""
		if row.ServerID.Valid {
			serverID = uuid.UUID(row.ServerID.Bytes).String()
		}
		return p4Application{
			ID:         uuid.UUID(row.ID.Bytes).String(),
			Name:       row.Name,
			Repo:       row.Repo,
			CloneURL:   row.CloneUrl,
			Branch:     row.Branch,
			BuildPack:  row.BuildPack,
			BaseDomain: row.BaseDomain,
			Port:       row.Port,
			HostPort:   row.HostPort,
			ServerID:   serverID,
		}
	}
	t.Fatalf("application %q not found", name)
	return p4Application{}
}

// deliverGitHubPR replays one signed GitHub pull_request delivery.
func (h *p4Harness) deliverGitHubPR(t *testing.T, secret, delivery string, payload map[string]any) (int, p4Delivery, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal pull_request payload: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(encoded)

	request, err := http.NewRequest(http.MethodPost, h.baseURL+"/v1/webhooks/github", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build delivery request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", "pull_request")
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
