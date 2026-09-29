package webhooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/teams"
)

// githubPRBody is a GitHub-shaped pull_request delivery body.
func githubPRBody(action string, number int, head, base, sha string) string {
	return `{"action":"` + action + `","number":` + itoa(number) + `,"pull_request":{"number":` + itoa(number) +
		`,"head":{"ref":"` + head + `","sha":"` + sha + `"},"base":{"ref":"` + base + `"}},` +
		`"repository":{"full_name":"octo/gotham"}}`
}

// gitLabMRBody is a GitLab-shaped merge request delivery body.
func gitLabMRBody(action string, iid int, source, target, sha string) string {
	return `{"object_attributes":{"iid":` + itoa(iid) + `,"action":"` + action + `","source_branch":"` + source +
		`","target_branch":"` + target + `","last_commit":{"id":"` + sha + `"}},` +
		`"project":{"path_with_namespace":"octo/gotham"}}`
}

// itoa renders a small non-negative int for the payload helpers.
func itoa(n int) string { return strconv.Itoa(n) }

// prRequest signs a GitHub pull_request delivery for the fake repository's
// hook. GitLab and Gitea bodies are exercised at the parsing layer; the
// service path is provider-agnostic below that.
func prRequest(event, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+providers.NameGitHub, strings.NewReader(body))
	req.Header.Set(headerGitHubSignature, githubSignaturePrefix+hmacHex(testHookSecret, []byte(body)))
	req.Header.Set(headerGitHubEvent, event)
	req.Header.Set(headerGitHubDelivery, "pr-delivery-"+uuid.NewString())
	req.RemoteAddr = "203.0.113.9:1234"
	return req
}

// newPreviewService wires a service with a commenting fake and previews
// explicitly enabled.
func newPreviewService(t *testing.T, repo *fakeRepository, deployer *fakeDeployer, commenter *fakeCommenter) *Service {
	t.Helper()
	t.Setenv(FeatureEnv, "true")
	return newTestServiceWith(Config{
		Repository:  repo,
		Installer:   &fakeInstaller{},
		Deployer:    deployer,
		Provisioner: deployer,
		Commenter:   commenter,
		Logger:      discardLogger(),
	})
}

func TestParsePullRequestDelivery(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		event      string
		body       string
		wantNumber int
		wantAction string
		wantHead   string
		wantBase   string
		wantSHA    string
		wantNil    bool
	}{
		{
			name:     "github opened",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubPRBody("opened", 7, "feat/x", "main", "abc"),
			wantNumber: 7, wantAction: "opened", wantHead: "feat/x", wantBase: "main", wantSHA: "abc",
		},
		{
			name:     "github synchronize",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubPRBody("synchronize", 7, "feat/x", "main", "def"),
			wantNumber: 7, wantAction: "synchronize", wantHead: "feat/x", wantBase: "main", wantSHA: "def",
		},
		{
			name:     "github closed",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubPRBody("closed", 7, "feat/x", "main", "def"),
			wantNumber: 7, wantAction: "closed", wantHead: "feat/x", wantBase: "main", wantSHA: "def",
		},
		{
			name:     "gitea synchronized",
			provider: providers.NameGitea, event: "pull_request",
			body:       githubPRBody("synchronized", 8, "fix/y", "main", "123"),
			wantNumber: 8, wantAction: "synchronized", wantHead: "fix/y", wantBase: "main", wantSHA: "123",
		},
		{
			name:     "gitlab update",
			provider: providers.NameGitLab, event: "Merge Request Hook",
			body:       gitLabMRBody("update", 9, "feat/z", "main", "456"),
			wantNumber: 9, wantAction: "update", wantHead: "feat/z", wantBase: "main", wantSHA: "456",
		},
		{
			name:     "gitlab merge",
			provider: providers.NameGitLab, event: "Merge Request Hook",
			body:       gitLabMRBody("merge", 9, "feat/z", "main", "456"),
			wantNumber: 9, wantAction: "merge", wantHead: "feat/z", wantBase: "main", wantSHA: "456",
		},
		{
			name:     "push body carries no pull request",
			provider: providers.NameGitHub, event: "push",
			body:    `{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"o/r"}}`,
			wantNil: true,
		},
		{
			name:     "pull request without a number is not a pull request",
			provider: providers.NameGitHub, event: "pull_request",
			body:    `{"action":"opened","pull_request":{"head":{"ref":"feat/x"}},"repository":{"full_name":"o/r"}}`,
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			header.Set(deliveryHeaderName(tc.provider), tc.event)
			parsed, err := parseDelivery(tc.provider, header, []byte(tc.body))
			if err != nil {
				t.Fatalf("parseDelivery: %v", err)
			}
			if tc.wantNil {
				if parsed.PullRequest != nil {
					t.Fatalf("PullRequest = %+v, want nil", parsed.PullRequest)
				}
				return
			}
			pr := parsed.PullRequest
			if pr == nil {
				t.Fatal("PullRequest = nil, want the parsed pull request")
			}
			if pr.Number != tc.wantNumber || pr.Action != tc.wantAction ||
				pr.HeadBranch != tc.wantHead || pr.BaseBranch != tc.wantBase || pr.HeadSHA != tc.wantSHA {
				t.Errorf("pull request = %+v, want number=%d action=%q head=%q base=%q sha=%q",
					pr, tc.wantNumber, tc.wantAction, tc.wantHead, tc.wantBase, tc.wantSHA)
			}
		})
	}
}

// deliveryHeaderName returns the event header of a provider.
func deliveryHeaderName(provider string) string {
	event, _ := deliveryHeaders(provider)
	return event
}

func TestIsPullRequestEvent(t *testing.T) {
	cases := []struct {
		provider string
		event    string
		want     bool
	}{
		{providers.NameGitHub, "pull_request", true},
		{providers.NameGitHub, "push", false},
		{providers.NameGitLab, "merge request hook", true},
		{providers.NameGitLab, "push hook", false},
		{providers.NameGitea, "pull_request", true},
		{providers.NameGitea, "issues", false},
	}
	for _, tc := range cases {
		if got := isPullRequestEvent(tc.provider, tc.event); got != tc.want {
			t.Errorf("isPullRequestEvent(%s, %q) = %v, want %v", tc.provider, tc.event, got, tc.want)
		}
	}
}

func TestNormalizePullRequestAction(t *testing.T) {
	cases := map[string]string{
		"opened":       prActionStart,
		"open":         prActionStart,
		"reopened":     prActionStart,
		"reopen":       prActionStart,
		"synchronize":  prActionStart,
		"synchronized": prActionStart,
		"update":       prActionStart,
		"closed":       prActionClose,
		"close":        prActionClose,
		"merge":        prActionClose,
		"merged":       prActionClose,
		"labeled":      "",
		"":             "",
	}
	for action, want := range cases {
		if got := normalizePullRequestAction(action); got != want {
			t.Errorf("normalizePullRequestAction(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestPreviewHostAndName(t *testing.T) {
	if got, want := previewHost("Gotham App", "apps.example.com", 12), "pr-12-gotham-app.apps.example.com"; got != want {
		t.Errorf("previewHost = %q, want %q", got, want)
	}
	if got, want := previewHost("", "apps.example.com", 3), "pr-3.apps.example.com"; got != want {
		t.Errorf("previewHost(no name) = %q, want %q", got, want)
	}
	// A name with slashes and repeated separators collapses into one label.
	if got, want := previewHost("feat//My_App", "example.com", 4), "pr-4-feat-my-app.example.com"; got != want {
		t.Errorf("previewHost(slug) = %q, want %q", got, want)
	}
	// The label never exceeds 63 characters and never ends in a hyphen.
	long := strings.Repeat("a", 80)
	host := previewHost(long, "example.com", 1)
	label := strings.TrimSuffix(host, ".example.com")
	if len(label) > 63 || strings.HasSuffix(label, "-") {
		t.Errorf("previewHost(long) label = %q", label)
	}
	if got := previewHost("app", "", 5); got != "" {
		t.Errorf("previewHost(no domain) = %q, want empty", got)
	}
	if got, want := previewName("gotham", 7), "gotham-pr-7"; got != want {
		t.Errorf("previewName = %q, want %q", got, want)
	}
}

func TestReceivePullRequestCreatesPreviewAndDeploys(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newPreviewService(t, repo, deployer, commenter)

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusQueued || delivery.Host != "pr-7-gotham.apps.example.com" {
		t.Fatalf("delivery = %+v, want queued on the derived host", delivery)
	}

	if got := deployer.provisionCount(); got != 1 {
		t.Fatalf("siblings provisioned = %d, want 1", got)
	}
	deployer.mu.Lock()
	provisioned := deployer.provisioned[0]
	deployer.mu.Unlock()
	if provisioned.Name != "gotham-pr-7" || provisioned.Branch != "feat/x" ||
		provisioned.BaseDomain != "pr-7-gotham.apps.example.com" {
		t.Errorf("provisioned = %+v", provisioned)
	}

	deployer.mu.Lock()
	deployedApp := deployer.deployed[len(deployer.deployed)-1]
	deployedSibling := deployer.provisionID
	deployer.mu.Unlock()
	if deployedApp != deployedSibling && deployedSibling != uuid.Nil {
		t.Errorf("deployed %s, want the provisioned sibling", deployedApp)
	}

	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewActive || preview.Branch != "feat/x" || preview.HeadSHA != "abc123" ||
		preview.Host != "pr-7-gotham.apps.example.com" {
		t.Errorf("preview = %+v", preview)
	}
	if preview.TeamID != repo.app.TeamID {
		t.Errorf("preview team = %s, want the base application's team %s", preview.TeamID, repo.app.TeamID)
	}

	if got := commenter.commentCount(); got != 1 {
		t.Fatalf("comments = %d, want 1", got)
	}
	commenter.mu.Lock()
	comment, target := commenter.bodies[0], commenter.targets[0]
	commenter.mu.Unlock()
	if !strings.Contains(comment, "http://pr-7-gotham.apps.example.com") {
		t.Errorf("comment = %q, want the preview URL", comment)
	}
	if target.UserID != repo.app.UserID || target.Repo != repo.app.Repo {
		t.Errorf("comment target = %+v", target)
	}
}

func TestReceivePullRequestSyncReusesSibling(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	for _, sha := range []string{"abc123", "def456"} {
		body := githubPRBody("synchronize", 7, "feat/x", "main", sha)
		if _, err := svc.Receive(context.Background(), providers.NameGitHub,
			prRequest("pull_request", body)); err != nil {
			t.Fatalf("Receive(%s): %v", sha, err)
		}
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want 1 (the second push reuses it)", got)
	}
	if got := deployer.deployCount(); got != 2 {
		t.Errorf("deployments queued = %d, want 2", got)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.HeadSHA != "def456" {
		t.Errorf("head sha = %q, want def456", preview.HeadSHA)
	}
}

func TestReceivePullRequestRedeliveryIsDuplicate(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	req := prRequest("pull_request", body)
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, req); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	// A redelivery of the same body (same head SHA) is an anti-spam no-op.
	redelivered := prRequest("pull_request", body)
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, redelivered)
	if err != nil {
		t.Fatalf("Receive(redelivery): %v", err)
	}
	if delivery.Status != StatusDuplicate {
		t.Fatalf("redelivery = %+v, want duplicate", delivery)
	}
	if got := deployer.deployCount(); got != 1 {
		t.Errorf("deployments queued = %d, want 1", got)
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want 1", got)
	}
}

func TestReceivePullRequestIgnoresUnwatchedBaseBranch(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "release", "abc123")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "branch" {
		t.Fatalf("delivery = %+v, want ignored for the unwatched base branch", delivery)
	}
	if deployer.provisionCount() != 0 || deployer.deployCount() != 0 {
		t.Fatalf("a PR against an unwatched branch must not deploy (provisioned=%d deployed=%d)",
			deployer.provisionCount(), deployer.deployCount())
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0", repo.claimCount())
	}
}

func TestReceivePullRequestIgnoresUnwatchedRepository(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := strings.ReplaceAll(githubPRBody("opened", 7, "feat/x", "main", "abc123"), "octo/gotham", "other/repo")
	_, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Receive = %v, want ErrUnauthorized for an unwatched repository", err)
	}
	if deployer.provisionCount() != 0 {
		t.Errorf("siblings provisioned = %d, want 0", deployer.provisionCount())
	}
}

func TestReceivePullRequestWithoutDomainIsIgnored(t *testing.T) {
	repo := newFakeRepository().withTarget()
	repo.app.BaseDomain = ""
	repo.target.BaseDomain = ""
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "application has no domain" {
		t.Fatalf("delivery = %+v, want ignored without a domain", delivery)
	}
	if deployer.provisionCount() != 0 {
		t.Errorf("siblings provisioned = %d, want 0", deployer.provisionCount())
	}
}

func TestReceivePullRequestCloseDeletesSibling(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newPreviewService(t, repo, deployer, commenter)

	open := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", open)); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}

	closeBody := githubPRBody("closed", 7, "feat/x", "main", "def456")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", closeBody))
	if err != nil {
		t.Fatalf("Receive(close): %v", err)
	}
	if delivery.Status != StatusDeleted {
		t.Fatalf("close delivery = %+v, want deleted", delivery)
	}
	if got := deployer.deleteCount(); got != 1 {
		t.Fatalf("siblings deleted = %d, want 1", got)
	}
	deployer.mu.Lock()
	deleted := deployer.deleted[0]
	deployer.mu.Unlock()
	if deleted != preview.PreviewApplicationID {
		t.Errorf("deleted %s, want the preview sibling %s", deleted, preview.PreviewApplicationID)
	}

	closed, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview(after close): %v", err)
	}
	if closed.State != PreviewDeleted || closed.DeletedAt.IsZero() {
		t.Errorf("preview after close = %+v, want deleted with a timestamp", closed)
	}
	if got := commenter.commentCount(); got != 2 {
		t.Errorf("comments = %d, want 2 (started and removed)", got)
	}

	// A redelivered close is idempotent: no second teardown.
	redelivered := githubPRBody("closed", 7, "feat/x", "main", "def456")
	delivery, err = svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", redelivered))
	if err != nil {
		t.Fatalf("Receive(close redelivery): %v", err)
	}
	if delivery.Status != StatusDuplicate {
		t.Errorf("close redelivery = %+v, want duplicate", delivery)
	}
}

func TestReceivePullRequestCommentFailureDoesNotFailDelivery(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{err: errors.New("git host down")}
	svc := newPreviewService(t, repo, deployer, commenter)

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("delivery = %+v, want queued despite the comment failure", delivery)
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments queued = %d, want 1", deployer.deployCount())
	}

	// The close path is equally resilient.
	closeBody := githubPRBody("closed", 7, "feat/x", "main", "def456")
	delivery, err = svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", closeBody))
	if err != nil {
		t.Fatalf("Receive(close): %v", err)
	}
	if delivery.Status != StatusDeleted {
		t.Errorf("close delivery = %+v, want deleted", delivery)
	}
	if preview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7); preview.State != PreviewDeleted {
		t.Errorf("preview state = %q, want deleted", preview.State)
	}
}

func TestReceivePullRequestRecreatesMissingSibling(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body)); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}

	// The sibling was deleted out of band: the next synchronize recreates it.
	deployer.mu.Lock()
	deployer.missing = map[uuid.UUID]bool{preview.PreviewApplicationID: true}
	deployer.mu.Unlock()

	sync := githubPRBody("synchronize", 7, "feat/x", "main", "def456")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", sync)); err != nil {
		t.Fatalf("Receive(sync): %v", err)
	}
	if got := deployer.provisionCount(); got != 2 {
		t.Errorf("siblings provisioned = %d, want 2", got)
	}
	refreshed, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview(refreshed): %v", err)
	}
	if refreshed.PreviewApplicationID == preview.PreviewApplicationID {
		t.Error("preview still points at the deleted sibling")
	}
}

func TestPreviewsDisabledIgnoresPullRequests(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Commenter: commenter, Logger: discardLogger(),
	})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "previews disabled" {
		t.Fatalf("delivery = %+v, want ignored with previews disabled", delivery)
	}
	if deployer.provisionCount() != 0 || deployer.deployCount() != 0 || commenter.commentCount() != 0 {
		t.Fatalf("previews disabled must not provision, deploy or comment (provisioned=%d deployed=%d comments=%d)",
			deployer.provisionCount(), deployer.deployCount(), commenter.commentCount())
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0", repo.claimCount())
	}

	// Push deliveries are untouched by the preview flag.
	push := pushBody("push-sha")
	delivery, err = svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("push", push))
	if err != nil {
		t.Fatalf("Receive(push): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("push delivery = %+v, want queued", delivery)
	}
}

func TestCreateWebhookEventsFollowPreviewsFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	provisioner := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: installer, Deployer: provisioner,
		Provisioner: provisioner, Logger: discardLogger(),
	})
	if _, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks"); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	installer.mu.Lock()
	events := installer.created[0].Events
	installer.mu.Unlock()
	if strings.Join(events, ",") != "push,pull_request" {
		t.Errorf("installed events = %v, want push and pull_request", events)
	}

	t.Setenv(FeatureEnv, "false")
	repo = newFakeRepository()
	installer = &fakeInstaller{}
	svc = newTestService(repo, installer, &fakeDeployer{})
	if _, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks"); err != nil {
		t.Fatalf("CreateWebhook(flag off): %v", err)
	}
	installer.mu.Lock()
	events = installer.created[0].Events
	installer.mu.Unlock()
	if strings.Join(events, ",") != "push" {
		t.Errorf("installed events with previews off = %v, want push only", events)
	}
}

func TestSweepPreviewsTearsDownStaleBindings(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	now := time.Now().UTC()
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Logger: discardLogger(),
		Now: func() time.Time { return now }, PreviewTTL: time.Hour,
	})

	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID, TeamID: repo.app.TeamID, Provider: repo.app.Provider,
		Repo: repo.app.Repo, PRNumber: 7, Branch: "feat/x", Host: "pr-7-gotham.apps.example.com",
		PreviewApplicationID: uuid.New(), State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}
	// Fresh: the sweep leaves it alone.
	removed, err := svc.SweepPreviews(context.Background())
	if err != nil {
		t.Fatalf("SweepPreviews: %v", err)
	}
	if removed != 0 || deployer.deleteCount() != 0 {
		t.Fatalf("fresh sweep removed %d previews, want 0", removed)
	}

	// Idle past the TTL: the sweep tears it down.
	repo.mu.Lock()
	stale := repo.previews[previewKey(repo.app.ID, 7)]
	stale.UpdatedAt = now.Add(-2 * time.Hour)
	repo.previews[previewKey(repo.app.ID, 7)] = stale
	repo.mu.Unlock()

	removed, err = svc.SweepPreviews(context.Background())
	if err != nil {
		t.Fatalf("SweepPreviews(stale): %v", err)
	}
	if removed != 1 || deployer.deleteCount() != 1 {
		t.Fatalf("stale sweep removed %d previews (deletes=%d), want 1", removed, deployer.deleteCount())
	}
	if got, _ := repo.GetPreview(context.Background(), repo.app.ID, 7); got.State != PreviewDeleted {
		t.Errorf("preview state = %q, want deleted", got.State)
	}
}

func TestDeletePreviewsTearsDownBaseApplicationSiblings(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	for _, number := range []int{7, 9} {
		if _, err := repo.UpsertPreview(context.Background(), Preview{
			ApplicationID: repo.app.ID, TeamID: repo.app.TeamID, Provider: repo.app.Provider,
			Repo: repo.app.Repo, PRNumber: number, Host: "pr-x.apps.example.com",
			PreviewApplicationID: uuid.New(), State: PreviewActive,
		}); err != nil {
			t.Fatalf("UpsertPreview: %v", err)
		}
	}
	svc.DeletePreviews(context.Background(), repo.app.ID)
	if got := deployer.deleteCount(); got != 2 {
		t.Fatalf("siblings deleted = %d, want 2", got)
	}
	previews, err := svc.ListPreviews(context.Background(), repo.app.UserID, repo.app.ID)
	if err != nil {
		t.Fatalf("ListPreviews: %v", err)
	}
	for _, preview := range previews {
		if preview.State != PreviewDeleted {
			t.Errorf("preview %d state = %q, want deleted", preview.PRNumber, preview.State)
		}
	}
}

func TestListPreviewsEnforcesTeam(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	svc := newPreviewService(t, repo, &fakeDeployer{}, &fakeCommenter{})

	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID, TeamID: repo.app.TeamID, Provider: repo.app.Provider,
		Repo: repo.app.Repo, PRNumber: 7, Host: "pr-7-gotham.apps.example.com",
		PreviewApplicationID: uuid.New(), State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}

	// The creator (no team context) may read.
	if previews, err := svc.ListPreviews(context.Background(), repo.app.UserID, repo.app.ID); err != nil || len(previews) != 1 {
		t.Fatalf("creator ListPreviews = (%d, %v), want one preview", len(previews), err)
	}
	// Another team's member cannot even see the application.
	stranger := teams.WithScope(context.Background(), teams.Scope{
		UserID: uuid.New(), TeamID: uuid.New(), Role: teams.RoleOwner,
	})
	if _, err := svc.ListPreviews(stranger, uuid.New(), repo.app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign ListPreviews = %v, want ErrNotFound", err)
	}
	// A read_only member of the application's team may read.
	viewer := teams.WithScope(context.Background(), teams.Scope{
		UserID: uuid.New(), TeamID: repo.app.TeamID, Role: teams.RoleReadOnly,
	})
	if previews, err := svc.ListPreviews(viewer, uuid.New(), repo.app.ID); err != nil || len(previews) != 1 {
		t.Fatalf("read_only ListPreviews = (%d, %v), want one preview", len(previews), err)
	}
}

func TestPreviewRoutesListAndFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	svc := newPreviewService(t, repo, &fakeDeployer{}, &fakeCommenter{})
	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID, TeamID: repo.app.TeamID, Provider: repo.app.Provider,
		Repo: repo.app.Repo, PRNumber: 7, Branch: "feat/x", HeadSHA: "abc",
		Host: "pr-7-gotham.apps.example.com", PreviewApplicationID: uuid.New(), State: PreviewActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}

	server := newRouteServer(svc, repo.app.UserID)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, managementRequest(http.MethodGet, "/v1/applications/"+repo.app.ID.String()+"/previews"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pr-7-gotham.apps.example.com") ||
		!strings.Contains(rec.Body.String(), `"pr_number":7`) {
		t.Errorf("body = %s", rec.Body.String())
	}

	// Flag off: the preview surface (route included) does not exist.
	t.Setenv(FeatureEnv, "false")
	off := newRouteServer(svc, repo.app.UserID)
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, managementRequest(http.MethodGet, "/v1/applications/"+repo.app.ID.String()+"/previews"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("previews route with the flag off = %d, want 404", rec.Code)
	}
}

func TestPreviewsDisabledSkipsSweep(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer, Provisioner: deployer,
		Logger: discardLogger(),
	})
	svc.StartPreviews()
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if deployer.deleteCount() != 0 {
		t.Errorf("siblings deleted = %d, want 0", deployer.deleteCount())
	}
}

func TestDeploySystemErrorReleasesTheClaim(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: errors.New("agent unavailable")}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub,
		prRequest("pull_request", body)); err == nil {
		t.Fatal("Receive: no error, want the queue failure to surface")
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want the failed delivery released", repo.claimCount())
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewFailed {
		t.Errorf("preview state = %q, want failed", preview.State)
	}
}

// TestPreviewProvisionerContract pins the deploy seam the webhooks service
// depends on to the methods the deploy service actually implements.
func TestPreviewProvisionerContract(t *testing.T) {
	var _ PreviewProvisioner = (*deploy.Service)(nil)
}
