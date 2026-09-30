package agent

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/justindeelux/gotham/agent/stats"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Defaults for the register/heartbeat loop.
const (
	defaultHeartbeatInterval = 10 * time.Second
	defaultMinBackoff        = time.Second
	defaultMaxBackoff        = 30 * time.Second
	// dockerCallTimeout bounds best-effort Docker calls made from the loop.
	dockerCallTimeout = 5 * time.Second
	// nodeIDMetadataKey carries the node identity on RPCs where the gateway
	// cannot read it from a client certificate (the bootstrap connection).
	nodeIDMetadataKey = "node-id"
)

// Agent connects to the control plane, registers this node and streams
// heartbeats, reconnecting with backoff when the connection drops.
type Agent struct {
	cfg         Config
	log         *slog.Logger
	docker      dockerClient
	sampler     *stats.Sampler
	interval    time.Duration
	minBackoff  time.Duration
	maxBackoff  time.Duration
	dialOptions []grpc.DialOption

	versionMu sync.Mutex
	version   string
	updater   *updater
}

// Option customizes an Agent. Options are primarily used by tests.
type Option func(*Agent)

// WithHeartbeatInterval overrides the heartbeat cadence (default 10s).
func WithHeartbeatInterval(interval time.Duration) Option {
	return func(a *Agent) {
		if interval > 0 {
			a.interval = interval
		}
	}
}

// WithBackoff overrides the reconnect backoff bounds.
func WithBackoff(minBackoff, maxBackoff time.Duration) Option {
	return func(a *Agent) {
		if minBackoff > 0 {
			a.minBackoff = minBackoff
		}
		if maxBackoff >= minBackoff && maxBackoff > 0 {
			a.maxBackoff = maxBackoff
		}
	}
}

// WithDialOptions appends gRPC dial options, for example a bufconn dialer in
// tests.
func WithDialOptions(options ...grpc.DialOption) Option {
	return func(a *Agent) {
		a.dialOptions = append(a.dialOptions, options...)
	}
}

// NewAgent returns an Agent that reports Docker state through docker.
func NewAgent(cfg Config, log *slog.Logger, docker dockerClient, options ...Option) *Agent {
	if log == nil {
		log = slog.Default()
	}
	agent := &Agent{
		cfg:        cfg,
		log:        log,
		docker:     docker,
		sampler:    stats.New(),
		interval:   defaultHeartbeatInterval,
		minBackoff: defaultMinBackoff,
		maxBackoff: defaultMaxBackoff,
		version:    cfg.Version,
	}
	for _, option := range options {
		option(agent)
	}
	// The updater is disabled (nil) when no release public key is configured or
	// no fixed binary path is set; it never blocks construction.
	if u, err := newUpdater(cfg, log, agent.Version, agent.setVersion); err != nil {
		log.Warn("agent: self-update disabled", "error", err)
	} else {
		agent.updater = u
	}
	return agent
}

// Version returns the version this agent currently reports.
func (a *Agent) Version() string {
	a.versionMu.Lock()
	defer a.versionMu.Unlock()
	return a.version
}

// setVersion records the version after a successful update.
func (a *Agent) setVersion(version string) {
	if version == "" {
		return
	}
	a.versionMu.Lock()
	a.version = version
	a.versionMu.Unlock()
}

// Run connects to the control plane and runs the register/heartbeat loop until
// ctx is canceled. onRegister is invoked after every successful registration.
// It typically starts the DockerService server using the issued certificate,
// and returning an error stops the agent. Run returns nil on a graceful
// shutdown.
func (a *Agent) Run(ctx context.Context, onRegister func(*agentv1.RegisterResponse) error) error {
	options, err := a.dialOptionsFor()
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(a.cfg.CPAddr, options...)
	if err != nil {
		return fmt.Errorf("agent: dial control plane %s: %w", a.cfg.CPAddr, err)
	}
	defer func() { _ = conn.Close() }()

	client := agentv1.NewAgentServiceClient(conn)
	updateClient := agentv1.NewUpdateServiceClient(conn)
	backoff := a.minBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}

		response, err := a.register(ctx, client)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			a.log.Warn("register failed; retrying", "error", err, "backoff", backoff.String())
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff, a.maxBackoff)
			continue
		}

		backoff = a.minBackoff
		a.log.Info("registered with control plane",
			slog.String("node_id", a.cfg.NodeID),
			slog.String("cp_version", response.GetCpVersion()),
		)
		if cert := response.GetCert(); len(cert) > 0 {
			if _, err := SaveAgentCert(a.cfg.CertDir, cert); err != nil {
				a.log.Warn("failed to persist agent certificate", "error", err)
			}
		}
		if onRegister != nil {
			if err := onRegister(response); err != nil {
				return err
			}
		}

		// Poll for a signed update alongside the heartbeat stream. The poll
		// stops when the heartbeat stream ends so a reconnect starts a fresh
		// one; the download never blocks the heartbeat because it runs on its
		// own goroutine.
		hbCtx, hbCancel := context.WithCancel(ctx)
		var updateWG sync.WaitGroup
		if a.updater != nil {
			updateWG.Add(1)
			go func() {
				defer updateWG.Done()
				a.updater.run(hbCtx, updateClient)
			}()
		}

		heartbeatErr := a.heartbeat(ctx, client)
		hbCancel()
		updateWG.Wait()
		if heartbeatErr != nil && ctx.Err() == nil {
			a.log.Warn("heartbeat stream ended; reconnecting", "error", heartbeatErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		if !sleepContext(ctx, backoff) {
			return nil
		}
		backoff = nextBackoff(backoff, a.maxBackoff)
	}
}

// dialOptionsFor builds the gRPC dial options, including transport credentials
// derived from Config.CA.
func (a *Agent) dialOptionsFor() ([]grpc.DialOption, error) {
	creds, dev, err := clientCredentials(a.cfg.CA)
	if err != nil {
		return nil, err
	}
	if dev {
		a.log.Warn("no CA configured; connecting to the control plane without TLS")
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return append(options, a.dialOptions...), nil
}

// register sends a single Register RPC.
func (a *Agent) register(ctx context.Context, client agentv1.AgentServiceClient) (*agentv1.RegisterResponse, error) {
	return client.Register(ctx, a.registerRequest(ctx))
}

// registerRequest assembles the node's static identity and capabilities.
func (a *Agent) registerRequest(ctx context.Context) *agentv1.RegisterRequest {
	sample, _ := a.sampler.Sample()

	dockerVersion := ""
	if a.docker != nil {
		callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
		defer cancel()
		version, err := a.docker.Version(callCtx)
		if err != nil {
			a.log.Debug("docker version unavailable", "error", err)
		} else {
			dockerVersion = version
		}
	}

	osName := runtime.GOOS
	if release := stats.OSVersion(); release != "" {
		osName += " " + release
	}

	request := &agentv1.RegisterRequest{
		NodeId:        a.cfg.NodeID,
		Os:            osName,
		DockerVersion: dockerVersion,
		Arch:          runtime.GOARCH,
		TotalMem:      int64(sample.TotalMem),
		TotalDisk:     int64(sample.TotalDisk),
	}
	if csr, err := a.certificateRequest(); err != nil {
		// The CSR is best effort: a node that cannot build one still
		// registers and receives a certificate for its node id (dev fallback).
		a.log.Warn("failed to build certificate signing request; registering without one", "error", err)
	} else {
		request.Csr = csr
	}
	return request
}

// certificateRequest ensures the agent's keypair exists and returns a PEM
// PKCS#10 CSR for it. The private key stays on the node; only the CSR is sent.
// It resolves the key exactly as registration does: an explicit Config.KeyFile
// wins, otherwise <CertDir>/agent.key.
func (a *Agent) certificateRequest() ([]byte, error) {
	var (
		key crypto.Signer
		err error
	)
	if a.cfg.KeyFile != "" {
		key, _, err = ensureKeyAt(a.cfg.KeyFile)
	} else {
		key, _, err = EnsureKey(a.cfg.CertDir)
	}
	if err != nil {
		return nil, err
	}
	return GenerateCSR(a.cfg.NodeID, key)
}

// heartbeat opens the Heartbeat client stream and sends a sample immediately
// and then once per interval until the stream fails or ctx is canceled.
func (a *Agent) heartbeat(ctx context.Context, client agentv1.AgentServiceClient) error {
	// The bootstrap connection presents no client certificate, so the gateway
	// learns the node identity from metadata to attribute heartbeats.
	streamCtx := metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, a.cfg.NodeID)
	stream, err := client.Heartbeat(streamCtx)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		if err := stream.Send(a.heartbeatRequest(ctx)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// heartbeatRequest samples the host and current container count.
func (a *Agent) heartbeatRequest(ctx context.Context) *agentv1.HeartbeatRequest {
	sample, _ := a.sampler.Sample()
	return &agentv1.HeartbeatRequest{
		CpuUsage:       sample.CPU,
		MemUsage:       sample.Mem,
		DiskUsage:      sample.Disk,
		ContainerCount: a.containerCount(ctx),
		SentAt:         timestamppb.Now(),
		NetRxBps:       sample.NetRxBps,
		NetTxBps:       sample.NetTxBps,
		DiskReadBps:    sample.DiskReadBps,
		DiskWriteBps:   sample.DiskWriteBps,
		AgentVersion:   a.Version(),
	}
}

// containerCount returns the number of containers known to Docker, or 0 when
// it cannot be determined.
func (a *Agent) containerCount(ctx context.Context) int64 {
	if a.docker == nil {
		return 0
	}
	callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()
	containers, err := a.docker.ListContainers(callCtx, true)
	if err != nil {
		a.log.Debug("container count unavailable", "error", err)
		return 0
	}
	return int64(len(containers))
}

// sleepContext waits for d or until ctx is canceled, reporting whether the full
// duration elapsed.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// nextBackoff doubles current, capped at max.
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next <= 0 || next > max {
		return max
	}
	return next
}
