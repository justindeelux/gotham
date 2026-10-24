package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/instance"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/server"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
)

func runInstance(args []string) int {
	if len(args) == 1 && args[0] == "wait-localhost" {
		if err := waitForLocalhost(); err != nil {
			fmt.Fprintf(os.Stderr, "instance wait-localhost: %v\n", err)
			return exitError
		}
		return exitOK
	}
	if len(args) != 2 || args[0] != "set-url" {
		fmt.Fprintln(os.Stderr, "Usage: gotham instance set-url <http(s)://host> | wait-localhost")
		return exitUsage
	}
	if err := instanceSetURL(args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "instance set-url: %v\n", err)
		return exitError
	}
	return exitOK
}

func instanceSetURL(raw string) error {
	raw = strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
	loadEnvFile()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := store.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)
	svc := instance.NewService(instance.NewStoreRepository(st), nil, nil)
	defer svc.Close()
	current, err := svc.ControlPlaneURL(ctx)
	if err != nil {
		return err
	}
	if current != raw {
		if err := checkPanelURL(ctx, raw); err != nil {
			return err
		}
	}
	authority, err := servers.LoadAuthority(cfg.CA.Dir)
	if err != nil {
		return err
	}
	secret, _, _ := server.ResolveSecretKey(cfg.SecretKey, cfg.CA.Dir)
	nodes := servers.NewService(servers.Config{Store: st, Authority: authority, Secret: secret})
	containerService := newInstanceContainers(nodes, cfg.Redis.Addr)
	defer func() { _ = containerService.Close() }()
	px := proxy.NewService(proxy.Config{
		Store: st, Services: services.NewProxySource(st), Containers: containerService,
		Secret: secret, ServerPort: cfg.Server.Port,
		ControlPlaneURL: svc.ControlPlaneURL, ControlPlaneURLs: svc.ControlPlaneURLs,
		Dial: func(ctx context.Context, id uuid.UUID) (proxy.AgentClient, error) {
			return nodes.DialProxyClient(ctx, id)
		},
	})
	svc.SyncPanelCertificates = px.ReconcilePanelCertificates
	svc.ResyncProxy = px.SyncControlPlane
	svc.CheckControlPlaneHost = px.CheckControlPlaneHost
	state, err := setInstanceURL(ctx, svc, raw)
	if err != nil {
		return err
	}
	fmt.Printf("Panel URL: %s\n", state.General.ControlPlaneURL.Value)
	if p := state.PendingControlPlaneURL; p != nil {
		fmt.Printf("Open http://%s and confirm the domain in General settings before %s; HTTPS starts after confirmation.\n", mustPanelHost(raw), p.Deadline.Format(time.RFC3339))
	}
	return nil
}

type instanceAgentRegistry interface {
	containers.Registry
	DialDockerClient(context.Context, uuid.UUID, ...servers.DockerDialOption) (*servers.DockerClient, error)
}

func newInstanceContainers(nodes instanceAgentRegistry, redisAddr string) *containers.Service {
	service := containers.NewDefaultService(nodes, redisAddr)
	service.SetDial(func(ctx context.Context, node *servers.Server) (containers.DockerClient, error) {
		return nodes.DialDockerClient(ctx, node.ID)
	})
	return service
}

func mustPanelHost(raw string) string {
	u, _ := url.Parse(raw)
	return u.Host
}

func setInstanceURL(ctx context.Context, svc *instance.Service, raw string) (instance.State, error) {
	state, err := svc.Get(ctx)
	if err != nil {
		return instance.State{}, err
	}
	state, err = svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{
		ControlPlaneURL: raw, InstanceName: state.General.InstanceName.Value, Timezone: state.General.Timezone.Value,
	})
	if err != nil {
		return instance.State{}, err
	}
	if p := state.PendingControlPlaneURL; p != nil && !p.Synced {
		if _, err := svc.RevertControlPlaneURL(ctx, uuid.Nil); err != nil {
			return instance.State{}, fmt.Errorf("proxy sync failed; rollback: %w", err)
		}
		return instance.State{}, instance.ErrDomainNotSynced
	}
	return state, nil
}

// Check before the instance service starts Traefik and takes these ports.
func checkPanelURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("use an http(s) URL containing only a domain")
	}
	if err := proxy.ValidateDomain(u.Hostname()); err != nil {
		return err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("resolve panel domain: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("panel domain has no DNS addresses")
	}
	fmt.Fprintln(os.Stderr, "Ensure the domain's DNS addresses route to this host; public addresses behind NAT need not appear on a local interface.")
	for _, port := range []string{"80", "443"} {
		listener, err := net.Listen("tcp", ":"+port)
		if err != nil {
			return fmt.Errorf("port %s must be free: %w", port, err)
		}
		_ = listener.Close()
	}
	return nil
}

// Wait for both a recorded heartbeat and the agent's authenticated RPC listener.
func waitForLocalhost() error {
	loadEnvFile()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	authority, err := servers.LoadAuthority(cfg.CA.Dir)
	if err != nil {
		return err
	}
	st := store.New(pool)
	nodes := servers.NewService(servers.Config{Store: st, Authority: authority})
	inventory := newInstanceContainers(nodes, cfg.Redis.Addr)
	defer func() { _ = inventory.Close() }()
	nodeID := "localhost"
	for {
		probe, stop := context.WithTimeout(ctx, time.Second)
		row, readErr := st.GetServerByNodeID(probe, &nodeID)
		if readErr == nil && row.Status == servers.StatusReady && row.LastSeen.Valid {
			_, readErr = inventory.ListFresh(probe, uuid.UUID(row.ID.Bytes))
			if readErr == nil {
				stop()
				return nil
			}
		}
		stop()
		select {
		case <-ctx.Done():
			return fmt.Errorf("localhost agent is not ready after 60 seconds: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
