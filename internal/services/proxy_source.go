package services

import (
	"context"
	"fmt"
	"strings"

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
	proxied := make([]proxy.ProxiedService, 0, len(rows))
	for _, row := range rows {
		service, err := serviceFromRow(row)
		if err != nil {
			return nil, err
		}
		entry := proxy.ProxiedService{
			ID:       service.ID,
			ServerID: service.ServerID,
			Name:     service.Name,
			Project:  ProjectName(service.ID),
		}
		rendered, err := Render(service.ComposeYAML, service.Env)
		if err != nil {
			if strings.Contains(service.ComposeYAML, LabelDomain) {
				entry.Unroutable = "the stored document does not render: " + Redact(err.Error(), service.Env)
				proxied = append(proxied, entry)
			}
			continue
		}
		for _, route := range rendered.Spec.Domains {
			entry.Domains = append(entry.Domains, proxy.ServiceDomain{
				Service: route.Service,
				Host:    route.Domain,
				Port:    route.Port,
			})
		}
		if len(entry.Domains) == 0 {
			continue
		}
		proxied = append(proxied, entry)
	}
	return proxied, nil
}
