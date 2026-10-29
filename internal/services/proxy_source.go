package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/store"
)

type ProxySource struct {
	store *store.Store
}

var _ proxy.ServiceSource = (*ProxySource)(nil)

func NewProxySource(st *store.Store) *ProxySource {
	return &ProxySource{store: st}
}

func (s *ProxySource) ListProxiedServices(ctx context.Context) ([]proxy.ProxiedService, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	rows, err := s.store.ListRoutableServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("services: list routable services: %w", err)
	}
	managed, err := s.store.ListProxiedServiceDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("services: list proxied service domains: %w", err)
	}
	managedByService := make(map[pgtype.UUID][]proxy.ServiceDomain, len(rows))
	for _, row := range managed {
		managedByService[row.ServiceID] = append(managedByService[row.ServiceID], proxy.ServiceDomain{
			Service:  row.ComposeService,
			Host:     row.Domain,
			Port:     row.Port,
			Protocol: proxy.NormalizeProtocol(row.Protocol),
		})
	}
	proxied := make([]proxy.ProxiedService, 0, len(rows))
	for _, row := range rows {
		service, err := serviceFromRow(row)
		if err != nil {
			return nil, err
		}
		domains := managedByService[row.ID]
		entry := proxy.ProxiedService{
			ID:       service.ID,
			ServerID: service.ServerID,
			Name:     service.Name,
			Project:  ProjectName(service.ID),
			Domains:  domains,
		}
		rendered, _, err := RenderService(service.ComposeYAML, service.Env, managedDomains(domains))
		if err != nil {
			if len(domains) > 0 || strings.Contains(service.ComposeYAML, LabelDomain) {
				entry.Domains = nil
				entry.Unroutable = "the stored document does not render: " + Redact(err.Error(), service.Env)
				proxied = append(proxied, entry)
			}
			continue
		}
		for _, route := range rendered.Spec.Domains {
			entry.Domains = append(entry.Domains, proxy.ServiceDomain{
				Service:  route.Service,
				Host:     route.Domain,
				Port:     route.Port,
				Protocol: proxy.ProtocolHTTP,
			})
		}
		if len(entry.Domains) == 0 {
			continue
		}
		proxied = append(proxied, entry)
	}
	return proxied, nil
}

func managedDomains(domains []proxy.ServiceDomain) []ServiceDomain {
	converted := make([]ServiceDomain, 0, len(domains))
	for _, domain := range domains {
		converted = append(converted, ServiceDomain{
			ComposeService: domain.Service,
			Port:           domain.Port,
			Domain:         domain.Host,
			Protocol:       domain.Protocol,
		})
	}
	return converted
}
