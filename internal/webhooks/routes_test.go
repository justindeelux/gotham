package webhooks

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

// contextKey marks a request the test auth middleware accepted.
type contextKey struct{}

// newRouteServer mounts the webhook routes behind an auth middleware that
// rejects requests with no Authorization header — the shape of the server's
// RequireAuth for bearer and token callers.
func newRouteServer(svc *Service, userID uuid.UUID) http.Handler {
	r := chi.NewRouter()
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, true)))
		})
	}
	Mount(r, auth, alwaysUser(userID), svc)
	return r
}

// alwaysUser returns a UserIDFunc that reports one fixed, authenticated user.
func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

// pushBody is a GitHub/Gitea-shaped push notification for the fake repo.
func pushBody(commit string) string {
	return `{"ref":"refs/heads/main","after":"` + commit + `","repository":{"full_name":"octo/gotham"}}`
}

// gitLabPushBody is the same push as the GitLab body shape.
func gitLabPushBody(commit string) string {
	return `{"ref":"refs/heads/main","after":"` + commit +
		`","project":{"path_with_namespace":"octo/gotham"}}`
}

// deliveryRequest builds a delivery for provider with the given headers.
func deliveryRequest(provider, body string, headers map[string]string, sourceIP string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+provider, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.RemoteAddr = sourceIP
	return req
}

// githubPushHeaders signs body with secret for a GitHub push delivery.
func githubPushHeaders(secret, body, deliveryID string) map[string]string {
	return map[string]string{
		headerGitHubSignature: githubSignaturePrefix + hmacHex(secret, []byte(body)),
		headerGitHubEvent:     "push",
		headerGitHubDelivery:  deliveryID,
	}
}

// managementRequest builds an authenticated management request.
func managementRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer token")
	return req
}

// TestRoutesCreateWebhookIgnoresUntrustedForwardedScheme pins L6: an untrusted
// peer cannot redirect the hook callback to https by sending X-Forwarded-Proto.
func TestRoutesCreateWebhookIgnoresUntrustedForwardedScheme(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	srv := newRouteServer(newTestService(repo, installer, &fakeDeployer{}), repo.app.UserID)
	path := "/v1/applications/" + repo.app.ID.String() + "/webhooks"

	req := managementRequest(http.MethodPost, path)
	req.Host = "cp.example.com"
	req.RemoteAddr = "203.0.113.7:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if got, want := installer.created[0].URL, "http://cp.example.com/api/v1/webhooks/github"; got != want {
		t.Errorf("hook url = %q, want %q (untrusted peer must not force https)", got, want)
	}
}

// TestRoutesCreateWebhookTrustedProxyScheme pins that a trusted proxy's
// X-Forwarded-Proto is honored for the callback origin.
func TestRoutesCreateWebhookTrustedProxyScheme(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	trusted, err := clientip.Parse([]string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("clientip.Parse: %v", err)
	}
	svc := newTestServiceWith(Config{
		Repository:     repo,
		Installer:      installer,
		Deployer:       &fakeDeployer{},
		Logger:         discardLogger(),
		TrustedProxies: trusted,
	})
	srv := newRouteServer(svc, repo.app.UserID)
	path := "/v1/applications/" + repo.app.ID.String() + "/webhooks"

	req := managementRequest(http.MethodPost, path)
	req.Host = "cp.example.com"
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if got, want := installer.created[0].URL, "https://cp.example.com/api/v1/webhooks/github"; got != want {
		t.Errorf("hook url = %q, want %q", got, want)
	}
}

func TestRoutesDeliveryValidSignaturePerProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		body     string
		headers  func(secret, body string) map[string]string
	}{
		{
			name:     "github hmac sha256",
			provider: providers.NameGitHub,
			body:     pushBody("abc123"),
			headers: func(secret, body string) map[string]string {
				return githubPushHeaders(secret, body, "delivery-1")
			},
		},
		{
			name:     "gitlab shared token",
			provider: providers.NameGitLab,
			body:     gitLabPushBody("abc123"),
			headers: func(secret, _ string) map[string]string {
				return map[string]string{
					headerGitLabToken:    secret,
					headerGitLabEvent:    "Push Hook",
					headerGitLabDelivery: "delivery-1",
				}
			},
		},
		{
			name:     "gitea hmac sha256",
			provider: providers.NameGitea,
			body:     pushBody("abc123"),
			headers: func(secret, body string) map[string]string {
				return map[string]string{
					headerGiteaSignature: hmacHex(secret, []byte(body)),
					headerGiteaEvent:     "push",
					headerGiteaDelivery:  "delivery-1",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "hook-secret"
			repo := newFakeRepositoryFor(tc.provider).withTarget()
			deployer := &fakeDeployer{}
			svc := newTestService(repo, &fakeInstaller{}, deployer)
			srv := newRouteServer(svc, repo.app.UserID)

			req := deliveryRequest(tc.provider, tc.body, tc.headers(secret, tc.body), "10.0.0.1:4242")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
			}
			var got Delivery
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Status != StatusQueued || got.DeploymentID == "" {
				t.Fatalf("delivery = %+v, want a queued deployment", got)
			}
			if deployer.deployCount() != 1 {
				t.Errorf("deployments = %d, want 1", deployer.deployCount())
			}
			if repo.claimCount() != 1 {
				t.Errorf("claims = %d, want 1", repo.claimCount())
			}
		})
	}
}

func TestRoutesDeliveryInvalidSignaturePerProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		body     string
		headers  func() map[string]string
	}{
		{
			name:     "github wrong hmac",
			provider: providers.NameGitHub,
			body:     pushBody("abc123"),
			headers: func() map[string]string {
				return map[string]string{
					headerGitHubSignature: githubSignaturePrefix + "deadbeef",
					headerGitHubEvent:     "push",
				}
			},
		},
		{
			name:     "github missing signature",
			provider: providers.NameGitHub,
			body:     pushBody("abc123"),
			headers: func() map[string]string {
				return map[string]string{headerGitHubEvent: "push"}
			},
		},
		{
			name:     "gitlab wrong token",
			provider: providers.NameGitLab,
			body:     gitLabPushBody("abc123"),
			headers: func() map[string]string {
				return map[string]string{
					headerGitLabToken: "not-the-token",
					headerGitLabEvent: "Push Hook",
				}
			},
		},
		{
			name:     "gitea wrong hmac",
			provider: providers.NameGitea,
			body:     pushBody("abc123"),
			headers: func() map[string]string {
				return map[string]string{
					headerGiteaSignature: "deadbeef",
					headerGiteaEvent:     "push",
				}
			},
		},
		{
			name:     "gitea signature over another body",
			provider: providers.NameGitea,
			body:     pushBody("tampered"),
			headers: func() map[string]string {
				return map[string]string{
					// A valid signature replayed over edited bytes must fail.
					headerGiteaSignature: hmacHex("hook-secret", []byte(pushBody("abc123"))),
					headerGiteaEvent:     "push",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepositoryFor(tc.provider).withTarget()
			deployer := &fakeDeployer{}
			svc := newTestService(repo, &fakeInstaller{}, deployer)
			srv := newRouteServer(svc, repo.app.UserID)

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, deliveryRequest(tc.provider, tc.body, tc.headers(), "10.0.0.1:4242"))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			if deployer.deployCount() != 0 {
				t.Errorf("deployments = %d, want 0", deployer.deployCount())
			}
			if repo.claimCount() != 0 {
				t.Errorf("claims = %d, want 0", repo.claimCount())
			}
		})
	}
}

func TestRoutesDeliveryDedupeByCommitSHA(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

	body := pushBody("abc123")
	send := func(t *testing.T, deliveryID string) *httptest.ResponseRecorder {
		t.Helper()
		headers := githubPushHeaders(secret, body, deliveryID)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body, headers, "10.0.0.1:4242"))
		return rec
	}

	if rec := send(t, "delivery-1"); rec.Code != http.StatusAccepted {
		t.Fatalf("first delivery status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	// The same commit under a different delivery GUID — a provider retry, or a
	// second hook firing for the same push — must not queue a second build.
	rec := send(t, "delivery-2")
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got Delivery
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusDuplicate {
		t.Errorf("status = %q, want %q", got.Status, StatusDuplicate)
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments = %d, want 1", deployer.deployCount())
	}
	if repo.claimCount() != 1 {
		t.Errorf("claims = %d, want 1", repo.claimCount())
	}
}

// TestRoutesDeliveryConflictRedelivers pins the redelivery half of C3-3: the
// 503 leaves the claim released, so when the host retries the same push after
// the active build finishes the retry queues the deployment normally.
func TestRoutesDeliveryConflictRedelivers(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: deploy.ErrConflict}
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

	body := pushBody("abc123")
	send := func(t *testing.T, deliveryID string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body,
			githubPushHeaders(secret, body, deliveryID), "10.0.0.1:4242"))
		return rec
	}

	if rec := send(t, "delivery-1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	deployer.setErr(nil) // the active build finished
	if rec := send(t, "delivery-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments = %d, want 1", deployer.deployCount())
	}
}

// gatedDeployer blocks DeploySystem until release, so a test can interleave a
// duplicate delivery while the winning claim is still in flight (no deployment
// linked yet).
type gatedDeployer struct {
	entered chan struct{}
	release chan struct{}
}

func (d *gatedDeployer) DeploySystem(_ context.Context, appID uuid.UUID) (deploy.Deployment, error) {
	close(d.entered)
	<-d.release
	return deploy.Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          deploy.KindDeploy,
		State:         deploy.StateQueued,
	}, nil
}

// TestReceiveDuplicateWaitsForDurableClaim pins C3-4: a duplicate that arrives
// while the winning delivery has claimed but not yet linked its deployment must
// not be acknowledged — if it were 200 and the winner then released its claim,
// the commit would be dropped. It answers retryable, and once the winner's
// deployment is durable the same redelivery becomes a 200 duplicate no-op.
func TestReceiveDuplicateWaitsForDurableClaim(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &gatedDeployer{entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(Config{Repository: repo, Deployer: deployer, Logger: discardLogger()})

	body := pushBody("abc123")
	request := func(deliveryID string) *http.Request {
		return deliveryRequest(providers.NameGitHub, body,
			githubPushHeaders(secret, body, deliveryID), "10.0.0.1:4242")
	}

	winner := make(chan error, 1)
	go func() {
		_, err := svc.Receive(context.Background(), providers.NameGitHub, request("delivery-1"))
		winner <- err
	}()
	<-deployer.entered // the winner has claimed; DeploymentSystem is in flight

	if _, err := svc.Receive(context.Background(), providers.NameGitHub, request("delivery-2")); !errors.Is(err, ErrRetryable) {
		t.Fatalf("duplicate while winner in flight error = %v, want ErrRetryable", err)
	}

	close(deployer.release)
	if err := <-winner; err != nil {
		t.Fatalf("winner: %v", err)
	}

	// The winner's deployment is now linked: the same delivery is a durable
	// duplicate no-op instead of a retry.
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, request("delivery-3"))
	if err != nil {
		t.Fatalf("durable duplicate: %v", err)
	}
	if delivery.Status != StatusDuplicate {
		t.Errorf("status = %q, want %q", delivery.Status, StatusDuplicate)
	}
	if repo.claimCount() != 1 {
		t.Errorf("claims = %d, want 1", repo.claimCount())
	}
}

// cancelOnDeploy cancels the delivery context as it crosses the deploy boundary
// and then returns err, modelling a client disconnect at the deadline.
type cancelOnDeploy struct {
	cancel context.CancelFunc
	err    error
}

func (d *cancelOnDeploy) DeploySystem(_ context.Context, appID uuid.UUID) (deploy.Deployment, error) {
	d.cancel()
	if d.err != nil {
		return deploy.Deployment{}, d.err
	}
	return deploy.Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          deploy.KindDeploy,
		State:         deploy.StateQueued,
	}, nil
}

// TestReceiveReleasesClaimOnCancelledRequest pins U1: when the request context
// is cancelled at the deploy boundary, the claim must still be released (on a
// detached context) or it is stranded and every redelivery answers 503.
func TestReceiveReleasesClaimOnCancelledRequest(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	ctx, cancel := context.WithCancel(context.Background())
	svc := NewService(Config{
		Repository: repo,
		Deployer:   &cancelOnDeploy{cancel: cancel, err: deploy.ErrConflict},
		Logger:     discardLogger(),
	})
	body := pushBody("abc123")

	_, err := svc.Receive(ctx, providers.NameGitHub,
		deliveryRequest(providers.NameGitHub, body, githubPushHeaders(secret, body, "delivery-1"), "10.0.0.1:4242"))
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("err = %v, want ErrRetryable", err)
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0 (released despite the cancelled request context)", repo.claimCount())
	}
}

// TestReceiveLinksClaimOnCancelledRequest pins the link half of U1: a
// successful deploy whose request context was cancelled must still link the
// deployment, so a redelivery is a durable duplicate rather than a retry.
func TestReceiveLinksClaimOnCancelledRequest(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	ctx, cancel := context.WithCancel(context.Background())
	svc := NewService(Config{
		Repository: repo,
		Deployer:   &cancelOnDeploy{cancel: cancel},
		Logger:     discardLogger(),
	})
	body := pushBody("abc123")

	delivery, err := svc.Receive(ctx, providers.NameGitHub,
		deliveryRequest(providers.NameGitHub, body, githubPushHeaders(secret, body, "delivery-1"), "10.0.0.1:4242"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("status = %q, want %q", delivery.Status, StatusQueued)
	}

	dup, err := svc.Receive(context.Background(), providers.NameGitHub,
		deliveryRequest(providers.NameGitHub, body, githubPushHeaders(secret, body, "delivery-2"), "10.0.0.1:4242"))
	if err != nil {
		t.Fatalf("redelivery = %v, want a durable duplicate (the link must have landed)", err)
	}
	if dup.Status != StatusDuplicate {
		t.Errorf("status = %q, want %q", dup.Status, StatusDuplicate)
	}
}

func TestRoutesDeliveryRateLimited(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{}
	svc := NewService(Config{
		Repository: repo,
		Installer:  &fakeInstaller{},
		Deployer:   deployer,
		Logger:     discardLogger(),
		Limit:      rate.Every(time.Hour),
		Burst:      2,
	})
	srv := newRouteServer(svc, repo.app.UserID)

	statuses := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		// A fresh commit per request so the anti-spam dedupe never masks the
		// rate limiter this test is about.
		push := pushBody("commit-" + strconv.Itoa(i))
		headers := githubPushHeaders(secret, push, "delivery-"+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, push, headers, "10.0.0.9:4242"))
		statuses = append(statuses, rec.Code)
	}

	if statuses[0] != http.StatusAccepted || statuses[1] != http.StatusAccepted {
		t.Fatalf("first two statuses = %v, want 202 202", statuses)
	}
	if statuses[2] != http.StatusTooManyRequests {
		t.Errorf("third status = %d, want 429 (body exhausted)", statuses[2])
	}
	if deployer.deployCount() != 2 {
		t.Errorf("deployments = %d, want 2", deployer.deployCount())
	}
}

// TestRoutesDeliveryClientIPBehindTrustedProxy pins that the delivery limiter
// keys on X-Forwarded-For when the direct peer is a trusted proxy: distinct
// forwarded clients behind one proxy do not share a bucket.
func TestRoutesDeliveryClientIPBehindTrustedProxy(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	trusted, err := clientip.Parse([]string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("clientip.Parse: %v", err)
	}
	svc := NewService(Config{
		Repository:     repo,
		Installer:      &fakeInstaller{},
		Deployer:       &fakeDeployer{},
		Logger:         discardLogger(),
		Limit:          rate.Every(time.Hour),
		Burst:          1,
		TrustedProxies: trusted,
	})
	srv := newRouteServer(svc, repo.app.UserID)

	post := func(client, commit string) int {
		push := pushBody(commit)
		headers := githubPushHeaders(secret, push, "delivery-"+commit)
		rec := httptest.NewRecorder()
		req := deliveryRequest(providers.NameGitHub, push, headers, "127.0.0.1:5000")
		req.Header.Set("X-Forwarded-For", client)
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := post("203.0.113.1", "c1"); got != http.StatusAccepted {
		t.Fatalf("first client status = %d, want 202", got)
	}
	if got := post("203.0.113.2", "c2"); got != http.StatusAccepted {
		t.Fatalf("second client status = %d, want 202 (separate bucket)", got)
	}
	if got := post("203.0.113.1", "c3"); got != http.StatusTooManyRequests {
		t.Fatalf("repeat client status = %d, want 429", got)
	}
}

func TestRoutesDeliveryIgnoresNonBuildingEvents(t *testing.T) {
	const secret = "hook-secret"
	cases := []struct {
		name   string
		header string
		value  string
		body   string
	}{
		{
			name:   "provider ping",
			header: headerGitHubEvent,
			value:  "ping",
			body:   `{"zen":"Design for failure.","repository":{"full_name":"octo/gotham"}}`,
		},
		{
			name:   "another branch",
			header: headerGitHubEvent,
			value:  "push",
			body:   `{"ref":"refs/heads/feature","after":"abc123","repository":{"full_name":"octo/gotham"}}`,
		},
		{
			name:   "tag push",
			header: headerGitHubEvent,
			value:  "push",
			body:   `{"ref":"refs/tags/v1.0.0","after":"abc123","repository":{"full_name":"octo/gotham"}}`,
		},
		{
			name:   "deleted branch",
			header: headerGitHubEvent,
			value:  "push",
			body:   `{"ref":"refs/heads/main","after":"0000000000000000000000000000000000000000","repository":{"full_name":"octo/gotham"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository().withTarget()
			deployer := &fakeDeployer{}
			srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

			headers := map[string]string{
				headerGitHubSignature: githubSignaturePrefix + hmacHex(secret, []byte(tc.body)),
				headerGitHubDelivery:  "delivery-1",
				tc.header:             tc.value,
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, tc.body, headers, "10.0.0.1:4242"))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			var got Delivery
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Status != StatusIgnored {
				t.Errorf("status = %q, want %q", got.Status, StatusIgnored)
			}
			if deployer.deployCount() != 0 {
				t.Errorf("deployments = %d, want 0", deployer.deployCount())
			}
		})
	}
}

// TestRoutesDeliveryBranchMatchIsExact pins C3-7: branch names are
// case-sensitive, so a push to "main" must not consume the watched branch's
// claim when the application watches "Main" (the clone still checks out
// "Main").
func TestRoutesDeliveryBranchMatchIsExact(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	repo.app.Branch = "Main"
	repo.target.Branch = "Main"
	deployer := &fakeDeployer{}
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

	push := func(branch, commit, deliveryID string) *httptest.ResponseRecorder {
		body := `{"ref":"refs/heads/` + branch + `","after":"` + commit +
			`","repository":{"full_name":"octo/gotham"}}`
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body,
			githubPushHeaders(secret, body, deliveryID), "10.0.0.1:4242"))
		return rec
	}

	// A push to the differently-cased branch is ignored and claims nothing.
	rec := push("main", "abc123", "delivery-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("lower-case branch status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got Delivery
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusIgnored {
		t.Errorf("status = %q, want %q", got.Status, StatusIgnored)
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0 (the watched branch's claim must not be consumed)", repo.claimCount())
	}
	if deployer.deployCount() != 0 {
		t.Errorf("deployments = %d, want 0", deployer.deployCount())
	}

	// The exact watched branch still deploys.
	if rec := push("Main", "abc123", "delivery-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("watched branch status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments = %d, want 1", deployer.deployCount())
	}
}

func TestRoutesDeliveryUnknownRepositoryIsUnauthorized(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, &fakeDeployer{}), repo.app.UserID)

	body := `{"ref":"refs/heads/main","after":"abc123","repository":{"full_name":"someone/else"}}`
	headers := githubPushHeaders(secret, body, "delivery-1")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body, headers, "10.0.0.1:4242"))

	// An unknown repository must answer exactly like a bad signature.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRoutesDeliveryUnknownProvider(t *testing.T) {
	repo := newFakeRepository().withTarget()
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, &fakeDeployer{}), repo.app.UserID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deliveryRequest("bitbucket", "{}", nil, "10.0.0.1:4242"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRoutesDeliveryMalformedBody(t *testing.T) {
	repo := newFakeRepository().withTarget()
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, &fakeDeployer{}), repo.app.UserID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, "not json", nil, "10.0.0.1:4242"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRoutesDeliveryDeployDisabled(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: deploy.ErrDisabled}
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

	body := pushBody("abc123")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body,
		githubPushHeaders(secret, body, "delivery-1"), "10.0.0.1:4242"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	// Nothing was queued, so the claim must be released: a later retry of this
	// delivery is a legitimate attempt, not spam.
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0", repo.claimCount())
	}
}

// TestRoutesDeliveryConflictIsRetryable pins C3-3: a push that arrives while a
// build is in flight must not be acknowledged as delivered (200) or the commit
// is silently dropped. The route answers 503 so the Git host redelivers it, and
// the claim is released so that redelivery is not mistaken for spam.
func TestRoutesDeliveryConflictIsRetryable(t *testing.T) {
	const secret = "hook-secret"
	repo := newFakeRepository().withTarget()
	deployer := &fakeDeployer{err: deploy.ErrConflict}
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, deployer), repo.app.UserID)

	body := pushBody("abc123")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deliveryRequest(providers.NameGitHub, body,
		githubPushHeaders(secret, body, "delivery-1"), "10.0.0.1:4242"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	if repo.claimCount() != 0 {
		t.Errorf("claims = %d, want 0 (the redelivery must be able to claim again)", repo.claimCount())
	}
}

func TestRoutesManagementRequiresAuth(t *testing.T) {
	repo := newFakeRepository()
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, &fakeDeployer{}), repo.app.UserID)
	appID := repo.app.ID.String()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/applications/" + appID + "/webhooks"},
		{http.MethodDelete, "/v1/applications/" + appID + "/webhooks"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRoutesCreateWebhookIsIdempotent(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	srv := newRouteServer(newTestService(repo, installer, &fakeDeployer{}), repo.app.UserID)
	path := "/v1/applications/" + repo.app.ID.String() + "/webhooks"

	send := func(t *testing.T) hookResponse {
		t.Helper()
		req := managementRequest(http.MethodPost, path)
		req.Host = "cp.example.com"
		req.TLS = &tls.ConnectionState{}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
		var envelope hookEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return envelope.Hook
	}

	first := send(t)
	second := send(t)

	if first.ID != second.ID {
		t.Errorf("ids = %s and %s, want the same hook", first.ID, second.ID)
	}
	if len(installer.created) != 1 {
		t.Errorf("provider installs = %d, want 1 (create must not repeat)", len(installer.created))
	}
	if got := installer.created[0].URL; got != "https://cp.example.com/api/v1/webhooks/github" {
		t.Errorf("hook url = %q, want the public delivery endpoint", got)
	}
	if installer.created[0].Secret == "" {
		t.Error("hook secret is empty")
	}
	if repo.hook == nil || repo.hook.HookID == "" {
		t.Fatalf("stored hook = %+v, want the provider hook id", repo.hook)
	}
	if repo.hook.ID.String() != first.ID {
		t.Errorf("stored row id = %s, response id = %s", repo.hook.ID, first.ID)
	}
}

func TestRoutesDeleteWebhookIsIdempotent(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})
	srv := newRouteServer(svc, repo.app.UserID)
	appID := repo.app.ID.String()

	if _, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example.com/api/v1/webhooks"); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	send := func(t *testing.T, want bool) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, managementRequest(http.MethodDelete, "/v1/applications/"+appID+"/webhooks"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var got deleteEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Deleted != want {
			t.Errorf("deleted = %v, want %v", got.Deleted, want)
		}
	}

	send(t, true)
	send(t, false)

	if len(installer.deleted) != 1 {
		t.Errorf("provider deletions = %d, want 1 (delete must not repeat)", len(installer.deleted))
	}
	if repo.hook != nil {
		t.Errorf("stored hook = %+v, want it removed", repo.hook)
	}
}

func TestRoutesDeleteWebhookProviderFailureKeepsRow(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{deleteErr: io.ErrUnexpectedEOF}
	svc := newTestService(repo, installer, &fakeDeployer{})
	if _, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example.com/api/v1/webhooks"); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	rec := httptest.NewRecorder()
	newRouteServer(svc, repo.app.UserID).ServeHTTP(rec,
		managementRequest(http.MethodDelete, "/v1/applications/"+repo.app.ID.String()+"/webhooks"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
	if repo.hook == nil {
		t.Error("stored hook was removed although the host still has it")
	}
}

// TestRoutesForgetWebhookForceEscapeHatch pins ?force=true: the caller
// acknowledges that the provider cannot be reached, the stored row is dropped
// without any host call (so a stalled provider cannot block the hatch), and
// the response reports the removal. The strict route keeps the row and answers
// 502 — see TestRoutesDeleteWebhookProviderFailureKeepsRow.
func TestRoutesForgetWebhookForceEscapeHatch(t *testing.T) {
	repo := newFakeRepository().withTarget()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	rec := httptest.NewRecorder()
	newRouteServer(svc, repo.app.UserID).ServeHTTP(rec,
		managementRequest(http.MethodDelete,
			"/v1/applications/"+repo.app.ID.String()+"/webhooks?force=true"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got deleteEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Deleted {
		t.Error("deleted = false, want true")
	}
	if repo.hook != nil {
		t.Errorf("stored hook = %+v, want it forgotten", repo.hook)
	}
	if len(installer.deleted) != 0 {
		t.Errorf("provider deletions = %v, want none (force must not contact the host)", installer.deleted)
	}
}

func TestRoutesCreateWebhookProviderFailure(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{createErr: io.ErrUnexpectedEOF}
	srv := newRouteServer(newTestService(repo, installer, &fakeDeployer{}), repo.app.UserID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, managementRequest(http.MethodPost,
		"/v1/applications/"+repo.app.ID.String()+"/webhooks"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
	if repo.hook != nil {
		t.Error("a hook was stored although the host refused the install")
	}
}

func TestRoutesCreateWebhookForeignApplicationIsNotFound(t *testing.T) {
	repo := newFakeRepository()
	srv := newRouteServer(newTestService(repo, &fakeInstaller{}, &fakeDeployer{}), repo.app.UserID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, managementRequest(http.MethodPost,
		"/v1/applications/"+uuid.NewString()+"/webhooks"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRoutesCreateWebhookRequiresConnectedProvider(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{createErr: providers.ErrNotConnected}
	srv := newRouteServer(newTestService(repo, installer, &fakeDeployer{}), repo.app.UserID)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, managementRequest(http.MethodPost,
		"/v1/applications/"+repo.app.ID.String()+"/webhooks"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}
