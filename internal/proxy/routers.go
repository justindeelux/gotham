package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// RouterKind names the control-plane row a generated router comes from.
type RouterKind string

const (
	// RouterKindApplication is a router serving an application's base domain.
	RouterKindApplication RouterKind = "application"
	// RouterKindService is a router serving a compose service's declared host.
	RouterKindService RouterKind = "service"
	// RouterKindRedirect is a router sending one host to another (its
	// redirectRegex middleware terminates the request before any backend).
	RouterKindRedirect RouterKind = "redirect"
)

// RouterSyncStatus is one node's configuration state as observed on read:
// the freshly generated documents are compared against the last synced
// version in history.
type RouterSyncStatus string

const (
	// RouterSyncSynced means the node serves the current generated state.
	RouterSyncSynced RouterSyncStatus = "synced"
	// RouterSyncPending means the generated state differs from the last
	// synced version (or was never synced): run a proxy sync.
	RouterSyncPending RouterSyncStatus = "pending"
	// RouterSyncUnknown means no sync history is available, so no claim is
	// possible.
	RouterSyncUnknown RouterSyncStatus = "unknown"
)

// RouterInfo is one generated Traefik router as served on a node: the host
// rule, the backend service, the entrypoints, the TLS resolver (empty for
// plain HTTP) and the owning control-plane row. One row covers both the web
// and the websecure router of a host, so entrypoints list both for HTTPS
// hosts. OwnerID is the owning application or service id, or the redirect
// rule id for RouterKindRedirect.
type RouterInfo struct {
	Host        string     `json:"host"`
	Rule        string     `json:"rule"`
	Service     string     `json:"service"`
	Target      string     `json:"target,omitempty"`
	EntryPoints []string   `json:"entrypoints"`
	Middlewares []string   `json:"middlewares,omitempty"`
	TLSResolver string     `json:"tls_resolver,omitempty"`
	Kind        RouterKind `json:"kind"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	OwnerName   string     `json:"owner_name,omitempty"`
	ServerID    uuid.UUID  `json:"server_id"`
	ServerName  string     `json:"server_name,omitempty"`
}

// RouterNodeState is one node's read outcome. Error is set (and no rows are
// reported for the node) when the node could not be read: an unreachable
// node is reported, never filled with fabricated rows.
type RouterNodeState struct {
	ServerID    uuid.UUID        `json:"server_id"`
	ServerName  string           `json:"server_name,omitempty"`
	SyncStatus  RouterSyncStatus `json:"sync_status"`
	Error       string           `json:"error,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
}

// RouterListResponse is the body of GET /v1/proxy/routers.
type RouterListResponse struct {
	Routers []RouterInfo      `json:"routers"`
	Nodes   []RouterNodeState `json:"nodes"`
}

// RouterService lists the generated Traefik routers of one node (or every
// node when serverID is uuid.Nil) from the same generator state a sync would
// push. Implementations must never invent rows for a node they cannot read.
type RouterService interface {
	ListRouters(ctx context.Context, serverID uuid.UUID) (RouterListResponse, error)
}

// Compile-time guarantee that SyncService satisfies the read contract.
var _ RouterService = (*SyncService)(nil)

// AsRouterService returns svc as a RouterService when it implements the read
// side, or nil so the HTTP wiring can pass its result to the mount
// unconditionally.
func AsRouterService(svc ProxyService) RouterService {
	if routers, ok := svc.(RouterService); ok {
		return routers
	}
	return nil
}

// NamedNode is one registered node with its display name.
type NamedNode struct {
	ID   uuid.UUID
	Name string
}

// namedNodeSource optionally lists registered nodes with display names. The
// production storeSource implements it; other NodeSources keep reporting ids
// only.
type namedNodeSource interface {
	ListNamedNodes(ctx context.Context) ([]NamedNode, error)
}

// ListNamedNodes maps the server registry to node ids and display names.
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

// ListRouters regenerates each requested node's routing state in memory and
// reports the resulting routers plus the per-node sync status. It writes
// nothing: the documents are the same bytes a sync would push. A node whose
// containers cannot be listed (unreachable agent) is reported with an error
// and contributes no rows; a single-node request for such a node fails, so
// the API answers 502 instead of an empty-but-200 list that looks healthy.
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

// listNodeRouters resolves one node's live endpoints, regenerates its
// documents and reads the router rows back from the generated configuration,
// so the list is literally what a sync would push — never a parallel
// re-implementation of the generator.
func (s *SyncService) listNodeRouters(ctx context.Context, apps []ProxiedApplication, proxiedServices []ProxiedService, rules []RedirectRule, providers []DNSProvider, access map[uuid.UUID]providerAccess, appNames, serviceNames map[uuid.UUID]string, serverID uuid.UUID, serverName string) ([]RouterInfo, RouterNodeState, error) {
	state := RouterNodeState{ServerID: serverID, ServerName: serverName, SyncStatus: RouterSyncUnknown}
	nodeList, err := s.listNodeContainers(ctx, serverID)
	if err != nil {
		return nil, state, mapNodeError(err)
	}
	routes, diagnostics := routesForServer(apps, serverID, s.backendHost, nodeList, access)
	serviceRoutes, serviceDiagnostics, serviceHosts := serviceRoutesForServer(
		proxiedServices, serverID, s.backendHost, nodeList, applicationHosts(apps, serverID))
	routes = append(routes, serviceRoutes...)
	diagnostics = append(diagnostics, serviceDiagnostics...)
	redirects, redirectDiagnostics := redirectsForServer(apps, rules, serverID, serviceHosts...)
	diagnostics = append(diagnostics, redirectDiagnostics...)
	cfg := BuildConfig(routes, redirects, providers, s.acmeEmail, s.caServer)
	files, err := Generate(cfg, s.format)
	if err != nil {
		return nil, state, err
	}
	routers := routersFromConfig(cfg, routes, len(routes)-len(serviceRoutes), redirects, redirectOwners(rules, serverID), serverID, serverName, appNames, serviceNames)
	state.Diagnostics = diagnostics
	state.SyncStatus = s.routerSyncStatus(ctx, serverID, files)
	return routers, state, nil
}

// routersFromConfig reads the router rows back from a generated
// configuration: one row per host for application and service routes (the web
// and websecure routers merged), plus one row per redirect rule. appRoutes
// counts the leading application routes of routes; the rest are service
// routes.
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

// redirectOwners maps redirect rule ids to their owning application on one
// node, so redirect rows can name the owning resource.
func redirectOwners(rules []RedirectRule, serverID uuid.UUID) map[uuid.UUID]uuid.UUID {
	owners := make(map[uuid.UUID]uuid.UUID, len(rules))
	for _, rule := range rules {
		if rule.ServerID == serverID {
			owners[rule.ID] = rule.ApplicationID
		}
	}
	return owners
}

// containsName reports whether list holds name.
func containsName(list []string, name string) bool {
	for _, entry := range list {
		if entry == name {
			return true
		}
	}
	return false
}

// routerNodeSet resolves the requested nodes: exactly serverID when set,
// otherwise every node hosting a routable row plus every registered node
// (mirroring SyncAll, so removing the last domain stays visible too).
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

// applicationNames maps application ids to display names for the owning
// resource column.
func applicationNames(apps []ProxiedApplication) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(apps))
	for _, app := range apps {
		if _, ok := names[app.ID]; !ok {
			names[app.ID] = app.Name
		}
	}
	return names
}

// proxiedServiceNames maps compose service ids to display names.
func proxiedServiceNames(services []ProxiedService) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(services))
	for _, service := range services {
		if _, ok := names[service.ID]; !ok {
			names[service.ID] = service.Name
		}
	}
	return names
}

// sortRouters orders rows by host for a stable response.
func sortRouters(routers []RouterInfo) {
	sort.Slice(routers, func(i, j int) bool {
		if routers[i].Host != routers[j].Host {
			return routers[i].Host < routers[j].Host
		}
		return routers[i].Kind < routers[j].Kind
	})
}

// routerSyncStatus compares the freshly generated documents against the last
// synced version: equal means the node serves the current state, anything
// else means a sync is due. Without history (or on a lookup failure) no
// claim is possible.
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
