package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Phase 6 (BE-6.3) production acceptance. The test drives the real control
// plane surface (store → proxy services → mTLS agent → Docker) against the
// local Docker daemon and the dev Postgres database and proves:
//
//   - a per-application redirect rule answers the source host through the
//     bootstrapped Traefik with the configured code, target and preserved
//     path, and disappears when the rule is disabled,
//   - a restarted proxy keeps serving the redirect without a control-plane
//     action,
//   - certificate status/expiry are read from the node's real acme.json
//     through the agent RPC: absent without storage, present with the stored
//     notAfter, unknown for an unreadable storage or an unreachable node.
//
// No ACME issuance happens: the storage fixture is written directly, so the
// test never consumes production (or staging) certificate quota.
func TestP6RedirectsAndCertificateStatus(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := testLogger(t)

	engine, err := agent.NewDockerClient(e2eDockerSock())
	if err != nil {
		t.Fatalf("docker client for %s: %v", e2eDockerSock(), err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	version, err := engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Fatalf("docker daemon unreachable at %s: %v", e2eDockerSock(), err)
	}
	t.Logf("docker %s at %s", version, e2eDockerSock())

	dsn := e2eDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("Postgres/migrations unavailable at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)", dsn, err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Postgres unavailable at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := uuid.New().String()[:8]
	userRow, err := st.CreateUser(ctx, "p6-redirects-"+suffix+"@example.com", nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userRow.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p6-redirects-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})
	// A second, agent-less node: its certificate status must report unknown.
	ghostRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p6-redirects-ghost-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create ghost server: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", ghostRow.ID); err != nil {
			t.Logf("cleanup ghost server: %v", err)
		}
	})

	nodeID := "p6-redirects-" + suffix
	configDir := p6CanonicalTempDir(t)
	acmeDir := configDir + "/acme"
	if err := os.MkdirAll(acmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	debugLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	proxyAgent := agent.NewProxyServer(agent.ProxyServerConfig{Root: configDir, Logger: debugLogger})
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithProxyService(proxyAgent))

	if existing := p6FindContainer(t, ctx, engine); existing != "" {
		t.Fatalf("a gotham-traefik container already exists (%s); refusing to remove a possibly live proxy", existing)
	}
	createdIDs := []string{}
	trackCreated := func(id string) string {
		if id != "" {
			createdIDs = append(createdIDs, id)
		}
		return id
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		for _, id := range createdIDs {
			if err := engine.Remove(cleanupCtx, id); err != nil {
				t.Logf("cleanup container %s: %v", id, err)
			}
		}
	})

	registry := p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1", NodeID: &nodeID}}
	containerService := containers.NewService(containers.Config{
		Registry: registry,
		Cache:    containers.NopCache{},
		Dial: func(dialCtx context.Context, _ *servers.Server) (containers.DockerClient, error) {
			return servers.DialDockerClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger: logger,
	})
	t.Cleanup(func() { _ = containerService.Close() })

	backendHost := proxy.DefaultBackendHost
	if runtime.GOOS == "darwin" {
		backendHost = "host.docker.internal"
	}
	dialProxy := func(dialCtx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
		if id != serverID {
			return nil, servers.ErrNotFound
		}
		return servers.DialProxyClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
	}
	proxyService := proxy.NewService(proxy.Config{
		Store:       st,
		Containers:  containerService,
		BackendHost: backendHost,
		Dial:        dialProxy,
		Logger:      logger,
		ConfigDir:   configDir,
		AcmeDir:     acmeDir,
	})

	pullCtx, pullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if err := engine.PullImage(pullCtx, p6NginxImage); err != nil {
		pullCancel()
		t.Fatalf("pull %s: %v", p6NginxImage, err)
	}
	pullCancel()

	targetDomain := "target-" + suffix + ".example.test"
	sourceDomain := "legacy-" + suffix + ".example.test"
	targetContainer := p6RunNginx(t, ctx, engine, "p6-redirect-target-"+suffix, fmt.Sprintf("%d:80", freeTCPPort(t)))
	targetApp := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "redirect-target-"+suffix, targetDomain, 0, false)
	p6CreateRunningDeployment(t, ctx, pool, targetApp, targetContainer)

	// The real services over the real store: a redirect rule and a
	// certificate intent for the target application.
	redirectService := proxy.NewDefaultRedirectService(proxy.RedirectConfig{Store: proxy.NewStoreRedirect(st), Logger: logger})
	certificateService := proxy.NewDefaultCertificateService(proxy.SSLConfig{Store: proxy.NewStoreSSL(st), Logger: logger})
	if redirectService == nil || certificateService == nil {
		t.Fatal("proxy services are not configured")
	}
	redirect, err := redirectService.CreateRedirect(ctx, proxy.CreateRedirectInput{
		ApplicationID: targetApp,
		SourceDomain:  strings.ToUpper(sourceDomain),
		TargetDomain:  targetDomain,
		Code:          proxy.RedirectCodePermanent,
	})
	if err != nil {
		t.Fatalf("create redirect: %v", err)
	}
	if redirect.SourceDomain != sourceDomain {
		t.Fatalf("source domain = %q, want normalized %q", redirect.SourceDomain, sourceDomain)
	}

	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	traefikID := trackCreated(p6WaitForContainer(t, ctx, engine))
	p6AssertTraefikBootstrap(t, traefikID)
	// The target route itself still serves plain HTTP here: no certificate
	// intent exists yet, so the shared HTTP→HTTPS redirect is not attached.
	p6ExpectHTTP(t, targetDomain, http.StatusOK)

	// The redirect answers on the web entrypoint with the configured code,
	// target and preserved path+query.
	location := p6ExpectHostStatus(t, http.MethodGet, sourceDomain, http.StatusMovedPermanently)
	if want := "https://" + targetDomain + p6RedirectProbePath; location != want {
		t.Fatalf("redirect location = %q, want %q", location, want)
	}

	// Host variants the Host(source) router accepts while the raw URL keeps
	// them: a port (numeric or otherwise), an empty port, a fully-qualified
	// trailing dot and any case must still terminate in the redirect, never
	// fall through to the backend-less noop service.
	for _, variant := range []string{
		sourceDomain + ":80",
		sourceDomain + ":",
		sourceDomain + ".",
	} {
		location := p6ExpectHostStatus(t, http.MethodGet, variant, http.StatusMovedPermanently)
		if want := "https://" + targetDomain + p6RedirectProbePath; location != want {
			t.Fatalf("redirect location for Host %q = %q, want %q", variant, location, want)
		}
	}
	location = p6ExpectHostStatus(t, http.MethodGet, strings.ToUpper(sourceDomain)+":80", http.StatusMovedPermanently)
	if want := "https://" + targetDomain + p6RedirectProbePath; location != want {
		t.Fatalf("redirect location for an uppercase ported Host = %q, want %q", location, want)
	}

	// Traefik special-cases only GET: the permanent intent answers 301 to GET
	// while HEAD and every other method answer 308.
	p6ExpectHostStatus(t, http.MethodHead, sourceDomain, http.StatusPermanentRedirect)
	p6ExpectHostStatus(t, http.MethodPost, sourceDomain, http.StatusPermanentRedirect)

	// Pausing the rule removes the router; re-enabling restores it.
	disabled := false
	if _, err := redirectService.UpdateRedirect(ctx, redirect.ID, proxy.UpdateRedirectInput{Enabled: &disabled}); err != nil {
		t.Fatalf("disable redirect: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after disable: %v", err)
	}
	p6ExpectHTTP(t, sourceDomain, http.StatusNotFound)
	enabled := true
	if _, err := redirectService.UpdateRedirect(ctx, redirect.ID, proxy.UpdateRedirectInput{Enabled: &enabled}); err != nil {
		t.Fatalf("re-enable redirect: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after re-enable: %v", err)
	}
	p6ExpectHostStatus(t, http.MethodGet, sourceDomain, http.StatusMovedPermanently)

	// The temporary intent answers 302 to GET and 307 to HEAD/other methods.
	code := proxy.RedirectCodeTemporary
	if _, err := redirectService.UpdateRedirect(ctx, redirect.ID, proxy.UpdateRedirectInput{Code: &code}); err != nil {
		t.Fatalf("switch to temporary: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after temporary switch: %v", err)
	}
	p6ExpectHostStatus(t, http.MethodGet, sourceDomain, http.StatusFound)
	p6ExpectHostStatus(t, http.MethodHead, sourceDomain, http.StatusTemporaryRedirect)
	p6ExpectHostStatus(t, http.MethodPost, sourceDomain, http.StatusTemporaryRedirect)
	code = proxy.RedirectCodePermanent
	if _, err := redirectService.UpdateRedirect(ctx, redirect.ID, proxy.UpdateRedirectInput{Code: &code}); err != nil {
		t.Fatalf("restore permanent: %v", err)
	}
	if err := p6Sync(proxyService, serverID); err != nil {
		t.Fatalf("sync after permanent restore: %v", err)
	}

	// A restarted proxy keeps serving the redirect without any CP action.
	if err := engine.Restart(ctx, traefikID); err != nil {
		t.Fatalf("restart traefik: %v", err)
	}
	p6ExpectHostStatus(t, http.MethodGet, sourceDomain, http.StatusMovedPermanently)

	// Now record the certificate intent the status read reports on. No sync
	// is needed: the status is computed from the node storage on read.
	certificate, err := certificateService.CreateCertificate(ctx, proxy.CreateCertificateInput{
		ApplicationID: targetApp,
		Challenge:     proxy.ChallengeHTTP01,
	})
	if err != nil {
		t.Fatalf("create certificate intent: %v", err)
	}

	// Certificate status: the agent prepares the node's ACME storage file
	// before Traefik starts (mode 0600, owned by the agent's user), so Traefik
	// keeps it readable instead of creating a root-only file. The file exists
	// and is empty, and the node holds no certificate yet: the intent is
	// absent (never fabricated).
	storagePath := filepath.Join(acmeDir, "acme.json")
	if info, err := os.Stat(storagePath); err != nil || info.Size() != 0 {
		t.Fatalf("agent-prepared acme storage = %v (%v), want an existing empty file", info, err)
	}
	statusService := proxy.NewDefaultCertificateStatusService(proxy.CertificateStatusConfig{
		Store: proxy.NewStoreCertificateStatus(st),
		Dial: func(dialCtx context.Context, id uuid.UUID) (proxy.ACMEReader, error) {
			if id != serverID {
				return nil, errors.New("no agent for this node")
			}
			return servers.DialProxyClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger: debugLogger,
	})
	if statusService == nil {
		t.Fatal("certificate status service is not configured")
	}
	statuses := statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate})
	if got := statuses[certificate.ID].Status; got != proxy.CertificateStatusAbsent {
		t.Fatalf("status with empty storage = %q, want absent", got)
	}

	// A storage file the agent cannot read (the mode a root-run Traefik would
	// leave behind on a node without the agent's placeholder) is a genuine
	// read failure: unknown, never a fabricated absent. Only an unprivileged
	// process is subject to the permission check.
	if os.Geteuid() != 0 {
		if err := os.Chmod(storagePath, 0); err != nil {
			t.Fatalf("chmod acme storage: %v", err)
		}
		statuses = statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate})
		if got := statuses[certificate.ID].Status; got != proxy.CertificateStatusUnknown {
			t.Fatalf("status with unreadable storage = %q, want unknown", got)
		}
		if err := os.Chmod(storagePath, 0o600); err != nil {
			t.Fatalf("restore mode: %v", err)
		}
	}

	// A missing storage file is absent too (removal needs the directory, not
	// the file, so an unreadable fixture cannot make this flaky).
	p6RemoveACMEStorage(t, acmeDir)
	statuses = statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate})
	if got := statuses[certificate.ID].Status; got != proxy.CertificateStatusAbsent {
		t.Fatalf("status with missing storage = %q, want absent", got)
	}

	// A real acme.json fixture for the recorded domain reports present with
	// the stored expiry over the real agent RPC (no ACME quota burned).
	expiry := time.Now().Add(45 * 24 * time.Hour).UTC().Truncate(time.Second)
	fixture := p6ACMEStorage(t, certificate.Domain, expiry)
	p6WriteACMEStorage(t, acmeDir, fixture)
	statuses = statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate})
	observed := statuses[certificate.ID]
	if observed.Status != proxy.CertificateStatusPresent {
		t.Fatalf("status with storage = %q, want present", observed.Status)
	}
	if !observed.NotAfter.Equal(expiry) {
		t.Fatalf("notAfter = %s, want %s", observed.NotAfter, expiry)
	}

	// A malformed storage file is unknown, and the intent never turns into a
	// fabricated present/absent.
	p6WriteACMEStorage(t, acmeDir, []byte(`{"letsencrypt": {"Certificates": [{"domain": {"main": "x"}, "certificate": "bad"}]}}`))
	statuses = statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate})
	if got := statuses[certificate.ID].Status; got != proxy.CertificateStatusUnknown {
		t.Fatalf("status with malformed storage = %q, want unknown", got)
	}

	// A certificate on an unreachable node is unknown, while the reachable
	// node's intent keeps its real observation in the same call.
	p6WriteACMEStorage(t, acmeDir, fixture)
	ghostDomain := "ghost-" + suffix + ".example.test"
	ghostApp := p6CreateApplication(t, ctx, pool, userRow.ID, ghostRow.ID, "redirect-ghost-"+suffix, ghostDomain, 0, false)
	ghostCertificate, err := certificateService.CreateCertificate(ctx, proxy.CreateCertificateInput{
		ApplicationID: ghostApp,
		Challenge:     proxy.ChallengeHTTP01,
	})
	if err != nil {
		t.Fatalf("create ghost certificate intent: %v", err)
	}
	statuses = statusService.CertificateStatuses(ctx, []proxy.DomainCertificate{certificate, ghostCertificate})
	if got := statuses[ghostCertificate.ID].Status; got != proxy.CertificateStatusUnknown {
		t.Fatalf("unreachable node status = %q, want unknown", got)
	}
	if got := statuses[certificate.ID].Status; got != proxy.CertificateStatusPresent {
		t.Fatalf("reachable node status = %q, want present", got)
	}
	if statuses[ghostCertificate.ID].NotAfter != (time.Time{}) {
		t.Fatalf("unknown status fabricated an expiry: %s", statuses[ghostCertificate.ID].NotAfter)
	}

	// The agent's RPC response itself carries only metadata: the fixture's
	// private key material never crosses the node boundary.
	client, err := servers.DialProxyClient(ctx, agentAddr, authority, servers.WithDockerServerName(nodeID))
	if err != nil {
		t.Fatalf("dial proxy service: %v", err)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
	response, err := client.ReadACMEStorage(readCtx, &agentv1.ReadACMEStorageRequest{})
	readCancel()
	_ = client.Close()
	if err != nil {
		t.Fatalf("ReadACMEStorage: %v", err)
	}
	rendered, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, needle := range p6KeyMarkers(t, fixture) {
		if strings.Contains(string(rendered), needle) {
			t.Fatalf("key material %q leaked through the ACME read RPC", needle)
		}
	}
}

// p6RedirectProbePath is the path+query every redirect assertion requests, so
// the preserved-path expectation is one constant.
const p6RedirectProbePath = "/some/path?q=1"

// p6ExpectHostStatus polls a route through Traefik with the given method and
// Host header until it answers the wanted status and returns the Location
// header (empty for non-redirect statuses).
func p6ExpectHostStatus(t *testing.T, method, host string, wantStatus int) string {
	t.Helper()
	deadline := time.Now().Add(p6HTTPWait)
	var lastStatus int
	var lastLocation string
	for {
		status, location := p6HostRequest(t, method, host, p6RedirectProbePath)
		lastStatus, lastLocation = status, location
		if status == wantStatus || time.Now().After(deadline) {
			break
		}
		time.Sleep(p6PollInterval)
	}
	if lastStatus != wantStatus {
		t.Fatalf("%s (Host %s) = %d, want %d (location %q)", method, host, lastStatus, wantStatus, lastLocation)
	}
	return lastLocation
}

// p6HostRequest performs one request with the given method and Host header,
// without following redirects, and returns the status and Location header.
func p6HostRequest(t *testing.T, method, host, path string) (int, string) {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequest(method, p6BaseURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Host = host
	request.URL.Path = strings.SplitN(path, "?", 2)[0]
	if query := strings.SplitN(path, "?", 2); len(query) == 2 {
		request.URL.RawQuery = query[1]
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, ""
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
	_ = response.Body.Close()
	return response.StatusCode, response.Header.Get("Location")
}

// p6RemoveACMEStorage removes the node's ACME storage file if present.
// Removal needs write permission on the directory, not on the file, so a
// foreign-owned or unreadable file cannot make the test flaky; the directory
// is never touched.
func p6RemoveACMEStorage(t *testing.T, acmeDir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(acmeDir, "acme.json")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove acme storage: %v", err)
	}
}

// p6WriteACMEStorage replaces the node's ACME storage file with content.
func p6WriteACMEStorage(t *testing.T, acmeDir string, content []byte) {
	t.Helper()
	p6RemoveACMEStorage(t, acmeDir)
	if err := os.WriteFile(filepath.Join(acmeDir, "acme.json"), content, 0o600); err != nil {
		t.Fatalf("write acme storage: %v", err)
	}
}

// p6ACMEStorage renders a Traefik-shaped acme.json body holding one
// self-signed certificate for domain, plus account and certificate keys whose
// payloads p6KeyMarkers can later search for.
func p6ACMEStorage(t *testing.T, domain string, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	body := map[string]any{
		"letsencrypt": map[string]any{
			"Account": map[string]any{
				"Email":      "p6-fixture@example.test",
				"PrivateKey": string(keyPEM),
			},
			"Certificates": []map[string]any{{
				"domain":      map[string]any{"main": domain},
				"certificate": base64.StdEncoding.EncodeToString(leafPEM),
				"key":         string(keyPEM),
			}},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal acme fixture: %v", err)
	}
	return encoded
}

// p6KeyMarkers extracts the base64 body segment of every private key in the
// fixture so a test can prove none of it crosses the agent boundary.
func p6KeyMarkers(t *testing.T, fixture []byte) []string {
	t.Helper()
	var decoded map[string]struct {
		Account struct {
			PrivateKey string `json:"PrivateKey"`
		} `json:"Account"`
		Certificates []struct {
			Key string `json:"key"`
		} `json:"Certificates"`
	}
	if err := json.Unmarshal(fixture, &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	markers := []string{}
	for _, data := range decoded {
		for _, text := range []string{data.Account.PrivateKey} {
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "-----") {
					continue
				}
				if len(line) > 40 {
					line = line[:40]
				}
				markers = append(markers, line)
				break
			}
		}
		for _, certificate := range data.Certificates {
			for _, line := range strings.Split(certificate.Key, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "-----") {
					continue
				}
				if len(line) > 40 {
					line = line[:40]
				}
				markers = append(markers, line)
				break
			}
		}
	}
	if len(markers) == 0 {
		t.Fatal("fixture carries no key material to prove against")
	}
	return markers
}
