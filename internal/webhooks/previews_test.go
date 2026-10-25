package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

func githubPRBody(action string, number int, head, base, sha string) string {
	return `{"action":"` + action + `","number":` + strconv.Itoa(number) +
		`,"pull_request":{"number":` + strconv.Itoa(number) +
		`,"head":{"ref":"` + head + `","sha":"` + sha + `","repo":{"full_name":"octo/gotham","fork":false}},` +
		`"base":{"ref":"` + base + `"}},` +
		`"repository":{"full_name":"octo/gotham"}}`
}

func githubHeadRepoPRBody(headRepo string, fork bool) string {
	return `{"action":"opened","number":7,` +
		`"pull_request":{"number":7,"head":{"ref":"feat/x","sha":"abc",` +
		`"repo":{"full_name":"` + headRepo + `","fork":` + strconv.FormatBool(fork) + `}},` +
		`"base":{"ref":"main"}},` +
		`"repository":{"full_name":"octo/gotham"}}`
}

func gitLabMRBody(action string, iid int, source, target, sha string) string {
	return `{"object_attributes":{"iid":` + strconv.Itoa(iid) + `,"action":"` + action +
		`","source_branch":"` + source + `","target_branch":"` + target +
		`","source_project_id":7,"target_project_id":7,` +
		`"last_commit":{"id":"` + sha + `"}},` +
		`"project":{"path_with_namespace":"octo/gotham"}}`
}

func gitLabForkMRBody(action string, iid int, source, target, sha string) string {
	return `{"object_attributes":{"iid":` + strconv.Itoa(iid) + `,"action":"` + action +
		`","source_branch":"` + source + `","target_branch":"` + target +
		`","source_project_id":41,"target_project_id":7,` +
		`"last_commit":{"id":"` + sha + `"}},` +
		`"project":{"path_with_namespace":"octo/gotham"}}`
}

func prRequest(event, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+providers.NameGitHub, strings.NewReader(body))
	req.Header.Set(headerGitHubSignature, githubSignaturePrefix+hmacHex(testHookSecret, []byte(body)))
	req.Header.Set(headerGitHubEvent, event)
	req.Header.Set(headerGitHubDelivery, "pr-delivery-"+uuid.NewString())
	req.RemoteAddr = "203.0.113.9:1234"
	return req
}

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

func receive(t *testing.T, svc *Service, body string) (Delivery, error) {
	t.Helper()
	return svc.Receive(context.Background(), providers.NameGitHub, prRequest("pull_request", body))
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
		wantFork   bool
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
			name:     "github fork head",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubHeadRepoPRBody("stranger/gotham", true),
			wantNumber: 7, wantAction: "opened", wantHead: "feat/x", wantBase: "main", wantSHA: "abc", wantFork: true,
		},
		{
			name:     "github same-repo head with the fork flag missing",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubHeadRepoPRBody("Octo/Gotham", false),
			wantNumber: 7, wantAction: "opened", wantHead: "feat/x", wantBase: "main", wantSHA: "abc",
		},
		{
			name:     "github foreign head without the fork flag",
			provider: providers.NameGitHub, event: "pull_request",
			body:       githubHeadRepoPRBody("stranger/gotham", false),
			wantNumber: 7, wantAction: "opened", wantHead: "feat/x", wantBase: "main", wantSHA: "abc", wantFork: true,
		},
		{
			name:     "github without the head repository",
			provider: providers.NameGitHub, event: "pull_request",
			body: `{"action":"opened","number":7,"pull_request":{"number":7,` +
				`"head":{"ref":"feat/x","sha":"abc"},"base":{"ref":"main"}},` +
				`"repository":{"full_name":"octo/gotham"}}`,
			wantNumber: 7, wantAction: "opened", wantHead: "feat/x", wantBase: "main", wantSHA: "abc", wantFork: true,
		},
		{
			name:     "gitlab without project ids",
			provider: providers.NameGitLab, event: "Merge Request Hook",
			body: `{"object_attributes":{"iid":9,"action":"open","source_branch":"feat/z",` +
				`"target_branch":"main","last_commit":{"id":"456"}},` +
				`"project":{"path_with_namespace":"octo/gotham"}}`,
			wantNumber: 9, wantAction: "open", wantHead: "feat/z", wantBase: "main", wantSHA: "456", wantFork: true,
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
			name:     "gitlab fork merge request",
			provider: providers.NameGitLab, event: "Merge Request Hook",
			body:       gitLabForkMRBody("open", 9, "feat/z", "main", "456"),
			wantNumber: 9, wantAction: "open", wantHead: "feat/z", wantBase: "main", wantSHA: "456", wantFork: true,
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
				pr.HeadBranch != tc.wantHead || pr.BaseBranch != tc.wantBase || pr.HeadSHA != tc.wantSHA ||
				pr.Fork != tc.wantFork {
				t.Errorf("pull request = %+v, want number=%d action=%q head=%q base=%q sha=%q fork=%v",
					pr, tc.wantNumber, tc.wantAction, tc.wantHead, tc.wantBase, tc.wantSHA, tc.wantFork)
			}
		})
	}
}

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
	if got, want := previewHost("feat//My_App", "example.com", 4), "pr-4-feat-my-app.example.com"; got != want {
		t.Errorf("previewHost(slug) = %q, want %q", got, want)
	}
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
	delivery, err := receive(t, svc, body)
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
		t.Errorf("deployed %s, want the provisioned sibling %s", deployedApp, deployedSibling)
	}

	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewActive || preview.Branch != "feat/x" || preview.HeadSHA != "abc123" ||
		preview.Host != "pr-7-gotham.apps.example.com" {
		t.Errorf("preview = %+v", preview)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want the in-flight lease released after the queue", got)
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
		if _, err := receive(t, svc, body); err != nil {
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
	if _, err := receive(t, svc, body); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	delivery, err := receive(t, svc, body)
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

func TestReopenAtSameSHAAfterClose(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	open := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := receive(t, svc, open); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	closed := githubPRBody("closed", 7, "feat/x", "main", "abc123")
	delivery, err := receive(t, svc, closed)
	if err != nil {
		t.Fatalf("Receive(close): %v", err)
	}
	if delivery.Status != StatusDeleted {
		t.Fatalf("close = %+v, want deleted", delivery)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations after close = %d, want 0", got)
	}

	delivery, err = receive(t, svc, open)
	if err != nil {
		t.Fatalf("Receive(reopen): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("reopen = %+v, want queued", delivery)
	}
	if got := deployer.provisionCount(); got != 2 {
		t.Errorf("siblings provisioned = %d, want 2 (delete then reopen)", got)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewActive {
		t.Errorf("preview after reopen = %+v", preview)
	}
}

func TestSecondPRAtSameSHADeploys(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	if _, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "same-sha")); err != nil {
		t.Fatalf("Receive(PR 7): %v", err)
	}
	delivery, err := receive(t, svc, githubPRBody("opened", 8, "feat/y", "main", "same-sha"))
	if err != nil {
		t.Fatalf("Receive(PR 8): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("PR 8 = %+v, want queued", delivery)
	}
	if got := deployer.provisionCount(); got != 2 {
		t.Errorf("siblings provisioned = %d, want 2", got)
	}
	for _, number := range []int{7, 8} {
		if _, err := repo.GetPreview(context.Background(), repo.app.ID, number); err != nil {
			t.Errorf("preview %d: %v", number, err)
		}
	}
}

func TestPushAtPRHeadSHAStillDeploys(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	if _, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "shared-sha")); err != nil {
		t.Fatalf("Receive(PR): %v", err)
	}
	push := `{"ref":"refs/heads/main","after":"shared-sha","repository":{"full_name":"octo/gotham"}}`
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, prRequest("push", push))
	if err != nil {
		t.Fatalf("Receive(push): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("push = %+v, want queued (the PR ledger must not suppress it)", delivery)
	}
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, prRequest("push", push)); err != nil {
		t.Fatalf("Receive(push redelivery): %v", err)
	}
	if got := repo.claimCount(); got != 1 {
		t.Errorf("push claims = %d, want 1", got)
	}
}

func TestBusyDeploymentIsRetryable(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	if _, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "old-sha")); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.HeadSHA != "old-sha" {
		t.Fatalf("head sha = %q, want old-sha", preview.HeadSHA)
	}

	newSHA := githubPRBody("synchronize", 7, "feat/x", "main", "new-sha")
	deployer.mu.Lock()
	deployer.err = deploy.ErrConflict
	deployer.mu.Unlock()

	_, err = receive(t, svc, newSHA)
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("busy synchronize = %v, want ErrRetryable", err)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations after the busy delivery = %d, want 0 (released)", got)
	}
	preview, _ = repo.GetPreview(context.Background(), repo.app.ID, 7)
	if preview.HeadSHA != "old-sha" {
		t.Errorf("head sha after the busy delivery = %q, want the actually queued old-sha", preview.HeadSHA)
	}

	deployer.mu.Lock()
	deployer.err = nil
	deployer.mu.Unlock()
	delivery, err := receive(t, svc, newSHA)
	if err != nil {
		t.Fatalf("Receive(retry): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("retry = %+v, want queued", delivery)
	}
	preview, _ = repo.GetPreview(context.Background(), repo.app.ID, 7)
	if preview.HeadSHA != "new-sha" {
		t.Errorf("head sha after the retry = %q, want new-sha", preview.HeadSHA)
	}
	if got := deployer.deployCount(); got != 2 {
		t.Errorf("deployments queued = %d, want 2", got)
	}
}

func TestBindingWriteFailureCompensatesAndRetries(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	repo.mu.Lock()
	repo.promoteErr = errors.New("database down")
	repo.mu.Unlock()

	if _, err := receive(t, svc, body); err == nil {
		t.Fatal("Receive: no error, want the binding write failure to surface")
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Fatalf("siblings provisioned = %d, want 1", got)
	}
	if got := deployer.deleteCount(); got != 1 {
		t.Fatalf("siblings deleted = %d, want the failed binding compensated", got)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want 0", got)
	}

	repo.mu.Lock()
	repo.promoteErr = nil
	repo.mu.Unlock()
	delivery, err := receive(t, svc, body)
	if err != nil {
		t.Fatalf("Receive(retry): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("retry = %+v, want queued", delivery)
	}
	if got := deployer.provisionCount(); got != 2 {
		t.Errorf("siblings provisioned = %d, want 2", got)
	}
	if _, err := repo.GetPreview(context.Background(), repo.app.ID, 7); err != nil {
		t.Fatalf("preview after retry: %v", err)
	}
}

func TestRetryRecoversReservedSibling(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	open := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := receive(t, svc, open); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	deployer.mu.Lock()
	deployer.err = deploy.ErrConflict
	deployer.mu.Unlock()
	sync := githubPRBody("synchronize", 7, "feat/x", "main", "new-sha")
	if _, err := receive(t, svc, sync); !errors.Is(err, ErrRetryable) {
		t.Fatalf("busy synchronize = %v, want ErrRetryable", err)
	}

	deployer.mu.Lock()
	deployer.err = nil
	deployer.mu.Unlock()
	delivery, err := receive(t, svc, sync)
	if err != nil {
		t.Fatalf("Receive(retry): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("retry = %+v, want queued", delivery)
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want 1 (the reserved sibling is reused)", got)
	}
}

func TestCapLimitsLivePreviews(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	if _, err := receive(t, svc, githubPRBody("opened", 1, "feat/one", "main", "sha-one")); err != nil {
		t.Fatalf("Receive(PR 1): %v", err)
	}
	for pr := 2; pr <= maxLivePreviewsPerApplication; pr++ {
		if _, err := repo.UpsertPreview(context.Background(), Preview{
			ApplicationID: repo.app.ID,
			Repo:          repo.app.Repo, PRNumber: pr, Host: "pr-x.apps.example.com",
			PreviewApplicationID: uuid.New(), State: PreviewActive,
		}); err != nil {
			t.Fatalf("UpsertPreview(PR %d): %v", pr, err)
		}
	}
	if got := repo.countLiveForTest(repo.app.ID); got != maxLivePreviewsPerApplication {
		t.Fatalf("live previews = %d, want the cap filled", got)
	}

	delivery, err := receive(t, svc, githubPRBody("opened", 9, "feat/nine", "main", "sha-nine"))
	if err != nil {
		t.Fatalf("Receive(PR 9): %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "preview limit reached" {
		t.Fatalf("capped delivery = %+v, want ignored at the cap", delivery)
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want 1 (the capped PR must not provision)", got)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want 0 (the capped delivery holds no lease)", got)
	}

	delivery, err = receive(t, svc, githubPRBody("synchronize", 1, "feat/one", "main", "sha-one-next"))
	if err != nil {
		t.Fatalf("Receive(PR 1 sync): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("refresh at the cap = %+v, want queued", delivery)
	}
}

func TestForkPullRequestsAreIgnored(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		body     string
	}{
		{"github fork flag", providers.NameGitHub,
			githubHeadRepoPRBody("stranger/gotham", true)},
		{"github foreign head", providers.NameGitHub,
			githubHeadRepoPRBody("stranger/gotham", false)},
		{"gitlab foreign project", providers.NameGitLab,
			gitLabForkMRBody("open", 7, "feat/x", "main", "abc")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepositoryFor(tc.provider).withTarget()
			deployer := &fakeDeployer{}
			svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

			var req *http.Request
			if tc.provider == providers.NameGitLab {
				req = httptest.NewRequest(http.MethodPost, "/v1/webhooks/gitlab", strings.NewReader(tc.body))
				req.Header.Set(headerGitLabToken, testHookSecret)
				req.Header.Set(headerGitLabEvent, "Merge Request Hook")
			} else {
				req = prRequest("pull_request", tc.body)
			}
			req.RemoteAddr = "203.0.113.9:1234"
			delivery, err := svc.Receive(context.Background(), tc.provider, req)
			if err != nil {
				t.Fatalf("Receive: %v", err)
			}
			if delivery.Status != StatusIgnored || delivery.Reason != "fork" {
				t.Fatalf("delivery = %+v, want ignored (fork)", delivery)
			}
			if got := deployer.provisionCount(); got != 0 {
				t.Errorf("siblings provisioned = %d, want 0", got)
			}
		})
	}

	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})
	delivery, err := receive(t, svc, githubHeadRepoPRBody("Octo/Gotham", false))
	if err != nil {
		t.Fatalf("Receive(same repo): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("same-repo delivery = %+v, want queued", delivery)
	}
}

func TestReceivePullRequestIgnoresUnwatchedBaseBranch(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	delivery, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "release", "abc123"))
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
	if repo.reservationCount() != 0 {
		t.Errorf("reservations = %d, want 0", repo.reservationCount())
	}
}

func TestReceivePullRequestBaseBranchMatchIsExact(t *testing.T) {
	repo := newFakeRepository().withTarget()
	repo.app.Branch = "Main"
	repo.target.Branch = "Main"
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	delivery, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "abc123"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "branch" {
		t.Fatalf("delivery = %+v, want ignored for the differently-cased base branch", delivery)
	}
	if deployer.provisionCount() != 0 || repo.reservationCount() != 0 {
		t.Fatalf("provisioned=%d reservations=%d, want none",
			deployer.provisionCount(), repo.reservationCount())
	}

	delivery, err = receive(t, svc, githubPRBody("opened", 8, "feat/y", "Main", "def456"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("delivery = %+v, want queued", delivery)
	}
}

func TestReceivePullRequestIgnoresUnwatchedRepository(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := strings.ReplaceAll(githubPRBody("opened", 7, "feat/x", "main", "abc123"), "octo/gotham", "other/repo")
	_, err := receive(t, svc, body)
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

	delivery, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "abc123"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "application has no domain" {
		t.Fatalf("delivery = %+v, want ignored without a domain", delivery)
	}
	if deployer.provisionCount() != 0 {
		t.Errorf("siblings provisioned = %d, want 0", deployer.provisionCount())
	}
	if repo.reservationCount() != 0 {
		t.Errorf("reservations = %d, want none (checked before reserving)", repo.reservationCount())
	}
}

func TestReceivePullRequestCloseDeletesSibling(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newPreviewService(t, repo, deployer, commenter)

	open := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := receive(t, svc, open); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}

	closeBody := githubPRBody("closed", 7, "feat/x", "main", "def456")
	delivery, err := receive(t, svc, closeBody)
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
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations after close = %d, want 0", got)
	}
	if got := commenter.commentCount(); got != 2 {
		t.Errorf("comments = %d, want 2 (started and removed)", got)
	}

	delivery, err = receive(t, svc, closeBody)
	if err != nil {
		t.Fatalf("Receive(close redelivery): %v", err)
	}
	if delivery.Status != StatusDuplicate {
		t.Errorf("close redelivery = %+v, want duplicate", delivery)
	}
	if got := deployer.deleteCount(); got != 1 {
		t.Errorf("siblings deleted = %d, want 1 after the redelivery", got)
	}
}

func TestReceivePullRequestCommentFailureDoesNotFailDelivery(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{err: errors.New("git host down")}
	svc := newPreviewService(t, repo, deployer, commenter)

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	delivery, err := receive(t, svc, body)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("delivery = %+v, want queued despite the comment failure", delivery)
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments queued = %d, want 1", deployer.deployCount())
	}

	closeBody := githubPRBody("closed", 7, "feat/x", "main", "def456")
	delivery, err = receive(t, svc, closeBody)
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
	if _, err := receive(t, svc, body); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}

	deployer.mu.Lock()
	deployer.missing = map[uuid.UUID]bool{preview.PreviewApplicationID: true}
	deployer.mu.Unlock()

	sync := githubPRBody("synchronize", 7, "feat/x", "main", "def456")
	if _, err := receive(t, svc, sync); err != nil {
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
	delivery, err := receive(t, svc, body)
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
	if repo.reservationCount() != 0 {
		t.Errorf("reservations = %d, want 0", repo.reservationCount())
	}

	push := pushBody("push-sha")
	delivery, err = svc.Receive(context.Background(), providers.NameGitHub, prRequest("push", push))
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

func TestSweepIsOrphanOnly(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Logger: discardLogger(),
	})

	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 7, Branch: "feat/x", Host: "pr-7-gotham.apps.example.com",
		PreviewApplicationID: uuid.New(), State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}
	repo.mu.Lock()
	stale := repo.previews[previewKey(repo.app.ID, 7)]
	stale.UpdatedAt = time.Now().UTC().Add(-365 * 24 * time.Hour)
	repo.previews[previewKey(repo.app.ID, 7)] = stale
	repo.mu.Unlock()
	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 8, Host: "pr-8-gotham.apps.example.com", State: PreviewDeploying,
	}); err != nil {
		t.Fatalf("UpsertPreview(orphan): %v", err)
	}

	removed, err := svc.SweepPreviews(context.Background())
	if err != nil {
		t.Fatalf("SweepPreviews: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the orphaned binding)", removed)
	}
	if deployer.deleteCount() != 0 {
		t.Errorf("siblings deleted = %d, want 0 (nothing live was bound)", deployer.deleteCount())
	}
	current, _ := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if current.State != PreviewActive {
		t.Errorf("bound preview state = %q, want active (age must not matter)", current.State)
	}
	orphaned, _ := repo.GetPreview(context.Background(), repo.app.ID, 8)
	if orphaned.State != PreviewDeleted {
		t.Errorf("orphaned binding = %+v, want deleted", orphaned)
	}

	orphanApp := uuid.New()
	repo.mu.Lock()
	repo.orphanApps = []uuid.UUID{orphanApp}
	repo.mu.Unlock()
	removed, err = svc.SweepPreviews(context.Background())
	if err != nil {
		t.Fatalf("SweepPreviews(orphan app): %v", err)
	}
	if removed != 1 || deployer.deleteCount() != 1 {
		t.Fatalf("removed = %d (deletes=%d), want the orphan application deleted", removed, deployer.deleteCount())
	}
	deployer.mu.Lock()
	deleted := deployer.deleted[0]
	deployer.mu.Unlock()
	if deleted != orphanApp {
		t.Errorf("deleted %s, want the orphan application %s", deleted, orphanApp)
	}
}

func TestCleanupApplicationTearsDownBaseSiblings(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	for _, number := range []int{7, 9} {
		if _, err := repo.UpsertPreview(context.Background(), Preview{
			ApplicationID: repo.app.ID,
			Repo:          repo.app.Repo, PRNumber: number, Host: "pr-x.apps.example.com",
			PreviewApplicationID: uuid.New(), State: PreviewActive,
		}); err != nil {
			t.Fatalf("UpsertPreview: %v", err)
		}
	}
	if err := svc.CleanupApplication(context.Background(), repo.app.ID); err != nil {
		t.Fatalf("CleanupApplication: %v", err)
	}
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

	repo2 := newFakeRepository().withTarget()
	deployer2 := &fakeDeployer{deleteErr: errors.New("database down")}
	svc2 := newPreviewService(t, repo2, deployer2, &fakeCommenter{})
	if _, err := repo2.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo2.app.ID,
		Repo:          repo2.app.Repo, PRNumber: 7, Host: "pr-7.apps.example.com",
		PreviewApplicationID: uuid.New(), State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}
	if err := svc2.CleanupApplication(context.Background(), repo2.app.ID); err == nil {
		t.Fatal("CleanupApplication with a failing teardown: no error, want one")
	}
	preview, _ := repo2.GetPreview(context.Background(), repo2.app.ID, 7)
	if preview.State != PreviewClosing {
		t.Errorf("preview after a failed cleanup = %+v, want the close intent kept", preview)
	}
	deployer2.mu.Lock()
	deployer2.deleteErr = nil
	deployer2.mu.Unlock()
	if err := svc2.CleanupApplication(context.Background(), repo2.app.ID); err != nil {
		t.Fatalf("CleanupApplication(retry): %v", err)
	}
	preview, _ = repo2.GetPreview(context.Background(), repo2.app.ID, 7)
	if preview.State != PreviewDeleted {
		t.Errorf("preview after the retry = %q, want deleted", preview.State)
	}
}

func TestCleanupApplicationMarksSiblingBindingDeleted(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	sibling := uuid.New()
	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 7, Host: "pr-7.apps.example.com",
		PreviewApplicationID: sibling, State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}
	repo.mu.Lock()
	repo.reservations[ReservationKey(DeliveryReservation{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart, HeadSHA: "stale-head",
	})] = DeliveryReservation{
		ID: uuid.New(), ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart,
		HeadSHA: "stale-head", ExpiresAt: time.Now().Add(time.Hour),
	}
	repo.mu.Unlock()

	if err := svc.CleanupApplication(context.Background(), sibling); err != nil {
		t.Fatalf("CleanupApplication(sibling): %v", err)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewDeleted {
		t.Errorf("binding state = %q, want deleted", preview.State)
	}
	if preview.PreviewApplicationID != sibling {
		t.Errorf("binding sibling link = %s, want the (row-level) link kept for the FK", preview.PreviewApplicationID)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want the sibling delete to clear the ledger", got)
	}
	if got := deployer.deleteCount(); got != 0 {
		t.Errorf("siblings deleted = %d, want 0 (the sibling is being deleted by its owner)", got)
	}
}

func TestListPreviewsEnforcesOwnership(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	svc := newPreviewService(t, repo, &fakeDeployer{}, &fakeCommenter{})

	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 7, Host: "pr-7-gotham.apps.example.com",
		PreviewApplicationID: uuid.New(), State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}

	if previews, err := svc.ListPreviews(context.Background(), repo.app.UserID, repo.app.ID); err != nil || len(previews) != 1 {
		t.Fatalf("creator ListPreviews = (%d, %v), want one preview", len(previews), err)
	}
}

func TestPreviewRoutesListAndFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	svc := newPreviewService(t, repo, &fakeDeployer{}, &fakeCommenter{})
	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 7, Branch: "feat/x", HeadSHA: "abc",
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

	t.Setenv(FeatureEnv, "false")
	off := newRouteServer(svc, repo.app.UserID)
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, managementRequest(http.MethodGet, "/v1/applications/"+repo.app.ID.String()+"/previews"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("previews route with the flag off = %d, want 404", rec.Code)
	}
}

func TestRetryableDeliveryAnswers503(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: deploy.ErrConflict}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})
	server := newRouteServer(svc, repo.app.UserID)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, prRequest("pull_request", githubPRBody("opened", 7, "feat/x", "main", "abc")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("busy delivery = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want 0 (released for the retry)", got)
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

func TestReturnToEarlierHeadDeploys(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	heads := []string{"head-a", "head-b", "head-a"}
	for _, head := range heads {
		delivery, err := receive(t, svc, githubPRBody("synchronize", 7, "feat/x", "main", head))
		if err != nil {
			t.Fatalf("Receive(%s): %v", head, err)
		}
		if delivery.Status != StatusQueued {
			t.Fatalf("delivery for %s = %+v, want queued", head, delivery)
		}
	}
	if got := deployer.deployCount(); got != 3 {
		t.Fatalf("deployments queued = %d, want 3 (A, B, A)", got)
	}
	if got := deployer.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want 1", got)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.HeadSHA != "head-a" {
		t.Errorf("binding head = %q, want head-a (the latest queued revision)", preview.HeadSHA)
	}
	if delivery, err := receive(t, svc, githubPRBody("synchronize", 7, "feat/x", "main", "head-a")); err != nil {
		t.Fatalf("Receive(current head): %v", err)
	} else if delivery.Status != StatusDuplicate {
		t.Errorf("current-head re-delivery = %+v, want duplicate", delivery)
	}
	if got := deployer.deployCount(); got != 3 {
		t.Errorf("deployments queued after the current-head re-delivery = %d, want 3", got)
	}
}

func TestCloseRetryClearsPoisonedLedger(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, body); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	closed := githubPRBody("closed", 7, "feat/x", "main", "head-a")
	if delivery, err := receive(t, svc, closed); err != nil || delivery.Status != StatusDeleted {
		t.Fatalf("first close = %+v / %v, want deleted", delivery, err)
	}

	repo.mu.Lock()
	repo.reservations[ReservationKey(DeliveryReservation{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart, HeadSHA: "head-a",
	})] = DeliveryReservation{
		ID: uuid.New(), ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart,
		HeadSHA: "head-a", ExpiresAt: time.Now().Add(time.Hour),
	}
	repo.markClosedErr = errors.New("database down")
	repo.mu.Unlock()

	if _, err := receive(t, svc, closed); err == nil {
		t.Fatal("already-deleted close with a failing ledger cleanup: no error, want one")
	}
	if got := repo.reservationCount(); got != 1 {
		t.Fatalf("reservations after the failing close = %d, want the stale lease kept", got)
	}

	repo.mu.Lock()
	repo.markClosedErr = nil
	repo.mu.Unlock()
	delivery, err := receive(t, svc, closed)
	if err != nil {
		t.Fatalf("Receive(close retry): %v", err)
	}
	if delivery.Status != StatusDuplicate {
		t.Fatalf("close retry = %+v, want duplicate", delivery)
	}
	if got := repo.reservationCount(); got != 0 {
		t.Fatalf("reservations after the close retry = %d, want the ledger cleared", got)
	}
	reopen, err := receive(t, svc, body)
	if err != nil {
		t.Fatalf("Receive(reopen): %v", err)
	}
	if reopen.Status != StatusQueued {
		t.Fatalf("reopen = %+v, want queued", reopen)
	}
}

func TestCloseFailureIsRetriedByTheSweep(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{deleteErr: fmt.Errorf("%w: a deployment is in progress", deploy.ErrConflict)}
	now := time.Now().UTC()
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Logger: discardLogger(),
		Now: func() time.Time { return now },
	})

	body := githubPRBody("opened", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, body); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	closed := githubPRBody("closed", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, closed); err == nil {
		t.Fatal("close with a failing teardown: no error, want one")
	}
	preview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if preview.State != PreviewClosing {
		t.Fatalf("binding after the failed close = %q, want closing", preview.State)
	}

	deployer.mu.Lock()
	deployer.deleteErr = nil
	deployer.mu.Unlock()
	repo.mu.Lock()
	stale := repo.previews[previewKey(repo.app.ID, 7)]
	stale.UpdatedAt = now.Add(-time.Hour)
	repo.previews[previewKey(repo.app.ID, 7)] = stale
	repo.mu.Unlock()

	removed, err := svc.SweepPreviews(context.Background())
	if err != nil {
		t.Fatalf("SweepPreviews: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the closing preview)", removed)
	}
	if got := deployer.deleteCount(); got != 1 {
		t.Errorf("siblings deleted = %d, want 1 (the failed attempt never reached the delete, the sweep retry did)", got)
	}
	closedPreview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if closedPreview.State != PreviewDeleted {
		t.Errorf("binding after the sweep = %q, want deleted", closedPreview.State)
	}
}

func TestConcurrentDistinctPRsRespectTheCap(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newPreviewService(t, repo, deployer, commenter)

	const attempts = 12
	var wg sync.WaitGroup
	statuses := make([]string, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := githubPRBody("opened", 100+i, "feat/x", "main", "sha-"+strconv.Itoa(i))
			delivery, err := receive(t, svc, body)
			if err != nil {
				t.Errorf("Receive(PR %d): %v", 100+i, err)
			}
			statuses[i] = delivery.Status
		}(i)
	}
	wg.Wait()

	if got := repo.countLiveForTest(repo.app.ID); got > maxLivePreviewsPerApplication {
		t.Fatalf("live previews = %d, want at most the cap %d", got, maxLivePreviewsPerApplication)
	}
	if got := deployer.provisionCount(); got > maxLivePreviewsPerApplication {
		t.Errorf("siblings provisioned = %d, want at most the cap", got)
	}
	queued, ignored := 0, 0
	for _, status := range statuses {
		switch status {
		case StatusQueued:
			queued++
		case StatusIgnored:
			ignored++
		default:
			t.Errorf("unexpected status %q", status)
		}
	}
	if queued != maxLivePreviewsPerApplication || ignored != attempts-maxLivePreviewsPerApplication {
		t.Errorf("queued/ignored = %d/%d, want %d/%d", queued, ignored, maxLivePreviewsPerApplication, attempts-maxLivePreviewsPerApplication)
	}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) get() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type blockingProvisioner struct {
	*fakeDeployer
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (b *blockingProvisioner) CreatePreviewApplication(ctx context.Context, baseAppID uuid.UUID, in deploy.PreviewApplicationInput) (deploy.Application, error) {
	b.mu.Lock()
	b.calls++
	first := b.calls == 1
	b.mu.Unlock()
	if first {
		close(b.started)
		<-b.release
	}
	return b.fakeDeployer.CreatePreviewApplication(ctx, baseAppID, in)
}

type blockingQueue struct {
	*fakeDeployer
	mu      sync.Mutex
	armed   bool
	started chan struct{}
	release chan struct{}
}

func (b *blockingQueue) arm() {
	b.mu.Lock()
	b.armed = true
	b.mu.Unlock()
}

func (b *blockingQueue) DeploySystem(ctx context.Context, appID uuid.UUID) (deploy.Deployment, error) {
	b.mu.Lock()
	block := b.armed
	b.armed = false
	b.mu.Unlock()
	if block {
		close(b.started)
		<-b.release
	}
	return b.fakeDeployer.DeploySystem(ctx, appID)
}

func TestClosingSameHeadReopenIsRetryable(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{deleteErr: errors.New("node unreachable")}
	now := time.Now().UTC()
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Logger: discardLogger(),
		Now: func() time.Time { return now },
	})
	repo.mu.Lock()
	repo.now = func() time.Time { return now }
	repo.mu.Unlock()

	body := githubPRBody("opened", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, body); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	closed := githubPRBody("closed", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, closed); err == nil {
		t.Fatal("close with a failing teardown: no error, want one")
	}
	preview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if preview.State != PreviewClosing {
		t.Fatalf("binding = %q, want closing", preview.State)
	}

	_, err := receive(t, svc, body)
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("same-head reopen during closing = %v, want ErrRetryable", err)
	}

	deployer.mu.Lock()
	deployer.deleteErr = nil
	deployer.mu.Unlock()
	repo.mu.Lock()
	stale := repo.previews[previewKey(repo.app.ID, 7)]
	stale.UpdatedAt = now.Add(-time.Hour)
	repo.previews[previewKey(repo.app.ID, 7)] = stale
	repo.mu.Unlock()
	if removed, err := svc.SweepPreviews(context.Background()); err != nil || removed != 1 {
		t.Fatalf("SweepPreviews = %d / %v, want the closing preview completed", removed, err)
	}
	if completed, _ := repo.GetPreview(context.Background(), repo.app.ID, 7); completed.State != PreviewDeleted {
		t.Fatalf("binding after the sweep = %q, want deleted", completed.State)
	}
	delivery, err := receive(t, svc, body)
	if err != nil {
		t.Fatalf("Receive(reopen retry): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("reopen retry = %+v, want queued", delivery)
	}
	if got := deployer.provisionCount(); got != 2 {
		t.Errorf("siblings provisioned = %d, want 2 (original + reopen)", got)
	}
}

func TestExpiredWorkerCannotExceedTheCap(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	repo := newFakeRepository().withTarget()
	repo.mu.Lock()
	repo.now = clock.get
	repo.mu.Unlock()
	blocking := &blockingProvisioner{
		fakeDeployer: &fakeDeployer{},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: blocking,
		Provisioner: blocking, Logger: discardLogger(), Now: clock.get,
	})

	type result struct {
		delivery Delivery
		err      error
	}
	slow := make(chan result, 1)
	go func() {
		delivery, err := receive(t, svc, githubPRBody("opened", 1, "feat/one", "main", "sha-one"))
		slow <- result{delivery, err}
	}()
	<-blocking.started

	clock.advance(16 * time.Minute)
	for pr := 2; pr <= 1+maxLivePreviewsPerApplication; pr++ {
		delivery, err := receive(t, svc, githubPRBody("opened", pr, "feat/x", "main", "sha-"+strconv.Itoa(pr)))
		if err != nil || delivery.Status != StatusQueued {
			t.Fatalf("Receive(PR %d) = %+v / %v, want queued", pr, delivery, err)
		}
	}

	close(blocking.release)
	res := <-slow
	if !errors.Is(res.err, ErrRetryable) {
		t.Fatalf("resumed stale worker = %+v / %v, want ErrRetryable", res.delivery, res.err)
	}
	if got := repo.countLiveForTest(repo.app.ID); got > maxLivePreviewsPerApplication {
		t.Fatalf("live previews = %d, want at most the cap %d", got, maxLivePreviewsPerApplication)
	}
	if got := blocking.deleteCount(); got != 1 {
		t.Errorf("compensated siblings = %d, want the stale worker's sibling deleted", got)
	}
	if _, err := repo.GetPreview(context.Background(), repo.app.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("PR 1 binding = %v, want none (the stale worker never promoted)", err)
	}
}

func TestCloseWinsOverRacingSynchronize(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	queue := &blockingQueue{
		fakeDeployer: &fakeDeployer{},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: queue,
		Provisioner: queue, Logger: discardLogger(),
	})

	open := githubPRBody("opened", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, open); err != nil {
		t.Fatalf("Receive(open): %v", err)
	}
	preview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7)
	sibling := preview.PreviewApplicationID

	queue.arm()
	synced := make(chan error, 1)
	go func() {
		_, err := receive(t, svc, githubPRBody("synchronize", 7, "feat/x", "main", "head-b"))
		synced <- err
	}()
	<-queue.started

	if _, err := receive(t, svc, githubPRBody("closed", 7, "feat/x", "main", "head-b")); err != nil {
		t.Fatalf("Receive(close): %v", err)
	}
	queue.mu.Lock()
	queue.fakeDeployer.mu.Lock()
	queue.missing = map[uuid.UUID]bool{sibling: true}
	queue.fakeDeployer.mu.Unlock()
	queue.mu.Unlock()
	close(queue.release)

	if err := <-synced; !errors.Is(err, ErrRetryable) {
		t.Fatalf("racing synchronize = %v, want ErrRetryable (the close won)", err)
	}
	resurrected, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if resurrected.State != PreviewDeleted {
		t.Fatalf("binding after the race = %q, want deleted (never resurrected)", resurrected.State)
	}
	if got := queue.provisionCount(); got != 1 {
		t.Errorf("siblings provisioned = %d, want only the original", got)
	}
}

func TestOwnLeaseDoesNotDenyTheCap(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	seedLive := func(pr int) {
		t.Helper()
		if _, err := repo.UpsertPreview(context.Background(), Preview{
			ApplicationID: repo.app.ID,
			Repo:          repo.app.Repo, PRNumber: pr, Host: "pr-x.apps.example.com",
			PreviewApplicationID: uuid.New(), State: PreviewActive,
		}); err != nil {
			t.Fatalf("UpsertPreview(PR %d): %v", pr, err)
		}
	}
	for pr := 1; pr <= 4; pr++ {
		seedLive(pr)
	}
	repo.mu.Lock()
	repo.reservations[ReservationKey(DeliveryReservation{
		ApplicationID: repo.app.ID, PRNumber: 9, Kind: ReservationStart, HeadSHA: "old-head",
	})] = DeliveryReservation{
		ID: uuid.New(), ApplicationID: repo.app.ID, PRNumber: 9, Kind: ReservationStart,
		HeadSHA: "old-head", ExpiresAt: time.Now().Add(time.Hour),
	}
	repo.mu.Unlock()

	delivery, err := receive(t, svc, githubPRBody("synchronize", 9, "feat/x", "main", "new-head"))
	if err != nil {
		t.Fatalf("Receive(PR 9): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("PR 9 new head = %+v, want queued (its own lease must not deny it)", delivery)
	}

	seedLive(5)
	repo.mu.Lock()
	repo.reservations[ReservationKey(DeliveryReservation{
		ApplicationID: repo.app.ID, PRNumber: 10, Kind: ReservationStart, HeadSHA: "old-head",
	})] = DeliveryReservation{
		ID: uuid.New(), ApplicationID: repo.app.ID, PRNumber: 10, Kind: ReservationStart,
		HeadSHA: "old-head", ExpiresAt: time.Now().Add(time.Hour),
	}
	repo.mu.Unlock()
	delivery, err = receive(t, svc, githubPRBody("synchronize", 10, "feat/x", "main", "new-head"))
	if err != nil {
		t.Fatalf("Receive(PR 10): %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "preview limit reached" {
		t.Fatalf("PR 10 = %+v, want ignored at the cap", delivery)
	}
}

func TestFinalPromotionFailureIsRetryable(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	repo.mu.Lock()
	repo.promoteErr = errors.New("database down")
	repo.promoteErrAt = 2
	repo.mu.Unlock()

	body := githubPRBody("opened", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, body); err == nil {
		t.Fatal("final promotion failure: no error, want a non-2xx so the host retries")
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want the failed lease released", got)
	}
	if got := deployer.deployCount(); got != 1 {
		t.Errorf("deployments = %d, want the deployment still queued", got)
	}

	repo.mu.Lock()
	repo.promoteErr = nil
	repo.promoteErrAt = 0
	repo.mu.Unlock()
	delivery, err := receive(t, svc, body)
	if err != nil {
		t.Fatalf("Receive(retry): %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("retry = %+v, want queued", delivery)
	}
	if preview, _ := repo.GetPreview(context.Background(), repo.app.ID, 7); preview.State != PreviewActive {
		t.Errorf("binding after the retry = %q, want active", preview.State)
	}
}

func TestRetryableFailureReleasesTheReservation(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: errors.New("agent unavailable")}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	body := githubPRBody("opened", 7, "feat/x", "main", "abc123")
	if _, err := receive(t, svc, body); err == nil {
		t.Fatal("Receive: no error, want the queue failure to surface")
	}
	if got := repo.reservationCount(); got != 0 {
		t.Errorf("reservations = %d, want the failed delivery released", got)
	}
	preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7)
	if err != nil {
		t.Fatalf("GetPreview: %v", err)
	}
	if preview.State != PreviewDeploying {
		t.Errorf("preview state = %q, want deploying (the sibling is reserved)", preview.State)
	}
	if preview.HeadSHA != "" {
		t.Errorf("head sha = %q, want empty (never queued)", preview.HeadSHA)
	}
}

func TestPreviewProvisionerContract(t *testing.T) {
	var _ PreviewProvisioner = (*deploy.Service)(nil)
}

func TestNoBindingCloseBlocksTheRacingPromote(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: &fakeInstaller{}, Deployer: deployer,
		Provisioner: deployer, Commenter: &fakeCommenter{}, Logger: discardLogger(),
	})

	openClaim, err := repo.ClaimPreviewDelivery(context.Background(), PreviewClaim{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart,
		HeadSHA: "head-a", LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil || !openClaim.Approved {
		t.Fatalf("open claim = %+v / %v, want approved", openClaim, err)
	}
	closeClaim, err := repo.ClaimPreviewDelivery(context.Background(), PreviewClaim{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationClose,
		LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil || !closeClaim.Approved {
		t.Fatalf("close claim = %+v / %v, want approved", closeClaim, err)
	}

	promoted, err := repo.WritePreviewBinding(context.Background(), PreviewBindingWrite{
		ApplicationID: repo.app.ID, PRNumber: 7,
		ReservationID: openClaim.Reservation.ID, LeaseHeadSHA: "head-a", HeadSHA: "head-a",
		State: PreviewActive, ConsumeLease: true, LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	if promoted.Refused != BindingRefusedClosing {
		t.Fatalf("promotion = %+v, want refused closing", promoted)
	}
	if _, err := repo.GetPreview(context.Background(), repo.app.ID, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding after the refused promotion = %v, want none", err)
	}

	body := githubPRBody("opened", 7, "feat/x", "main", "head-b")
	if _, err := receive(t, svc, body); !errors.Is(err, ErrRetryable) {
		t.Fatalf("open during the close = %v, want ErrRetryable", err)
	}
	if got := deployer.provisionCount(); got != 0 {
		t.Fatalf("siblings provisioned = %d, want none while the close owns the PR", got)
	}

	if err := repo.ClearPreviewDeliveries(context.Background(), repo.app.ID, 7); err != nil {
		t.Fatalf("ClearPreviewDeliveries: %v", err)
	}
	reopen, err := repo.ClaimPreviewDelivery(context.Background(), PreviewClaim{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart,
		HeadSHA: "head-b", LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil || !reopen.Approved {
		t.Fatalf("reopen claim = %+v / %v, want approved", reopen, err)
	}
	written, err := repo.WritePreviewBinding(context.Background(), PreviewBindingWrite{
		ApplicationID: repo.app.ID, PRNumber: 7,
		ReservationID: reopen.Reservation.ID, LeaseHeadSHA: "head-b", HeadSHA: "head-b",
		State: PreviewActive, ConsumeLease: true, LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil || written.Refused != "" {
		t.Fatalf("reopen promotion = %+v / %v, want stored", written, err)
	}
	if preview, err := repo.GetPreview(context.Background(), repo.app.ID, 7); err != nil || preview.State != PreviewActive {
		t.Fatalf("binding after the reopen = %+v / %v, want active", preview, err)
	}
}

func TestDeployFinishedUpdatesThePreviewComment(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	commenter := &fakeCommenter{}
	svc := newPreviewService(t, repo, deployer, commenter)

	sibling := uuid.New()
	if _, err := repo.UpsertPreview(context.Background(), Preview{
		ApplicationID: repo.app.ID,
		Repo:          repo.app.Repo, PRNumber: 7, Host: "pr-7-gotham.apps.example.com",
		PreviewApplicationID: sibling, State: PreviewActive,
	}); err != nil {
		t.Fatalf("UpsertPreview: %v", err)
	}

	svc.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: sibling, State: deploy.StateRunning, Host: "pr-7-gotham.apps.example.com",
	})
	if got := commenter.commentCount(); got != 1 {
		t.Fatalf("comments after running = %d, want 1", got)
	}
	commenter.mu.Lock()
	number, body, target := commenter.numbers[0], commenter.bodies[0], commenter.targets[0]
	commenter.mu.Unlock()
	if number != 7 || !strings.Contains(body, "live") {
		t.Errorf("running comment = PR %d %q, want PR 7 to report the preview live", number, body)
	}
	if target.UserID != repo.app.UserID || target.Repo != repo.app.Repo {
		t.Errorf("comment target = %+v, want the base application's identity", target)
	}

	svc.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: sibling, State: deploy.StateFailed, Host: "pr-7-gotham.apps.example.com",
	})
	if got := commenter.commentCount(); got != 2 {
		t.Fatalf("comments after failed = %d, want 2", got)
	}
	commenter.mu.Lock()
	number, body = commenter.numbers[1], commenter.bodies[1]
	commenter.mu.Unlock()
	if number != 7 || !strings.Contains(body, "failed") {
		t.Errorf("failed comment = PR %d %q, want PR 7 to report the failure", number, body)
	}

	svc.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: uuid.New(), State: deploy.StateRunning, Host: "other.example.com",
	})
	if got := commenter.commentCount(); got != 2 {
		t.Fatalf("comments after an unrelated deploy = %d, want no extra comment", got)
	}

	repo.mu.Lock()
	closing := repo.previews[previewKey(repo.app.ID, 7)]
	closing.State = PreviewClosing
	repo.previews[previewKey(repo.app.ID, 7)] = closing
	repo.mu.Unlock()
	svc.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: sibling, State: deploy.StateRunning, Host: "pr-7-gotham.apps.example.com",
	})
	if got := commenter.commentCount(); got != 2 {
		t.Fatalf("comments after a closing preview's deploy = %d, want no close overwrite", got)
	}
}

func TestFailedNoBindingCloseKeepsTheFence(t *testing.T) {
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := newPreviewService(t, repo, deployer, &fakeCommenter{})

	openClaim, err := repo.ClaimPreviewDelivery(context.Background(), PreviewClaim{
		ApplicationID: repo.app.ID, PRNumber: 7, Kind: ReservationStart,
		HeadSHA: "head-a", LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil || !openClaim.Approved {
		t.Fatalf("open claim = %+v / %v, want approved", openClaim, err)
	}

	repo.mu.Lock()
	repo.clearErr = errors.New("database down")
	repo.mu.Unlock()
	closeBody := githubPRBody("closed", 7, "feat/x", "main", "head-a")
	if _, err := receive(t, svc, closeBody); err == nil {
		t.Fatal("close with a failing ledger clear: no error, want one")
	} else if !errors.Is(err, ErrRetryable) {
		t.Fatalf("failed close = %v, want ErrRetryable so the host redelivers", err)
	}
	if !repo.hasCloseMarker(repo.app.ID, 7) {
		t.Fatal("the close marker was released by the failed clear")
	}

	promoted, err := repo.WritePreviewBinding(context.Background(), PreviewBindingWrite{
		ApplicationID: repo.app.ID, PRNumber: 7, ReservationID: openClaim.Reservation.ID,
		LeaseHeadSHA: "head-a", HeadSHA: "head-a", State: PreviewActive,
		ConsumeLease: true, LiveLimit: maxLivePreviewsPerApplication,
	})
	if err != nil {
		t.Fatalf("WritePreviewBinding: %v", err)
	}
	if promoted.Refused != BindingRefusedClosing {
		t.Fatalf("promotion after the failed close = %+v, want refused closing", promoted)
	}
	if _, err := repo.GetPreview(context.Background(), repo.app.ID, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding after the failed close = %v, want none", err)
	}

	repo.mu.Lock()
	repo.clearErr = nil
	repo.mu.Unlock()
	delivery, err := receive(t, svc, closeBody)
	if err != nil {
		t.Fatalf("Receive(close retry): %v", err)
	}
	if delivery.Status != StatusIgnored || delivery.Reason != "no preview" {
		t.Fatalf("close retry = %+v, want the no-binding close completed", delivery)
	}
	if repo.hasCloseMarker(repo.app.ID, 7) {
		t.Fatal("the close marker survived the completed close")
	}

	reopen, err := receive(t, svc, githubPRBody("opened", 7, "feat/x", "main", "head-b"))
	if err != nil || reopen.Status != StatusQueued {
		t.Fatalf("reopen = %+v / %v, want queued", reopen, err)
	}
}
