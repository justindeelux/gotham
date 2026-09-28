package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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
)

// Phase 6 (BE-6.2) issuance acceptance. The test drives the real service
// surface (store -> SSL services -> proxy.SyncService -> containers.Service ->
// mTLS agent -> Docker) against a real Cloudflare zone and a real Let's
// Encrypt ACME server, and asserts the certificate Traefik serves:
//
//   - a token preflight creates and deletes a TXT record through the
//     Cloudflare API (stopping on 403 instead of retrying);
//   - the DNS provider is created through proxy.DNSProviderService and the
//     DNS-01 certificate intent through proxy.CertificateService;
//   - the bootstrapped Traefik obtains the certificate and serves it for
//     SNI=gotham.deelux.dev from the loopback gateway;
//   - the served chain is issued by the configured CA: Let's Encrypt staging
//     by default, production only with an explicit GOTHAM_E2E_ACME_CA.
//
// Secrets: the CF token reaches this process through the environment, is
// sealed through the normal provider surface and is delivered to Traefik as
// a container environment variable only. It is never logged, rendered into a
// document, or written to the evidence artifact.

// Environment knobs of the issuance acceptance test. GOTHAM_E2E_ACME_CA
// defaults to staging so that running the suite can never burn production
// rate limits by accident.
const (
	p6IssuanceTokenEnv  = "CF_DNS_API_TOKEN"
	p6IssuanceDomainEnv = "GOTHAM_TEST_DOMAIN"
	p6IssuanceZoneEnv   = "GOTHAM_TEST_ZONE"
	p6IssuanceCAEnv     = "GOTHAM_E2E_ACME_CA"
	p6IssuanceOutEnv    = "GOTHAM_E2E_EVIDENCE_DIR"

	// p6LEStagingDirectory is the Let's Encrypt staging ACME directory the
	// caServer knob renders for staging runs; the production run leaves the
	// knob empty (Traefik's default).
	p6LEStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

	p6IssuanceTimeout     = 6 * time.Minute
	p6IssuancePoll        = 2 * time.Second
	p6IssuanceCleanupWait = 60 * time.Second
)

// TestP6DNS01Issuance is gated by GOTHAM_E2E=1 like the rest of the suite.
// With the gate on, the credential environment is required: a missing token,
// domain or zone fails the test instead of skipping it green.
func TestP6DNS01Issuance(t *testing.T) {
	requireE2E(t)

	token, domain, zone := p6IssuanceEnv(t)
	caName, caServer := p6IssuanceCA(t)
	t.Logf("issuance acceptance: ca=%s domain=%s zone=%s", caName, domain, zone)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := testLogger(t)

	// Cloudflare preflight first: prove the token can create and delete a TXT
	// record in the zone before any ACME order exists. A 403 (or any other
	// failure) stops the run here; there are no blind retries.
	cf := newCFAPI(token)
	preflightCtx, preflightCancel := context.WithTimeout(ctx, time.Minute)
	zoneID, err := p6CFPreflight(preflightCtx, cf, zone)
	preflightCancel()
	if err != nil {
		t.Fatalf("cloudflare token preflight: %v", err)
	}
	t.Logf("cloudflare preflight ok: token created and deleted a TXT record in zone %s", zone)

	// Docker, Postgres and the one local node: the same stack the BE-6.1
	// acceptance test builds.
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
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}

	dsn := e2eDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("Postgres/migrations unavailable at %s: %v", dsn, err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Postgres unavailable at %s: %v", dsn, err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := uuid.New().String()[:8]
	userRow, err := st.CreateUser(ctx, "p6-issuance-"+suffix+"@example.com", nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p6-issuance-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)

	// The node agent rooted in a temp directory the CP mounts into Traefik.
	nodeID := "p6-issuance-" + suffix
	configDir := p6CanonicalTempDir(t)
	acmeDir := configDir + "/acme"
	if err := os.MkdirAll(acmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	proxyAgent := agent.NewProxyServer(agent.ProxyServerConfig{Root: configDir, Logger: logger})
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithProxyService(proxyAgent))

	registry := p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1", NodeID: &nodeID}}
	// R4: never remove a fixed-name container this test cannot prove it owns.
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		for _, id := range createdIDs {
			if err := engine.Remove(cleanupCtx, id); err != nil {
				t.Logf("cleanup container %s: %v", id, err)
			}
		}
	})
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

	// A trivial backend for the domain and the application/deployment rows
	// the routing state is built from.
	pullCtx, pullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if err := engine.PullImage(pullCtx, p6NginxImage); err != nil {
		pullCancel()
		t.Fatalf("pull %s: %v", p6NginxImage, err)
	}
	pullCancel()
	hostPort := freeTCPPort(t)
	backend := p6RunNginx(t, ctx, engine, "p6-issuance-app-"+suffix, fmt.Sprintf("%d:80", hostPort))
	appID := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, "issuance-"+suffix, domain, hostPort, false)
	p6CreateRunningDeployment(t, ctx, pool, appID, backend)

	var providerID uuid.UUID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, statement := range []string{
			"DELETE FROM domain_certificates WHERE application_id = $1",
			"DELETE FROM deployments WHERE application_id = $1",
			"DELETE FROM applications WHERE id = $1",
		} {
			if _, err := pool.Exec(cleanupCtx, statement, appID); err != nil {
				t.Logf("cleanup %q: %v", statement, err)
			}
		}
		if providerID != uuid.Nil {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM dns_providers WHERE id = $1", providerID); err != nil {
				t.Logf("cleanup provider %s: %v", providerID, err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userRow.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	// A fresh deployment secret keys credential sealing for this run.
	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		t.Fatalf("random secret: %v", err)
	}
	secret := hex.EncodeToString(rawSecret)
	t.Setenv("GOTHAM_SECRET_KEY", secret)

	// The DNS provider and the certificate intent go through the real service
	// surface, exactly as the HTTP API wires them.
	sslStore := proxy.NewStoreSSL(st)
	providerService := proxy.NewDefaultProviderService(proxy.SSLConfig{Store: sslStore, Secret: secret, Logger: logger})
	if providerService == nil {
		t.Fatal("provider service is disabled (FEATURE_PROXY=false)")
	}
	certificateService := proxy.NewDefaultCertificateService(proxy.SSLConfig{Store: sslStore, Secret: secret, Logger: logger})
	if certificateService == nil {
		t.Fatal("certificate service is disabled (FEATURE_PROXY=false)")
	}
	provider, err := providerService.CreateProvider(ctx, proxy.CreateDNSProviderInput{
		Provider:   proxy.ProviderCloudflare,
		Name:       "be-6.2 acceptance " + suffix,
		Zones:      []string{zone},
		Credential: token,
	})
	if err != nil {
		t.Fatalf("create cloudflare provider: %v", err)
	}
	providerID = provider.ID
	certificate, err := certificateService.CreateCertificate(ctx, proxy.CreateCertificateInput{
		ApplicationID: appID,
		Challenge:     proxy.ChallengeDNS01,
		DNSProviderID: provider.ID,
	})
	if err != nil {
		t.Fatalf("create dns-01 certificate intent: %v", err)
	}
	t.Logf("created dns-01 certificate intent %s for %s (resolver %s)", certificate.ID, certificate.Domain, proxy.DNSResolverName(proxy.ProviderCloudflare))

	// The sync bootstraps Traefik with the DNS-01 resolver and the credential
	// environment, and pushes the HTTPS route.
	proxyService := proxy.NewService(proxy.Config{
		Store:       st,
		Containers:  containerService,
		BackendHost: backendHost,
		Dial: func(dialCtx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
			if id != serverID {
				return nil, servers.ErrNotFound
			}
			return servers.DialProxyClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
		Logger:    logger,
		ConfigDir: configDir,
		AcmeDir:   acmeDir,
		Secret:    secret,
		CAServer:  caServer,
	})
	syncCtx, syncCancel := context.WithTimeout(ctx, p6SyncTimeout)
	err = proxyService.SyncServer(syncCtx, serverID)
	syncCancel()
	if err != nil {
		t.Fatalf("sync with the dns-01 certificate: %v", err)
	}
	traefikID := trackCreated(p6WaitForContainer(t, ctx, engine))
	p6AssertTraefikBootstrap(t, traefikID)
	t.Logf("bootstrapped gotham-traefik %s", shortID(traefikID))

	// The generated static document carries the knob: the staging directory
	// for staging runs, no caServer key at all for production.
	static, err := os.ReadFile(filepath.Join(configDir, proxy.StaticFileName(proxy.FormatYAML)))
	if err != nil {
		t.Fatalf("read generated static config: %v", err)
	}
	if caServer == "" {
		if strings.Contains(string(static), "caServer") {
			t.Fatal("production run rendered a caServer override; the production default must stay implicit")
		}
	} else if !strings.Contains(string(static), caServer) {
		t.Fatalf("generated static config does not carry the caServer override %q", caServer)
	}
	t.Logf("generated resolver carries caServer=%t", caServer != "")

	// Snapshot the challenge records that already exist so cleanup touches
	// only records this run created.
	challengeName := "_acme-challenge." + domain
	foreign := map[string]bool{}
	if existing, err := cf.listTXT(ctx, zoneID, challengeName); err != nil {
		t.Fatalf("list existing challenge TXT records: %v", err)
	} else {
		for _, record := range existing {
			foreign[record.ID] = true
		}
		if len(foreign) > 0 {
			t.Logf("zone already had %d TXT record(s) at %s; they are left untouched", len(foreign), challengeName)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		records, err := cf.listTXT(cleanupCtx, zoneID, challengeName)
		if err != nil {
			t.Logf("challenge TXT cleanup list: %v", err)
			return
		}
		for _, record := range records {
			if foreign[record.ID] {
				continue
			}
			if err := cf.deleteRecord(cleanupCtx, zoneID, record.ID); err != nil {
				t.Logf("delete challenge TXT %s: %v", record.ID, err)
			}
		}
	})

	// Wait for the real issuance: poll the Cloudflare API for the challenge
	// record and the loopback gateway for the served chain. Every handshake
	// with SNI=domain also triggers Traefik's on-demand ACME order.
	deadline := time.Now().Add(p6IssuanceTimeout)
	var (
		servedChain       []*x509.Certificate
		observedTXT       bool
		lastHandshakeErr  error
		lastIssuer        string
		txtObservedDetail string
	)
	for time.Now().Before(deadline) {
		if !observedTXT {
			if records, err := cf.listTXT(ctx, zoneID, challengeName); err == nil {
				for _, record := range records {
					if !foreign[record.ID] {
						observedTXT = true
						txtObservedDetail = record.Name + " -> " + record.Content
						t.Logf("dns-01 challenge TXT observed through the Cloudflare API: %s", txtObservedDetail)
						break
					}
				}
			}
		}
		chain, err := p6ServedTLSCross(domain)
		if err != nil {
			lastHandshakeErr = err
		} else if len(chain) > 0 {
			lastIssuer = chain[0].Issuer.CommonName
			if p6CertMatchesCA(caName, chain) {
				servedChain = chain
				break
			}
		}
		time.Sleep(p6IssuancePoll)
	}
	if servedChain == nil {
		if logs, err := runDocker(context.Background(), "logs", "--tail", "200", traefikID); err == nil {
			t.Logf("traefik logs while waiting for issuance:\n%s", logs)
		}
		t.Fatalf("no %s certificate served for %s within %s (last handshake error: %v; last issuer: %q)",
			caName, domain, p6IssuanceTimeout, lastHandshakeErr, lastIssuer)
	}
	if !observedTXT {
		t.Fatalf("certificate issued but the DNS-01 challenge TXT record was never observed through the Cloudflare API")
	}
	leaf := servedChain[0]
	if err := leaf.VerifyHostname(domain); err != nil {
		t.Fatalf("served certificate does not cover %s: %v", domain, err)
	}
	t.Logf("served certificate: subject=%q issuer=%q dns=%v not_after=%s",
		leaf.Subject.CommonName, leaf.Issuer.CommonName, leaf.DNSNames, leaf.NotAfter.Format(time.RFC3339))

	// The certificate must also be present in Traefik's ACME storage on the
	// node (the mount survives config rewrites).
	storagePresent := p6ACMEStorageHasCert(t, filepath.Join(acmeDir, "acme.json"), domain)
	t.Logf("certificate present in Traefik ACME storage: %t", storagePresent)
	if !storagePresent {
		t.Fatal("served certificate is absent from the Traefik ACME storage")
	}

	// lego removes the challenge record after validation; wait briefly so the
	// created/cleaned pair is observed through the API in the same run.
	txtCleaned := p6WaitTXTGone(t, ctx, cf, zoneID, challengeName, foreign)
	t.Logf("dns-01 challenge TXT cleaned through the Cloudflare API: %t", txtCleaned)

	p6WriteIssuanceEvidence(t, caName, caServer, domain, zone, leaf, servedChain,
		observedTXT, txtCleaned, storagePresent, traefikID, txtObservedDetail)
}

// p6IssuanceEnv reads the credential environment. GOTHAM_E2E=1 is an explicit
// opt-in, so a missing value is a failure, never a green skip.
func p6IssuanceEnv(t *testing.T) (token, domain, zone string) {
	t.Helper()
	read := func(env string) string {
		value := strings.TrimSpace(os.Getenv(env))
		if value == "" {
			t.Fatalf("GOTHAM_E2E=1 requires %s (load the owner credential file, e.g. `set -a; . $HOME/.config/gotham/cf-test.env; set +a`)", env)
		}
		return value
	}
	return read(p6IssuanceTokenEnv), read(p6IssuanceDomainEnv), read(p6IssuanceZoneEnv)
}

// p6IssuanceCA resolves the CA selection. Production requires the explicit
// value; anything unset defaults to staging.
func p6IssuanceCA(t *testing.T) (name, caServer string) {
	t.Helper()
	switch strings.ToLower(strings.TrimSpace(os.Getenv(p6IssuanceCAEnv))) {
	case "", "staging":
		return "staging", p6LEStagingDirectory
	case "production":
		// Production is Traefik's default: the knob must stay empty.
		return "production", ""
	default:
		t.Fatalf("%s must be %q or %q", p6IssuanceCAEnv, "staging", "production")
		return "", ""
	}
}

// p6ServedTLSCross dials the loopback gateway with SNI=domain and returns the
// served chain. The chain is inspected below, so the dial itself skips
// verification.
func p6ServedTLSCross(domain string) ([]*x509.Certificate, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true, // The issuer is asserted by p6CertMatchesCA.
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if len(conn.ConnectionState().PeerCertificates) == 0 {
		return nil, errors.New("no peer certificates in the TLS handshake")
	}
	return conn.ConnectionState().PeerCertificates, nil
}

// p6CertMatchesCA reports whether the served chain belongs to the configured
// CA: Let's Encrypt staging intermediates carry a STAGING/Fake marker, while a
// production chain must be issued by a Let's Encrypt production intermediate.
func p6CertMatchesCA(caName string, chain []*x509.Certificate) bool {
	if len(chain) == 0 {
		return false
	}
	leaf := chain[0]
	if caName == "staging" {
		return strings.Contains(strings.ToUpper(leaf.Issuer.CommonName), "STAGING") ||
			strings.Contains(leaf.Issuer.CommonName, "Fake LE")
	}
	for _, cert := range chain {
		if strings.Contains(strings.ToUpper(cert.Issuer.CommonName), "STAGING") ||
			strings.Contains(cert.Issuer.CommonName, "Fake LE") {
			return false
		}
	}
	return len(leaf.Issuer.Organization) > 0 && strings.Contains(leaf.Issuer.Organization[0], "Let's Encrypt")
}

// p6ACMEStorageHasCert reports whether Traefik's acme.json holds a certificate
// for domain. Only the certificate metadata is inspected; the file also holds
// private keys and account material and is never logged or copied.
func p6ACMEStorageHasCert(t *testing.T, path, domain string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("read ACME storage %s: %v", path, err)
		return false
	}
	var resolvers map[string]json.RawMessage
	if err := json.Unmarshal(data, &resolvers); err != nil {
		t.Logf("parse ACME storage: %v", err)
		return false
	}
	for _, raw := range resolvers {
		var content struct {
			Certificates []struct {
				Domain struct {
					Main string   `json:"main"`
					SANs []string `json:"sans"`
				} `json:"domain"`
			} `json:"Certificates"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			continue
		}
		for _, cert := range content.Certificates {
			if cert.Domain.Main == domain {
				return true
			}
			for _, san := range cert.Domain.SANs {
				if san == domain {
					return true
				}
			}
		}
	}
	return false
}

// p6WaitTXTGone polls until no non-foreign TXT record remains at the challenge
// name, reporting whether the API showed the cleanup within the wait.
func p6WaitTXTGone(t *testing.T, ctx context.Context, cf *cfAPI, zoneID, name string, foreign map[string]bool) bool {
	t.Helper()
	deadline := time.Now().Add(p6IssuanceCleanupWait)
	for time.Now().Before(deadline) {
		records, err := cf.listTXT(ctx, zoneID, name)
		if err == nil {
			remaining := 0
			for _, record := range records {
				if !foreign[record.ID] {
					remaining++
				}
			}
			if remaining == 0 {
				return true
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

// p6IssuanceEvidence is the redacted acceptance artifact written when
// GOTHAM_E2E_EVIDENCE_DIR is set: certificate facts and challenge
// observations only, never credentials or private keys.
type p6IssuanceEvidence struct {
	CA                 string   `json:"ca"`
	CAServer           string   `json:"ca_server,omitempty"`
	Domain             string   `json:"domain"`
	Zone               string   `json:"zone"`
	Subject            string   `json:"subject"`
	Issuer             string   `json:"issuer"`
	DNSNames           []string `json:"dns_names"`
	SerialNumber       string   `json:"serial_number"`
	NotBefore          string   `json:"not_before"`
	NotAfter           string   `json:"not_after"`
	ChainSubjects      []string `json:"chain_subjects"`
	VerifyHostname     string   `json:"verify_hostname"`
	TXTRecordObserved  bool     `json:"txt_record_observed"`
	TXTRecordDetail    string   `json:"txt_record_detail,omitempty"`
	TXTRecordCleaned   bool     `json:"txt_record_cleaned"`
	ACMEStoragePresent bool     `json:"acme_storage_present"`
	TraefikContainer   string   `json:"traefik_container"`
	RecordedAt         string   `json:"recorded_at"`
}

// p6WriteIssuanceEvidence writes the redacted summary and the public chain PEM
// when an evidence directory is configured.
func p6WriteIssuanceEvidence(
	t *testing.T,
	caName, caServer, domain, zone string,
	leaf *x509.Certificate,
	chain []*x509.Certificate,
	observedTXT, txtCleaned, storagePresent bool,
	traefikID, txtDetail string,
) {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv(p6IssuanceOutEnv))
	if dir == "" {
		return
	}
	verify := "ok"
	if err := leaf.VerifyHostname(domain); err != nil {
		verify = err.Error()
	}
	evidence := p6IssuanceEvidence{
		CA:                 caName,
		CAServer:           caServer,
		Domain:             domain,
		Zone:               zone,
		Subject:            leaf.Subject.String(),
		Issuer:             leaf.Issuer.String(),
		DNSNames:           leaf.DNSNames,
		SerialNumber:       leaf.SerialNumber.String(),
		NotBefore:          leaf.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:           leaf.NotAfter.UTC().Format(time.RFC3339),
		VerifyHostname:     verify,
		TXTRecordObserved:  observedTXT,
		TXTRecordDetail:    txtDetail,
		TXTRecordCleaned:   txtCleaned,
		ACMEStoragePresent: storagePresent,
		TraefikContainer:   traefikID,
		RecordedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	for _, cert := range chain {
		evidence.ChainSubjects = append(evidence.ChainSubjects, cert.Subject.String())
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create evidence dir: %v", err)
	}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	jsonPath := filepath.Join(dir, "p6-issuance-"+caName+".json")
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	var chainPEM bytes.Buffer
	for _, cert := range chain {
		if err := pem.Encode(&chainPEM, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
			t.Fatalf("encode chain: %v", err)
		}
	}
	pemPath := filepath.Join(dir, "p6-issuance-"+caName+"-chain.pem")
	if err := os.WriteFile(pemPath, chainPEM.Bytes(), 0o644); err != nil {
		t.Fatalf("write chain: %v", err)
	}
	t.Logf("evidence written: %s, %s", jsonPath, pemPath)
}

// --- Cloudflare API client -------------------------------------------------

// cfAPI is a minimal Cloudflare v4 client for the acceptance test: token
// verification, zone lookup and TXT record create/list/delete. The token is
// held in memory and sent in the Authorization header only.
type cfAPI struct {
	token string
	http  *http.Client
}

// cfRecord is the subset of a DNS record the test uses.
type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// newCFAPI builds the client.
func newCFAPI(token string) *cfAPI {
	return &cfAPI{token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// cfEnvelope is the Cloudflare v4 response envelope.
type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfAPIError    `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

// cfAPIError is one Cloudflare API error (no credential material).
type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// do performs one API call and unwraps the result. Failures include the HTTP
// status and Cloudflare's error codes, never the token.
func (c *cfAPI) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: encode request: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, reader)
	if err != nil {
		return fmt.Errorf("%s %s: build request: %w", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", method, path, err)
	}
	var envelope cfEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("%s %s: HTTP %d: unreadable response", method, path, response.StatusCode)
	}
	if !envelope.Success {
		parts := make([]string, 0, len(envelope.Errors))
		for _, apiErr := range envelope.Errors {
			parts = append(parts, fmt.Sprintf("%d %s", apiErr.Code, apiErr.Message))
		}
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, response.StatusCode, strings.Join(parts, "; "))
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return fmt.Errorf("%s %s: decode result: %w", method, path, err)
		}
	}
	return nil
}

// verifyToken checks the token itself before any zone operation.
func (c *cfAPI) verifyToken(ctx context.Context) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, &result); err != nil {
		return err
	}
	if result.Status != "active" {
		return fmt.Errorf("token status %q, want active", result.Status)
	}
	return nil
}

// zoneID resolves one zone by name, failing when the token cannot see it.
func (c *cfAPI) zoneID(ctx context.Context, zone string) (string, error) {
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("zone %q is not visible to the token", zone)
	}
	return zones[0].ID, nil
}

// listTXT lists the TXT records at one exact name.
func (c *cfAPI) listTXT(ctx context.Context, zoneID, name string) ([]cfRecord, error) {
	var records []cfRecord
	path := fmt.Sprintf("/zones/%s/dns_records?type=TXT&name=%s&per_page=100", zoneID, url.QueryEscape(name))
	if err := c.do(ctx, http.MethodGet, path, nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// createTXT creates one TXT record and returns its id.
func (c *cfAPI) createTXT(ctx context.Context, zoneID, name, content string) (string, error) {
	var created cfRecord
	payload := map[string]any{"type": "TXT", "name": name, "content": content, "ttl": 120}
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", payload, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// deleteRecord deletes one DNS record by id.
func (c *cfAPI) deleteRecord(ctx context.Context, zoneID, recordID string) error {
	return c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
}

// p6CFPreflight verifies the token, resolves the zone and proves the token can
// create then delete a TXT record in it. Any failure (including 403) is
// returned as-is; the caller stops instead of retrying.
func p6CFPreflight(ctx context.Context, cf *cfAPI, zone string) (string, error) {
	if err := cf.verifyToken(ctx); err != nil {
		return "", fmt.Errorf("token verify: %w", err)
	}
	zoneID, err := cf.zoneID(ctx, zone)
	if err != nil {
		return "", fmt.Errorf("zone %s: %w", zone, err)
	}
	name := "_gotham-be62-preflight." + zone
	recordID, err := cf.createTXT(ctx, zoneID, name, "gotham-be62-preflight-"+uuid.NewString()[:8])
	if err != nil {
		return "", fmt.Errorf("create preflight TXT: %w", err)
	}
	if err := cf.deleteRecord(ctx, zoneID, recordID); err != nil {
		return "", fmt.Errorf("delete preflight TXT %s: %w", recordID, err)
	}
	return zoneID, nil
}
