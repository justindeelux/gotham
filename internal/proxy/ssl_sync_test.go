package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/providers"
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

// TestRedactEnvValues scrubs only the credential values.
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
