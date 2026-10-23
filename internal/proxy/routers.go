package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

type RouterKind string

const (
	RouterKindControlPlane RouterKind = "control_plane"
	RouterKindApplication  RouterKind = "application"
	RouterKindService      RouterKind = "service"
	RouterKindRedirect     RouterKind = "redirect"
)

type RouterSyncStatus string

const (
	RouterSyncSynced  RouterSyncStatus = "synced"
	RouterSyncPending RouterSyncStatus = "pending"
	RouterSyncUnknown RouterSyncStatus = "unknown"
)

type RouterInfo struct {
	Certificate *PanelCertificateState `json:"certificate,omitempty"`
	Host        string                 `json:"host"`
	Rule        string                 `json:"rule"`
	Service     string                 `json:"service"`
	Target      string                 `json:"target,omitempty"`
	EntryPoints []string               `json:"entrypoints"`
	Middlewares []string               `json:"middlewares,omitempty"`
	TLSResolver string                 `json:"tls_resolver,omitempty"`
	Kind        RouterKind             `json:"kind"`
	OwnerID     uuid.UUID              `json:"owner_id"`
	OwnerName   string                 `json:"owner_name,omitempty"`
	ServerID    uuid.UUID              `json:"server_id"`
	ServerName  string                 `json:"server_name,omitempty"`
}

type RouterNodeState struct {
	ServerID    uuid.UUID        `json:"server_id"`
	ServerName  string           `json:"server_name,omitempty"`
	SyncStatus  RouterSyncStatus `json:"sync_status"`
	Error       string           `json:"error,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
}

type RouterListResponse struct {
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Routers     []RouterInfo      `json:"routers"`
	Nodes       []RouterNodeState `json:"nodes"`
}

type RouterService interface {
	ListRouters(ctx context.Context, serverID uuid.UUID) (RouterListResponse, error)
}

var _ RouterService = (*SyncService)(nil)

func AsRouterService(svc ProxyService) RouterService {
	if routers, ok := svc.(RouterService); ok {
		return routers
	}
	return nil
}

type NamedNode struct {
	ID   uuid.UUID
	Name string
}

type namedNodeSource interface {
	ListNamedNodes(ctx context.Context) ([]NamedNode, error)
}

func (s storeSource) ListNamedNodes(ctx context.Context) ([]NamedNode, error) {
	rows, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]NamedNode, 0, len(rows))
	for _, row := range rows {
		if id := uuidFromPG(row.ID); id != uuid.Nil {
			nodes = append(nodes, NamedNode{ID: id, Name: row.Name})
		}
	}
	return nodes, nil
}

func (s *SyncService) ListRouters(ctx context.Context, serverID uuid.UUID) (RouterListResponse, error) {
	if s == nil || s.source == nil {
		return RouterListResponse{}, errors.New("proxy: application source is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return RouterListResponse{}, fmt.Errorf("proxy: list proxied applications: %w", err)
	}
	proxiedServices, err := s.listServices(ctx)
	if err != nil {
		return RouterListResponse{}, fmt.Errorf("proxy: list proxied services: %w", err)
	}
	rules, err := s.listRedirectRules(ctx)
	if err != nil {
		return RouterListResponse{}, fmt.Errorf("proxy: list redirect rules: %w", err)
	}
	providers, err := s.listProviders(ctx)
	if err != nil {
		return RouterListResponse{}, fmt.Errorf("proxy: list dns providers: %w", err)
	}
	ids, names, err := s.routerNodeSet(ctx, apps, serverID)
	if err != nil {
		return RouterListResponse{}, err
	}
	access := s.openProviders(providers)
	appNames := applicationNames(apps)
	serviceNames := proxiedServiceNames(proxiedServices)

	response := RouterListResponse{}
	if serverID == uuid.Nil {
		_, response.Diagnostics = s.controlPlaneRoute(ctx, uuid.Nil, nil, access)
	}
	for _, id := range ids {
		state := RouterNodeState{ServerID: id, ServerName: names[id], SyncStatus: RouterSyncUnknown}
		routers, nodeState, err := s.listNodeRouters(ctx, apps, proxiedServices, rules, providers, access, appNames, serviceNames, id, names[id])
		if err != nil {
			state.Error = err.Error()
			if serverID != uuid.Nil {
				return RouterListResponse{}, err
			}
			response.Nodes = append(response.Nodes, state)
			continue
		}
		state.SyncStatus = nodeState.SyncStatus
		state.Diagnostics = nodeState.Diagnostics
		response.Routers = append(response.Routers, routers...)
		response.Nodes = append(response.Nodes, state)
	}
	sortRouters(response.Routers)
	return response, nil
}

func (s *SyncService) listNodeRouters(ctx context.Context, apps []ProxiedApplication, proxiedServices []ProxiedService, rules []RedirectRule, providers []DNSProvider, access map[uuid.UUID]providerAccess, appNames, serviceNames map[uuid.UUID]string, serverID uuid.UUID, serverName string) ([]RouterInfo, RouterNodeState, error) {
	state := RouterNodeState{ServerID: serverID, ServerName: serverName, SyncStatus: RouterSyncUnknown}
	nodeList, err := s.listNodeContainers(ctx, serverID)
	if err != nil {
		return nil, state, mapNodeError(err)
	}
	routes, redirects, diagnostics, appCount := s.gatherRoutes(ctx, apps, proxiedServices, rules, serverID, nodeList, access)
	cfg := BuildConfig(routes, redirects, providers, s.acmeEmail, s.caServer)
	files, err := Generate(cfg, s.format)
	if err != nil {
		return nil, state, err
	}
	routers := routersFromConfig(cfg, routes, appCount, redirects, redirectOwners(rules, serverID), serverID, serverName, appNames, serviceNames)
	state.Diagnostics = diagnostics
	state.SyncStatus = s.routerSyncStatus(ctx, serverID, files)
	return routers, state, nil
}

func routersFromConfig(cfg ProxyConfig, routes []Route, appRoutes int, redirects []Redirect, ruleOwners map[uuid.UUID]uuid.UUID, serverID uuid.UUID, serverName string, appNames, serviceNames map[uuid.UUID]string) []RouterInfo {
	routers := make([]RouterInfo, 0, len(routes)+len(redirects))
	for i, route := range routes {
		base := route.Name
		if base == "" {
			base = serviceName(route.AppID)
		}
		web, ok := cfg.Routers[base+"-web"]
		if !ok {
			continue
		}
		row := RouterInfo{
			Host:        route.Domain,
			Certificate: route.PanelCertificate,
			Rule:        web.Rule,
			Service:     web.Service,
			Target:      route.Target,
			EntryPoints: append([]string{}, web.EntryPoints...),
			Middlewares: append([]string{}, web.Middlewares...),
			Kind:        RouterKindApplication,
			OwnerID:     route.AppID,
			OwnerName:   appNames[route.AppID],
			ServerID:    serverID,
			ServerName:  serverName,
		}
		if i >= appRoutes {
			row.Kind = RouterKindService
			row.OwnerName = serviceNames[route.AppID]
		}
		if route.Name == "panel" || route.Name == "panel-previous" {
			row.Kind = RouterKindControlPlane
			row.OwnerName = "Gotham"
		}
		if websecure, ok := cfg.Routers[base+"-websecure"]; ok {
			row.EntryPoints = append(row.EntryPoints, websecure.EntryPoints...)
			for _, middleware := range websecure.Middlewares {
				if !containsName(row.Middlewares, middleware) {
					row.Middlewares = append(row.Middlewares, middleware)
				}
			}
			if websecure.TLS != nil {
				row.TLSResolver = websecure.TLS.CertResolver
			}
		}
		routers = append(routers, row)
	}
	for _, redirect := range redirects {
		router, ok := cfg.Routers[RedirectNamePrefix+redirect.ID.String()]
		if !ok {
			continue
		}
		routers = append(routers, RouterInfo{
			Host:        redirect.Source,
			Rule:        router.Rule,
			Service:     router.Service,
			Target:      "https://" + redirect.Target,
			EntryPoints: append([]string{}, router.EntryPoints...),
			Middlewares: append([]string{}, router.Middlewares...),
			Kind:        RouterKindRedirect,
			OwnerID:     redirect.ID,
			OwnerName:   appNames[ruleOwners[redirect.ID]],
			ServerID:    serverID,
			ServerName:  serverName,
		})
	}
	return routers
}

func redirectOwners(rules []RedirectRule, serverID uuid.UUID) map[uuid.UUID]uuid.UUID {
	owners := make(map[uuid.UUID]uuid.UUID, len(rules))
	for _, rule := range rules {
		if rule.ServerID == serverID {
			owners[rule.ID] = rule.ApplicationID
		}
	}
	return owners
}

func containsName(list []string, name string) bool {
	for _, entry := range list {
		if entry == name {
			return true
		}
	}
	return false
}

func (s *SyncService) routerNodeSet(ctx context.Context, apps []ProxiedApplication, serverID uuid.UUID) ([]uuid.UUID, map[uuid.UUID]string, error) {
	names := map[uuid.UUID]string{}
	if named, ok := s.nodes.(namedNodeSource); ok && named != nil {
		listed, err := named.ListNamedNodes(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("proxy: list nodes: %w", err)
		}
		for _, node := range listed {
			names[node.ID] = node.Name
		}
		if serverID != uuid.Nil {
			return []uuid.UUID{serverID}, names, nil
		}
	} else if serverID != uuid.Nil {
		return []uuid.UUID{serverID}, names, nil
	}
	set := make(map[uuid.UUID]bool)
	for _, app := range apps {
		if app.ServerID != uuid.Nil {
			set[app.ServerID] = true
		}
	}
	if s.nodes != nil {
		if _, ok := s.nodes.(namedNodeSource); ok {
			for id := range names {
				set[id] = true
			}
		} else {
			registered, err := s.nodes.ListNodes(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("proxy: list nodes: %w", err)
			}
			for _, id := range registered {
				if id != uuid.Nil {
					set[id] = true
				}
			}
		}
	}
	ids := make([]uuid.UUID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids, names, nil
}

func applicationNames(apps []ProxiedApplication) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(apps))
	for _, app := range apps {
		if _, ok := names[app.ID]; !ok {
			names[app.ID] = app.Name
		}
	}
	return names
}

func proxiedServiceNames(services []ProxiedService) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(services))
	for _, service := range services {
		if _, ok := names[service.ID]; !ok {
			names[service.ID] = service.Name
		}
	}
	return names
}

func sortRouters(routers []RouterInfo) {
	sort.Slice(routers, func(i, j int) bool {
		if routers[i].Host != routers[j].Host {
			return routers[i].Host < routers[j].Host
		}
		return routers[i].Kind < routers[j].Kind
	})
}

func (s *SyncService) routerSyncStatus(ctx context.Context, serverID uuid.UUID, files []File) RouterSyncStatus {
	if s.history == nil {
		return RouterSyncUnknown
	}
	active, err := s.history.ActiveConfigVersion(ctx, serverID)
	if err != nil {
		if errors.Is(err, ErrVersionNotFound) {
			return RouterSyncPending
		}
		return RouterSyncUnknown
	}
	if !active.Pending && active.ContentHash == configHash(files) {
		return RouterSyncSynced
	}
	return RouterSyncPending
}
