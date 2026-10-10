package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeSSLStore is an in-memory SSLStore with the two reference guards the
// production schema enforces: one enabled provider per type and one
// certificate config per application.
type fakeSSLStore struct {
	mu            sync.Mutex
	providers     []DNSProvider
	certificates  []DomainCertificate
	applications  map[uuid.UUID]ApplicationInfo
	createErr     error
	updateErr     error
	deleteErr     error
	certCreateErr error
	certUpdateErr error
	certDeleteErr error
	getAppErr     error

	// countHook runs after CountEnabledCertificatesByProvider computes its
	// result, outside the store lock: tests use it to hold a mutation inside
	// its critical section.
	countHook func()

	lastProviderWrite DNSProviderWrite
	lastCertWrite     CertificateWrite
	metaUpdates       int
	rotations         int
}

func newFakeSSLStore() *fakeSSLStore {
	return &fakeSSLStore{applications: map[uuid.UUID]ApplicationInfo{}}
}

func (f *fakeSSLStore) CreateProvider(_ context.Context, in DNSProviderWrite) (DNSProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return DNSProvider{}, f.createErr
	}
	f.lastProviderWrite = in
	if in.Enabled {
		for _, provider := range f.providers {
			if provider.Enabled && provider.Provider == in.Provider {
				return DNSProvider{}, fmt.Errorf("%w: dns_providers_enabled_type_idx", ErrConflict)
			}
		}
	}
	provider := DNSProvider{
		ID:               uuid.New(),
		Provider:         in.Provider,
		Name:             in.Name,
		Zones:            append([]string{}, in.Zones...),
		Enabled:          in.Enabled,
		SealedCredential: in.SealedCredential,
	}
	f.providers = append(f.providers, provider)
	return provider, nil
}

func (f *fakeSSLStore) GetProvider(_ context.Context, id uuid.UUID) (DNSProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, provider := range f.providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return DNSProvider{}, ErrNotFound
}

func (f *fakeSSLStore) ListProviders(context.Context) ([]DNSProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DNSProvider{}, f.providers...), nil
}

func (f *fakeSSLStore) UpdateProvider(_ context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return DNSProvider{}, f.updateErr
	}
	f.lastProviderWrite = in
	f.rotations++
	for i, provider := range f.providers {
		if provider.ID != id {
			continue
		}
		updated := DNSProvider{
			ID:               id,
			Provider:         in.Provider,
			Name:             in.Name,
			Zones:            append([]string{}, in.Zones...),
			Enabled:          in.Enabled,
			SealedCredential: in.SealedCredential,
		}
		f.providers[i] = updated
		return updated, nil
	}
	return DNSProvider{}, ErrNotFound
}

// UpdateProviderMeta mirrors the credential-preserving production write: every
// mutable field except the sealed credential is replaced.
func (f *fakeSSLStore) UpdateProviderMeta(_ context.Context, id uuid.UUID, in DNSProviderWrite) (DNSProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return DNSProvider{}, f.updateErr
	}
	f.lastProviderWrite = in
	f.metaUpdates++
	for i, provider := range f.providers {
		if provider.ID != id {
			continue
		}
		updated := provider
		updated.Provider = in.Provider
		updated.Name = in.Name
		updated.Zones = append([]string{}, in.Zones...)
		updated.Enabled = in.Enabled
		f.providers[i] = updated
		return updated, nil
	}
	return DNSProvider{}, ErrNotFound
}

func (f *fakeSSLStore) DeleteProvider(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	for i, provider := range f.providers {
		if provider.ID == id {
			f.providers = append(f.providers[:i], f.providers[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeSSLStore) CountCertificatesByProvider(_ context.Context, providerID uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, cert := range f.certificates {
		if cert.DNSProviderID == providerID {
			count++
		}
	}
	return count, nil
}

func (f *fakeSSLStore) CountEnabledCertificatesByProvider(_ context.Context, providerID uuid.UUID) (int64, error) {
	f.mu.Lock()
	var count int64
	for _, cert := range f.certificates {
		if cert.DNSProviderID == providerID && cert.Enabled {
			count++
		}
	}
	f.mu.Unlock()
	if f.countHook != nil {
		f.countHook()
	}
	return count, nil
}

func (f *fakeSSLStore) ListEnabledCertificatesByProvider(_ context.Context, providerID uuid.UUID) ([]DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []DomainCertificate{}
	for _, cert := range f.certificates {
		if cert.DNSProviderID == providerID && cert.Enabled {
			out = append(out, cert)
		}
	}
	return out, nil
}

func (f *fakeSSLStore) CreateCertificate(_ context.Context, in CertificateWrite) (DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certCreateErr != nil {
		return DomainCertificate{}, f.certCreateErr
	}
	f.lastCertWrite = in
	for _, cert := range f.certificates {
		if cert.ApplicationID == in.ApplicationID && cert.Domain == in.Domain {
			return DomainCertificate{}, fmt.Errorf("%w: domain_certificates_app_domain_unique", ErrConflict)
		}
	}
	certificate := DomainCertificate{
		ID:            uuid.New(),
		ApplicationID: in.ApplicationID,
		Domain:        in.Domain,
		Enabled:       in.Enabled,
		Challenge:     in.Challenge,
		DNSProviderID: in.DNSProviderID,
		Wildcard:      in.Wildcard,
	}
	f.certificates = append(f.certificates, certificate)
	return certificate, nil
}

func (f *fakeSSLStore) GetCertificate(_ context.Context, id uuid.UUID) (DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cert := range f.certificates {
		if cert.ID == id {
			return cert, nil
		}
	}
	return DomainCertificate{}, ErrNotFound
}

func (f *fakeSSLStore) ListCertificatesByApplication(_ context.Context, applicationID uuid.UUID) ([]DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []DomainCertificate{}
	for _, cert := range f.certificates {
		if cert.ApplicationID == applicationID {
			out = append(out, cert)
		}
	}
	return out, nil
}

func (f *fakeSSLStore) ListCertificates(context.Context) ([]DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DomainCertificate{}, f.certificates...), nil
}

func (f *fakeSSLStore) UpdateCertificate(_ context.Context, id uuid.UUID, in CertificateWrite) (DomainCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certUpdateErr != nil {
		return DomainCertificate{}, f.certUpdateErr
	}
	f.lastCertWrite = in
	for i, cert := range f.certificates {
		if cert.ID != id {
			continue
		}
		updated := DomainCertificate{
			ID:            id,
			ApplicationID: in.ApplicationID,
			Domain:        in.Domain,
			Enabled:       in.Enabled,
			Challenge:     in.Challenge,
			DNSProviderID: in.DNSProviderID,
			Wildcard:      in.Wildcard,
		}
		f.certificates[i] = updated
		return updated, nil
	}
	return DomainCertificate{}, ErrNotFound
}

func (f *fakeSSLStore) DeleteCertificate(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certDeleteErr != nil {
		return f.certDeleteErr
	}
	for i, cert := range f.certificates {
		if cert.ID == id {
			f.certificates = append(f.certificates[:i], f.certificates[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeSSLStore) GetApplication(_ context.Context, id uuid.UUID) (ApplicationInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getAppErr != nil {
		return ApplicationInfo{}, f.getAppErr
	}
	app, ok := f.applications[id]
	if !ok {
		return ApplicationInfo{}, ErrNotFound
	}
	return app, nil
}

// newSSLServiceFixture wires an sslService over the fake store.
func newSSLServiceFixture(t *testing.T, resync func(context.Context) error) (*sslService, *fakeSSLStore) {
	t.Helper()
	store := newFakeSSLStore()
	service := newSSLService(SSLConfig{
		Store:  store,
		Secret: "test-key",
		Logger: discardLogger(),
		Resync: resync,
	})
	return service, store
}

// enableProvider seeds an enabled, sealed provider.
func enableProvider(t *testing.T, store *fakeSSLStore, zones ...string) DNSProvider {
	t.Helper()
	providerType := ProviderCloudflare
	sealed, err := sealCredential("test-key", "token-"+string(providerType))
	if err != nil {
		t.Fatalf("sealCredential: %v", err)
	}
	provider, err := store.CreateProvider(context.Background(), DNSProviderWrite{
		Provider:         providerType,
		Name:             string(providerType),
		Zones:            zones,
		SealedCredential: sealed,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return provider
}

func TestProviderCreateSealsCredential(t *testing.T) {
	resyncCalls := 0
	service, _ := newSSLServiceFixture(t, func(context.Context) error {
		resyncCalls++
		return nil
	})

	provider, err := service.CreateProvider(context.Background(), CreateDNSProviderInput{
		Provider:   ProviderCloudflare,
		Name:       "  prod  ",
		Zones:      []string{" Example.com ", "example.org"},
		Credential: "cf-secret-token",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if provider.Provider != ProviderCloudflare || provider.Name != "prod" {
		t.Fatalf("provider = %#v", provider)
	}
	if len(provider.Zones) != 2 || provider.Zones[0] != "example.com" || provider.Zones[1] != "example.org" {
		t.Fatalf("zones = %v, want normalized and sorted", provider.Zones)
	}
	if !provider.Enabled {
		t.Fatal("providers default to enabled")
	}
	if provider.SealedCredential == "cf-secret-token" || provider.SealedCredential == "" {
		t.Fatalf("credential was not sealed: %q", provider.SealedCredential)
	}
	opened, err := openCredential("test-key", provider.SealedCredential)
	if err != nil || opened != "cf-secret-token" {
		t.Fatalf("sealed credential does not round-trip: (%q, %v)", opened, err)
	}
	if resyncCalls != 1 {
		t.Fatalf("resync calls = %d, want 1", resyncCalls)
	}
}

func TestProviderCreateValidation(t *testing.T) {
	longName := strings.Repeat("n", maxProviderName+1)
	tooManyZones := make([]string, 0, MaxProviderZones+1)
	for i := 0; i <= MaxProviderZones; i++ {
		tooManyZones = append(tooManyZones, fmt.Sprintf("z%d.example.com", i))
	}
	cases := []struct {
		name string
		in   CreateDNSProviderInput
	}{
		{name: "unknown provider", in: CreateDNSProviderInput{Provider: "route53", Zones: []string{"example.com"}, Credential: "t"}},
		{name: "empty credential", in: CreateDNSProviderInput{Provider: ProviderCloudflare, Zones: []string{"example.com"}}},
		{name: "no zones", in: CreateDNSProviderInput{Provider: ProviderCloudflare, Credential: "t"}},
		{name: "invalid zone", in: CreateDNSProviderInput{Provider: ProviderCloudflare, Zones: []string{"*.example.com"}, Credential: "t"}},
		{name: "too many zones", in: CreateDNSProviderInput{Provider: ProviderCloudflare, Zones: tooManyZones, Credential: "t"}},
		{name: "name too long", in: CreateDNSProviderInput{Provider: ProviderCloudflare, Name: longName, Zones: []string{"example.com"}, Credential: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := newSSLServiceFixture(t, nil)
			if _, err := service.CreateProvider(context.Background(), tc.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

// TestProviderCreateRefusesEmptySecret proves a DNS credential write is
// refused when no deployment secret is configured, and that nothing is
// persisted under the public empty-string key.
func TestProviderCreateRefusesEmptySecret(t *testing.T) {
	store := newFakeSSLStore()
	service := newSSLService(SSLConfig{Store: store, Logger: discardLogger()})

	_, err := service.CreateProvider(context.Background(), CreateDNSProviderInput{
		Provider:   ProviderCloudflare,
		Zones:      []string{"example.com"},
		Credential: "cf-token",
	})
	if !errors.Is(err, ErrSecret) {
		t.Fatalf("CreateProvider err = %v, want ErrSecret", err)
	}
	stored, listErr := store.ListProviders(context.Background())
	if listErr != nil {
		t.Fatalf("ListProviders: %v", listErr)
	}
	if len(stored) != 0 {
		t.Fatalf("credential row persisted without a deployment secret: %#v", stored)
	}
}

// TestProviderRotationRefusesEmptySecret proves a rotation through a service
// without a deployment secret is refused and leaves the previously sealed
// value (sealed with the real key) untouched.
func TestProviderRotationRefusesEmptySecret(t *testing.T) {
	_, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")

	empty := newSSLService(SSLConfig{Store: store, Logger: discardLogger()})
	if _, err := empty.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{
		Credential: ptr("replacement"),
	}); !errors.Is(err, ErrSecret) {
		t.Fatalf("rotation err = %v, want ErrSecret", err)
	}
	stored, err := store.GetProvider(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	plain, err := openCredential("test-key", stored.SealedCredential)
	if err != nil || plain != "token-cloudflare" {
		t.Fatalf("stored credential = (%q, %v), want the untouched original", plain, err)
	}
}

// TestProviderUpdateWithoutCredentialPreservesSealedCredential proves a
// name-only update uses the credential-preserving write, so it can never
// restore an older sealed value over a rotation.
func TestProviderUpdateWithoutCredentialPreservesSealedCredential(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{
		Credential: ptr("rotated-token"),
	}); err != nil {
		t.Fatalf("rotation: %v", err)
	}

	updated, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{
		Name: ptr("renamed"),
	})
	if err != nil {
		t.Fatalf("name-only update: %v", err)
	}
	if store.rotations != 1 || store.metaUpdates != 1 {
		t.Fatalf("writes: rotations=%d meta=%d, want 1/1", store.rotations, store.metaUpdates)
	}
	if updated.Name != "renamed" {
		t.Fatalf("name = %q, want the update applied", updated.Name)
	}
	plain, err := openCredential("test-key", updated.SealedCredential)
	if err != nil || plain != "rotated-token" {
		t.Fatalf("sealed credential = (%q, %v), want the rotated value preserved", plain, err)
	}
}

// TestSSLProviderAndCertificateMutationsAreSerialized is the controlled
// interleaving regression for the reference invariant: while a provider
// disable holds the shared mutation boundary (paused inside its reference
// guard), a concurrent certificate create must not slip between the guard read
// and the provider write. Without the shared boundary the create would see an
// enabled provider, commit an enabled certificate, and then the disable would
// commit — leaving an enabled certificate referencing a disabled provider.
func TestSSLProviderAndCertificateMutationsAreSerialized(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}

	guardRead := make(chan struct{})
	releaseGuard := make(chan struct{})
	var once sync.Once
	store.countHook = func() {
		once.Do(func() {
			close(guardRead)
			<-releaseGuard
		})
	}
	t.Cleanup(func() { once.Do(func() { close(releaseGuard) }) })

	disableDone := make(chan error, 1)
	go func() {
		_, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Enabled: ptr(false)})
		disableDone <- err
	}()
	select {
	case <-guardRead:
	case <-time.After(5 * time.Second):
		t.Fatal("provider disable never reached its reference guard")
	}

	createDone := make(chan error, 1)
	go func() {
		_, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
			ApplicationID: appID,
			Challenge:     ChallengeDNS01,
			DNSProviderID: provider.ID,
		})
		createDone <- err
	}()
	select {
	case err := <-createDone:
		close(releaseGuard)
		<-disableDone
		t.Fatalf("certificate create interleaved with the provider guard (err=%v); the shared mutation boundary is missing", err)
	case <-time.After(200 * time.Millisecond):
		// Serialized as required: the create is blocked behind the provider
		// mutation's critical section.
	}

	close(releaseGuard)
	if err := <-disableDone; err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := <-createDone; !errors.Is(err, ErrValidation) {
		t.Fatalf("create after the disable = %v, want ErrValidation (provider disabled)", err)
	}
	certificates, err := store.ListCertificates(context.Background())
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(certificates) != 0 {
		t.Fatalf("inconsistent state: %d enabled certificate(s) reference a disabled provider", len(certificates))
	}
}

// TestCertificateUpdateEnablingIsSerializedWithProviderGuard proves the same
// boundary covers PATCH: enabling a certificate config validates the provider
// inside the critical section.
func TestCertificateUpdateEnablingIsSerializedWithProviderGuard(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}
	certificate, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
		ApplicationID: appID,
		Enabled:       ptr(false),
		Challenge:     ChallengeDNS01,
		DNSProviderID: provider.ID,
	})
	if err != nil {
		t.Fatalf("create disabled certificate: %v", err)
	}

	guardRead := make(chan struct{})
	releaseGuard := make(chan struct{})
	var once sync.Once
	store.countHook = func() {
		once.Do(func() {
			close(guardRead)
			<-releaseGuard
		})
	}
	t.Cleanup(func() { once.Do(func() { close(releaseGuard) }) })

	disableDone := make(chan error, 1)
	go func() {
		_, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Enabled: ptr(false)})
		disableDone <- err
	}()
	select {
	case <-guardRead:
	case <-time.After(5 * time.Second):
		t.Fatal("provider disable never reached its reference guard")
	}

	enableDone := make(chan error, 1)
	go func() {
		_, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{Enabled: ptr(true)})
		enableDone <- err
	}()
	select {
	case err := <-enableDone:
		close(releaseGuard)
		<-disableDone
		t.Fatalf("certificate enable interleaved with the provider guard (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(releaseGuard)
	if err := <-disableDone; err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := <-enableDone; !errors.Is(err, ErrValidation) {
		t.Fatalf("enable after the disable = %v, want ErrValidation", err)
	}
	certificates, _ := store.ListCertificates(context.Background())
	if certificates[0].Enabled {
		t.Fatal("inconsistent state: enabled certificate references a disabled provider")
	}
}

func TestProviderCreateRejectsSecondEnabledType(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	enableProvider(t, store, "example.com")
	_, err := service.CreateProvider(context.Background(), CreateDNSProviderInput{
		Provider:   ProviderCloudflare,
		Zones:      []string{"other.com"},
		Credential: "token",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a second enabled provider of the same type", err)
	}
}

func TestProviderUpdateRotatesCredential(t *testing.T) {
	resyncCalls := 0
	service, store := newSSLServiceFixture(t, func(context.Context) error {
		resyncCalls++
		return nil
	})
	provider := enableProvider(t, store, "example.com")

	updated, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{
		Credential: ptr("rotated-token"),
	})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	opened, err := openCredential("test-key", updated.SealedCredential)
	if err != nil || opened != "rotated-token" {
		t.Fatalf("credential not rotated: (%q, %v)", opened, err)
	}
	if resyncCalls != 1 {
		t.Fatalf("resync calls = %d, want 1", resyncCalls)
	}
}

func TestProviderUpdateGuardsReferencedProvider(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}
	if _, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
		ApplicationID: appID,
		Challenge:     ChallengeDNS01,
		DNSProviderID: provider.ID,
		Wildcard:      true,
	}); err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Enabled: ptr(false)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("disable err = %v, want ErrConflict", err)
	}
	providerType := ProviderDigitalOcean
	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Provider: &providerType}); !errors.Is(err, ErrConflict) {
		t.Fatalf("type change err = %v, want ErrConflict", err)
	}
	zones := []string{"other.example"}
	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Zones: &zones}); !errors.Is(err, ErrConflict) {
		t.Fatalf("zone narrowing err = %v, want ErrConflict", err)
	}
	// A zone widening keeps the referenced domain covered and is allowed.
	zones = []string{"other.example", "example.com"}
	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Zones: &zones}); err != nil {
		t.Fatalf("zone widening err = %v", err)
	}

	// Once the certificate config is disabled the provider can be disabled.
	certificates, _ := store.ListCertificates(context.Background())
	if _, err := service.UpdateCertificate(context.Background(), certificates[0].ID, UpdateCertificateInput{Enabled: ptr(false)}); err != nil {
		t.Fatalf("disable certificate: %v", err)
	}
	if _, err := service.UpdateProvider(context.Background(), provider.ID, UpdateDNSProviderInput{Enabled: ptr(false)}); err != nil {
		t.Fatalf("disable unreferenced provider: %v", err)
	}
}

func TestProviderDeleteGuard(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}
	if _, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
		ApplicationID: appID,
		Challenge:     ChallengeDNS01,
		DNSProviderID: provider.ID,
	}); err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	if err := service.DeleteProvider(context.Background(), provider.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete err = %v, want ErrConflict while referenced", err)
	}
	certificates, _ := store.ListCertificates(context.Background())
	if err := service.DeleteCertificate(context.Background(), certificates[0].ID); err != nil {
		t.Fatalf("delete certificate: %v", err)
	}
	if err := service.DeleteProvider(context.Background(), provider.ID); err != nil {
		t.Fatalf("delete unreferenced provider: %v", err)
	}
	if _, err := service.GetProvider(context.Background(), provider.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted provider err = %v, want ErrNotFound", err)
	}
}

func TestCertificateCreateValidation(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")

	appWithDomain := uuid.New()
	store.applications[appWithDomain] = ApplicationInfo{ID: appWithDomain, BaseDomain: "app.example.com"}
	appWithoutDomain := uuid.New()
	store.applications[appWithoutDomain] = ApplicationInfo{ID: appWithoutDomain}
	appDisabledDomain := uuid.New()
	store.applications[appDisabledDomain] = ApplicationInfo{ID: appDisabledDomain, BaseDomain: "legacy.example.com", DomainDisabled: true}
	appOutsideZone := uuid.New()
	store.applications[appOutsideZone] = ApplicationInfo{ID: appOutsideZone, BaseDomain: "app.other.example"}

	cases := []struct {
		name string
		in   CreateCertificateInput
		want error
	}{
		{name: "missing application id", in: CreateCertificateInput{}, want: ErrValidation},
		{name: "unknown application", in: CreateCertificateInput{ApplicationID: uuid.New()}, want: ErrNotFound},
		{name: "application without domain", in: CreateCertificateInput{ApplicationID: appWithoutDomain}, want: ErrValidation},
		{name: "application domain disabled", in: CreateCertificateInput{ApplicationID: appDisabledDomain}, want: ErrValidation},
		{name: "unknown challenge", in: CreateCertificateInput{ApplicationID: appWithDomain, Challenge: "tls-alpn-01"}, want: ErrValidation},
		{name: "wildcard with http-01", in: CreateCertificateInput{ApplicationID: appWithDomain, Challenge: ChallengeHTTP01, Wildcard: true}, want: ErrValidation},
		{name: "http-01 with provider", in: CreateCertificateInput{ApplicationID: appWithDomain, Challenge: ChallengeHTTP01, DNSProviderID: provider.ID}, want: ErrValidation},
		{name: "dns-01 without provider", in: CreateCertificateInput{ApplicationID: appWithDomain, Challenge: ChallengeDNS01}, want: ErrValidation},
		{name: "dns-01 with unknown provider", in: CreateCertificateInput{ApplicationID: appWithDomain, Challenge: ChallengeDNS01, DNSProviderID: uuid.New()}, want: ErrValidation},
		{name: "domain outside provider zones", in: CreateCertificateInput{ApplicationID: appOutsideZone, Challenge: ChallengeDNS01, DNSProviderID: provider.ID, Wildcard: true}, want: ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.CreateCertificate(context.Background(), tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCertificateCreateRejectsDisabledProviderWhenEnabled(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	provider := enableProvider(t, store, "example.com")
	store.providers[0].Enabled = false

	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}
	if _, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
		ApplicationID: appID,
		Challenge:     ChallengeDNS01,
		DNSProviderID: provider.ID,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for a disabled provider", err)
	}

	// A disabled certificate config may reference the disabled provider.
	certificate, err := service.CreateCertificate(context.Background(), CreateCertificateInput{
		ApplicationID: appID,
		Enabled:       ptr(false),
		Challenge:     ChallengeDNS01,
		DNSProviderID: provider.ID,
	})
	if err != nil {
		t.Fatalf("disabled certificate with a disabled provider: %v", err)
	}
	if certificate.Enabled {
		t.Fatal("certificate should be disabled")
	}
}

func TestCertificateCreateRecordsApplicationDomain(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "  App.Example.COM "}

	certificate, err := service.CreateCertificate(context.Background(), CreateCertificateInput{ApplicationID: appID})
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	if certificate.Domain != "app.example.com" {
		t.Fatalf("recorded domain = %q, want the normalized application domain", certificate.Domain)
	}
	if certificate.Challenge != ChallengeHTTP01 {
		t.Fatalf("challenge = %q, want the http-01 default", certificate.Challenge)
	}
	if !certificate.Enabled {
		t.Fatal("certificate configs default to enabled")
	}
	if certificate.DNSProviderID != uuid.Nil {
		t.Fatalf("dns provider = %s, want none for http-01", certificate.DNSProviderID)
	}
}

func TestCertificateCreateRejectsSecondConfigForApplication(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}
	if _, err := service.CreateCertificate(context.Background(), CreateCertificateInput{ApplicationID: appID}); err != nil {
		t.Fatalf("first CreateCertificate: %v", err)
	}
	if _, err := service.CreateCertificate(context.Background(), CreateCertificateInput{ApplicationID: appID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreateCertificate err = %v, want ErrConflict", err)
	}
}

func TestCertificateUpdateReRecordsDomainAndValidates(t *testing.T) {
	service, store := newSSLServiceFixture(t, nil)
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "old.example.com"}
	certificate, err := service.CreateCertificate(context.Background(), CreateCertificateInput{ApplicationID: appID})
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	// The deploy API changes the application domain: the stored config stays
	// put until an explicit re-target, and any other edit is rejected while
	// the recorded host is detached.
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "new.example.com"}
	if _, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{Enabled: ptr(true)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateCertificate on a detached domain err = %v, want ErrValidation", err)
	}
	updated, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{
		Domain:  ptr("new.example.com"),
		Enabled: ptr(true),
	})
	if err != nil {
		t.Fatalf("UpdateCertificate: %v", err)
	}
	if updated.Domain != "new.example.com" {
		t.Fatalf("recorded domain = %q, want the re-targeted application domain", updated.Domain)
	}

	// Switching to dns-01 without a provider is rejected; switching back to
	// http-01 drops a previously referenced provider.
	provider := enableProvider(t, store, "new.example.com")
	if _, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{
		Challenge:     ptrChallenge(ChallengeDNS01),
		DNSProviderID: ptrUUID(provider.ID),
	}); err != nil {
		t.Fatalf("switch to dns-01: %v", err)
	}
	reverted, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{
		Challenge: ptrChallenge(ChallengeHTTP01),
	})
	if err != nil {
		t.Fatalf("switch back to http-01: %v", err)
	}
	if reverted.DNSProviderID != uuid.Nil || reverted.Wildcard {
		t.Fatalf("http-01 kept provider/wildcard state: %#v", reverted)
	}
}

func TestCertificateMutationsTriggerResync(t *testing.T) {
	resyncCalls := 0
	service, store := newSSLServiceFixture(t, func(context.Context) error {
		resyncCalls++
		return nil
	})
	appID := uuid.New()
	store.applications[appID] = ApplicationInfo{ID: appID, BaseDomain: "app.example.com"}

	certificate, err := service.CreateCertificate(context.Background(), CreateCertificateInput{ApplicationID: appID})
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	if _, err := service.UpdateCertificate(context.Background(), certificate.ID, UpdateCertificateInput{Enabled: ptr(false)}); err != nil {
		t.Fatalf("UpdateCertificate: %v", err)
	}
	if err := service.DeleteCertificate(context.Background(), certificate.ID); err != nil {
		t.Fatalf("DeleteCertificate: %v", err)
	}
	if resyncCalls != 3 {
		t.Fatalf("resync calls = %d, want one per committed mutation", resyncCalls)
	}
}

func TestSSLServiceResyncNeverFailsTheMutation(t *testing.T) {
	service, store := newSSLServiceFixture(t, func(context.Context) error {
		return errors.New("node unreachable")
	})
	if _, err := service.CreateProvider(context.Background(), CreateDNSProviderInput{
		Provider:   ProviderCloudflare,
		Zones:      []string{"example.com"},
		Credential: "token",
	}); err != nil {
		t.Fatalf("CreateProvider with a failing resync: %v", err)
	}
	providers, _ := store.ListProviders(context.Background())
	if len(providers) != 1 {
		t.Fatalf("providers = %d, want the mutation committed despite the resync failure", len(providers))
	}
}

func TestNewDefaultSSLServicesRespectFeatureFlag(t *testing.T) {
	store := newFakeSSLStore()
	if service := NewDefaultProviderService(SSLConfig{Store: nil}); service != nil {
		t.Fatal("provider service must be nil without a store")
	}
	if service := NewDefaultCertificateService(SSLConfig{Store: nil}); service != nil {
		t.Fatal("certificate service must be nil without a store")
	}
	t.Setenv(FeatureEnv, "false")
	if service := NewDefaultProviderService(SSLConfig{Store: store}); service != nil {
		t.Fatal("provider service must be nil while the feature is disabled")
	}
	if service := NewDefaultCertificateService(SSLConfig{Store: store}); service != nil {
		t.Fatal("certificate service must be nil while the feature is disabled")
	}
}

// ptr is a small helper for optional update fields.
func ptr[T any](value T) *T { return &value }

// ptrChallenge and ptrUUID keep the call sites readable.
func ptrChallenge(value ChallengeMode) *ChallengeMode { return &value }

func ptrUUID(value uuid.UUID) *uuid.UUID { return &value }
