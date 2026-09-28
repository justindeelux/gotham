package proxy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

// ProxiedApplication is one routing input row: an application that declares a
// base domain, together with the node it runs on and the endpoint of its
// newest running deployment.
type ProxiedApplication struct {
	// ID identifies the application (kept in generated names for traceability).
	ID uuid.UUID
	// ServerID is the node hosting the application; uuid.Nil means unassigned.
	ServerID uuid.UUID
	// BaseDomain is the raw stored domain, normalized and validated during
	// generation.
	BaseDomain string
	// Disabled marks a legacy duplicate binding disabled by migration 00012:
	// the value is preserved but never routed until its owner resolves the
	// conflict.
	Disabled bool
	// Port is the container port; HostPort the pinned host port, if any.
	// HostPort 0 means Docker assigned an ephemeral port that only the
	// running container's published bindings can reveal.
	Port     int32
	HostPort int32
	// ContainerID is the container of the newest running deployment, empty
	// when no deployment is running yet (the route is pending, not broken).
	ContainerID string
	// Certificate is the application's certificate intent, nil when none is
	// configured. It is a raw join of the stored state: activation is decided
	// during generation against the routable domain and the provider.
	Certificate *CertificateIntent
}

// CertificateIntent is the stored certificate configuration joined onto a
// routing row (BE-6.2). The referenced provider is resolved separately from
// the dns_providers rows during generation, so this carries the reference
// only.
type CertificateIntent struct {
	// Domain records the host the configuration was made for.
	Domain    string
	Enabled   bool
	Challenge ChallengeMode
	Wildcard  bool
	// DNSProviderID is uuid.Nil for HTTP-01.
	DNSProviderID uuid.UUID
}

// RedirectRule is one redirect rule joined with its application's node state
// for generation: the owning node decides which Traefik instance serves the
// rule, and DomainDisabled marks an application whose route is already held
// back by the domain-uniqueness conflict (its redirects are held back with
// it).
type RedirectRule struct {
	ID             uuid.UUID
	ApplicationID  uuid.UUID
	SourceDomain   string
	TargetDomain   string
	Code           int
	PreservePath   bool
	Enabled        bool
	ServerID       uuid.UUID
	DomainDisabled bool
}

// ApplicationSource lists the routing input. The production implementation is
// *store.Store (through storeSource); tests substitute a fake.
type ApplicationSource interface {
	ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error)
}

// Compose container labels used to resolve a service project's containers on
// the node. They are Docker Compose's own labels (set by the CLI on every
// container it creates), so no Gotham label has to be injected into the
// document.
const (
	// ComposeProjectLabel carries the compose project name.
	ComposeProjectLabel = "com.docker.compose.project"
	// ComposeServiceLabel carries the compose service name.
	ComposeServiceLabel = "com.docker.compose.service"
)

// ServiceDomain is one host a compose service declares through the Gotham
// label convention (internal/services owns the label names and validation).
type ServiceDomain struct {
	// Service is the compose service name the route must target.
	Service string
	// Host is the declared host; it is normalized and validated during
	// generation.
	Host string
	// Port is the container port the backend must reach.
	Port int32
}

// ProxiedService is one compose service routing input row: a service with at
// least one declared host, the node it runs on, its compose project name and
// its container-port mappings.
type ProxiedService struct {
	// ID identifies the service (kept in generated names for traceability).
	ID uuid.UUID
	// ServerID is the node hosting the project; uuid.Nil means unassigned.
	ServerID uuid.UUID
	// Name is the user-facing service name, used in diagnostics.
	Name string
	// Project is the compose project name ("gotham-<id>"), matched against
	// the containers' com.docker.compose.project label.
	Project string
	// Domains are the declared host mappings.
	Domains []ServiceDomain
	// Unroutable explains why a row that declares routing cannot be served
	// (for example a stored document that no longer renders); empty for a
	// healthy row.
	Unroutable string
}

// ServiceSource lists compose services with declared domains for generation.
// The production implementation is internal/services.ProxySource, which
// renders each stored document through the services package (the proxy cannot
// import it without a cycle); nil disables service routing, so every existing
// application-only wiring keeps working unchanged.
type ServiceSource interface {
	ListProxiedServices(ctx context.Context) ([]ProxiedService, error)
}

// RedirectSource lists the redirect rules joined to their application's node.
// The production implementation is *store.Store (through storeSource); nil
// disables redirect generation.
type RedirectSource interface {
	ListRedirectRules(ctx context.Context) ([]RedirectRule, error)
}

// DNSProviderSource lists the configured DNS providers for generation. The
// production implementation is *store.Store (through storeSource); nil
// disables DNS-01 resolvers and credentials.
type DNSProviderSource interface {
	ListDNSProviders(ctx context.Context) ([]DNSProvider, error)
}

// NodeSource lists every registered node. A global sync includes nodes that
// currently host no proxied application, so removing the last domain is
// repaired too (BE-6.1 F9).
type NodeSource interface {
	ListNodes(ctx context.Context) ([]uuid.UUID, error)
}

// storeSource adapts the shared store to the source seams.
type storeSource struct {
	store *store.Store
}

// ListProxiedApplications maps the stored application rows to routing input.
func (s storeSource) ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error) {
	rows, err := s.store.ListProxiedApplications(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]ProxiedApplication, 0, len(rows))
	for _, row := range rows {
		app := ProxiedApplication{
			ID:          uuidFromPG(row.ID),
			ServerID:    uuidFromPG(row.ServerID),
			BaseDomain:  row.BaseDomain,
			Disabled:    row.BaseDomainDisabled,
			Port:        row.Port,
			HostPort:    row.HostPort,
			ContainerID: row.ContainerID,
		}
		if row.CertificateConfigured {
			app.Certificate = &CertificateIntent{
				Domain:        row.CertificateDomain,
				Enabled:       row.CertificateEnabled,
				Challenge:     ChallengeMode(row.CertificateChallenge),
				Wildcard:      row.CertificateWildcard,
				DNSProviderID: uuidFromPG(row.CertificateDnsProviderID),
			}
		}
		apps = append(apps, app)
	}
	return apps, nil
}

// ListDNSProviders maps the stored provider rows to the generation input.
func (s storeSource) ListDNSProviders(ctx context.Context) ([]DNSProvider, error) {
	rows, err := s.store.ListDNSProviders(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]DNSProvider, 0, len(rows))
	for _, row := range rows {
		providers = append(providers, dnsProviderFromRow(row))
	}
	return providers, nil
}

// ListNodes maps the server registry to node ids.
func (s storeSource) ListNodes(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if id := uuidFromPG(row.ID); id != uuid.Nil {
			nodes = append(nodes, id)
		}
	}
	return nodes, nil
}

// ListRedirectRules maps the stored redirect rows joined to their
// application's node state.
func (s storeSource) ListRedirectRules(ctx context.Context) ([]RedirectRule, error) {
	rows, err := s.store.ListRedirectRules(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]RedirectRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, RedirectRule{
			ID:             uuidFromPG(row.ID),
			ApplicationID:  uuidFromPG(row.ApplicationID),
			SourceDomain:   row.SourceDomain,
			TargetDomain:   row.TargetDomain,
			Code:           int(row.Code),
			PreservePath:   row.PreservePath,
			Enabled:        row.Enabled,
			ServerID:       uuidFromPG(row.ServerID),
			DomainDisabled: row.BaseDomainDisabled,
		})
	}
	return rules, nil
}

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// pgUUID converts a uuid.UUID to the pgx type (invalid for uuid.Nil).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
