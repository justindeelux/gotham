package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/providers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fakeProviderSource is a canned DNSProviderSource.
type fakeProviderSource struct {
	providers []DNSProvider
	err       error
}

// ListDNSProviders returns the canned providers.
func (f *fakeProviderSource) ListDNSProviders(context.Context) ([]DNSProvider, error) {
	return f.providers, f.err
}

// dnsCertificateApp returns a routable application carrying a dns-01
// certificate configuration referencing providerID.
func dnsCertificateApp(serverID, providerID uuid.UUID, wildcard bool) ProxiedApplication {
	app := runningApp(serverID, "app.example.com", "app-container", 3000, 18080)
	app.Certificate = &CertificateIntent{
		Domain:        "app.example.com",
		Enabled:       true,
		Challenge:     ChallengeDNS01,
		Wildcard:      wildcard,
		DNSProviderID: providerID,
	}
	return app
}

// sealedProvider builds an enabled cloudflare provider sealed with secret.
func sealedProvider(t *testing.T, secret string) DNSProvider {
	t.Helper()
	sealed, err := providers.SealSecret(secret, "cf-secret-token")
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	return DNSProvider{
		ID:               uuid.New(),
		Provider:         ProviderCloudflare,
		Zones:            []string{"example.com"},
		Enabled:          true,
		SealedCredential: sealed,
	}
}

// TestSyncServerDeliversDNSProviderCredentialsAsEnvironment proves the DNS-01
// credential reaches the container as an environment variable only: the
// generated documents and the labels never contain it, and the fingerprint
// label records the desired environment.
func TestSyncServerDeliversDNSProviderCredentialsAsEnvironment(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, true)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	if len(fixture.containers.runs) != 1 {
		t.Fatalf("runs = %d, want the bootstrap create", len(fixture.containers.runs))
	}
	run := fixture.containers.runs[0]
	if len(run.Env) != 1 || run.Env[0] != "CF_DNS_API_TOKEN=cf-secret-token" {
		t.Fatalf("env = %v, want only the referenced provider credential", run.Env)
	}
	if run.Labels[TraefikEnvHashLabel] != envFingerprint("test-secret", run.Env) {
		t.Fatalf("env fingerprint label = %q, want the keyed fingerprint", run.Labels[TraefikEnvHashLabel])
	}
	for name, value := range run.Labels {
		if strings.Contains(value, "cf-secret-token") {
			t.Fatalf("label %q leaks the credential", name)
		}
	}

	files := filesByPath(fixture.agent.calls[len(fixture.agent.calls)-1].GetFiles())
	for name, content := range files {
		if strings.Contains(content, "cf-secret-token") {
			t.Fatalf("%s contains the credential", name)
		}
	}
	static := files[StaticFileName(FormatYAML)]
	if !strings.Contains(static, "dnsChallenge:") || !strings.Contains(static, "provider: cloudflare") {
		t.Fatalf("static document is missing the DNS-01 resolver:\n%s", static)
	}
	dynamic := files[DynamicFileName(FormatYAML)]
	if !strings.Contains(dynamic, "certResolver: letsencrypt-dns-cloudflare") {
		t.Fatalf("dynamic document is missing the DNS-01 resolver:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "main: app.example.com") || !strings.Contains(dynamic, `- '*.example.com'`) {
		t.Fatalf("wildcard tls.domains missing:\n%s", dynamic)
	}
}

// TestSyncServerContainerConvergenceOnCredentialChange proves an existing
// container with the matching fingerprint is reused, while an environment
// change (rotation) recreates it with the new credentials.
func TestSyncServerContainerConvergenceOnCredentialChange(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, false)
	desiredEnv := []string{"CF_DNS_API_TOKEN=cf-secret-token"}

	t.Run("matching environment is reused", func(t *testing.T) {
		fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
		matching := runningTraefik()
		matching.Labels[TraefikEnvHashLabel] = envFingerprint("test-secret", desiredEnv)
		fixture.containers.list = []containers.Container{matching, appContainer("app-container", 32768)}
		fixture.service.secret = "test-secret"
		fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

		if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
			t.Fatalf("SyncServer: %v", err)
		}
		if len(fixture.containers.removed) != 0 {
			t.Fatalf("removed = %v, want the matched container reused", fixture.containers.removed)
		}
		if len(fixture.containers.runs) != 0 {
			t.Fatalf("runs = %d, want no recreate", len(fixture.containers.runs))
		}
	})

	t.Run("rotated credential recreates", func(t *testing.T) {
		fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
		stale := runningTraefik() // fingerprint of an empty environment
		fixture.containers.list = []containers.Container{stale, appContainer("app-container", 32768)}
		fixture.service.secret = "test-secret"
		fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

		if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
			t.Fatalf("SyncServer: %v", err)
		}
		if len(fixture.containers.removed) != 1 || fixture.containers.removed[0] != stale.ID {
			t.Fatalf("removed = %v, want the drifted container recreated", fixture.containers.removed)
		}
		if len(fixture.containers.runs) != 1 {
			t.Fatalf("runs = %d, want one recreate", len(fixture.containers.runs))
		}
		run := fixture.containers.runs[0]
		if len(run.Env) != 1 || run.Env[0] != "CF_DNS_API_TOKEN=cf-secret-token" {
			t.Fatalf("recreated env = %v, want the rotated credential", run.Env)
		}
	})
}

// dnsEnvToken is the credential value the DNS-env fixtures deliver.
const dnsEnvToken = "cf-secret-token"

// dnsEnv is the desired credential environment of a node with one active
// DNS-01 provider.
func dnsEnv() []string {
	return []string{"CF_DNS_API_TOKEN=" + dnsEnvToken}
}

// dnsEnvSyncFixture builds a fixture with one active DNS provider, so the
// desired environment of the node carries the token.
func dnsEnvSyncFixture(t *testing.T, serverID uuid.UUID) *syncFixture {
	t.Helper()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, false)
	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}
	return fixture
}

// matchingTraefik is the production container carrying the fingerprint of the
// DNS credential environment, so a sync reuses it instead of recreating.
func matchingTraefik() containers.Container {
	container := runningTraefik()
	container.Labels[TraefikEnvHashLabel] = envFingerprint("test-secret", dnsEnv())
	return container
}

// assertRedacted asserts the error text carries no credential value, keeps the
// redaction marker and preserves the diagnostic context.
func assertRedacted(t *testing.T, err error, wantContext string) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), dnsEnvToken) {
		t.Fatalf("credential leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("error was not redacted: %v", err)
	}
	if wantContext != "" && !strings.Contains(err.Error(), wantContext) {
		t.Fatalf("error lost its diagnostic context %q: %v", wantContext, err)
	}
}

// postSyncError runs the sync route with a canned service error and returns
// the recorder.
func postSyncError(t *testing.T, serverID uuid.UUID, err error) *httptest.ResponseRecorder {
	t.Helper()
	svc := &fakeProxyService{syncErr: err}
	return postSync(t, newRoutes(svc), `{"server_id":"`+serverID.String()+`"}`)
}

// TestSyncServerRedactsEveryPushFailurePath injects a token-bearing failure at
// each non-Run push step (pull, start, remove, agent write) and requires the
// shared boundary to scrub it while keeping the diagnostic context.
func TestSyncServerRedactsEveryPushFailurePath(t *testing.T) {
	serverID := uuid.New()
	tokenErr := func(context string) error {
		return fmt.Errorf("%s: invalid environment CF_DNS_API_TOKEN=%s", context, dnsEnvToken)
	}
	cases := []struct {
		name    string
		prepare func(*syncFixture)
		want    string
	}{
		{
			name: "pull failure",
			prepare: func(f *syncFixture) {
				f.containers.list = []containers.Container{appContainer("app-container", 32768)}
				f.containers.pullErr = tokenErr("pull image")
			},
			want: "pull image",
		},
		{
			name: "start failure",
			prepare: func(f *syncFixture) {
				stopped := matchingTraefik()
				stopped.State = "stopped"
				f.containers.list = []containers.Container{stopped, appContainer("app-container", 32768)}
				f.containers.startErr = tokenErr("start container")
			},
			want: "start container",
		},
		{
			name: "remove failure",
			prepare: func(f *syncFixture) {
				drifted := runningTraefik()
				drifted.Image = "traefik:v2"
				f.containers.list = []containers.Container{drifted, appContainer("app-container", 32768)}
				f.containers.removeErr = tokenErr("remove container")
			},
			want: "remove container",
		},
		{
			name: "agent write failure",
			prepare: func(f *syncFixture) {
				f.containers.list = []containers.Container{matchingTraefik(), appContainer("app-container", 32768)}
				f.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
					return nil, tokenErr("agent write")
				}
			},
			want: "agent write",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := dnsEnvSyncFixture(t, serverID)
			tc.prepare(fixture)
			err := fixture.service.SyncServer(context.Background(), serverID)
			assertRedacted(t, err, tc.want)
		})
	}
}

// TestSyncServerRedactsReloadPingError covers the ping/reload path: an agent
// response echoing the token must not reach the API and must keep the ErrReload
// classification and the established 502 status.
func TestSyncServerRedactsReloadPingError(t *testing.T) {
	serverID := uuid.New()
	fixture := dnsEnvSyncFixture(t, serverID)
	fixture.containers.list = []containers.Container{matchingTraefik(), appContainer("app-container", 32768)}
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return &agentv1.WriteProxyConfigResponse{PingError: "ping rejected CF_DNS_API_TOKEN=" + dnsEnvToken}, nil
	}

	err := fixture.service.SyncServer(context.Background(), serverID)
	assertRedacted(t, err, "")
	if !errors.Is(err, ErrReload) {
		t.Fatalf("errors.Is(err, ErrReload) = false for %v", err)
	}
	recorder := postSyncError(t, serverID, err)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("reload status = %d, want 502 (body %s)", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), dnsEnvToken) {
		t.Fatalf("route response leaked the credential: %s", recorder.Body.String())
	}
}

// TestRevertServerRedactsPingError covers the revert path: the reverted push
// uses the current credential environment and its ping error must be scrubbed
// too.
func TestRevertServerRedactsPingError(t *testing.T) {
	serverID := uuid.New()
	fixture := dnsEnvSyncFixture(t, serverID)
	fixture.containers.list = []containers.Container{matchingTraefik(), appContainer("app-container", 32768)}
	versionA := []File{{Name: "dynamic/gotham.yml", Content: []byte("A")}}
	versionB := []File{{Name: "dynamic/gotham.yml", Content: []byte("B")}}
	fixture.history.active = &ConfigVersion{ID: uuid.New(), Files: versionB, ContentHash: configHash(versionB)}
	fixture.history.superseded = []ConfigVersion{{ID: uuid.New(), Files: versionA, ContentHash: configHash(versionA)}}
	fixture.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
		return &agentv1.WriteProxyConfigResponse{PingError: "revert ping saw CF_DNS_API_TOKEN=" + dnsEnvToken}, nil
	}

	err := fixture.service.RevertServer(context.Background(), serverID)
	assertRedacted(t, err, "")
	if !errors.Is(err, ErrReload) {
		t.Fatalf("errors.Is(err, ErrReload) = false for %v", err)
	}
}

// TestRedactionPreservesErrorClassificationAndStatus proves the sanitizer
// wraps the original error instead of replacing it: a token-bearing backend
// failure keeps its proxy sentinel and its established HTTP status.
func TestRedactionPreservesErrorClassificationAndStatus(t *testing.T) {
	serverID := uuid.New()
	cases := []struct {
		name       string
		backendErr error
		wantErr    error
		wantStatus int
	}{
		{
			name:       "agent unavailable",
			backendErr: fmt.Errorf("%w: invalid env CF_DNS_API_TOKEN=%s", containers.ErrAgentUnavailable, dnsEnvToken),
			wantErr:    ErrAgentUnavailable,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "validation",
			backendErr: fmt.Errorf("%w: invalid env CF_DNS_API_TOKEN=%s", containers.ErrValidation, dnsEnvToken),
			wantErr:    ErrValidation,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "server not found",
			backendErr: fmt.Errorf("%w: invalid env CF_DNS_API_TOKEN=%s", containers.ErrServerNotFound, dnsEnvToken),
			wantErr:    ErrServerNotFound,
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := dnsEnvSyncFixture(t, serverID)
			fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}
			fixture.containers.runErr = tc.backendErr

			err := fixture.service.SyncServer(context.Background(), serverID)
			assertRedacted(t, err, "")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("errors.Is(err, %v) = false for %v", tc.wantErr, err)
			}
			recorder := postSyncError(t, serverID, err)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("API status = %d, want %d (body %s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), dnsEnvToken) {
				t.Fatalf("route response leaked the credential: %s", recorder.Body.String())
			}
		})
	}
}

// TestPushRedactsCloseErrorInLogs proves the deferred agent Close error is
// scrubbed before it reaches the logs.
func TestPushRedactsCloseErrorInLogs(t *testing.T) {
	serverID := uuid.New()
	fixture := dnsEnvSyncFixture(t, serverID)
	fixture.containers.list = []containers.Container{matchingTraefik(), appContainer("app-container", 32768)}
	fixture.agent.closeErr = fmt.Errorf("close failed: CF_DNS_API_TOKEN=%s", dnsEnvToken)
	var logs bytes.Buffer
	fixture.service.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	output := logs.String()
	if strings.Contains(output, dnsEnvToken) {
		t.Fatalf("close error leaked the credential into logs: %s", output)
	}
	if !strings.Contains(output, "<redacted>") {
		t.Fatalf("close log was not redacted: %s", output)
	}
	if !strings.Contains(output, "close agent connection") {
		t.Fatalf("close log entry missing: %s", output)
	}
}

// TestSyncServerDiagnosesMissingDeploymentSecret proves the sync never opens a
// stored credential under the empty public key: without a deployment secret
// the affected route stays HTTP-only with an actionable diagnostic, no
// credential enters the container environment, and the fingerprint is the
// fixed unconfigured marker.
func TestSyncServerDiagnosesMissingDeploymentSecret(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, false)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}
	// fixture.service.secret stays empty.

	err := fixture.service.SyncServer(context.Background(), serverID)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a missing-secret diagnostic", err)
	}
	if len(partial.Diagnostics) != 1 || !strings.Contains(partial.Diagnostics[0].Reason, "deployment secret") {
		t.Fatalf("diagnostics = %#v", partial.Diagnostics)
	}
	if len(fixture.containers.runs) != 1 {
		t.Fatalf("runs = %d, want the bootstrap create", len(fixture.containers.runs))
	}
	run := fixture.containers.runs[0]
	if len(run.Env) != 0 {
		t.Fatalf("env = %v, want none without a deployment secret", run.Env)
	}
	if run.Labels[TraefikEnvHashLabel] != "unconfigured" {
		t.Fatalf("fingerprint label = %q, want the fixed unconfigured marker", run.Labels[TraefikEnvHashLabel])
	}
	dynamic := filesByPath(fixture.agent.calls[len(fixture.agent.calls)-1].GetFiles())[DynamicFileName(FormatYAML)]
	if strings.Contains(dynamic, "websecure") {
		t.Fatalf("a route without usable credentials must stay HTTP-only:\n%s", dynamic)
	}
}

// TestRedactEnvValues scrubs only the credential values and preserves the
// original error chain.
func TestRedactEnvValues(t *testing.T) {
	env := []string{"CF_DNS_API_TOKEN=alpha-token", "DO_AUTH_TOKEN=beta-token"}
	err := redactEnvValues(errors.New("run failed: alpha-token and beta-token rejected"), env)
	if strings.Contains(err.Error(), "alpha-token") || strings.Contains(err.Error(), "beta-token") {
		t.Fatalf("credential values survived redaction: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("error was not marked as redacted: %v", err)
	}
	unrelated := errors.New("unrelated failure")
	if redactEnvValues(unrelated, env) != unrelated {
		t.Fatal("an error without credential values must pass through unchanged")
	}
	if redactEnvValues(nil, env) != nil {
		t.Fatal("nil must stay nil")
	}

	// errors.Is/errors.As must keep working through the sanitized wrapper.
	sentinel := errors.New("backend sentinel")
	wrapped := fmt.Errorf("%w: rejected alpha-token", sentinel)
	redacted := redactEnvValues(wrapped, env)
	if !errors.Is(redacted, sentinel) {
		t.Fatalf("redaction dropped the error chain: %v", redacted)
	}
	if strings.Contains(redacted.Error(), "alpha-token") || !strings.Contains(redacted.Error(), "<redacted>") {
		t.Fatalf("redacted error = %q", redacted.Error())
	}
}

// TestBootstrapRedactsEnvValuesFromBackendErrors is the injected-error check:
// a container backend error that echoes the request environment must not
// surface the DNS token through the sync error (and therefore not through the
// API or the logs).
func TestBootstrapRedactsEnvValuesFromBackendErrors(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, false)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{appContainer("app-container", 32768)}
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}
	fixture.containers.runErr = fmt.Errorf("docker: create %s: invalid environment %q",
		TraefikContainerName, "CF_DNS_API_TOKEN=cf-secret-token")

	err := fixture.service.SyncServer(context.Background(), serverID)
	if err == nil {
		t.Fatal("want the bootstrap failure")
	}
	if strings.Contains(err.Error(), "cf-secret-token") {
		t.Fatalf("credential leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("error was not redacted: %v", err)
	}
	if !strings.Contains(err.Error(), TraefikContainerName) {
		t.Fatalf("error lost its diagnostic context: %v", err)
	}
}

// TestSyncServerDiagnosesCredentialFailure proves an unopenable provider
// credential keeps the affected route HTTP-only with a diagnostic instead of
// failing the whole node (or delivering an empty environment under an active
// domain).
func TestSyncServerDiagnosesCredentialFailure(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "another-key")
	app := dnsCertificateApp(serverID, provider.ID, false)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

	err := fixture.service.SyncServer(context.Background(), serverID)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want ErrPartialSync with a diagnostic", err)
	}
	if len(partial.Diagnostics) != 1 || !strings.Contains(partial.Diagnostics[0].Reason, "credentials could not be opened") {
		t.Fatalf("diagnostics = %#v", partial.Diagnostics)
	}
	dynamic := filesByPath(fixture.agent.calls[len(fixture.agent.calls)-1].GetFiles())[DynamicFileName(FormatYAML)]
	if strings.Contains(dynamic, "websecure") {
		t.Fatalf("a route with an unusable credential must stay HTTP-only:\n%s", dynamic)
	}
}

// TestSyncServerDiagnosesStaleCertificateDomain proves the recorded domain
// guards against issuing for a stale host: the route stays plain HTTP and the
// divergence is reported.
func TestSyncServerDiagnosesStaleCertificateDomain(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	app := dnsCertificateApp(serverID, provider.ID, false)
	app.Certificate.Domain = "old.example.com"

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

	err := fixture.service.SyncServer(context.Background(), serverID)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a stale-host diagnostic", err)
	}
	if len(partial.Diagnostics) != 1 || !strings.Contains(partial.Diagnostics[0].Reason, "recorded for") {
		t.Fatalf("diagnostics = %#v", partial.Diagnostics)
	}
	dynamic := filesByPath(fixture.agent.calls[len(fixture.agent.calls)-1].GetFiles())[DynamicFileName(FormatYAML)]
	if strings.Contains(dynamic, "websecure") {
		t.Fatalf("a route with a stale certificate host must stay HTTP-only:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "app.example.com") {
		t.Fatalf("the route must stay routable over HTTP:\n%s", dynamic)
	}
}

// TestSyncServerDiagnosesDisabledProvider proves a disabled provider
// deactivates its DNS-01 routes with a diagnostic and an empty environment.
func TestSyncServerDiagnosesDisabledProvider(t *testing.T) {
	serverID := uuid.New()
	provider := sealedProvider(t, "test-secret")
	provider.Enabled = false
	app := dnsCertificateApp(serverID, provider.ID, false)

	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = []containers.Container{runningTraefik(), appContainer("app-container", 32768)}
	fixture.service.secret = "test-secret"
	fixture.service.providers = &fakeProviderSource{providers: []DNSProvider{provider}}

	err := fixture.service.SyncServer(context.Background(), serverID)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want a disabled-provider diagnostic", err)
	}
	if len(partial.Diagnostics) != 1 || !strings.Contains(partial.Diagnostics[0].Reason, "provider is disabled") {
		t.Fatalf("diagnostics = %#v", partial.Diagnostics)
	}
	dynamic := filesByPath(fixture.agent.calls[len(fixture.agent.calls)-1].GetFiles())[DynamicFileName(FormatYAML)]
	if strings.Contains(dynamic, "websecure") {
		t.Fatalf("a route with a disabled provider must stay HTTP-only:\n%s", dynamic)
	}
}
