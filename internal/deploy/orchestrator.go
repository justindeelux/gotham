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
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/taskevents"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	defaultWorkers       = 2
	defaultQueueSize     = 64
	defaultMaxAttempts   = 3
	defaultStepTimeout   = 5 * time.Minute
	defaultBuildTimeout  = 20 * time.Minute
	defaultHealthTimeout = 90 * time.Second
	defaultHealthPoll    = 3 * time.Second
)

type job struct {
	app      Application
	dep      Deployment
	previous string
}

const proxySyncTimeout = time.Minute

type ProxySync interface {
	SyncServer(ctx context.Context, serverID uuid.UUID) error
}

type runState struct {
	app           Application
	dep           Deployment
	node          Node
	previous      string
	repoDir       string
	target        Target
	log           func(string)
	rec           *logRecorder
	composeRaw    string
	composeYAML   string
	composeImages []string
	composeCommit string
	composeEnv    map[string]string
	commitInfo    CommitInfo
	commitDone    bool
}

type Orchestrator struct {
	repo       Repository
	source     Source
	dial       DialFunc
	emitter    *Emitter
	tasks      *taskevents.Tracker
	taskEmit   *taskevents.Emitter
	notifier   Notifier
	secret     string
	logger     *slog.Logger
	proxy      ProxySync
	toolStatus ToolStatus
	gitLookup  gitHostLookupFunc

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

func newOrchestrator(cfg Config) *Orchestrator {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	warnManagedVolumeRoot(logger)
	repo := cfg.repository()
	source := cfg.Source
	if source == nil {
		source = gitSource{keys: repo, appTokens: cfg.AppTokens, creds: repo, logger: logger, lookup: cfg.GitLookupHost}
	}
	if cfg.GitAllowPrivateHosts {
		SetGitAllowPrivateHosts(true)
	}
	emitter := cfg.Emitter
	if emitter == nil {
		emitter = NewEmitter(defaultPublisher(cfg))
	}
	tasks := cfg.Tasks
	if tasks == nil {
		tasks = taskevents.NewTracker()
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

	checkGitVersionForPin(logger)

	return &Orchestrator{
		repo:          repo,
		source:        source,
		dial:          cfg.Dial,
		emitter:       emitter,
		tasks:         tasks,
		taskEmit:      taskevents.NewEmitter(emitter.pub),
		notifier:      cfg.Notifier,
		secret:        cfg.Secret,
		logger:        logger,
		proxy:         cfg.Proxy,
		toolStatus:    cfg.ToolStatus,
		gitLookup:     cfg.GitLookupHost,
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

func (o *Orchestrator) start() {
	o.startOnce.Do(func() {
		for i := 0; i < o.workers; i++ {
			o.wg.Add(1)
			go o.worker()
		}
	})
}

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

func (o *Orchestrator) Close() error {
	o.closeOnce.Do(func() {
		o.baseCancel()
		o.wg.Wait()
	})
	return nil
}

func (o *Orchestrator) run(ctx context.Context, j job) {
	st := &runState{
		app:      j.app,
		dep:      j.dep,
		previous: j.previous,
		target:   Target{ServerID: j.app.ServerID, DeploymentID: j.dep.ID},
		rec:      &logRecorder{},
	}
	st.log = func(line string) {
		st.rec.record(line)
		o.emitter.Log(ctx, st.target, line)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			o.panicFail(st, recovered)
			panic(recovered)
		}
	}()
	defer func() {
		if st.node != nil {
			if err := st.node.Close(); err != nil {
				o.logger.Debug("deploy: close agent connection", "deployment_id", st.dep.ID, "error", err)
			}
		}
	}()

	st.log("deployment " + string(st.dep.Kind) + " accepted")

	if isComposeApp(j.app) && j.dep.Kind == KindRollback {
		if strings.TrimSpace(j.dep.ComposeDocument) == "" {
			o.fail(ctx, st, fmt.Errorf("%w: rollback has no stored compose document", ErrValidation))
			return
		}
		if err := guardValidate(j.dep.ComposeDocument, j.app.ID); err != nil {
			o.fail(ctx, st, err)
			return
		}
	}

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

	if isComposeApp(j.app) {
		if err := o.probeComposeConfinement(ctx, st.node, st.app); err != nil {
			o.fail(ctx, st, err)
			return
		}
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
	o.removeRetired(ctx, st)
	o.syncProxy(ctx, st.app)
	o.logger.Info("deploy: deployment finished",
		"deployment_id", st.dep.ID, "application_id", st.app.ID, "image", st.dep.ImageTag)
}

func (o *Orchestrator) syncProxy(ctx context.Context, app Application) {
	if app.PrimaryDomain == nil {
		return
	}
	o.syncProxyServer(ctx, app.ServerID)
}

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

func (o *Orchestrator) execute(ctx context.Context, st *runState) error {
	steps := stepsForApp(st.dep.Kind, st.app.SourceType, st.app.Provider)
	for i, step := range steps {
		o.publishTask(ctx, st.app, st.dep, taskevents.StatusRunning, step, i*100/len(steps), "")
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

func (o *Orchestrator) runStep(ctx context.Context, st *runState, step State) error {
	if isComposeApp(st.app) {
		switch step {
		case StateCloning:
			return o.attempt(ctx, st, "clone", func() error {
				if err := o.cloneCompose(ctx, st); err != nil {
					return err
				}
				return o.recordCommit(ctx, st)
			})
		case StateBuilding:
			return o.attempt(ctx, st, "build", func() error { return o.buildCompose(ctx, st) })
		case StatePushing:
			return o.attempt(ctx, st, "push", func() error { return o.pushCompose(ctx, st) })
		case StateStarting:
			return o.attempt(ctx, st, "start", func() error { return o.startCompose(ctx, st) })
		default:
			return fmt.Errorf("deploy: unexpected step %q", step)
		}
	}
	switch step {
	case StateCloning:
		return o.attempt(ctx, st, "clone", func() error {
			if err := o.cloneSource(ctx, st.app, st.repoDir, st.log); err != nil {
				return err
			}
			return o.recordCommit(ctx, st)
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

const composeConfinementProbe = "services:\n  probe:\n    image: scratch\n"

func (o *Orchestrator) probeComposeConfinement(ctx context.Context, node Node, app Application) error {
	_, enforced, err := node.ComposeValidate(ctx, services.ProjectName(app.ID), []byte(composeConfinementProbe))
	if err != nil {
		return err
	}
	if !enforced {
		return fmt.Errorf("%w: node does not enforce confined compose documents; upgrade the agent on the node",
			ErrValidation)
	}
	return nil
}

func (o *Orchestrator) cloneSource(ctx context.Context, app Application, dir string, log func(string)) error {
	if !ValidSourceType(app.SourceType) {
		return fmt.Errorf("%w: unknown source type %q", ErrValidation, app.SourceType)
	}
	if !SourceTypeImplemented(app.SourceType) {
		return fmt.Errorf("%w: source type %q", ErrSourceNotImplemented, app.SourceType)
	}
	if app.SourceType == SourceDockerfile {
		if err := ValidateDockerfileContent(app.DockerfileContent); err != nil {
			return err
		}
		if err := writeDockerfileContext(dir, app.DockerfileContent); err != nil {
			return fmt.Errorf("deploy: write Dockerfile context: %w", err)
		}
		if log != nil {
			log("Dockerfile source materialized")
		}
		return nil
	}
	if isComposeApp(app) {
		_, _, err := o.resolveComposeContent(ctx, app, dir, log)
		return err
	}
	return o.source.Clone(ctx, app, dir, log)
}

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

func (o *Orchestrator) installedTools(ctx context.Context, serverID uuid.UUID, kind builds.EngineKind) map[string]bool {
	if o.toolStatus == nil || kind != builds.EngineAuto || serverID == uuid.Nil {
		return nil
	}
	tools, err := o.toolStatus.InstalledToolIDs(ctx, serverID)
	if err != nil {
		o.logger.Warn("deploy: tool lookup for auto-detection failed; using marker-only detection",
			"server_id", serverID, "error", err)
		return nil
	}
	return tools
}

func (o *Orchestrator) build(ctx context.Context, st *runState) error {
	kind := builds.EngineDockerfile
	buildArgs := st.app.BuildArgs
	if st.app.SourceType != SourceDockerfile {
		var err error
		kind, err = builds.ParseEngineKind(st.app.BuildPack)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}
	logs := newLineLog(st.log)
	builder := newNodeBuilder(st.node, st.app.ID, st.dep.ID, func(chunk []byte) {
		_, _ = logs.Write(chunk)
	})
	registry := builds.NewRegistry(builder)

	st.log("building with engine " + string(kind))
	ref, err := registry.Build(ctx, builds.BuildOptions{
		RepoDir:        st.repoDir,
		AppID:          st.app.ID,
		DeployID:       st.dep.ID,
		BuildPack:      kind,
		BuildArgs:      buildArgs,
		InstalledTools: o.installedTools(ctx, st.app.ServerID, kind),
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

func (o *Orchestrator) push(ctx context.Context, st *runState) error {
	if NormalizeSourceType(st.app.SourceType, st.app.Provider) == SourceImage {
		return o.pullImage(ctx, st)
	}
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

func (o *Orchestrator) pullImage(ctx context.Context, st *runState) error {
	ref := strings.TrimSpace(st.app.ImageRef)
	if st.dep.Kind == KindRollback {
		ref = strings.TrimSpace(st.dep.RegistryImage)
	}
	if ref == "" {
		return fmt.Errorf("%w: image source has no reference to pull", ErrValidation)
	}
	var username, password string
	if st.app.RegistryPasswordCiphertext != "" {
		pullHost, appHost := RegistryHost(ref), RegistryHost(st.app.ImageRef)
		if pullHost != "" && pullHost == appHost {
			opened, err := providers.OpenSecret(o.secret, st.app.RegistryPasswordCiphertext)
			if err != nil {
				return fmt.Errorf("%w: open registry credential: %v", ErrValidation, err)
			}
			username, password = st.app.RegistryUsername, opened
		} else {
			st.log("registry credential not sent: " + ref + " is on another registry host")
		}
	}
	st.log("pulling image " + ref)
	digest, err := st.node.PullWithAuth(ctx, ref, username, password)
	if err != nil {
		return err
	}
	if parsed, parseErr := ParseImageReference(ref); parseErr == nil && parsed.Pinned() &&
		digest != "" && !strings.EqualFold(digest, parsed.Digest) {
		return fmt.Errorf("%w: pulled digest %s does not match the pinned %s", ErrValidation, digest, parsed.Digest)
	}
	if st.dep.Kind != KindRollback {
		st.dep.ImageTag = ref
	}
	st.dep.RegistryImage = ref
	if digest != "" {
		st.dep.Digest = digest
	} else if st.dep.Kind == KindRollback {
		st.log("rollback pulled " + ref + " without a reported digest")
	} else {
		st.log("image pulled without a recorded digest: a rollback of this release will re-pull the tag " + ref)
	}
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return fmt.Errorf("deploy: persist image reference: %w", err)
	}
	line := "image pulled: " + ref
	if st.dep.Digest != "" {
		line += " (" + st.dep.Digest + ")"
	}
	st.log(line)
	return nil
}

func (o *Orchestrator) startContainer(ctx context.Context, st *runState) error {
	envVars, secrets, err := o.repo.ListEnvConfig(ctx, st.app.ID)
	if err != nil {
		return err
	}
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
	domains, err := o.repo.ListApplicationDomains(ctx, st.app.ID)
	if err != nil {
		return err
	}
	domainPorts, primaryPort := domainTargetPorts(domains)
	request, err := buildRunRequest(st.app, st.dep, envVars, secrets, storages, o.secret, domainPorts, primaryPort)
	if err != nil {
		return err
	}
	if strings.TrimSpace(st.dep.RegistryImage) == "" {
		return fmt.Errorf("%w: image %s was not pushed to the node registry; refusing to retire the running container",
			ErrValidation, st.dep.ImageTag)
	}

	if st.previous != "" {
		if err := o.retirePrevious(ctx, st); err != nil {
			return err
		}
	}
	o.reconcileDetached(ctx, st)

	st.log("starting container " + request.Name + " from " + request.Image)
	containerID, err := st.node.Run(ctx, request)
	if err != nil {
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

func sameContainer(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func (o *Orchestrator) reconcileDetached(ctx context.Context, st *runState) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	o.reconcileApplicationContainers(cleanupCtx, st)
}

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
		return "healthy", nil
	}
}

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
	if to.Terminal() {
		st.rec.record(stateLine(from, to))
		o.persistBuildLog(ctx, st)
	}
	o.emitter.State(ctx, st.target, from, to)
	if to.Terminal() {
		o.notify(ctx, st, to)
		if to == StateRunning {
			detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			o.publishTask(detached, st.app, st.dep, taskevents.StatusSucceeded, "", 100, "")
			cancel()
		}
	}
	return nil
}

func (o *Orchestrator) persistBuildLog(ctx context.Context, st *runState) {
	if o.repo == nil {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := o.repo.UpdateDeploymentBuildLog(persistCtx, st.dep.ID, st.rec.finalize()); err != nil {
		o.logger.Warn("deploy: could not persist build log",
			"deployment_id", st.dep.ID, "error", err)
	}
}

func (o *Orchestrator) fail(ctx context.Context, st *runState, cause error) {
	st.dep.Error = truncateError(cause)
	o.logger.Error("deploy: deployment failed",
		"deployment_id", st.dep.ID, "application_id", st.app.ID, "state", st.dep.State, "error", cause)

	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	st.log("deployment failed: " + st.dep.Error)
	adopted, err := o.forceFail(detached, st)
	if err != nil {
		o.logger.Error("deploy: could not persist failed state",
			"deployment_id", st.dep.ID, "error", err)
	}
	if !adopted {
		o.publishTask(detached, st.app, st.dep, taskevents.StatusFailed, "", 0, st.dep.Error)
	}
}

func (o *Orchestrator) forceFail(ctx context.Context, st *runState) (bool, error) {
	from := st.dep.State
	if current, err := o.repo.GetDeployment(ctx, st.dep.ApplicationID, st.dep.ID); err == nil && current.State.Terminal() {
		st.dep = current
		o.persistBuildLog(ctx, st)
		return true, nil
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
		return false, err
	}
	st.dep = updated
	if from != StateFailed {
		st.rec.record(stateLine(from, StateFailed))
		o.persistBuildLog(ctx, st)
		o.emitter.State(ctx, st.target, from, StateFailed)
		o.notify(ctx, st, StateFailed)
	}
	return false, nil
}

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

func (o *Orchestrator) publishTask(ctx context.Context, app Application, dep Deployment, status taskevents.Status, step State, progress int, errMsg string) {
	if o == nil || o.tasks == nil {
		return
	}
	ev := taskevents.Event{
		TaskID:        dep.ID.String(),
		Kind:          taskevents.KindDeploy,
		Name:          app.Name,
		Status:        status,
		Step:          string(step),
		Progress:      progress,
		Seq:           o.tasks.Next(dep.ID.String()),
		AppID:         app.ID.String(),
		DeploymentID:  dep.ID.String(),
		ServerID:      app.ServerID.String(),
		ProjectID:     app.ProjectID.String(),
		EnvironmentID: app.EnvironmentID.String(),
		Error:         errMsg,
	}
	o.tasks.Set(ev)
	if o.taskEmit != nil {
		o.taskEmit.Emit(ctx, ev)
	}
}

func (s *Service) TaskTracker() *taskevents.Tracker {
	if s == nil || s.Orchestrator == nil {
		return nil
	}
	return s.tasks
}

func (o *Orchestrator) panicFail(st *runState, recovered any) {
	if o == nil || st == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		st.dep.Error = truncateError(fmt.Errorf("deploy: panic: %v", recovered))
		detached, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		adopted, err := o.forceFail(detached, st)
		if err != nil {
			o.logger.Error("deploy: could not persist failed state after panic",
				"deployment_id", st.dep.ID, "error", err)
		}
		if !adopted {
			o.publishTask(detached, st.app, st.dep, taskevents.StatusFailed, "", 0, st.dep.Error)
		}
	}()
}

type lineLog struct {
	mu      sync.Mutex
	pending []byte
	emit    func(string)
}

func newLineLog(emit func(string)) *lineLog {
	return &lineLog{emit: emit}
}

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

func (l *lineLog) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) > 0 {
		l.emitLine(string(l.pending))
		l.pending = nil
	}
}

func (l *lineLog) emitLine(line string) {
	if l.emit == nil || strings.TrimSpace(line) == "" {
		return
	}
	l.emit(line)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
