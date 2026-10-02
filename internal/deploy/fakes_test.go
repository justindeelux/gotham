package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestMain clears the feature flag so the suite runs with the applications
// surface enabled; individual tests override it with t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

// discardLogger keeps the orchestrator's operational logging out of the test
// output; assertions read the persisted rows instead.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeRepository is a scriptable in-memory Repository. Rows are kept in
// insertion order and ListDeployments returns them newest first.
//
// The zero-value `app` field is the seeded application the pre-existing tests
// build their fixtures around; every application created through
// CreateApplication is kept in `apps`. Configuration rows (env vars, secrets,
// storages) are matched by application_id, and a row left without one by a test
// fixture belongs to whichever application is asked for.
type fakeRepository struct {
	mu          sync.Mutex
	app         Application
	apps        []Application
	deployments []Deployment
	envVars     []EnvVar
	secrets     []Secret
	storages    []Storage

	// deployKeys holds the single deploy key per application (the unique
	// index on application_id); privateKeys mirrors private_keys, keyed by
	// the private key id, because the real schema has no application FK
	// there: deleting an application cascades only the mapping.
	deployKeys  map[uuid.UUID]fakeDeployKey
	privateKeys map[uuid.UUID]string

	// certificates mirrors domain_certificates (one intent per application);
	// dnsProviders mirrors the provider rows the preview clone validates
	// against. certErr fails every certificate write (the best-effort test).
	certificates []CertificateIntent
	dnsProviders map[uuid.UUID]DNSProviderInfo
	certErr      error

	// unknownServers names servers ServerExists must report as missing.
	unknownServers map[uuid.UUID]bool
	// serverTeams maps a registered server to its team; an entry without a
	// team (missing or zero) is a legacy shared node.
	serverTeams map[uuid.UUID]uuid.UUID

	getErr       error
	createErr    error
	failStaleErr error

	// deployKeyErr fails every deploy-key write (tests the rollback with).
	deployKeyErr error

	// states records every persisted deployment state in order, so tests can
	// assert the exact state-machine walk.
	states []State

	// events, when set, records repository operations so tests can pin their
	// ordering against the hook lifecycle calls.
	events *[]string
}

// record appends one repository operation to the shared test event log.
func (r *fakeRepository) record(event string) {
	if r.events != nil {
		*r.events = append(*r.events, event)
	}
}

// staleDeploymentError mirrors the message the boot-time sweep SQL writes.
const staleDeploymentError = "control plane restarted before the deployment finished"

// Compile-time guarantee that fakeRepository satisfies the seam.
var _ Repository = (*fakeRepository)(nil)

// GetApplication implements Repository.
func (r *fakeRepository) GetApplication(_ context.Context, appID uuid.UUID) (Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return Application{}, r.getErr
	}
	if r.app.ID != uuid.Nil && r.app.ID == appID {
		return r.app, nil
	}
	for _, app := range r.apps {
		if app.ID == appID {
			return app, nil
		}
	}
	return Application{}, ErrNotFound
}

// ListApplications implements Repository: the active team's applications, or
// the creator's when the scope has no team context (pre-teams behavior),
// newest first (created_at DESC, id DESC).
func (r *fakeRepository) ListApplications(_ context.Context, scope teams.Scope) ([]Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	matches := func(app Application) bool {
		if scope.Active() {
			return app.TeamID == scope.TeamID
		}
		return app.UserID == scope.UserID
	}
	var out []Application
	if r.app.ID != uuid.Nil && matches(r.app) {
		out = append(out, r.app)
	}
	for _, app := range r.apps {
		if matches(app) {
			out = append(out, app)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	return out, nil
}

// CreateApplication implements Repository: it assigns an ID and timestamps like
// the database and stores the configuration rows with the application.
func (r *fakeRepository) CreateApplication(_ context.Context, app Application, envVars []EnvVar, secrets []Secret, storages []Storage) (Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Application{}, r.createErr
	}
	for _, existing := range r.ownedApplications() {
		if existing.UserID == app.UserID && existing.Name == app.Name {
			return Application{}, fmt.Errorf("%w: an application named %q already exists", ErrValidation, app.Name)
		}
	}
	now := time.Now().UTC()
	// Keep insertion order observable: two rows created inside the same clock
	// tick would otherwise sort back into an arbitrary order (the production
	// query breaks created_at ties on id DESC).
	for _, existing := range r.ownedApplications() {
		if !now.After(existing.CreatedAt) {
			now = existing.CreatedAt.Add(time.Nanosecond)
		}
	}
	if app.ID == uuid.Nil {
		app.ID = uuid.New()
	}
	app.CreatedAt = now
	app.UpdatedAt = now
	r.apps = append(r.apps, app)
	for _, v := range envVars {
		v.ApplicationID = app.ID
		v.ID = uuid.New()
		r.envVars = append(r.envVars, v)
	}
	for _, s := range secrets {
		if s.ID == uuid.Nil {
			s.ID = uuid.New()
		}
		s.ApplicationID = app.ID
		r.secrets = append(r.secrets, s)
	}
	for _, s := range storages {
		s.ApplicationID = app.ID
		r.storages = append(r.storages, s)
	}
	return app, nil
}

// UpdateApplication implements Repository and bumps updated_at.
func (r *fakeRepository) UpdateApplication(_ context.Context, app Application) (Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.app.ID == app.ID {
		app.CreatedAt = r.app.CreatedAt
		app.UpdatedAt = time.Now().UTC()
		r.app = app
		return app, nil
	}
	for i, existing := range r.apps {
		if existing.ID == app.ID {
			app.CreatedAt = existing.CreatedAt
			app.UpdatedAt = time.Now().UTC()
			r.apps[i] = app
			return app, nil
		}
	}
	return Application{}, ErrNotFound
}

// DeleteApplication implements Repository with the schema's cascade: the row
// goes, and its configuration and deployments with it.
func (r *fakeRepository) DeleteApplication(_ context.Context, appID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("application deleted")
	if r.app.ID == appID {
		r.app = Application{}
	}
	for i, app := range r.apps {
		if app.ID == appID {
			r.apps = append(r.apps[:i], r.apps[i+1:]...)
			break
		}
	}
	deployments := make([]Deployment, 0, len(r.deployments))
	for _, dep := range r.deployments {
		if dep.ApplicationID != appID {
			deployments = append(deployments, dep)
		}
	}
	r.deployments = deployments
	envVars := make([]EnvVar, 0, len(r.envVars))
	for _, v := range r.envVars {
		if v.ApplicationID != appID && v.ApplicationID != uuid.Nil {
			envVars = append(envVars, v)
		}
	}
	r.envVars = envVars
	secrets := make([]Secret, 0, len(r.secrets))
	for _, s := range r.secrets {
		if s.ApplicationID != appID && s.ApplicationID != uuid.Nil {
			secrets = append(secrets, s)
		}
	}
	r.secrets = secrets
	storages := make([]Storage, 0, len(r.storages))
	for _, s := range r.storages {
		if s.ApplicationID != appID && s.ApplicationID != uuid.Nil {
			storages = append(storages, s)
		}
	}
	r.storages = storages
	// Cascade the deploy-key mapping only: private_keys has no application FK
	// (application_deploy_keys.private_key_id points at it), so the sealed
	// row survives a bare application delete — the paths that must remove it
	// call DeleteDeployKey explicitly.
	delete(r.deployKeys, appID)
	certificates := make([]CertificateIntent, 0, len(r.certificates))
	for _, cert := range r.certificates {
		if cert.ApplicationID != appID {
			certificates = append(certificates, cert)
		}
	}
	r.certificates = certificates
	return nil
}

// ReplaceEnvVars implements Repository, replacing both collections as one set.
func (r *fakeRepository) ReplaceEnvVars(_ context.Context, appID uuid.UUID, envVars []EnvVar, secrets []Secret) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return r.getErr
	}
	keptVars := make([]EnvVar, 0, len(r.envVars))
	for _, v := range r.envVars {
		if v.ApplicationID != uuid.Nil && v.ApplicationID != appID {
			keptVars = append(keptVars, v)
		}
	}
	keptSecrets := make([]Secret, 0, len(r.secrets))
	for _, s := range r.secrets {
		if s.ApplicationID != uuid.Nil && s.ApplicationID != appID {
			keptSecrets = append(keptSecrets, s)
		}
	}
	for _, v := range envVars {
		v.ApplicationID = appID
		v.ID = uuid.New()
		keptVars = append(keptVars, v)
	}
	for _, s := range secrets {
		if s.ID == uuid.Nil {
			s.ID = uuid.New()
		}
		s.ApplicationID = appID
		keptSecrets = append(keptSecrets, s)
	}
	r.envVars, r.secrets = keptVars, keptSecrets
	return nil
}

// ReplaceStorages implements Repository, replacing the collection as one set.
func (r *fakeRepository) ReplaceStorages(_ context.Context, appID uuid.UUID, storages []Storage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return r.getErr
	}
	kept := make([]Storage, 0, len(r.storages))
	for _, s := range r.storages {
		if s.ApplicationID != uuid.Nil && s.ApplicationID != appID {
			kept = append(kept, s)
		}
	}
	for _, s := range storages {
		s.ApplicationID = appID
		s.ID = uuid.New()
		kept = append(kept, s)
	}
	r.storages = kept
	return nil
}

// ServerExists implements Repository: the node must be known and actionable by
// the caller's active team.
func (r *fakeRepository) ServerExists(_ context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if serverID == uuid.Nil {
		return false, nil
	}
	if r.unknownServers[serverID] {
		return false, nil
	}
	if err := scope.AuthorizeOptionalTeam(r.serverTeams[serverID], true); err != nil {
		return false, nil
	}
	return true, nil
}

// ServerTeam implements Repository: the registered node's team (zero for a
// legacy shared node).
func (r *fakeRepository) ServerTeam(_ context.Context, serverID uuid.UUID) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if serverID == uuid.Nil || r.unknownServers[serverID] {
		return uuid.Nil, false, nil
	}
	return r.serverTeams[serverID], true, nil
}

// seedServerForTeam registers a node owned by teamID (the zero UUID seeds a
// legacy shared node).
func (r *fakeRepository) seedServerForTeam(teamID uuid.UUID) uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverTeams == nil {
		r.serverTeams = map[uuid.UUID]uuid.UUID{}
	}
	id := uuid.New()
	r.serverTeams[id] = teamID
	return id
}

// ownedApplications returns every application the fake holds (seed first).
func (r *fakeRepository) ownedApplications() []Application {
	applications := make([]Application, 0, len(r.apps)+1)
	if r.app.ID != uuid.Nil {
		applications = append(applications, r.app)
	}
	return append(applications, r.apps...)
}

// CreateDeployment implements Repository, assigning an ID like the database.
func (r *fakeRepository) CreateDeployment(_ context.Context, dep Deployment) (Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Deployment{}, r.createErr
	}
	dep.ID = uuid.New()
	dep.CreatedAt = time.Now().UTC()
	dep.UpdatedAt = dep.CreatedAt
	r.deployments = append(r.deployments, dep)
	r.states = append(r.states, dep.State)
	return dep, nil
}

// GetDeployment implements Repository.
func (r *fakeRepository) GetDeployment(_ context.Context, appID, deploymentID uuid.UUID) (Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dep := range r.deployments {
		if dep.ID == deploymentID && dep.ApplicationID == appID {
			return dep, nil
		}
	}
	return Deployment{}, ErrNotFound
}

// ListDeployments implements Repository, newest first.
func (r *fakeRepository) ListDeployments(_ context.Context, appID uuid.UUID) ([]Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Deployment, 0, len(r.deployments))
	for i := len(r.deployments) - 1; i >= 0; i-- {
		if r.deployments[i].ApplicationID == appID {
			out = append(out, r.deployments[i])
		}
	}
	return out, nil
}

// FailStaleDeployments implements Repository: it marks every non-terminal
// deployment failed, mirroring the boot-time sweep, and reports the count.
func (r *fakeRepository) FailStaleDeployments(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failStaleErr != nil {
		return 0, r.failStaleErr
	}
	var n int64
	for i := range r.deployments {
		if r.deployments[i].State.Terminal() {
			continue
		}
		r.deployments[i].State = StateFailed
		r.deployments[i].Error = staleDeploymentError
		r.deployments[i].FinishedAt = time.Now().UTC()
		n++
	}
	return n, nil
}

// UpdateDeployment implements Repository and records the persisted state.
func (r *fakeRepository) UpdateDeployment(_ context.Context, dep Deployment) (Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.deployments {
		if existing.ID == dep.ID {
			dep.CreatedAt = existing.CreatedAt
			dep.UpdatedAt = time.Now().UTC()
			r.deployments[i] = dep
			r.states = append(r.states, dep.State)
			return dep, nil
		}
	}
	return Deployment{}, ErrNotFound
}

// ListEnvVars implements Repository, scoped to the application (a fixture row
// without an application id belongs to whichever application is asked for).
func (r *fakeRepository) ListEnvVars(_ context.Context, appID uuid.UUID) ([]EnvVar, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EnvVar, 0, len(r.envVars))
	for _, v := range r.envVars {
		if v.ApplicationID == uuid.Nil || v.ApplicationID == appID {
			out = append(out, v)
		}
	}
	return out, nil
}

// ListSecrets implements Repository (see ListEnvVars for fixture rows).
func (r *fakeRepository) ListSecrets(_ context.Context, appID uuid.UUID) ([]Secret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Secret, 0, len(r.secrets))
	for _, s := range r.secrets {
		if s.ApplicationID == uuid.Nil || s.ApplicationID == appID {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListStorages implements Repository (see ListEnvVars for fixture rows).
func (r *fakeRepository) ListStorages(_ context.Context, appID uuid.UUID) ([]Storage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Storage, 0, len(r.storages))
	for _, s := range r.storages {
		if s.ApplicationID == uuid.Nil || s.ApplicationID == appID {
			out = append(out, s)
		}
	}
	return out, nil
}

// fakeDeployKey is one row of the application_deploy_keys mapping (the private
// half lives separately in privateKeys, mirroring private_keys).
type fakeDeployKey struct {
	key DeployKey
}

// GetDeployKey implements Repository, keeping one key per application.
func (r *fakeRepository) GetDeployKey(_ context.Context, appID uuid.UUID) (DeployKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.deployKeys[appID]
	if !ok {
		return DeployKey{}, ErrNotFound
	}
	return stored.key, nil
}

// CreateDeployKey implements Repository, assigning IDs and timestamps like the
// database and failing on the scriptable deployKeyErr. It writes the mapping
// and the sealed private key as two rows, like the store transaction.
func (r *fakeRepository) CreateDeployKey(_ context.Context, key DeployKey, privateKeyPEM string) (DeployKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deployKeyErr != nil {
		return DeployKey{}, r.deployKeyErr
	}
	if r.deployKeys == nil {
		r.deployKeys = make(map[uuid.UUID]fakeDeployKey)
	}
	if r.privateKeys == nil {
		r.privateKeys = make(map[uuid.UUID]string)
	}
	if _, exists := r.deployKeys[key.ApplicationID]; exists {
		return DeployKey{}, fmt.Errorf("%w: application already has a deploy key", ErrConflict)
	}
	key.ID = uuid.New()
	key.PrivateKeyID = uuid.New()
	key.CreatedAt = time.Now().UTC()
	r.deployKeys[key.ApplicationID] = fakeDeployKey{key: key}
	r.privateKeys[key.PrivateKeyID] = privateKeyPEM
	return key, nil
}

// DeleteDeployKey implements Repository with the store's cascade: the mapping
// and the private key it points at go together.
func (r *fakeRepository) DeleteDeployKey(_ context.Context, appID uuid.UUID) (DeployKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("deploy key detached")
	stored, ok := r.deployKeys[appID]
	if !ok {
		return DeployKey{}, ErrNotFound
	}
	delete(r.deployKeys, appID)
	delete(r.privateKeys, stored.key.PrivateKeyID)
	return stored.key, nil
}

// DeployKeyPrivatePEM implements Repository: "" for an application without a
// key, the stored private half otherwise.
func (r *fakeRepository) DeployKeyPrivatePEM(_ context.Context, appID uuid.UUID) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return "", r.getErr
	}
	stored, ok := r.deployKeys[appID]
	if !ok {
		return "", nil
	}
	return r.privateKeys[stored.key.PrivateKeyID], nil
}

// GetCertificateIntent implements Repository.
func (r *fakeRepository) GetCertificateIntent(_ context.Context, appID uuid.UUID) (CertificateIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return CertificateIntent{}, r.getErr
	}
	for _, cert := range r.certificates {
		if cert.ApplicationID == appID {
			return cert, nil
		}
	}
	return CertificateIntent{}, ErrNotFound
}

// GetDNSProviderInfo implements Repository.
func (r *fakeRepository) GetDNSProviderInfo(_ context.Context, providerID uuid.UUID) (DNSProviderInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return DNSProviderInfo{}, r.getErr
	}
	info, ok := r.dnsProviders[providerID]
	if !ok {
		return DNSProviderInfo{}, ErrNotFound
	}
	return DNSProviderInfo{Zones: append([]string{}, info.Zones...), Enabled: info.Enabled}, nil
}

// CreateCertificateIntent implements Repository with the one-intent-per-
// application unique index.
func (r *fakeRepository) CreateCertificateIntent(_ context.Context, in CertificateIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.certErr != nil {
		return r.certErr
	}
	for _, cert := range r.certificates {
		if cert.ApplicationID == in.ApplicationID {
			return fmt.Errorf("%w: the application already has a certificate configuration", ErrConflict)
		}
	}
	r.certificates = append(r.certificates, in)
	return nil
}

// certificateIntent returns the stored intent of an application (test helper).
func (r *fakeRepository) certificateIntent(appID uuid.UUID) (CertificateIntent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cert := range r.certificates {
		if cert.ApplicationID == appID {
			return cert, true
		}
	}
	return CertificateIntent{}, false
}

// hasDeployKey reports whether an application holds a deploy key (test helper).
func (r *fakeRepository) hasDeployKey(appID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.deployKeys[appID]
	return ok
}

// hasPrivateKey reports whether a sealed private key row still exists (test
// helper); it mirrors private_keys, which the application cascade does not
// reach.
func (r *fakeRepository) hasPrivateKey(privateKeyID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.privateKeys[privateKeyID]
	return ok
}

// deployment returns the stored row by ID (test helper).
func (r *fakeRepository) deployment(id uuid.UUID) (Deployment, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dep := range r.deployments {
		if dep.ID == id {
			return dep, true
		}
	}
	return Deployment{}, false
}

// persistedStates returns the recorded walk with consecutive duplicates
// collapsed: an attempt counter update and a transition share a state.
func (r *fakeRepository) persistedStates() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]State, 0, len(r.states))
	for _, state := range r.states {
		if len(out) == 0 || out[len(out)-1] != state {
			out = append(out, state)
		}
	}
	return out
}

// fakeSource writes a minimal buildable tree instead of running git.
type fakeSource struct {
	calls  int
	err    error
	logs   []string
	branch string
}

// Compile-time guarantee that fakeSource satisfies the seam.
var _ Source = (*fakeSource)(nil)

// Clone implements Source by writing a Dockerfile the engine can build.
func (s *fakeSource) Clone(_ context.Context, app Application, dir string, log func(string)) error {
	s.calls++
	s.branch = app.Branch
	if log != nil {
		for _, line := range s.logs {
			log(line)
		}
	}
	if s.err != nil {
		return s.err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
}

// mockNode is a scriptable Node: the mock agent the state-machine tests run
// against. Every RPC counts its calls and records its arguments.
type mockNode struct {
	mu sync.Mutex

	buildErr  error
	buildLogs []string
	pullErr   error
	runErr    error
	stopErr   error
	startErr  error

	// registryAddr and digest seed the BuildImage-style outcome.
	registryAddr string
	digest       string

	// healthState/healthStatus describe the container Run created.
	containerID  string
	healthState  string
	healthStatus string

	buildCalls int
	pullCalls  int
	runCalls   int
	stopCalls  int
	startCalls int
	closed     int

	requests []*agentv1.CreateContainerRequest
	stopped  []string
	started  []string
	metas    []BuildMeta
}

// Compile-time guarantee that mockNode satisfies the seam.
var _ Node = (*mockNode)(nil)

// newMockNode returns a healthy mock agent: builds push to a node-local
// registry and the started container reports as running.
func newMockNode() *mockNode {
	return &mockNode{
		registryAddr: "127.0.0.1:5000",
		digest:       "sha256:cafebabedeadbeef",
		containerID:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		healthState:  "running",
		healthStatus: "Up Less than a second",
	}
}

// Build implements Node.
func (m *mockNode) Build(_ context.Context, meta BuildMeta, _ []byte, log func([]byte)) (BuildOutcome, error) {
	m.mu.Lock()
	m.buildCalls++
	m.metas = append(m.metas, meta)
	err := m.buildErr
	logs := append([]string(nil), m.buildLogs...)
	registry, digest := m.registryAddr, m.digest
	m.mu.Unlock()

	if err != nil {
		return BuildOutcome{}, err
	}
	for _, chunk := range logs {
		if log != nil {
			log([]byte(chunk))
		}
	}
	tag := "gotham/" + meta.AppID + ":" + meta.DeployID
	return BuildOutcome{
		ImageTag:      tag,
		RegistryImage: registry + "/" + tag,
		Digest:        digest,
		RegistryAddr:  registry,
	}, nil
}

// Pull implements Node.
func (m *mockNode) Pull(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pullCalls++
	return m.pullErr
}

// Run implements Node, recording the payload and "creating" a container that
// then answers healthchecks with the configured state.
func (m *mockNode) Run(_ context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCalls++
	m.requests = append(m.requests, req)
	if m.runErr != nil {
		return "", m.runErr
	}
	return m.containerID, nil
}

// Stop implements Node.
func (m *mockNode) Stop(_ context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls++
	m.stopped = append(m.stopped, containerID)
	return m.stopErr
}

// Start implements Node.
func (m *mockNode) Start(_ context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
	m.started = append(m.started, containerID)
	return m.startErr
}

// Containers implements Node, exposing the container Run created.
func (m *mockNode) Containers(_ context.Context) ([]*agentv1.ContainerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runCalls == 0 || m.runErr != nil {
		return nil, nil
	}
	return []*agentv1.ContainerInfo{{
		Id:     m.containerID,
		Name:   "gotham-app",
		Image:  "gotham/app",
		State:  m.healthState,
		Status: m.healthStatus,
	}}, nil
}

// Close implements Node.
func (m *mockNode) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return nil
}

// lastRequest returns the most recent Run payload.
func (m *mockNode) lastRequest() *agentv1.CreateContainerRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	return m.requests[len(m.requests)-1]
}

// recordPublisher captures published payloads for assertions.
type recordPublisher struct {
	mu       sync.Mutex
	events   []Event
	channels []string
}

// Compile-time guarantee that recordPublisher satisfies the seam.
var _ Publisher = (*recordPublisher)(nil)

// Publish implements Publisher.
func (p *recordPublisher) Publish(_ context.Context, channel, payload string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.channels = append(p.channels, channel)
	var event Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return err
	}
	p.events = append(p.events, event)
	return nil
}

// payloads returns every captured event.
func (p *recordPublisher) payloads() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Event(nil), p.events...)
}

// dialScript returns a DialFunc that fails the first n calls with err (or
// ErrAgentUnavailable) and then hands out node.
func dialScript(node Node, failures int, err error) (DialFunc, *int) {
	calls := new(int)
	if err == nil {
		err = ErrAgentUnavailable
	}
	return func(context.Context, uuid.UUID) (Node, error) {
		*calls++
		if *calls <= failures {
			return nil, err
		}
		return node, nil
	}, calls
}

// testApplication returns an application wired to a fake server and repo.
func testApplication(userID uuid.UUID) Application {
	return Application{
		ID:        uuid.New(),
		UserID:    userID,
		ServerID:  uuid.New(),
		Name:      "demo app",
		Provider:  "github",
		Repo:      "acme/demo",
		CloneURL:  "https://github.com/acme/demo.git",
		Branch:    "main",
		BuildPack: "dockerfile",
		Port:      3000,
		HostPort:  8080,
	}
}

// testEnv returns the standard environment fixture: one plain variable and a
// secret sealed with providers.SealSecret, the single crypto helper. It always
// seals with testSecretKey, the key newTestService wires the services with.
func testEnv(t *testing.T) ([]EnvVar, []Secret) {
	t.Helper()
	sealed, err := providers.SealSecret(testSecretKey, "hunter2")
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	return []EnvVar{{Key: "FOO", Value: "bar"}},
		[]Secret{{Key: "API_TOKEN", Ciphertext: sealed}}
}

// fakeHookLifecycle is a scriptable HookLifecycle. It records installs and
// removals, can fail a removal (the provider-outage case) and appends to a
// shared event log so a test can pin ordering against repository operations.
type fakeHookLifecycle struct {
	mu           sync.Mutex
	installCalls int
	removeCalls  int
	removeErr    error
	installedFor uuid.UUID
	removed      []uuid.UUID
	events       *[]string
}

// Compile-time guarantee that fakeHookLifecycle satisfies the seam.
var _ HookLifecycle = (*fakeHookLifecycle)(nil)

// InstallHook implements HookLifecycle.
func (f *fakeHookLifecycle) InstallHook(_ context.Context, _ uuid.UUID, appID uuid.UUID, _ *http.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installCalls++
	f.installedFor = appID
	return nil
}

// RemoveHook implements HookLifecycle.
func (f *fakeHookLifecycle) RemoveHook(_ context.Context, _ uuid.UUID, appID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCalls++
	f.removed = append(f.removed, appID)
	if f.events != nil {
		*f.events = append(*f.events, "hook removed")
	}
	return f.removeErr
}

// stallingHookLifecycle blocks every hook call until its context is done,
// simulating a provider that accepts the connection and then stalls. The
// recorded call counts let a test prove the call happened exactly once.
type stallingHookLifecycle struct {
	mu           sync.Mutex
	installCalls int
	removeCalls  int
}

// Compile-time guarantee that stallingHookLifecycle satisfies the seam.
var _ HookLifecycle = (*stallingHookLifecycle)(nil)

// InstallHook implements HookLifecycle.
func (f *stallingHookLifecycle) InstallHook(ctx context.Context, _, _ uuid.UUID, _ *http.Request) error {
	f.mu.Lock()
	f.installCalls++
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

// RemoveHook implements HookLifecycle.
func (f *stallingHookLifecycle) RemoveHook(ctx context.Context, _, _ uuid.UUID) error {
	f.mu.Lock()
	f.removeCalls++
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}
