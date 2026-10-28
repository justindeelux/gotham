package agent

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/agent/docker"
	"github.com/justindeelux/gotham/agent/stats"
	"github.com/justindeelux/gotham/agent/updates"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultHeartbeatInterval   = 10 * time.Second
	defaultMinBackoff          = time.Second
	defaultMaxBackoff          = 30 * time.Second
	defaultRegisterTimeout     = 30 * time.Second
	defaultBackoffResetAfter   = 30 * time.Second
	backoffJitterFraction      = 0.2
	defaultRenewBefore         = 30 * 24 * time.Hour
	dockerCallTimeout          = 5 * time.Second
	nodeIDMetadataKey          = "node-id"
	enrollmentTokenMetadataKey = "enrollment-token"
)

type Agent struct {
	cfg                Config
	log                *slog.Logger
	docker             docker.Client
	sampler            *stats.Sampler
	observationSampler *stats.Sampler
	interval           time.Duration
	minBackoff         time.Duration
	maxBackoff         time.Duration
	backoffResetAfter  time.Duration
	registerTimeout    time.Duration
	renewBefore        time.Duration
	dialOptions        []grpc.DialOption
	randFloat          func() float64
	sleep              func(context.Context, time.Duration) bool

	observationInterval time.Duration
	filesystems         *stats.FilesystemCollector
	observationSession  string
	sequenceMu          sync.Mutex
	observationSequence uint64
	containerSamples    *stats.ContainerSampler
	containerSampleAt   time.Time

	dockerRootMu sync.Mutex
	dockerRoot   string
	dockerRootAt time.Time

	versionMu sync.Mutex
	version   string
	updater   *updates.Updater
}

type Option func(*Agent)

func WithHeartbeatInterval(interval time.Duration) Option {
	return func(a *Agent) {
		if interval > 0 {
			a.interval = interval
		}
	}
}

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

func WithBackoffResetAfter(d time.Duration) Option {
	return func(a *Agent) {
		if d > 0 {
			a.backoffResetAfter = d
		}
	}
}

func WithDialOptions(options ...grpc.DialOption) Option {
	return func(a *Agent) {
		a.dialOptions = append(a.dialOptions, options...)
	}
}

func WithRegisterTimeout(timeout time.Duration) Option {
	return func(a *Agent) {
		if timeout > 0 {
			a.registerTimeout = timeout
		}
	}
}

func WithRenewBefore(d time.Duration) Option {
	return func(a *Agent) {
		if d > 0 {
			a.renewBefore = d
		}
	}
}

func NewAgent(cfg Config, log *slog.Logger, docker docker.Client, options ...Option) *Agent {
	if log == nil {
		log = slog.Default()
	}
	agent := &Agent{
		cfg:                cfg,
		log:                log,
		docker:             docker,
		sampler:            stats.New(),
		observationSampler: stats.New(),
		interval:           defaultHeartbeatInterval,
		minBackoff:         defaultMinBackoff,
		maxBackoff:         defaultMaxBackoff,
		backoffResetAfter:  defaultBackoffResetAfter,
		registerTimeout:    defaultRegisterTimeout,
		renewBefore:        defaultRenewBefore,
		randFloat:          rand.Float64,
		sleep:              sleepContext,
		version:            cfg.Version,

		observationInterval: defaultObservationInterval,
		filesystems:         stats.NewFilesystemCollector(),
		observationSession:  uuid.NewString(),
		containerSamples:    stats.NewContainerSampler(),
	}
	for _, option := range options {
		option(agent)
	}
	if cfg.EnrollmentToken != "" && cfg.CA == "" {
		log.Warn("agent: GOTHAM_AGENT_ENROLLMENT_TOKEN is set but the channel has no CA; the token is NOT sent over " +
			"plaintext. Set GOTHAM_AGENT_CA for a TLS channel, or use the control plane's open-enrollment opt-in")
	}
	if u, err := updates.New(agent.cfg.updateConfig(), log, agent.Version, agent.setVersion); err != nil {
		log.Warn("agent: self-update disabled", "error", err)
	} else {
		agent.updater = u
	}
	return agent
}

func (a *Agent) Version() string {
	a.versionMu.Lock()
	defer a.versionMu.Unlock()
	return a.version
}

func (a *Agent) setVersion(version string) {
	if version == "" {
		return
	}
	a.versionMu.Lock()
	a.version = version
	a.versionMu.Unlock()
}

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
			delay := a.backoffDelay(backoff)
			a.log.Warn("register failed; retrying", "error", err, "backoff", delay.String())
			if !a.sleep(ctx, delay) {
				return nil
			}
			backoff = nextBackoff(backoff, a.maxBackoff)
			continue
		}

		sessionStart := time.Now()
		a.log.Info("registered with control plane",
			slog.String("node_id", a.cfg.NodeID),
			slog.String("cp_version", response.GetCpVersion()),
		)
		newCert := false
		if cert := response.GetCert(); len(cert) > 0 {
			if _, err := SaveAgentCert(a.cfg.CertDir, cert); err != nil {
				a.log.Warn("failed to persist agent certificate", "error", err)
			} else {
				newCert = true
			}
		}
		if onRegister != nil {
			if err := onRegister(response); err != nil {
				return err
			}
		}
		if newCert {
			_ = conn.Close()
			conn, err = grpc.NewClient(a.cfg.CPAddr, options...)
			if err != nil {
				return fmt.Errorf("agent: dial control plane %s: %w", a.cfg.CPAddr, err)
			}
			client = agentv1.NewAgentServiceClient(conn)
			updateClient = agentv1.NewUpdateServiceClient(conn)
		}

		renewIn := a.renewalDelay(response.GetCert())

		hbCtx, hbCancel := context.WithCancel(ctx)
		sessionCtx := hbCtx
		var sessionCancel context.CancelFunc
		if renewIn > 0 {
			sessionCtx, sessionCancel = context.WithTimeout(hbCtx, renewIn)
		}
		var updateWG sync.WaitGroup
		if a.updater != nil {
			updateWG.Add(1)
			go func() {
				defer updateWG.Done()
				a.updater.Run(hbCtx, updateClient)
			}()
		}

		var observationWG sync.WaitGroup
		observationWG.Add(1)
		go func() {
			defer observationWG.Done()
			a.pushObservations(sessionCtx, client)
		}()

		heartbeatErr := a.heartbeat(sessionCtx, client)
		if sessionCancel != nil {
			sessionCancel()
		}
		hbCancel()
		observationWG.Wait()
		updateWG.Wait()
		if heartbeatErr != nil && ctx.Err() == nil {
			if errors.Is(heartbeatErr, context.DeadlineExceeded) {
				a.log.Info("certificate renewal due; re-registering", "renew_in", renewIn.String())
			} else {
				a.log.Warn("heartbeat stream ended; reconnecting", "error", heartbeatErr)
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(sessionStart) >= a.backoffResetAfter {
			backoff = a.minBackoff
		}
		delay := a.backoffDelay(backoff)
		if !a.sleep(ctx, delay) {
			return nil
		}
		backoff = nextBackoff(backoff, a.maxBackoff)
	}
}

func (a *Agent) dialOptionsFor() ([]grpc.DialOption, error) {
	certPath := ""
	if a.cfg.CA != "" {
		certPath = ClientCertPath(a.cfg.CertDir)
	}
	creds, dev, err := clientCredentials(a.cfg.CA, certPath, KeyPath(a.cfg.KeyFile, a.cfg.CertDir))
	if err != nil {
		return nil, err
	}
	if dev {
		a.log.Warn("no CA configured; connecting to the control plane without TLS")
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return append(options, a.dialOptions...), nil
}

func (a *Agent) register(ctx context.Context, client agentv1.AgentServiceClient) (*agentv1.RegisterResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, a.registerTimeout)
	defer cancel()
	if a.shouldSendEnrollmentToken() {
		callCtx = metadata.AppendToOutgoingContext(callCtx, enrollmentTokenMetadataKey, a.cfg.EnrollmentToken)
	}
	return client.Register(callCtx, a.registerRequest(callCtx))
}

func (a *Agent) shouldSendEnrollmentToken() bool {
	if a.cfg.EnrollmentToken == "" || a.cfg.CA == "" {
		return false
	}
	_, err := os.Stat(ClientCertPath(a.cfg.CertDir))
	return err != nil
}

func (a *Agent) renewalDelay(certPEM []byte) time.Duration {
	if len(certPEM) == 0 || a.renewBefore <= 0 {
		return 0
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return 0
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0
	}
	renewIn := time.Until(cert.NotAfter) - a.renewBefore
	if renewIn < time.Second {
		renewIn = time.Second
	}
	return renewIn
}

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
		a.log.Warn("failed to build certificate signing request; registering without one", "error", err)
	} else {
		request.Csr = csr
	}
	return request
}

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

func (a *Agent) heartbeat(ctx context.Context, client agentv1.AgentServiceClient) error {
	streamCtx, cancel := context.WithCancel(metadata.AppendToOutgoingContext(ctx, nodeIDMetadataKey, a.cfg.NodeID))
	defer cancel()
	stream, err := client.Heartbeat(streamCtx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.CloseSend() }()
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		if err := stream.Send(a.heartbeatRequest(ctx)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

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

func (a *Agent) containerCount(ctx context.Context) int64 {
	if a.docker == nil {
		return 0
	}
	callCtx, cancel := context.WithTimeout(ctx, dockerCallTimeout)
	defer cancel()
	containers, err := a.docker.ListContainers(callCtx, true, nil, false)
	if err != nil {
		a.log.Debug("container count unavailable", "error", err)
		return 0
	}
	return int64(len(containers))
}

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

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next <= 0 || next > max {
		return max
	}
	return next
}

func (a *Agent) backoffDelay(backoff time.Duration) time.Duration {
	return jitter(backoff, a.minBackoff, a.maxBackoff, a.randFloat)
}

func jitter(d, min, max time.Duration, randFloat func() float64) time.Duration {
	if d <= 0 {
		return min
	}
	spread := time.Duration(float64(d) * backoffJitterFraction)
	delta := time.Duration((randFloat()*2 - 1) * float64(spread))
	jittered := d + delta
	if jittered < min {
		jittered = min
	}
	if jittered > max {
		jittered = max
	}
	return jittered
}
