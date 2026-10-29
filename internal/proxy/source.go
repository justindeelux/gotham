package proxy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

type ProxiedDomain struct {
	Domain   string
	Port     int32
	Service  string
	Protocol string
	Disabled bool
}

type ProxiedApplication struct {
	ID           uuid.UUID
	Name         string
	ServerID     uuid.UUID
	Domains      []ProxiedDomain
	ContainerID  string
	Certificate  *CertificateIntent
	Certificates []*CertificateIntent
}

type CertificateIntent struct {
	Domain        string
	Enabled       bool
	Challenge     ChallengeMode
	Wildcard      bool
	DNSProviderID uuid.UUID
}

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

type ApplicationSource interface {
	ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error)
}

const (
	ComposeProjectLabel = "com.docker.compose.project"
	ComposeServiceLabel = "com.docker.compose.service"
)

type ServiceDomain struct {
	Service  string
	Host     string
	Port     int32
	Protocol string
}

type ProxiedService struct {
	ID         uuid.UUID
	ServerID   uuid.UUID
	Name       string
	Project    string
	Domains    []ServiceDomain
	Unroutable string
}

type ServiceSource interface {
	ListProxiedServices(ctx context.Context) ([]ProxiedService, error)
}

type RedirectSource interface {
	ListRedirectRules(ctx context.Context) ([]RedirectRule, error)
}

type DNSProviderSource interface {
	ListDNSProviders(ctx context.Context) ([]DNSProvider, error)
}

type NodeSource interface {
	ListNodes(ctx context.Context) ([]uuid.UUID, error)
}

type storeSource struct {
	store *store.Store
}

func (s storeSource) ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error) {
	rows, err := s.store.ListProxiedApplications(ctx)
	if err != nil {
		return nil, err
	}
	domains, err := s.store.ListProxiedApplicationDomains(ctx)
	if err != nil {
		return nil, err
	}
	intents, err := s.store.ListProxiedCertificates(ctx)
	if err != nil {
		return nil, err
	}
	domainsByApp := make(map[pgtype.UUID][]ProxiedDomain)
	for _, domain := range domains {
		if domain.Domain == "" {
			continue
		}
		key := domain.ApplicationID
		domainsByApp[key] = append(domainsByApp[key], ProxiedDomain{
			Domain:   domain.Domain,
			Port:     domain.Port,
			Service:  domain.Service,
			Protocol: NormalizeProtocol(domain.Protocol),
			Disabled: domain.Disabled,
		})
	}
	intentsByApp := make(map[pgtype.UUID][]*CertificateIntent)
	for _, intent := range intents {
		intentsByApp[intent.ApplicationID] = append(intentsByApp[intent.ApplicationID], &CertificateIntent{
			Domain:        intent.Domain,
			Enabled:       intent.Enabled,
			Challenge:     ChallengeMode(intent.Challenge),
			Wildcard:      intent.Wildcard,
			DNSProviderID: uuidFromPG(intent.DnsProviderID),
		})
	}
	apps := make([]ProxiedApplication, 0, len(rows))
	for _, row := range rows {
		key := row.ID
		app := ProxiedApplication{
			ID:          uuidFromPG(row.ID),
			ServerID:    uuidFromPG(row.ServerID),
			Name:        row.Name,
			ContainerID: row.ContainerID,
		}
		if owned, ok := domainsByApp[key]; ok {
			app.Domains = owned
		}
		if configured, ok := intentsByApp[key]; ok {
			app.Certificates = configured
			if len(configured) > 0 {
				app.Certificate = configured[0]
			}
		}
		apps = append(apps, app)
	}
	return apps, nil
}

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

func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
