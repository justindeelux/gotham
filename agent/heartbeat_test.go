package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"math/big"
	mathrand "math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeAgentService is an in-memory AgentServiceServer.
type fakeAgentService struct {
	agentv1.UnimplementedAgentServiceServer

	cpVersion   string
	cert        []byte
	registerErr error
	heartbeatCh chan *agentv1.HeartbeatRequest
	// registerGate, when non-nil, blocks Register until it is closed or the
	// call's context is canceled.
	registerGate chan struct{}
	// heartbeatFail closes the stream with an error after the first message.
	heartbeatFail bool

	mu         sync.Mutex
	registers  []*agentv1.RegisterRequest
	heartbeats []*agentv1.HeartbeatRequest
	// heartbeatNodes is the node id carried in the heartbeat stream metadata,
	// aligned with heartbeats (the request itself carries no node id).
	heartbeatNodes []string
}

func (f *fakeAgentService) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	f.mu.Lock()
	f.registers = append(f.registers, req)
	f.mu.Unlock()
	if f.registerGate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.registerGate:
		}
	}
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return &agentv1.RegisterResponse{Cert: f.cert, CpVersion: f.cpVersion}, nil
}

func (f *fakeAgentService) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
		}
		if err != nil {
			return err
		}
		f.mu.Lock()
		f.heartbeats = append(f.heartbeats, req)
		nodeID := ""
		if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
			if values := md.Get(nodeIDMetadataKey); len(values) > 0 {
				nodeID = values[0]
			}
		}
		f.heartbeatNodes = append(f.heartbeatNodes, nodeID)
		f.mu.Unlock()
		if f.heartbeatFail {
			return errors.New("heartbeat stream reset")
		}
		if f.heartbeatCh != nil {
			select {
			case f.heartbeatCh <- req:
			default:
			}
		}
	}
}

func (f *fakeAgentService) registerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registers)
}

// startFakeCP starts an in-process control plane and returns dial options that
// route the agent to it.
func startFakeCP(t *testing.T, fake *fakeAgentService) []grpc.DialOption {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	}
}

func TestAgentRegisterAndHeartbeat(t *testing.T) {
	fake := &fakeAgentService{
		cpVersion:   "0.2.0",
		cert:        []byte("CERTIFICATE"),
		heartbeatCh: make(chan *agentv1.HeartbeatRequest, 16),
	}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:     "passthrough:///bufnet",
		NodeID:     "node-1",
		CertDir:    t.TempDir(),
		DockerSock: defaultDockerSock,
		LogLevel:   "error",
	}
	certDir := cfg.CertDir
	docker := &fakeDockerClient{
		version:    "24.0.5",
		containers: []*agentv1.ContainerInfo{{Id: "a"}, {Id: "b"}, {Id: "c"}},
	}
	runner := NewAgent(cfg, discardLogger(), docker,
		WithHeartbeatInterval(20*time.Millisecond),
		WithDialOptions(dialOptions...),
	)

	registered := make(chan *agentv1.RegisterResponse, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx, func(response *agentv1.RegisterResponse) error {
			select {
			case registered <- response:
			default:
			}
			return nil
		})
	}()

	select {
	case response := <-registered:
		if string(response.GetCert()) != "CERTIFICATE" {
			t.Errorf("cert = %q; want CERTIFICATE", response.GetCert())
		}
		if response.GetCpVersion() != "0.2.0" {
			t.Errorf("cp version = %q; want 0.2.0", response.GetCpVersion())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not register")
	}

	select {
	case heartbeat := <-fake.heartbeatCh:
		assertHeartbeat(t, heartbeat, 3)
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat received")
	}

	fake.mu.Lock()
	if len(fake.registers) == 0 {
		fake.mu.Unlock()
		t.Fatal("Register was never called")
	}
	register := fake.registers[0]
	fake.mu.Unlock()

	if register.GetNodeId() != "node-1" {
		t.Errorf("NodeId = %q; want node-1", register.GetNodeId())
	}
	if register.GetArch() != runtime.GOARCH {
		t.Errorf("Arch = %q; want %q", register.GetArch(), runtime.GOARCH)
	}
	if !strings.HasPrefix(register.GetOs(), runtime.GOOS) {
		t.Errorf("Os = %q; want prefix %q", register.GetOs(), runtime.GOOS)
	}
	if register.GetDockerVersion() != "24.0.5" {
		t.Errorf("DockerVersion = %q; want 24.0.5", register.GetDockerVersion())
	}

	// The agent must send a CSR bound to its node identity so the CP can issue
	// a certificate for the agent's own keypair.
	if len(register.GetCsr()) == 0 {
		t.Fatal("RegisterRequest.csr is empty")
	}
	csr := parseCSRRequest(t, register.GetCsr())
	if csr.Subject.CommonName != "node-1" {
		t.Errorf("CSR common name = %q; want node-1", csr.Subject.CommonName)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "node-1" {
		t.Errorf("CSR DNS SANs = %v; want [node-1]", csr.DNSNames)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature is invalid: %v", err)
	}

	// The certificate returned by the CP is persisted next to the key, and the
	// key generated for the CSR is written once.
	if data, err := os.ReadFile(filepath.Join(certDir, certFileName)); err != nil {
		t.Errorf("agent.crt not persisted: %v", err)
	} else if string(data) != "CERTIFICATE" {
		t.Errorf("agent.crt = %q; want CERTIFICATE", data)
	}
	if _, err := os.Stat(filepath.Join(certDir, keyFileName)); err != nil {
		t.Errorf("agent.key not persisted: %v", err)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v; want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestAgentRetriesRegisterUntilCanceled(t *testing.T) {
	fake := &fakeAgentService{registerErr: errors.New("control plane unreachable")}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:     "passthrough:///bufnet",
		NodeID:     "node-retry",
		CertDir:    t.TempDir(),
		DockerSock: defaultDockerSock,
		LogLevel:   "error",
	}
	runner := NewAgent(cfg, discardLogger(), nil,
		WithBackoff(5*time.Millisecond, 10*time.Millisecond),
		WithDialOptions(dialOptions...),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := runner.Run(ctx, nil); err != nil {
		t.Fatalf("Run = %v; want nil after cancel", err)
	}
	if count := fake.registerCount(); count < 2 {
		t.Errorf("register attempts = %d; want >= 2", count)
	}
}

// TestNextBackoffGrowthAndCap pins the exponential growth and the cap that the
// jittered schedule is built on (A3-12).
func TestNextBackoffGrowthAndCap(t *testing.T) {
	const max = 30 * time.Second
	got := time.Second
	for _, want := range []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		max,
		max,
	} {
		got = nextBackoff(got, max)
		if got != want {
			t.Fatalf("nextBackoff = %v; want %v", got, want)
		}
	}
}

// TestJitterBounded pins the A3-12 jitter bounds: every wait stays within ±20%
// of the nominal backoff and is clamped to [min, max].
func TestJitterBounded(t *testing.T) {
	// Pin the documented fraction against a literal so changing the constant
	// cannot silently change the tested bound.
	if backoffJitterFraction != 0.2 {
		t.Fatalf("backoffJitterFraction = %v; want 0.2", backoffJitterFraction)
	}
	const (
		min      = 10 * time.Millisecond
		max      = time.Second
		base     = 100 * time.Millisecond
		fraction = 0.2
	)
	spread := time.Duration(float64(base) * fraction)
	seen := map[time.Duration]struct{}{}
	below, above := 0, 0
	for i := 0; i < 2000; i++ {
		got := jitter(base, min, max, mathrand.Float64)
		if got < base-spread || got > base+spread {
			t.Fatalf("jitter(%v) = %v; want within ±20%% [%v,%v]", base, got, base-spread, base+spread)
		}
		seen[got] = struct{}{}
		if got < base {
			below++
		} else if got > base {
			above++
		}
	}
	// The samples must actually spread around the base and land on both sides;
	// otherwise the "jitter" is a no-op.
	if len(seen) < 100 {
		t.Fatalf("jitter produced only %d distinct values; want real spread", len(seen))
	}
	if below == 0 || above == 0 {
		t.Fatalf("jitter never crossed the base (below=%d above=%d)", below, above)
	}
	floorSpread := time.Duration(float64(min) * fraction)
	capSpread := time.Duration(float64(max) * fraction)
	for i := 0; i < 200; i++ {
		if got := jitter(min, min, max, mathrand.Float64); got < min || got > min+floorSpread {
			t.Fatalf("jitter at the floor = %v; want [%v,%v]", got, min, min+floorSpread)
		}
		if got := jitter(max, min, max, mathrand.Float64); got < max-capSpread || got > max {
			t.Fatalf("jitter at the cap = %v; want [%v,%v]", got, max-capSpread, max)
		}
	}
	if got := jitter(0, min, max, mathrand.Float64); got != min {
		t.Fatalf("jitter(0) = %v; want the floor %v", got, min)
	}
}

// TestAgentRegisterFailureBackoffIsJittered is the A3-12 guard for the
// register-failure path: the wait is jittered around the nominal backoff, so a
// fixed random source yields a deterministic lower/upper schedule rather than
// the bare backoff.
func TestAgentRegisterFailureBackoffIsJittered(t *testing.T) {
	const (
		min = 10 * time.Millisecond
		max = time.Second
	)
	lower := []time.Duration{min, 16 * time.Millisecond, 32 * time.Millisecond} // nominal -20%, floor-clamped
	upper := []time.Duration{12 * time.Millisecond, 24 * time.Millisecond, 48 * time.Millisecond}

	for _, tc := range []struct {
		name string
		rand float64
		want []time.Duration
	}{
		{"lower", 0, lower},
		{"upper", 1, upper},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeAgentService{registerErr: errors.New("control plane unreachable")}
			dialOptions := startFakeCP(t, fake)
			runner := NewAgent(Config{
				CPAddr:  "passthrough:///bufnet",
				NodeID:  "node-register-jitter",
				CertDir: t.TempDir(),
			}, discardLogger(), nil,
				WithBackoff(min, max),
				WithDialOptions(dialOptions...),
			)
			runner.randFloat = func() float64 { return tc.rand }
			var delays []time.Duration
			runner.sleep = func(_ context.Context, d time.Duration) bool {
				delays = append(delays, d)
				return len(delays) < len(tc.want)
			}
			if err := runner.Run(context.Background(), nil); err != nil {
				t.Fatalf("Run = %v; want nil", err)
			}
			if len(delays) != len(tc.want) {
				t.Fatalf("recorded %d delays; want %d", len(delays), len(tc.want))
			}
			for i := range tc.want {
				if delays[i] != tc.want[i] {
					t.Fatalf("register-failure delay[%d] = %v; want %v (full %v)", i, delays[i], tc.want[i], delays)
				}
			}
		})
	}
}

// TestBackoffResetAfterHealthySession pins the reset gate (U7): a session at
// least backoffResetAfter long resets the backoff to the floor, a shorter one
// keeps growing. Both schedules are driven deterministically through the
// injected rand/sleep seams.
func TestBackoffResetAfterHealthySession(t *testing.T) {
	const min = 10 * time.Millisecond
	delays := func(resetAfter time.Duration) []time.Duration {
		fake := &fakeAgentService{heartbeatFail: true}
		dialOptions := startFakeCP(t, fake)
		runner := NewAgent(Config{
			CPAddr:  "passthrough:///bufnet",
			NodeID:  "node-reset",
			CertDir: t.TempDir(),
		}, discardLogger(), nil,
			WithHeartbeatInterval(time.Millisecond),
			WithBackoff(min, time.Second),
			WithBackoffResetAfter(resetAfter),
			WithDialOptions(dialOptions...),
		)
		runner.randFloat = func() float64 { return 0 } // nominal -20%
		var got []time.Duration
		runner.sleep = func(_ context.Context, d time.Duration) bool {
			got = append(got, d)
			return len(got) < 4
		}
		if err := runner.Run(context.Background(), nil); err != nil {
			t.Fatalf("Run = %v; want nil", err)
		}
		return got
	}

	// A session longer than the (tiny) reset window resets before every wait, so
	// the delay stays at the jittered floor.
	short := delays(time.Nanosecond)
	for i, d := range short {
		if d < 8*time.Millisecond || d > min {
			t.Fatalf("reset-window session: delay[%d] = %v; want ~%v", i, d, min)
		}
	}
	// A session shorter than the (huge) reset window never resets, so the delay
	// grows: 10, 20, 40, 80ms (each -20% jitter).
	long := delays(time.Hour)
	want := []time.Duration{min, 16 * time.Millisecond, 32 * time.Millisecond, 64 * time.Millisecond}
	if len(long) != len(want) {
		t.Fatalf("recorded %d delays; want %d", len(long), len(want))
	}
	for i := range want {
		if long[i] != want[i] {
			t.Fatalf("short-session delay[%d] = %v; want %v (full %v)", i, long[i], want[i], long)
		}
	}
}

// TestAgentShortSessionsDoNotHotLoop is the A3-12 guard: a control plane that
// accepts Register but drops every heartbeat stream must not make the agent
// retry at the floor. Before the fix the backoff reset to the floor after each
// short "success", pinning the loop at the minimum; now the backoff keeps
// growing until a session lasts backoffResetAfter.
func TestAgentShortSessionsDoNotHotLoop(t *testing.T) {
	fake := &fakeAgentService{heartbeatFail: true}
	dialOptions := startFakeCP(t, fake)

	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-flap",
		CertDir: t.TempDir(),
	}, discardLogger(), nil,
		WithHeartbeatInterval(time.Millisecond),
		WithBackoff(10*time.Millisecond, 320*time.Millisecond),
		WithDialOptions(dialOptions...),
	)

	// 250ms is far too short for the pre-fix floor-pinned loop to look like
	// anything but a hot loop (it would attempt roughly every 10ms), while the
	// growing schedule fits only a handful of attempts.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx, nil); err != nil {
		t.Fatalf("Run = %v; want nil after cancel", err)
	}
	if count := fake.registerCount(); count > 8 {
		t.Errorf("register attempts = %d in 250ms; the short-session backoff is not growing (hot loop)", count)
	}
}

func assertHeartbeat(t *testing.T, heartbeat *agentv1.HeartbeatRequest, wantContainers int64) {
	t.Helper()
	for name, value := range map[string]float64{
		"cpu":  heartbeat.GetCpuUsage(),
		"mem":  heartbeat.GetMemUsage(),
		"disk": heartbeat.GetDiskUsage(),
	} {
		if value < 0 || value > 1 {
			t.Errorf("%s usage = %v; want 0..1", name, value)
		}
	}
	// The I/O rates are zero on the first sample and on platforms that cannot
	// report them; they must never be negative or NaN.
	for name, value := range map[string]float64{
		"net rx":     heartbeat.GetNetRxBps(),
		"net tx":     heartbeat.GetNetTxBps(),
		"disk read":  heartbeat.GetDiskReadBps(),
		"disk write": heartbeat.GetDiskWriteBps(),
	} {
		if math.IsNaN(value) || value < 0 {
			t.Errorf("%s rate = %v; want a finite, non-negative rate", name, value)
		}
	}
	if heartbeat.GetContainerCount() != wantContainers {
		t.Errorf("container count = %d; want %d", heartbeat.GetContainerCount(), wantContainers)
	}
	if heartbeat.GetSentAt() == nil {
		t.Error("SentAt is nil")
	}
}

// parseCSRRequest decodes the first PKCS#10 certificate request in a PEM blob.
func parseCSRRequest(t *testing.T, csrPEM []byte) *x509.CertificateRequest {
	t.Helper()

	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("decode CSR: not PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse CSR: %v", err)
	}
	return csr
}

// newAgentConn builds a client connection with the agent's own dial options
// (including the fake CP's bufconn dialer).
func newAgentConn(t *testing.T, runner *Agent) *grpc.ClientConn {
	t.Helper()
	options, err := runner.dialOptionsFor()
	if err != nil {
		t.Fatalf("dialOptionsFor: %v", err)
	}
	conn, err := grpc.NewClient(runner.cfg.CPAddr, options...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestAgentRegisterDeadlineBounded is the FX-3 item-10 guard: a stalled control
// plane cannot pin a registration attempt past the per-attempt deadline.
func TestAgentRegisterDeadlineBounded(t *testing.T) {
	fake := &fakeAgentService{registerGate: make(chan struct{})}
	defer close(fake.registerGate)
	dialOptions := startFakeCP(t, fake)

	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-deadline",
		CertDir: t.TempDir(),
	}, discardLogger(), nil,
		WithDialOptions(dialOptions...),
		WithRegisterTimeout(100*time.Millisecond),
	)
	client := agentv1.NewAgentServiceClient(newAgentConn(t, runner))

	start := time.Now()
	_, err := runner.register(context.Background(), client)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("register = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("register took %v, want it bounded by the deadline", elapsed)
	}
}

// TestAgentHeartbeatCancelsStreamOnReturn is the FX-3 item-11 guard: the
// heartbeat stream's own context is canceled when the call returns, so a
// reconnect cannot leak the stream.
func TestAgentHeartbeatCancelsStreamOnReturn(t *testing.T) {
	fake := &fakeAgentService{heartbeatFail: true}
	dialOptions := startFakeCP(t, fake)

	var captured context.Context
	interceptor := grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		captured = ctx
		return streamer(ctx, desc, cc, method, opts...)
	})

	runner := NewAgent(Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-hb-cancel",
		CertDir: t.TempDir(),
	}, discardLogger(), nil,
		WithHeartbeatInterval(5*time.Millisecond),
		WithDialOptions(append(dialOptions, interceptor)...),
	)
	client := agentv1.NewAgentServiceClient(newAgentConn(t, runner))

	if err := runner.heartbeat(context.Background(), client); err == nil {
		t.Fatal("heartbeat returned nil, want the server's stream error")
	}
	if captured == nil {
		t.Fatal("the stream interceptor did not capture a context")
	}
	deadline := time.Now().Add(2 * time.Second)
	for captured.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if captured.Err() == nil {
		t.Fatal("the heartbeat stream context was not canceled on return")
	}
}

// TestRenewalDelaySchedulesBeforeExpiry pins the FX-3 item-7 renewal schedule.
func TestRenewalDelaySchedulesBeforeExpiry(t *testing.T) {
	certPEM, _, err := generateSelfSigned()
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}
	runner := NewAgent(Config{CertDir: t.TempDir()}, discardLogger(), nil, WithRenewBefore(30*24*time.Hour))

	got := runner.renewalDelay(certPEM)
	// selfSignedValidity is 365 days; renewing 30 days early leaves ~335.
	if got < 330*24*time.Hour || got > 340*24*time.Hour {
		t.Fatalf("renewalDelay = %v, want ~335d", got)
	}
	if runner.renewalDelay(nil) != 0 {
		t.Error("renewalDelay(empty) != 0")
	}
	if runner.renewalDelay([]byte("garbage")) != 0 {
		t.Error("renewalDelay(garbage) != 0")
	}
}

// TestAgentReRegistersBeforeCertificateExpiry is the FX-3 item-7 end-to-end
// behaviour: a short-lived certificate makes the agent re-register before it
// lapses rather than waiting for a stream failure.
func TestAgentReRegistersBeforeCertificateExpiry(t *testing.T) {
	fake := &fakeAgentService{cert: shortLivedCertPEM(t, 2*time.Second)}
	dialOptions := startFakeCP(t, fake)

	cfg := Config{
		CPAddr:  "passthrough:///bufnet",
		NodeID:  "node-renew",
		CertDir: t.TempDir(),
	}
	runner := NewAgent(cfg, discardLogger(), nil,
		WithHeartbeatInterval(20*time.Millisecond),
		WithBackoff(5*time.Millisecond, 20*time.Millisecond),
		WithRenewBefore(time.Second),
		WithDialOptions(dialOptions...),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- runner.Run(ctx, nil) }()

	deadline := time.Now().Add(4 * time.Second)
	for fake.registerCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if count := fake.registerCount(); count < 2 {
		t.Fatalf("register attempts = %d; want the agent to re-register before expiry", count)
	}
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// shortLivedCertPEM returns a self-signed certificate valid for validity.
func shortLivedCertPEM(t *testing.T, validity time.Duration) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "gotham-agent"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
