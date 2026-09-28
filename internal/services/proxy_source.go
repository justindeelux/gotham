package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/store"
)

// ProxySource adapts the stored compose services to the Phase 6 proxy routing
// input. The routing state is derived on every read from each service's
// current document and environment, so a redeploy, an edit, a stop or a
// delete is reflected without storing the domain map twice.
//
// It is plugged into proxy.Config.Services by the HTTP wiring; the proxy
// package cannot render compose documents itself without an import cycle.
type ProxySource struct {
	store *store.Store
}

// Compile-time guarantee that ProxySource satisfies the proxy seam.
var _ proxy.ServiceSource = (*ProxySource)(nil)

// NewProxySource builds the adapter over the shared store.
func NewProxySource(st *store.Store) *ProxySource {
	return &ProxySource{store: st}
}

// ListProxiedServices implements proxy.ServiceSource. A service whose stored
// document does not render is reported as unroutable (not silently dropped)
// when the document mentions the routing label, so an operator sees why a host
// stopped being served; the error message is redacted of environment values.
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
			// Only a document that declares routing produces a diagnostic;
			// a broken document with no routing label has nothing to report.
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
