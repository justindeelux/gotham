package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Orchestrator tunables. Every step runs under its own timeout; a build gets a
// much larger budget than a clone or a container start because it streams a
// whole toolchain run.
const (
	defaultWorkers       = 2
	defaultQueueSize     = 64
	defaultMaxAttempts   = 3
	defaultStepTimeout   = 5 * time.Minute
	defaultBuildTimeout  = 20 * time.Minute
	defaultHealthTimeout = 90 * time.Second
	defaultHealthPoll    = 3 * time.Second
)

// job is one queued deployment together with everything its run needs: the
// application snapshot taken at submit time, and the container the new
// release retires (best-effort stop before the new one starts).
type job struct {
	app      Application
	dep      Deployment
	previous string
}

// proxySyncTimeout bounds one best-effort proxy resync. A sync that must
// bootstrap the Traefik container (image pull) may exceed it; the node's
// validation bootstrap or a manual POST /v1/proxy/sync completes it then.
const proxySyncTimeout = time.Minute

// ProxySync receives a node id after a change that affects its routing
// configuration. It is implemented by *proxy.SyncService and wired by
// internal/server; nil disables proxy synchronization.
type ProxySync interface {
	SyncServer(ctx context.Context, serverID uuid.UUID) error
}

// runState is the mutable state of one running deployment.
type runState struct {
	app      Application
	dep      Deployment
	node     Node
	previous string
	repoDir  string
	target   Target
	log      func(string)
}

// Orchestrator runs deployment state machines on a fixed worker pool. Each
// run walks stepsFor(kind), persisting every transition and mirroring it to
// the realtime log channel; only ErrAgentUnavailable is retried, and only
// within the step that failed.
type Orchestrator struct {
	repo     Repository
	source   Source
	dial     DialFunc
	emitter  *Emitter
	notifier Notifier
	secret   string
	logger   *slog.Logger
	proxy    ProxySync

	queue         chan job
	workers       int
	maxAttempts   int
	stepTimeout   time.Duration
	buildTimeout  time.Duration
	healthTimeout time.Duration
	healthPoll    time.Duration

	baseCtx    context.Context
	baseCancel context.CancelFunc
	wg         sync.WaitGroup
	startOnce  sync.Once
	closeOnce  sync.Once
}

// newOrchestrator wires an Orchestrator from cfg, filling every unset tunable
// with its default.
func newOrchestrator(cfg Config) *Orchestrator {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	warnManagedVolumeRoot(logger)
	// The repository doubles as the cloner's deploy-key resolver, so a key
	// lookup and a deployment share one connection pool (and one secret).
	repo := cfg.repository()
	source := cfg.Source
	if source == nil {
		source = gitSource{keys: repo, logger: logger}
	}
	emitter := cfg.Emitter
	if emitter == nil {
		emitter = NewEmitter(defaultPublisher(cfg))
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	stepTimeout := cfg.StepTimeout
	if stepTimeout <= 0 {
		stepTimeout = defaultStepTimeout
	}
	buildTimeout := cfg.BuildTimeout
	if buildTimeout <= 0 {
		buildTimeout = defaultBuildTimeout
	}
	healthTimeout := cfg.HealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = defaultHealthTimeout
	}
	healthPoll := cfg.HealthPoll
	if healthPoll <= 0 {
		healthPoll = defaultHealthPoll
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())

	return &Orchestrator{
		repo:          repo,
		source:        source,
		dial:          cfg.Dial,
		emitter:       emitter,
		notifier:      cfg.Notifier,
		secret:        cfg.Secret,
		logger:        logger,
		proxy:         cfg.Proxy,
		queue:         make(chan job, queueSize),
		workers:       workers,
		maxAttempts:   maxAttempts,
		stepTimeout:   stepTimeout,
		buildTimeout:  buildTimeout,
		healthTimeout: healthTimeout,
		healthPoll:    healthPoll,
		baseCtx:       baseCtx,
		baseCancel:    baseCancel,
	}
}

// enqueue queues a deployment for the worker pool, starting the pool on first
// use. A full queue blocks until the caller's context ends, so a submit
// never silently drops a deployment.
func (o *Orchestrator) enqueue(ctx context.Context, j job) error {
	o.start()
	select {
	case o.queue <- j:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("deploy: queue deployment: %w", ctx.Err())
	case <-o.baseCtx.Done():
		return errors.New("deploy: orchestrator is shutting down")
	}
}

// start boots the worker pool exactly once.
func (o *Orchestrator) start() {
	o.startOnce.Do(func() {
		for i := 0; i < o.workers; i++ {
			o.wg.Add(1)
			go o.worker()
		}
	})
}

// worker consumes queued jobs until the orchestrator shuts down.
func (o *Orchestrator) worker() {
	defer o.wg.Done()
	for {
		select {
		case <-o.baseCtx.Done():
			return
		case j := <-o.queue:
			o.run(o.baseCtx, j)
		}
	}
}

// Close stops the pool and waits for in-flight runs to finish (they observe
// the cancelled base context and terminate their steps early).
func (o *Orchestrator) Close() error {
	o.closeOnce.Do(func() {
		o.baseCancel()
		o.wg.Wait()
	})
	return nil
}

// run drives one deployment from queued to a terminal state: dial the node,
// walk the steps, and on any error record it as the terminal failure.
func (o *Orchestrator) run(ctx context.Context, j job) {
	st := &runState{
		app:      j.app,
		dep:      j.dep,
		previous: j.previous,
		target:   Target{ServerID: j.app.ServerID, DeploymentID: j.dep.ID},
	}
	st.log = func(line string) {
		o.emitter.Log(ctx, st.target, line)
	}
	defer func() {
		if st.node != nil {
			if err := st.node.Close(); err != nil {
				o.logger.Debug("deploy: close agent connection", "deployment_id", st.dep.ID, "error", err)
			}
		}
	}()

	st.log("deployment " + string(st.dep.Kind) + " accepted")

	if err := o.attempt(ctx, st, "connect to node", func() error {
		node, err := o.dialNode(ctx, j.app.ServerID)
		if err != nil {
			return err
		}
		st.node = node
		return nil
	}); err != nil {
		o.fail(ctx, st, err)
		return
	}

	baseDir, err := os.MkdirTemp("", "gotham-deploy-*")
	if err != nil {
		o.fail(ctx, st, fmt.Errorf("deploy: workspace: %w", err))
		return
	}
	defer func() { _ = os.RemoveAll(baseDir) }()
	st.repoDir = filepath.Join(baseDir, "repo")

	if err := o.execute(ctx, st); err != nil {
		o.fail(ctx, st, err)
		return
	}
	// execute persisted the terminal running state, so the deployment row is
	// terminal here: removeRetired and syncProxy are idempotent best-effort
	// follow-ups that cannot change the outcome.
	//
	// The replacement is live: remove the retired container so its layers do not
	// accumulate. Best effort — rollback redeploys the registry image, not the
	// container, and the removal preserves named volumes and bind directories.
	o.removeRetired(ctx, st)
	// A release reached running: the node's routing may now point at the new
	// container's published port. Best effort, see syncProxy.
	o.syncProxy(ctx, st.app)
	o.logger.Info("deploy: deployment finished",
		"deployment_id", st.dep.ID, "application_id", st.app.ID, "image", st.dep.ImageTag)
}

// syncProxy pushes the routing state of the node hosting app. Applications
// without a domain have no route to refresh, so they are skipped.
func (o *Orchestrator) syncProxy(ctx context.Context, app Application) {
	if app.BaseDomain == "" {
		return
	}
	o.syncProxyServer(ctx, app.ServerID)
}

// syncProxyServer pushes the routing state of one node, best effort:
// configuration is a pure function of the database, so a failed push is
// repaired by the next mutation or by POST /v1/proxy/sync, and it must never
// fail the operation that triggered it.
func (o *Orchestrator) syncProxyServer(ctx context.Context, serverID uuid.UUID) {
	if o.proxy == nil || serverID == uuid.Nil {
		return
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), proxySyncTimeout)
	defer cancel()
	if err := o.proxy.SyncServer(syncCtx, serverID); err != nil {
		o.logger.Warn("deploy: proxy sync failed", "server_id", serverID.String(), "error", err)
	}
}

// execute walks the deployment's steps, persisting each transition before the
// step runs and stopping at the first failure.
func (o *Orchestrator) execute(ctx context.Context, st *runState) error {
	for _, step := range stepsFor(st.dep.Kind) {
		if err := o.transition(ctx, st, step); err != nil {
			return err
		}
		timeout := o.stepTimeout
		if step == StateBuilding {
			timeout = o.buildTimeout
		}
		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		err := o.runStep(stepCtx, st, step)
		cancel()
		if err != nil {
			st.log("step " + string(step) + " failed: " + truncateError(err))
			return err
		}
	}
	return o.transition(ctx, st, StateRunning)
}

// runStep dispatches one state-machine step.
func (o *Orchestrator) runStep(ctx context.Context, st *runState, step State) error {
	switch step {
	case StateCloning:
		return o.attempt(ctx, st, "clone", func() error {
			return o.cloneSource(ctx, st.app, st.repoDir, st.log)
		})
	case StateBuilding:
		return o.attempt(ctx, st, "build", func() error { return o.build(ctx, st) })
	case StatePushing:
		return o.attempt(ctx, st, "push", func() error { return o.push(ctx, st) })
	case StateStarting:
		return o.attempt(ctx, st, "start", func() error { return o.startContainer(ctx, st) })
	default:
		return fmt.Errorf("deploy: unexpected step %q", step)
	}
}

// cloneSource selects the fetch step by application source type (GS-2).
// Git-backed types share the git clone path (the cloner keys off the
// deploy key, so legacy rows with an empty type behave as before);
// Dockerfile, Compose and image sources have no fetch step until GS-7..GS-9
// and fail closed with ErrSourceNotImplemented.
func (o *Orchestrator) cloneSource(ctx context.Context, app Application, dir string, log func(string)) error {
	switch app.SourceType {
	case "", SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp:
		return o.source.Clone(ctx, app, dir, log)
	case SourceDockerfile, SourceCompose, SourceImage:
		return fmt.Errorf("%w: source type %q", ErrSourceNotImplemented, app.SourceType)
	default:
		return fmt.Errorf("%w: unknown source type %q", ErrValidation, app.SourceType)
	}
}

// attempt runs op up to MaxAttempts times, retrying only ErrAgentUnavailable
// (the agent being unreachable), and persists the attempt counter so the API
// shows retries. Every other failure — a build error, a failed healthcheck, a
// step timeout — is terminal.
func (o *Orchestrator) attempt(ctx context.Context, st *runState, what string, op func() error) error {
	var err error
	for try := 1; try <= o.maxAttempts; try++ {
		if st.dep.Attempt != int32(try) {
			st.dep.Attempt = int32(try)
			if _, updateErr := o.repo.UpdateDeployment(context.WithoutCancel(ctx), st.dep); updateErr != nil {
				return fmt.Errorf("deploy: persist attempt: %w", updateErr)
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", what, ctxErr)
		}
		if err = op(); err == nil {
			return nil
		}
		if !errors.Is(err, ErrAgentUnavailable) || try == o.maxAttempts {
			return err
		}
		st.log(fmt.Sprintf("%s: agent unavailable (%v), retrying %d/%d", what, err, try+1, o.maxAttempts))
	}
	return err
}

// build runs the application's build engine over the cloned tree. The node
// builder streams the context to the agent (BuildImage), which builds and
// pushes to the node-local registry; toolchain engines (railpack, buildpacks)
// shell out on the control plane instead and leave the image in the local
// daemon — the pushing step accounts for that difference.
func (o *Orchestrator) build(ctx context.Context, st *runState) error {
	kind, err := builds.ParseEngineKind(st.app.BuildPack)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	logs := newLineLog(st.log)
	builder := newNodeBuilder(st.node, st.app.ID, st.dep.ID, func(chunk []byte) {
		_, _ = logs.Write(chunk)
	})
	registry := builds.NewRegistry(builder)

	st.log("building with engine " + string(kind))
	ref, err := registry.Build(ctx, builds.BuildOptions{
		RepoDir:   st.repoDir,
		AppID:     st.app.ID,
		DeployID:  st.dep.ID,
		BuildPack: kind,
		Labels: map[string]string{
			labelAppID:        st.app.ID.String(),
			labelDeploymentID: st.dep.ID.String(),
		},
		LogWriter: logs,
	})
	logs.Flush()
	if err != nil {
		return err
	}

	st.dep.ImageTag = ref.Tag
	if outcome, ok := builder.outcomeOf(); ok {
		st.dep.RegistryImage = outcome.RegistryImage
		st.dep.Digest = outcome.Digest
	} else if ref.Digest != "" {
		st.dep.Digest = ref.Digest
	}
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return fmt.Errorf("deploy: persist image reference: %w", err)
	}
	st.log("image built: " + st.dep.ImageTag)
	return nil
}

// push makes sure the built image is usable from the node before any running
// container is retired. Every engine now builds through the node and returns a
// registry reference, so a missing reference is a hard error: retiring the
// previous container and only then discovering the image is absent would take
// the application down with no replacement.
func (o *Orchestrator) push(ctx context.Context, st *runState) error {
	if strings.TrimSpace(st.dep.RegistryImage) == "" {
		return fmt.Errorf("%w: image %s was not pushed to the node registry; refusing to retire the running container",
			ErrValidation, st.dep.ImageTag)
	}
	if err := st.node.Pull(ctx, st.dep.RegistryImage); err != nil {
		return err
	}
	line := "image available in the node registry: " + st.dep.RegistryImage
	if st.dep.Digest != "" {
		line += " (" + st.dep.Digest + ")"
	}
	st.log(line)
	return nil
}

// startContainer retires the container this deployment replaces, runs the new
// one with the application's runtime payload, and gates success on the
// post-start healthcheck.
//
// Ordering is a correctness property: the complete runtime payload is
// assembled and validated (config queries, secret decryption, volume specs)
// BEFORE the previous container is stopped, so a configuration failure fails
// the deploy without taking the live release down. Retirement itself must be
// confirmed — an unconfirmed stop would leave two releases running.
func (o *Orchestrator) startContainer(ctx context.Context, st *runState) error {
	envVars, secrets, err := o.repo.ListEnvConfig(ctx, st.app.ID)
	if err != nil {
		return err
	}
	// Shared variables layer beneath the application's own (project <
	// environment < application). The read lands before the payload is
	// assembled and validated, so a failure still fails the deploy without
	// retiring the live release. A preview inherits its base application's
	// environment, so its deploys merge the same scopes.
	sharedProject, sharedEnvironment, err := o.repo.ListSharedVariables(ctx, st.app.ProjectID, st.app.EnvironmentID)
	if err != nil {
		return err
	}
	envVars, secrets = mergeSharedVariables(sharedProject, sharedEnvironment, envVars, secrets)
	storages, err := o.repo.ListStorages(ctx, st.app.ID)
	if err != nil {
		return err
	}
	warnLegacyStorageResolution(o.logger, st.app.ID, storages)
	request, err := buildRunRequest(st.app, st.dep, envVars, secrets, storages, o.secret)
	if err != nil {
		return err
	}
	// A build that produced no registry reference has no image the node can
	// pull: refuse before stopping the live release rather than discovering it
	// after the replacement starts. This duplicates the push step's guard on
	// purpose: startContainer is the retirement boundary and must stand alone.
	if strings.TrimSpace(st.dep.RegistryImage) == "" {
		return fmt.Errorf("%w: image %s was not pushed to the node registry; refusing to retire the running container",
			ErrValidation, st.dep.ImageTag)
	}

	if st.previous != "" {
		if err := o.retirePrevious(ctx, st); err != nil {
			return err
		}
	}
	// Clear any other container of this application before Run. A lost Run
	// response whose id was never recorded would otherwise keep the
	// deterministic name and, with a pinned host port, that port — wedging
	// every later deploy. Detached and bounded like the other cleanup paths.
	o.reconcileDetached(ctx, st)

	st.log("starting container " + request.Name + " from " + request.Image)
	containerID, err := st.node.Run(ctx, request)
	if err != nil {
		// The create may have succeeded before the failure; remove whatever it
		// left so it cannot outlive the run untracked. reconcileDetached is
		// itself detached and bounded, so a step timeout or shutdown (exactly
		// when an untracked container is most likely) still cleans up.
		o.reconcileDetached(ctx, st)
		return err
	}
	st.dep.ContainerID = containerID
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return fmt.Errorf("deploy: persist container: %w", err)
	}
	st.log("container " + shortID(containerID) + " started")

	healthCtx, cancel := context.WithTimeout(ctx, o.healthTimeout)
	defer cancel()
	return o.waitHealthy(healthCtx, st, containerID)
}

// retirePrevious stops the container this deployment replaces and requires a
// confirmed retirement: the stop succeeded, or the container is verifiably
// already gone. Every other outcome fails the deploy closed, so a failed stop
// can never leave two releases running.
func (o *Orchestrator) retirePrevious(ctx context.Context, st *runState) error {
	stopErr := st.node.Stop(ctx, st.previous)
	if stopErr == nil {
		st.log("stopped previous container " + shortID(st.previous))
		return nil
	}
	present, listErr := o.containerPresent(ctx, st, st.previous)
	if listErr == nil && !present {
		st.log("previous container " + shortID(st.previous) + " is already gone")
		return nil
	}
	return fmt.Errorf("deploy: failed to retire previous container %s: %w", shortID(st.previous), stopErr)
}

// containerPresent reports whether containerID (or its unambiguous prefix) is
// still known to the node.
func (o *Orchestrator) containerPresent(ctx context.Context, st *runState, containerID string) (bool, error) {
	containers, err := st.node.Containers(ctx)
	if err != nil {
		return false, err
	}
	for _, candidate := range containers {
		if sameContainer(candidate.GetId(), containerID) {
			return true, nil
		}
	}
	return false, nil
}

// sameContainer reports whether two Docker ids refer to the same container,
// tolerating the short-id prefixes the agent may report.
func sameContainer(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// reconcileDetached runs reconcileApplicationContainers on a bounded context
// detached from the run, so a cancelled step or shutdown still lets the
// best-effort cleanup reach the node.
func (o *Orchestrator) reconcileDetached(ctx context.Context, st *runState) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	o.reconcileApplicationContainers(cleanupCtx, st)
}

// reconcileApplicationContainers removes every container of this application
// except the one the deployment replaces (st.previous, already retired). It is
// what clears an orphan left by a lost Run response whose deployment id was
// never recorded: such an orphan keeps its deterministic name and, with a
// pinned host port, that port, so it would otherwise wedge every later deploy.
// It is safe under the current fencing — delete, a move and manual control are
// refused while this deployment is non-terminal, so no other release of the
// application can be live — and st.previous is kept until the replacement is
// confirmed. Best effort: a listing or removal failure is logged, never fatal.
func (o *Orchestrator) reconcileApplicationContainers(ctx context.Context, st *runState) {
	containers, err := st.node.Containers(ctx)
	if err != nil {
		st.log("could not list containers to reconcile the application: " + truncateError(err))
		return
	}
	for _, candidate := range containers {
		if candidate.GetLabels()[labelAppID] != st.app.ID.String() {
			continue
		}
		if st.previous != "" && sameContainer(candidate.GetId(), st.previous) {
			continue
		}
		if err := st.node.Remove(ctx, candidate.GetId()); err != nil {
			st.log("could not remove leftover container " + shortID(candidate.GetId()) + ": " + truncateError(err))
			continue
		}
		st.log("removed leftover container " + shortID(candidate.GetId()))
	}
}

// removeRetired removes the container this deployment replaced, once the new
// release is running. Best effort: a failed removal only leaves layers behind.
func (o *Orchestrator) removeRetired(ctx context.Context, st *runState) {
	if st.previous == "" {
		return
	}
	if err := st.node.Remove(ctx, st.previous); err != nil {
		st.log("could not remove retired container " + shortID(st.previous) + ": " + truncateError(err))
		return
	}
	st.log("removed retired container " + shortID(st.previous))
}

// waitHealthy polls the node until the container reports healthy, exits or
// the health window closes. A container without a Docker healthcheck is
// healthy as soon as it is running, which is Docker's own semantics: only an
// explicit "(unhealthy)" marker or a non-running state fails the deploy.
func (o *Orchestrator) waitHealthy(ctx context.Context, st *runState, containerID string) error {
	ticker := time.NewTicker(o.healthPoll)
	defer ticker.Stop()
	for {
		state, err := o.healthState(ctx, st, containerID)
		switch {
		case err != nil:
			st.log("healthcheck: " + err.Error())
		case state == "healthy":
			st.log("container " + shortID(containerID) + " is healthy")
			return nil
		case state == "unhealthy":
			return fmt.Errorf("%w: container %s reported unhealthy", ErrHealthcheck, shortID(containerID))
		case state == "exited":
			return fmt.Errorf("%w: container %s is not running", ErrHealthcheck, shortID(containerID))
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: container %s did not become healthy within %s",
				ErrHealthcheck, shortID(containerID), o.healthTimeout)
		case <-ticker.C:
		}
	}
}

// healthState inspects one container on the node and classifies it as
// "healthy", "starting", "unhealthy" or "exited".
func (o *Orchestrator) healthState(ctx context.Context, st *runState, containerID string) (string, error) {
	containers, err := st.node.Containers(ctx)
	if err != nil {
		return "", err
	}
	var info *agentv1.ContainerInfo
	for _, candidate := range containers {
		id := candidate.GetId()
		if id == containerID || strings.HasPrefix(containerID, id) || strings.HasPrefix(id, containerID) {
			info = candidate
			break
		}
	}
	if info == nil {
		return "starting", nil
	}
	status := strings.ToLower(info.GetStatus())
	switch {
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy", nil
	case strings.Contains(status, "(health: starting)"), strings.Contains(status, "health: starting"):
		return "starting", nil
	case info.GetState() != "running":
		return "exited", nil
	default:
		// "(healthy)" or a container with no healthcheck configured at all.
		return "healthy", nil
	}
}

// transition persists one legal state-machine edge and mirrors it to the
// realtime channel. The deployment clock starts when it leaves queued and
// stops on a terminal state. In-memory state advances only after the write
// succeeds: a failed persist must not leave the run believing it reached a
// state the database never saw (which would make fail() unable to record the
// terminal failure and wedge the active-deployment index).
func (o *Orchestrator) transition(ctx context.Context, st *runState, to State) error {
	from := st.dep.State
	if !CanTransition(from, to) {
		return fmt.Errorf("deploy: illegal transition %s → %s", from, to)
	}
	next := st.dep
	if from == StateQueued {
		next.StartedAt = time.Now().UTC()
	}
	if to.Terminal() {
		next.FinishedAt = time.Now().UTC()
	}
	next.State = to

	updated, err := o.repo.UpdateDeployment(ctx, next)
	if err != nil {
		return fmt.Errorf("deploy: persist state %s: %w", to, err)
	}
	st.dep = updated
	o.emitter.State(ctx, st.target, from, to)
	if to.Terminal() {
		o.notify(ctx, st, to)
	}
	return nil
}

// fail records the terminal failure: the error text on the row, the failed
// transition and one log line. It uses a detached context so a cancelled run
// (shutdown, step timeout) still leaves a terminal row behind — otherwise the
// partial unique index would block every future deploy of the application. The
// failed state is forced directly rather than routed through CanTransition, so
// a run whose in-memory state was left terminal by a failed running write
// still records the failure.
func (o *Orchestrator) fail(ctx context.Context, st *runState, cause error) {
	st.dep.Error = truncateError(cause)
	o.logger.Error("deploy: deployment failed",
		"deployment_id", st.dep.ID, "application_id", st.app.ID, "state", st.dep.State, "error", cause)

	fresh, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := o.forceFail(fresh, st); err != nil {
		o.logger.Error("deploy: could not persist failed state",
			"deployment_id", st.dep.ID, "error", err)
		return
	}
	st.log("deployment failed: " + st.dep.Error)
}

// forceFail writes the terminal failed state unconditionally, emitting the
// transition event only when it is a real edge. It is the failure-path
// counterpart of transition: it must succeed even when the in-memory state no
// longer admits a legal edge to failed. It refuses to downgrade a row that a
// previous ambiguous write already committed terminal (running or failed).
func (o *Orchestrator) forceFail(ctx context.Context, st *runState) error {
	from := st.dep.State
	if current, err := o.repo.GetDeployment(ctx, st.dep.ApplicationID, st.dep.ID); err == nil && current.State.Terminal() {
		st.dep = current
		return nil
	}
	next := st.dep
	next.State = StateFailed
	if from == StateQueued && next.StartedAt.IsZero() {
		next.StartedAt = time.Now().UTC()
	}
	if next.FinishedAt.IsZero() {
		next.FinishedAt = time.Now().UTC()
	}
	updated, err := o.repo.UpdateDeployment(ctx, next)
	if err != nil {
		return err
	}
	st.dep = updated
	if from != StateFailed {
		o.emitter.State(ctx, st.target, from, StateFailed)
		o.notify(ctx, st, StateFailed)
	}
	return nil
}

// dialNode opens the agent of the deployment's server.
func (o *Orchestrator) dialNode(ctx context.Context, serverID uuid.UUID) (Node, error) {
	if o.dial == nil {
		return nil, fmt.Errorf("%w: agent dialer is not configured", ErrAgentUnavailable)
	}
	node, err := o.dial(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("%w: dial returned no node", ErrAgentUnavailable)
	}
	return node, nil
}

// lineLog accumulates streamed output and forwards complete lines to the
// deploy log, so partial chunks from a build tool never split a log event.
type lineLog struct {
	mu      sync.Mutex
	pending []byte
	emit    func(string)
}

// newLineLog binds a line writer to one log function.
func newLineLog(emit func(string)) *lineLog {
	return &lineLog{emit: emit}
}

// io.Writer implementation used as builds.BuildOptions.LogWriter and for raw
// build chunks.
func (l *lineLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.pending, p...)
	for {
		index := strings.IndexByte(string(l.pending), '\n')
		if index < 0 {
			break
		}
		line := string(l.pending[:index])
		l.pending = l.pending[index+1:]
		l.emitLine(strings.TrimRight(line, "\r"))
	}
	return len(p), nil
}

// Flush emits whatever partial line is buffered.
func (l *lineLog) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) > 0 {
		l.emitLine(string(l.pending))
		l.pending = nil
	}
}

// emitLine forwards one line to the log function, skipping blank ones.
func (l *lineLog) emitLine(line string) {
	if l.emit == nil || strings.TrimSpace(line) == "" {
		return
	}
	l.emit(line)
}

// shortID renders the head of a Docker ID for log lines.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
