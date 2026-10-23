package e2e

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/acme"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const (
	p6IssuanceTokenEnv  = "CF_DNS_API_TOKEN"
	p6IssuanceDomainEnv = "GOTHAM_TEST_DOMAIN"
	p6IssuanceZoneEnv   = "GOTHAM_TEST_ZONE"
	p6IssuanceCAEnv     = "GOTHAM_E2E_ACME_CA"
	p6IssuanceOutEnv    = "GOTHAM_E2E_EVIDENCE_DIR"
	p6StagingRootEnv    = "GOTHAM_E2E_STAGING_ROOT_PEM"
	p6IssuanceOptInEnv  = "GOTHAM_E2E_DNS01"

	p6LEStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

	p6IssuanceTimeout     = 6 * time.Minute
	p6IssuancePoll        = 2 * time.Second
	p6IssuanceCleanupWait = 60 * time.Second
	p6TXTNamePrefix       = "_acme-challenge."
)

func TestP6DNS01Issuance(t *testing.T) {
	requireE2E(t)
	if !p6IssuanceOptIn() {
		t.Skipf("set %s=1 (with GOTHAM_E2E=1) to run live DNS-01 issuance against the Cloudflare test zone", p6IssuanceOptInEnv)
	}

	token, domain, zone := p6IssuanceEnv(t)
	caName, caServer := p6IssuanceCA(t)
	t.Logf("issuance acceptance: ca=%s domain=%s zone=%s", caName, domain, zone)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := testLogger(t)

	cf := newCFAPI(token)
	preflightCtx, preflightCancel := context.WithTimeout(ctx, time.Minute)
	zoneID, err := p6CFPreflight(t, preflightCtx, cf, zone)
	preflightCancel()
	if err != nil {
		t.Fatalf("cloudflare token preflight: %v", err)
	}
	t.Logf("cloudflare preflight ok: token created and deleted a TXT record in zone %s", zone)

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
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
	t.Cleanup(func() { p6ExecCleanup(t, pool, "DELETE FROM users WHERE id = $1", userRow.ID) })
	envID := p6SeedEnvironment(t, ctx, pool, st, suffix)
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    "p6-issuance-" + suffix,
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { p6ExecCleanup(t, pool, "DELETE FROM servers WHERE id = $1", serverRow.ID) })
	serverID := uuid.UUID(serverRow.ID.Bytes)

	nodeID := "p6-issuance-" + suffix
	configDir := p6CanonicalTempDir(t)
	acmeDir := configDir + "/acme"
	if err := os.MkdirAll(acmeDir, 0o755); err != nil {
		t.Fatalf("create acme dir: %v", err)
	}
	proxyAgent := agent.NewProxyServer(agent.ProxyServerConfig{Root: configDir, Logger: logger})
	agentAddr, authority := startLocalAgentWithProxyRoot(t, ctx, engine, nodeID, configDir,
		agent.WithProxyService(proxyAgent))

	registry := p6Registry{server: &servers.Server{ID: serverID, IP: "127.0.0.1", NodeID: &nodeID}}
	if existing := p6FindContainer(t, ctx, engine); existing != "" {
		t.Fatalf("a gotham-traefik container already exists (%s); refusing to remove a possibly live proxy", existing)
	}
	t.Cleanup(func() { p6RemoveOwnedTraefik(t, engine, configDir) })
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

	pullCtx, pullCancel := context.WithTimeout(ctx, p6PullTimeout)
	if _, err := engine.PullImage(pullCtx, p6NginxImage, "", ""); err != nil {
		pullCancel()
		t.Fatalf("pull %s: %v", p6NginxImage, err)
	}
	pullCancel()
	hostPort := freeTCPPort(t)
	backend := p6RunNginx(t, ctx, engine, "p6-issuance-app-"+suffix, fmt.Sprintf("%d:80", hostPort))
	appID := p6CreateApplication(t, ctx, pool, userRow.ID, serverRow.ID, envID, "issuance-"+suffix, domain, hostPort, false)
	t.Cleanup(func() {
		p6ExecCleanup(t, pool, "DELETE FROM deployments WHERE application_id = $1", appID)
		p6ExecCleanup(t, pool, "DELETE FROM applications WHERE id = $1", appID)
	})
	p6CreateRunningDeployment(t, ctx, pool, appID, backend)

	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		t.Fatalf("random secret: %v", err)
	}
	secret := hex.EncodeToString(rawSecret)
	t.Setenv("GOTHAM_SECRET_KEY", secret)

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
	providerID := provider.ID
	t.Cleanup(func() { p6ExecCleanup(t, pool, "DELETE FROM dns_providers WHERE id = $1", providerID) })
	certificate, err := certificateService.CreateCertificate(ctx, proxy.CreateCertificateInput{
		ApplicationID: appID,
		Challenge:     proxy.ChallengeDNS01,
		DNSProviderID: provider.ID,
	})
	if err != nil {
		t.Fatalf("create dns-01 certificate intent: %v", err)
	}
	t.Cleanup(func() { p6ExecCleanup(t, pool, "DELETE FROM domain_certificates WHERE application_id = $1", appID) })
	t.Logf("created dns-01 certificate intent %s for %s (resolver %s)", certificate.ID, certificate.Domain, proxy.DNSResolverName(proxy.ProviderCloudflare))

	resolverName := proxy.DNSResolverName(proxy.ProviderCloudflare)
	acmeJSON := filepath.Join(acmeDir, "acme.json")
	challengeName := p6TXTNamePrefix + domain
	owned := newP6OwnedTXT()

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if values, err := p6DeriveChallengeValues(cleanupCtx, caServer, acmeJSON, resolverName, domain); err == nil {
			owned.addValues(values)
		} else {
			t.Logf("cleanup: derive challenge values: %v", err)
		}
		if records, err := cf.listTXT(cleanupCtx, zoneID, challengeName); err == nil {
			owned.observe(records)
		} else {
			t.Logf("cleanup: list challenge TXT records: %v", err)
		}
		p6DeleteOwnedTXT(t, cleanupCtx, cf, zoneID, owned)
	})

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
	traefikID := p6WaitForContainer(t, ctx, engine)
	if id, ownedContainer, err := p6DiscoverOwnedTraefik(engine, configDir); err != nil {
		t.Fatalf("discover proxy container: %v", err)
	} else if id != traefikID || !ownedContainer {
		t.Fatalf("gotham-traefik %s was not created by this run (owned=%t)", traefikID, ownedContainer)
	}
	p6AssertTraefikBootstrap(t, traefikID)
	t.Logf("bootstrapped gotham-traefik %s", shortID(traefikID))

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

	deadline := time.Now().Add(p6IssuanceTimeout)
	var (
		servedChain      []*x509.Certificate
		lastHandshakeErr error
		lastVerifyErr    error
	)
	for time.Now().Before(deadline) {
		if records, err := cf.listTXT(ctx, zoneID, challengeName); err == nil {
			before := owned.candidateCount()
			owned.observe(records)
			if seen := owned.candidateCount(); seen > before {
				t.Logf("dns-01 challenge TXT candidate observed through the Cloudflare API: %d record(s) at %s", seen, challengeName)
			}
		}
		chain, err := p6ServedTLSCross(domain)
		if err != nil {
			lastHandshakeErr = err
		} else if len(chain) > 0 {
			if err := p6VerifyServedChain(caName, domain, chain); err != nil {
				lastVerifyErr = err
			} else {
				servedChain = chain
				break
			}
		}
		time.Sleep(p6IssuancePoll)
	}
	if servedChain == nil {
		if logs, err := runDocker(context.Background(), "logs", "--tail", "200", traefikID); err == nil {
			t.Logf("traefik logs while waiting for issuance:\n%s", p6RedactSecrets(logs))
		}
		t.Fatalf("no %s certificate served for %s within %s (last handshake error: %v; last verify error: %v)",
			caName, domain, p6IssuanceTimeout, lastHandshakeErr, lastVerifyErr)
	}
	leaf := servedChain[0]
	t.Logf("served certificate: subject=%q issuer=%q dns=%v not_after=%s",
		leaf.Subject.CommonName, leaf.Issuer.CommonName, leaf.DNSNames, leaf.NotAfter.Format(time.RFC3339))

	var deriveErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var values []string
		values, deriveErr = p6DeriveChallengeValues(ctx, caServer, acmeJSON, resolverName, domain)
		if deriveErr == nil {
			for _, record := range owned.addValues(values) {
				t.Logf("dns-01 challenge TXT matched this run's derived value: %s (owned record %s)", record.Name, record.ID)
			}
			break
		}
		time.Sleep(p6IssuancePoll)
	}
	if deriveErr != nil {
		t.Fatalf("could not derive this run's dns-01 challenge value from its own ACME account: %v", deriveErr)
	}
	if records, err := cf.listTXT(ctx, zoneID, challengeName); err == nil {
		owned.observe(records)
	}
	if len(owned.records()) == 0 {
		t.Fatalf("certificate issued but no observed Cloudflare TXT record carried this run's derived challenge value (saw %d candidate record(s)); every record was left untouched", owned.candidateCount())
	}

	storageLeaf, err := p6ACMEStorageLeaf(acmeJSON, domain)
	if err != nil {
		t.Fatalf("decode the stored ACME certificate: %v", err)
	}
	if !bytes.Equal(storageLeaf.Raw, leaf.Raw) {
		t.Fatalf("stored ACME leaf differs from the served leaf (stored serial %s, served serial %s)",
			storageLeaf.SerialNumber, leaf.SerialNumber)
	}
	t.Logf("Traefik ACME storage holds the served leaf (serial %s)", leaf.SerialNumber)

	deletedByTest := false
	if !p6WaitOwnedGone(ctx, cf, zoneID, challengeName, owned) {
		t.Errorf("the ACME client did not remove its DNS-01 TXT record(s) within %s; deleting the owned record(s) from the test", p6IssuanceCleanupWait)
		p6DeleteOwnedTXT(t, ctx, cf, zoneID, owned)
		deletedByTest = true
	}
	t.Logf("dns-01 challenge TXT cleanup: acked by the ACME client=%t, deleted by the test=%t", !deletedByTest, deletedByTest)

	p6WriteIssuanceEvidence(t, caName, p6IssuanceEvidence{
		CA:                           caName,
		CAServer:                     caServer,
		Domain:                       domain,
		Zone:                         zone,
		Subject:                      leaf.Subject.String(),
		Issuer:                       leaf.Issuer.String(),
		DNSNames:                     leaf.DNSNames,
		SerialNumber:                 leaf.SerialNumber.String(),
		NotBefore:                    leaf.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:                     leaf.NotAfter.UTC().Format(time.RFC3339),
		ChainSubjects:                p6ChainSubjects(servedChain),
		ChainVerified:                true,
		VerifyHostname:               p6VerifyHostnameText(leaf, domain),
		TXTRecordName:                challengeName,
		TXTRecordObserved:            len(owned.records()) > 0,
		TXTRecordsSeen:               owned.candidateCount(),
		TXTRecordsOwned:              len(owned.records()),
		TXTRecordCleaned:             !deletedByTest,
		TXTRecordDeletedByTest:       deletedByTest,
		ACMEStorageLeafMatchesServed: true,
		TraefikContainer:             traefikID,
		RecordedAt:                   time.Now().UTC().Format(time.RFC3339),
	}, servedChain)
}

func p6IssuanceOptIn() bool {
	return os.Getenv(p6IssuanceOptInEnv) == "1"
}

func p6IssuanceEnv(t *testing.T) (token, domain, zone string) {
	t.Helper()
	read := func(env string) string {
		value := strings.TrimSpace(os.Getenv(env))
		if value == "" {
			t.Fatalf("GOTHAM_E2E_DNS01=1 requires %s (load the owner credential file, e.g. `set -a; . $HOME/.config/gotham/cf-test.env; set +a`)", env)
		}
		return value
	}
	return read(p6IssuanceTokenEnv), read(p6IssuanceDomainEnv), read(p6IssuanceZoneEnv)
}

func p6IssuanceCA(t *testing.T) (name, caServer string) {
	t.Helper()
	switch strings.ToLower(strings.TrimSpace(os.Getenv(p6IssuanceCAEnv))) {
	case "", "staging":
		return "staging", p6LEStagingDirectory
	case "production":
		return "production", ""
	default:
		t.Fatalf("%s must be %q or %q", p6IssuanceCAEnv, "staging", "production")
		return "", ""
	}
}

func p6ServedTLSCross(domain string) ([]*x509.Certificate, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
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

func p6ChainSubjects(chain []*x509.Certificate) []string {
	subjects := make([]string, 0, len(chain))
	for _, cert := range chain {
		subjects = append(subjects, cert.Subject.String())
	}
	return subjects
}

func p6VerifyHostnameText(leaf *x509.Certificate, domain string) string {
	if err := leaf.VerifyHostname(domain); err != nil {
		return err.Error()
	}
	return "ok"
}

func p6RedactSecrets(text string) string {
	for _, secret := range []string{os.Getenv(p6IssuanceTokenEnv)} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}

type p6OwnedTXT struct {
	mu         sync.Mutex
	values     map[string]bool
	candidates map[string]cfRecord
	ids        map[string]cfRecord
}

func newP6OwnedTXT() *p6OwnedTXT {
	return &p6OwnedTXT{
		values:     map[string]bool{},
		candidates: map[string]cfRecord{},
		ids:        map[string]cfRecord{},
	}
}

func (o *p6OwnedTXT) addValues(values []string) []cfRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, value := range values {
		if value != "" {
			o.values[value] = true
		}
	}
	return o.promoteLocked()
}

func (o *p6OwnedTXT) valueCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.values)
}

func (o *p6OwnedTXT) valuesCopy() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, 0, len(o.values))
	for value := range o.values {
		out = append(out, value)
	}
	return out
}

func (o *p6OwnedTXT) observe(records []cfRecord) []cfRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, record := range records {
		o.candidates[record.ID] = record
	}
	return o.promoteLocked()
}

func (o *p6OwnedTXT) promoteLocked() []cfRecord {
	var added []cfRecord
	for id, record := range o.candidates {
		if _, ok := o.ids[id]; ok {
			continue
		}
		for value := range o.values {
			if p6TXTValueMatches(value, record.Content) {
				o.ids[id] = record
				added = append(added, record)
				break
			}
		}
	}
	return added
}

func (o *p6OwnedTXT) records() []cfRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]cfRecord, 0, len(o.ids))
	for _, record := range o.ids {
		out = append(out, record)
	}
	return out
}

func (o *p6OwnedTXT) candidateCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.candidates)
}

func p6TXTValueMatches(derived, content string) bool {
	if derived == "" {
		return false
	}
	return strings.TrimSpace(strings.Trim(content, `"`)) == derived
}

func p6DeleteOwnedTXT(t *testing.T, ctx context.Context, cf *cfAPI, zoneID string, owned *p6OwnedTXT) {
	t.Helper()
	records := owned.records()
	if len(records) == 0 {
		return
	}
	values := owned.valuesCopy()
	for _, record := range records {
		current, err := cf.getRecord(ctx, zoneID, record.ID)
		if errors.Is(err, errCFNotFound) {
			continue
		}
		if err != nil {
			t.Errorf("cleanup: read owned TXT record %s: %v", record.ID, err)
			continue
		}
		matches := false
		for _, value := range values {
			if p6TXTValueMatches(value, current.Content) {
				matches = true
				break
			}
		}
		if !matches {
			t.Errorf("cleanup: owned TXT record %s no longer carries a derived challenge value; leaving it in place", record.ID)
			continue
		}
		if err := cf.deleteRecord(ctx, zoneID, record.ID); err != nil {
			t.Errorf("cleanup: delete owned TXT record %s: %v", record.ID, err)
		}
	}
}

func p6WaitOwnedGone(ctx context.Context, cf *cfAPI, zoneID, challengeName string, owned *p6OwnedTXT) bool {
	deadline := time.Now().Add(p6IssuanceCleanupWait)
	for time.Now().Before(deadline) {
		if records, err := cf.listTXT(ctx, zoneID, challengeName); err == nil {
			owned.observe(records)
		}
		remaining := 0
		for _, record := range owned.records() {
			if _, err := cf.getRecord(ctx, zoneID, record.ID); errors.Is(err, errCFNotFound) {
				continue
			}
			remaining++
		}
		if remaining == 0 {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func p6DeriveChallengeValues(ctx context.Context, caServer, acmeJSON, resolver, domain string) ([]string, error) {
	account, err := p6ReadACMEAccount(acmeJSON, resolver)
	if err != nil {
		return nil, err
	}
	signer, err := p6ParseAccountKey(account)
	if err != nil {
		return nil, err
	}
	directory := caServer
	if directory == "" {
		directory = acme.LetsEncryptURL
	}
	client := &acme.Client{Key: signer, DirectoryURL: directory}
	registration, err := client.GetReg(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetch ACME account: %w", err)
	}
	client.KID = acme.KeyID(registration.URI)
	order, err := p6ACMENewOrder(ctx, client, domain)
	if err != nil {
		return nil, err
	}
	var values []string
	validAuthz := false
	for _, authzURL := range order.Authorizations {
		authorization, err := client.GetAuthorization(ctx, authzURL)
		if err != nil || authorization.Identifier.Type != "dns" || authorization.Identifier.Value != domain {
			continue
		}
		if authorization.Status != acme.StatusValid {
			continue
		}
		validAuthz = true
		for _, challenge := range authorization.Challenges {
			if challenge.Type != "dns-01" || challenge.Token == "" {
				continue
			}
			value, err := client.DNS01ChallengeRecord(challenge.Token)
			if err != nil {
				return nil, fmt.Errorf("derive dns-01 challenge value: %w", err)
			}
			values = append(values, value)
		}
	}
	if !validAuthz {
		return nil, fmt.Errorf("this run's %s authorization is not validated yet", domain)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no dns-01 challenge for %s in this run's ACME account", domain)
	}
	return values, nil
}

func p6ReadACMEAccount(path, resolver string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var store map[string]json.RawMessage
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse acme.json: %w", err)
	}
	raw, ok := store[resolver]
	if !ok {
		return nil, fmt.Errorf("resolver %s has no stored data yet", resolver)
	}
	var entry struct {
		Account *struct {
			PrivateKey []byte `json:"PrivateKey"`
		} `json:"Account"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse account entry: %w", err)
	}
	if entry.Account == nil || len(entry.Account.PrivateKey) == 0 {
		return nil, errors.New("ACME account key not stored yet")
	}
	return entry.Account.PrivateKey, nil
}

func p6ParseAccountKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, fmt.Errorf("ACME account key type %T is not a signer", key)
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("unsupported ACME account key encoding")
}

type p6ACMEOrder struct {
	Status         string   `json:"status"`
	Authorizations []string `json:"authorizations"`
}

func p6ACMENewOrder(ctx context.Context, client *acme.Client, domain string) (*p6ACMEOrder, error) {
	directory, err := client.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover ACME directory: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"identifiers": []map[string]string{{"type": "dns", "value": domain}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode new order: %w", err)
	}
	status, body, err := p6ACMEPost(ctx, client, directory.OrderURL, payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("acme new order: HTTP %d: %.200s", status, body)
	}
	var order p6ACMEOrder
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, fmt.Errorf("acme new order: decode response: %w", err)
	}
	if len(order.Authorizations) == 0 {
		return nil, errors.New("acme new order: no authorizations in response")
	}
	return &order, nil
}

func p6ACMEPost(ctx context.Context, client *acme.Client, target string, payload []byte) (int, []byte, error) {
	directory, err := client.Discover(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("discover ACME directory: %w", err)
	}
	nonce, err := p6ACMEHTTPNonce(ctx, client, directory.NonceURL)
	if err != nil {
		return 0, nil, err
	}
	body, err := p6ACMEJWS(client.Key, string(client.KID), nonce, target, payload)
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/jose+json")
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("acme post %s: %w", target, err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("acme post %s: read response: %w", target, err)
	}
	return response.StatusCode, data, nil
}

func p6ACMEHTTPNonce(ctx context.Context, client *acme.Client, nonceURL string) (string, error) {
	if nonceURL == "" {
		return "", errors.New("acme nonce: directory exposes no newNonce URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, nonceURL, nil)
	if err != nil {
		return "", err
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("acme nonce: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("acme nonce: HTTP %d", response.StatusCode)
	}
	nonce := response.Header.Get("Replay-Nonce")
	if nonce == "" {
		return "", errors.New("acme nonce: empty Replay-Nonce")
	}
	return nonce, nil
}

func p6ACMEJWS(key crypto.Signer, kid, nonce, target string, payload []byte) ([]byte, error) {
	alg, hash, err := p6JWSAlgorithm(key)
	if err != nil {
		return nil, err
	}
	protected, err := json.Marshal(map[string]string{"alg": alg, "kid": kid, "nonce": nonce, "url": target})
	if err != nil {
		return nil, err
	}
	protectedB64 := base64.RawURLEncoding.EncodeToString(protected)
	payloadB64 := ""
	if payload != nil {
		payloadB64 = base64.RawURLEncoding.EncodeToString(payload)
	}
	signingInput := protectedB64 + "." + payloadB64
	hasher := hash.New()
	hasher.Write([]byte(signingInput))
	digest := hasher.Sum(nil)
	var signature []byte
	switch k := key.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, k, hash, digest)
		if err != nil {
			return nil, err
		}
	case *ecdsa.PrivateKey:
		r, s, err := ecdsa.Sign(rand.Reader, k, digest)
		if err != nil {
			return nil, err
		}
		size := (k.Curve.Params().BitSize + 7) / 8
		signature = make([]byte, 2*size)
		r.FillBytes(signature[:size])
		s.FillBytes(signature[size:])
	default:
		return nil, fmt.Errorf("unsupported ACME account key type %T", key)
	}
	return json.Marshal(map[string]string{
		"protected": protectedB64,
		"payload":   payloadB64,
		"signature": base64.RawURLEncoding.EncodeToString(signature),
	})
}

func p6JWSAlgorithm(key crypto.Signer) (string, crypto.Hash, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return "RS256", crypto.SHA256, nil
	case *ecdsa.PrivateKey:
		switch k.Curve.Params().BitSize {
		case 256:
			return "ES256", crypto.SHA256, nil
		case 384:
			return "ES384", crypto.SHA384, nil
		case 521:
			return "ES512", crypto.SHA512, nil
		}
	}
	return "", 0, fmt.Errorf("unsupported ACME account key type %T", key)
}

func p6VerifyServedChain(caName, domain string, servedChain []*x509.Certificate) error {
	roots, err := p6TrustAnchors(caName)
	if err != nil {
		return err
	}
	return p6VerifyChain(domain, servedChain, roots)
}

func p6VerifyChain(domain string, servedChain []*x509.Certificate, roots *x509.CertPool) error {
	if len(servedChain) == 0 {
		return errors.New("served chain is empty")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range servedChain[1:] {
		intermediates.AddCert(cert)
	}
	_, err := servedChain[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		DNSName:       domain,
	})
	return err
}

func p6TrustAnchors(caName string) (*x509.CertPool, error) {
	if caName != "staging" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system roots: %w", err)
		}
		if roots == nil {
			return nil, errors.New("system certificate roots are unavailable")
		}
		return roots, nil
	}
	pool := x509.NewCertPool()
	added := 0
	for _, pemText := range p6StagingRootsPEM {
		certs, err := p6PEMCertificates([]byte(pemText))
		if err != nil {
			return nil, fmt.Errorf("parse pinned staging anchor: %w", err)
		}
		for _, cert := range certs {
			if !strings.Contains(cert.Subject.CommonName, "(STAGING)") {
				return nil, fmt.Errorf("pinned staging anchor %q is not a staging certificate", cert.Subject.CommonName)
			}
			pool.AddCert(cert)
			added++
		}
	}
	if override := strings.TrimSpace(os.Getenv(p6StagingRootEnv)); override != "" {
		pemBytes, err := os.ReadFile(override)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p6StagingRootEnv, err)
		}
		certs, err := p6PEMCertificates(pemBytes)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p6StagingRootEnv, err)
		}
		for _, cert := range certs {
			pool.AddCert(cert)
			added++
		}
	}
	if added == 0 {
		return nil, errors.New("no staging trust anchors configured")
	}
	return pool, nil
}

func p6PEMCertificates(pemBytes []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificate blocks found")
	}
	return certs, nil
}

var p6StagingRootsPEM = []string{
	`-----BEGIN CERTIFICATE-----
MIIFmDCCA4CgAwIBAgIQU9C87nMpOIFKYpfvOHFHFDANBgkqhkiG9w0BAQsFADBm
MQswCQYDVQQGEwJVUzEzMDEGA1UEChMqKFNUQUdJTkcpIEludGVybmV0IFNlY3Vy
aXR5IFJlc2VhcmNoIEdyb3VwMSIwIAYDVQQDExkoU1RBR0lORykgUHJldGVuZCBQ
ZWFyIFgxMB4XDTE1MDYwNDExMDQzOFoXDTM1MDYwNDExMDQzOFowZjELMAkGA1UE
BhMCVVMxMzAxBgNVBAoTKihTVEFHSU5HKSBJbnRlcm5ldCBTZWN1cml0eSBSZXNl
YXJjaCBHcm91cDEiMCAGA1UEAxMZKFNUQUdJTkcpIFByZXRlbmQgUGVhciBYMTCC
AiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBALbagEdDTa1QgGBWSYkyMhsc
ZXENOBaVRTMX1hceJENgsL0Ma49D3MilI4KS38mtkmdF6cPWnL++fgehT0FbRHZg
jOEr8UAN4jH6omjrbTD++VZneTsMVaGamQmDdFl5g1gYaigkkmx8OiCO68a4QXg4
wSyn6iDipKP8utsE+x1E28SA75HOYqpdrk4HGxuULvlr03wZGTIf/oRt2/c+dYmD
oaJhge+GOrLAEQByO7+8+vzOwpNAPEx6LW+crEEZ7eBXih6VP19sTGy3yfqK5tPt
TdXXCOQMKAp+gCj/VByhmIr+0iNDC540gtvV303WpcbwnkkLYC0Ft2cYUyHtkstO
fRcRO+K2cZozoSwVPyB8/J9RpcRK3jgnX9lujfwA/pAbP0J2UPQFxmWFRQnFjaq6
rkqbNEBgLy+kFL1NEsRbvFbKrRi5bYy2lNms2NJPZvdNQbT/2dBZKmJqxHkxCuOQ
FjhJQNeO+Njm1Z1iATS/3rts2yZlqXKsxQUzN6vNbD8KnXRMEeOXUYvbV4lqfCf8
mS14WEbSiMy87GB5S9ucSV1XUrlTG5UGcMSZOBcEUpisRPEmQWUOTWIoDQ5FOia/
GI+Ki523r2ruEmbmG37EBSBXdxIdndqrjy+QVAmCebyDx9eVEGOIpn26bW5LKeru
mJxa/CFBaKi4bRvmdJRLAgMBAAGjQjBAMA4GA1UdDwEB/wQEAwIBBjAPBgNVHRMB
Af8EBTADAQH/MB0GA1UdDgQWBBS182Xy/rAKkh/7PH3zRKCsYyXDFDANBgkqhkiG
9w0BAQsFAAOCAgEAncDZNytDbrrVe68UT6py1lfF2h6Tm2p8ro42i87WWyP2LK8Y
nLHC0hvNfWeWmjZQYBQfGC5c7aQRezak+tHLdmrNKHkn5kn+9E9LCjCaEsyIIn2j
qdHlAkepu/C3KnNtVx5tW07e5bvIjJScwkCDbP3akWQixPpRFAsnP+ULx7k0aO1x
qAeaAhQ2rgo1F58hcflgqKTXnpPM02intVfiVVkX5GXpJjK5EoQtLceyGOrkxlM/
sTPq4UrnypmsqSagWV3HcUlYtDinc+nukFk6eR4XkzXBbwKajl0YjztfrCIHOn5Q
CJL6TERVDbM/aAPly8kJ1sWGLuvvWYzMYgLzDul//rUF10gEMWaXVZV51KpS9DY/
5CunuvCXmEQJHo7kGcViT7sETn6Jz9KOhvYcXkJ7po6d93A/jy4GKPIPnsKKNEmR
xUuXY4xRdh45tMJnLTUDdC9FIU0flTeO9/vNpVA8OPU1i14vCz+MU8KX1bV3GXm/
fxlB7VBBjX9v5oUep0o/j68R/iDlCOM4VVfRa8gX6T2FU7fNdatvGro7uQzIvWof
gN9WUwCbEMBy/YhBSrXycKA8crgGg3x1mIsopn88JKwmMBa68oS7EHM9w7C4y71M
7DiA+/9Qdp9RBWJpTS9i/mDnJg1xvo8Xz49mrrgfmcAXTCJqXi24NatI3Oc=
-----END CERTIFICATE-----`,
	`-----BEGIN CERTIFICATE-----
MIICTjCCAdSgAwIBAgIRAIPgc3k5LlLVLtUUvs4K/QcwCgYIKoZIzj0EAwMwaDEL
MAkGA1UEBhMCVVMxMzAxBgNVBAoTKihTVEFHSU5HKSBJbnRlcm5ldCBTZWN1cml0
eSBSZXNlYXJjaCBHcm91cDEkMCIGA1UEAxMbKFNUQUdJTkcpIEJvZ3VzIEJyb2Nj
b2xpIFgyMB4XDTIwMDkwNDAwMDAwMFoXDTQwMDkxNzE2MDAwMFowaDELMAkGA1UE
BhMCVVMxMzAxBgNVBAoTKihTVEFHSU5HKSBJbnRlcm5ldCBTZWN1cml0eSBSZXNl
YXJjaCBHcm91cDEkMCIGA1UEAxMbKFNUQUdJTkcpIEJvZ3VzIEJyb2Njb2xpIFgy
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEOvS+w1kCzAxYOJbA06Aw0HFP2tLBLKPo
FQqR9AMskl1nC2975eQqycR+ACvYelA8rfwFXObMHYXJ23XLB+dAjPJVOJ2OcsjT
VqO4dcDWu+rQ2VILdnJRYypnV1MMThVxo0IwQDAOBgNVHQ8BAf8EBAMCAQYwDwYD
VR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQU3tGjWWQOwZo2o0busBB2766XlWYwCgYI
KoZIzj0EAwMDaAAwZQIwRcp4ZKBsq9XkUuN8wfX+GEbY1N5nmCRc8e80kUkuAefo
uc2j3cICeXo1cOybQ1iWAjEA3Ooawl8eQyR4wrjCofUE8h44p0j7Yl/kBlJZT8+9
vbtH7QiVzeKCOTQPINyRql6P
-----END CERTIFICATE-----`,
}

func p6ACMEStorageLeaf(path, domain string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var resolvers map[string]json.RawMessage
	if err := json.Unmarshal(data, &resolvers); err != nil {
		return nil, fmt.Errorf("parse acme.json: %w", err)
	}
	for _, raw := range resolvers {
		var content struct {
			Certificates []struct {
				Domain struct {
					Main string   `json:"main"`
					SANs []string `json:"sans"`
				} `json:"domain"`
				Certificate []byte `json:"certificate"`
			} `json:"Certificates"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			continue
		}
		for _, entry := range content.Certificates {
			if entry.Domain.Main != domain && !slices.Contains(entry.Domain.SANs, domain) {
				continue
			}
			if block, _ := pem.Decode(entry.Certificate); block != nil {
				return x509.ParseCertificate(block.Bytes)
			}
			return x509.ParseCertificate(entry.Certificate)
		}
	}
	return nil, fmt.Errorf("no stored certificate for %s", domain)
}

func p6ExecCleanup(t *testing.T, pool *pgxpool.Pool, statement string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, statement, args...); err != nil {
		t.Errorf("cleanup %q: %v", statement, err)
	}
}

func p6DiscoverOwnedTraefik(engine *agent.DockerClient, configDir string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	listed, err := engine.ListContainers(ctx, true, nil, false)
	if err != nil {
		return "", false, err
	}
	id := ""
	for _, container := range listed {
		if container.GetName() == proxy.TraefikContainerName {
			id = container.GetId()
		}
	}
	if id == "" {
		return "", false, nil
	}
	label, err := runDocker(ctx, "inspect", "--format", `{{index .Config.Labels "gotham.proxy.config_dir"}}`, id)
	if err != nil {
		return id, false, fmt.Errorf("inspect proxy container %s: %w: %s", id, err, label)
	}
	return id, strings.TrimSpace(label) == configDir, nil
}

func p6RemoveOwnedTraefik(t *testing.T, engine *agent.DockerClient, configDir string) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
	defer cleanupCancel()
	id, owned, err := p6DiscoverOwnedTraefik(engine, configDir)
	if err != nil {
		t.Errorf("cleanup: discover proxy container: %v", err)
		return
	}
	if id == "" {
		return
	}
	if !owned {
		t.Errorf("cleanup: refusing to remove gotham-traefik %s: its config-dir label does not match this run (%s)", shortID(id), configDir)
		return
	}
	if err := engine.Remove(cleanupCtx, id); err != nil {
		t.Errorf("cleanup: remove owned proxy container %s: %v", shortID(id), err)
	}
}

type cfAPI struct {
	token string
	http  *http.Client
}

type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

var errCFNotFound = errors.New("cloudflare: not found")

func newCFAPI(token string) *cfAPI {
	return &cfAPI{token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfAPIError    `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

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
		if response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s %s", errCFNotFound, method, path)
		}
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

func (c *cfAPI) listTXT(ctx context.Context, zoneID, name string) ([]cfRecord, error) {
	var records []cfRecord
	path := fmt.Sprintf("/zones/%s/dns_records?type=TXT&name=%s&per_page=100", zoneID, url.QueryEscape(name))
	if err := c.do(ctx, http.MethodGet, path, nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (c *cfAPI) getRecord(ctx context.Context, zoneID, recordID string) (cfRecord, error) {
	var record cfRecord
	if err := c.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records/"+recordID, nil, &record); err != nil {
		return cfRecord{}, err
	}
	return record, nil
}

func (c *cfAPI) createTXT(ctx context.Context, zoneID, name, content string) (string, error) {
	var created cfRecord
	payload := map[string]any{"type": "TXT", "name": name, "content": content, "ttl": 120}
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", payload, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *cfAPI) deleteRecord(ctx context.Context, zoneID, recordID string) error {
	return c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
}

func p6CFPreflight(t *testing.T, ctx context.Context, cf *cfAPI, zone string) (string, error) {
	t.Helper()
	if err := cf.verifyToken(ctx); err != nil {
		return "", fmt.Errorf("token verify: %w", err)
	}
	zoneID, err := cf.zoneID(ctx, zone)
	if err != nil {
		return "", fmt.Errorf("zone %s: %w", zone, err)
	}
	name := "_gotham-be62-preflight." + zone
	value := "gotham-be62-preflight-" + uuid.NewString()[:8]
	recordID, err := cf.createTXT(ctx, zoneID, name, value)
	if err != nil {
		return "", fmt.Errorf("create preflight TXT: %w", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := p6DeletePreflightTXT(t, cleanupCtx, cf, zoneID, recordID, name, value); err != nil && !errors.Is(err, errPreflightOwnership) {
			t.Errorf("cleanup: %v", err)
		}
	})
	var deleteErr error
	for attempt := 1; attempt <= 3; attempt++ {
		deleteErr = p6DeletePreflightTXT(t, ctx, cf, zoneID, recordID, name, value)
		if deleteErr == nil {
			return zoneID, nil
		}
		if errors.Is(deleteErr, errPreflightOwnership) || attempt == 3 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("delete preflight TXT %s (%s): %w", recordID, name, deleteErr)
}

var errPreflightOwnership = errors.New("preflight TXT identity changed")

func p6DeletePreflightTXT(t *testing.T, ctx context.Context, cf *cfAPI, zoneID, recordID, name, value string) error {
	t.Helper()
	current, err := cf.getRecord(ctx, zoneID, recordID)
	if errors.Is(err, errCFNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read preflight TXT %s (%s): %w", recordID, name, err)
	}
	if !p6TXTValueMatches(value, current.Content) {
		t.Errorf("preflight TXT %s (%s) no longer carries this run's value; leaving it untouched", recordID, name)
		return errPreflightOwnership
	}
	if err := cf.deleteRecord(ctx, zoneID, recordID); err != nil && !errors.Is(err, errCFNotFound) {
		return fmt.Errorf("delete preflight TXT %s (%s): %w", recordID, name, err)
	}
	return nil
}

type p6IssuanceEvidence struct {
	CA                           string   `json:"ca"`
	CAServer                     string   `json:"ca_server,omitempty"`
	Domain                       string   `json:"domain"`
	Zone                         string   `json:"zone"`
	Subject                      string   `json:"subject"`
	Issuer                       string   `json:"issuer"`
	DNSNames                     []string `json:"dns_names"`
	SerialNumber                 string   `json:"serial_number"`
	NotBefore                    string   `json:"not_before"`
	NotAfter                     string   `json:"not_after"`
	ChainSubjects                []string `json:"chain_subjects"`
	ChainVerified                bool     `json:"chain_verified"`
	VerifyHostname               string   `json:"verify_hostname"`
	TXTRecordName                string   `json:"txt_record_name"`
	TXTRecordObserved            bool     `json:"txt_record_observed"`
	TXTRecordsSeen               int      `json:"txt_records_seen"`
	TXTRecordsOwned              int      `json:"txt_records_owned"`
	TXTRecordCleaned             bool     `json:"txt_record_cleaned"`
	TXTRecordDeletedByTest       bool     `json:"txt_record_deleted_by_test"`
	ACMEStorageLeafMatchesServed bool     `json:"acme_storage_leaf_matches_served"`
	TraefikContainer             string   `json:"traefik_container"`
	RecordedAt                   string   `json:"recorded_at"`
}

func p6WriteIssuanceEvidence(t *testing.T, caName string, evidence p6IssuanceEvidence, servedChain []*x509.Certificate) {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv(p6IssuanceOutEnv))
	if dir == "" {
		return
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
	for _, cert := range servedChain {
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

func TestP6IssuanceTXTValueOwnership(t *testing.T) {
	const derived = "uUdIZWNEZOpUmh5PIx-_-pk9J0DWQQ76EpK186qFyvA"
	records := []cfRecord{
		{ID: "owned-quoted", Name: "_acme-challenge.example.com", Content: `"` + derived + `"`},
		{ID: "owned-plain", Name: "_acme-challenge.example.com", Content: derived},
		{ID: "foreign-other", Name: "_acme-challenge.example.com", Content: `"other-client-value"`},
		{ID: "foreign-prefix", Name: "_acme-challenge.example.com", Content: `"` + derived[:20] + `"`},
		{ID: "foreign-case", Name: "_acme-challenge.example.com", Content: strings.ToUpper(derived)},
		{ID: "empty", Name: "_acme-challenge.example.com", Content: ""},
	}

	owned := newP6OwnedTXT()
	if added := owned.observe(records); len(added) != 0 || len(owned.records()) != 0 {
		t.Fatalf("records were owned before a derived value existed: %#v", added)
	}
	if owned.candidateCount() != len(records) {
		t.Fatalf("candidate count = %d, want %d", owned.candidateCount(), len(records))
	}

	added := owned.addValues([]string{derived})
	if len(added) != 2 {
		t.Fatalf("addValues promoted %d records, want the 2 exact matches: %#v", len(added), added)
	}
	for _, record := range owned.records() {
		if record.ID != "owned-quoted" && record.ID != "owned-plain" {
			t.Fatalf("record %s was treated as owned", record.ID)
		}
	}
	if len(owned.records()) != 2 || owned.valueCount() != 1 {
		t.Fatalf("owned = %d values = %d, want 2 owned and 1 value", len(owned.records()), owned.valueCount())
	}

	late := owned.observe([]cfRecord{
		{ID: "owned-late", Name: "_acme-challenge.example.com", Content: derived},
		{ID: "foreign-late", Name: "_acme-challenge.example.com", Content: `"yet-another"`},
	})
	if len(late) != 1 || late[0].ID != "owned-late" {
		t.Fatalf("late observation promoted %#v", late)
	}
	if len(owned.records()) != 3 {
		t.Fatalf("owned = %d, want 3", len(owned.records()))
	}

	if !p6TXTValueMatches(derived, `"`+derived+`"`) || !p6TXTValueMatches(derived, derived) {
		t.Fatal("exact challenge values must match quoted and unquoted record content")
	}
	if p6TXTValueMatches(derived, "") || p6TXTValueMatches("", derived) ||
		p6TXTValueMatches(derived, `"`+derived[:20]+`"`) || p6TXTValueMatches(derived, strings.ToUpper(derived)) {
		t.Fatal("non-exact challenge values must never match")
	}
}

func TestP6IssuanceChainVerificationRejectsForgery(t *testing.T) {
	forgedRoot, forgedLeaf := p6ForgeChain(t, "(STAGING) Ersatz Emmental X1", "gotham.deelux.dev")
	forged := []*x509.Certificate{forgedLeaf, forgedRoot}

	signerRoots := x509.NewCertPool()
	signerRoots.AddCert(forgedRoot)
	if err := p6VerifyChain("gotham.deelux.dev", forged, signerRoots); err != nil {
		t.Fatalf("verification rejected a chain signed by the provided anchor: %v", err)
	}

	stagingRoots, err := p6TrustAnchors("staging")
	if err != nil {
		t.Fatalf("staging anchors: %v", err)
	}
	if err := p6VerifyChain("gotham.deelux.dev", forged, stagingRoots); err == nil {
		t.Fatal("forged staging chain with matching issuer names was accepted")
	}

	productionRoots, err := p6TrustAnchors("production")
	if err != nil {
		t.Fatalf("production anchors: %v", err)
	}
	if err := p6VerifyChain("gotham.deelux.dev", forged, productionRoots); err == nil {
		t.Fatal("forged production chain with matching issuer names was accepted")
	}

	if err := p6VerifyChain("other.example.com", forged, signerRoots); err == nil {
		t.Fatal("chain verification ignored the requested hostname")
	}
}

func p6ForgeChain(t *testing.T, rootCN, leafDNS string) (*x509.Certificate, *x509.Certificate) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: rootCN, Organization: []string{"Internet Security Research Group"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: leafDNS},
		DNSNames:     []string{leafDNS},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return root, leaf
}

func TestP6IssuanceACMENewOrder(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate account key: %v", err)
	}
	const accountURL = "https://acme.example.test/acct/1"
	const authzURL = "https://acme.example.test/authz/1"
	var calls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/directory":
			writeJSON(t, w, http.StatusOK, map[string]string{
				"newNonce":   server.URL + "/nonce",
				"newAccount": server.URL + "/account",
				"newOrder":   server.URL + "/order",
				"revokeCert": server.URL + "/revoke",
				"keyChange":  server.URL + "/keychange",
			})
		case "/nonce":
			w.Header().Set("Replay-Nonce", "test-nonce")
			w.WriteHeader(http.StatusOK)
		case "/order":
			calls++
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var jws struct {
				Protected string `json:"protected"`
				Payload   string `json:"payload"`
				Signature string `json:"signature"`
			}
			if err := json.Unmarshal(body, &jws); err != nil {
				t.Errorf("server: decode JWS: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			protectedBytes, err := base64.RawURLEncoding.DecodeString(jws.Protected)
			if err != nil {
				t.Errorf("server: decode protected header: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var protected map[string]string
			if err := json.Unmarshal(protectedBytes, &protected); err != nil {
				t.Errorf("server: parse protected header: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if protected["alg"] != "RS256" || protected["kid"] != accountURL ||
				protected["nonce"] != "test-nonce" || protected["url"] != server.URL+"/order" {
				t.Errorf("server: protected header = %#v", protected)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			signature, err := base64.RawURLEncoding.DecodeString(jws.Signature)
			if err != nil {
				t.Errorf("server: decode signature: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			digest := sha256.Sum256([]byte(jws.Protected + "." + jws.Payload))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
				t.Errorf("server: verify signature: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			payloadBytes, err := base64.RawURLEncoding.DecodeString(jws.Payload)
			if err != nil {
				t.Errorf("server: decode payload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var payload struct {
				Identifiers []map[string]string `json:"identifiers"`
			}
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				t.Errorf("server: parse payload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(payload.Identifiers) != 1 || payload.Identifiers[0]["type"] != "dns" ||
				payload.Identifiers[0]["value"] != "gotham.deelux.dev" {
				t.Errorf("server: new order payload = %#v", payload)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			status := http.StatusCreated
			if calls > 1 {
				status = http.StatusOK
			}
			writeJSON(t, w, status, map[string]any{
				"status":         "pending",
				"identifiers":    []map[string]string{{"type": "dns", "value": "gotham.deelux.dev"}},
				"authorizations": []string{authzURL},
				"finalize":       server.URL + "/finalize/1",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &acme.Client{
		Key:          key,
		DirectoryURL: server.URL + "/directory",
		HTTPClient:   server.Client(),
		KID:          acme.KeyID(accountURL),
	}
	for call := 1; call <= 2; call++ {
		order, err := p6ACMENewOrder(context.Background(), client, "gotham.deelux.dev")
		if err != nil {
			t.Fatalf("new order call %d: %v", call, err)
		}
		if len(order.Authorizations) != 1 || order.Authorizations[0] != authzURL {
			t.Fatalf("call %d: order = %#v", call, order)
		}
	}
	if calls != 2 {
		t.Fatalf("server saw %d new-order calls, want 2", calls)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("server: encode response: %v", err)
	}
}
