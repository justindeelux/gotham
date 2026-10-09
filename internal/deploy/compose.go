package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

// guardOptions scopes the confinement allowlist to one application: its id
// owns the managed binds, under the control plane's configured root.
func guardOptions(appID uuid.UUID) composeguard.Options {
	return composeguard.Options{AppID: appID.String(), ManagedRoot: managedVolumeRoot()}
}

// guardValidate runs the confinement allowlist and maps a rejection onto
// the deploy validation sentinel, so the routes answer 400 instead of 500.
// The message is preserved verbatim (with its offending path) for the
// wizard and the deploy log.
func guardValidate(content string, appID uuid.UUID) error {
	if err := composeguard.Validate(content, guardOptions(appID)); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// ValidateComposeContent checks pasted compose text without executing
// anything: it must be non-empty, fit the size limit and pass the
// confinement allowlist (at least one service with an image, no build
// contexts, host namespaces, capabilities, devices, secrets, host binds
// outside the application's managed directory, or any other key outside
// the known-safe set). Anything deeper (a valid image, a working config)
// is the node's `docker compose config` at deploy time, behind the same
// allowlist enforced on the node.
func ValidateComposeContent(content string, appID uuid.UUID) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: compose content is required", ErrValidation)
	}
	if err := checkTextBytes(content, "compose content"); err != nil {
		return err
	}
	if len(content) > MaxComposeBytes {
		return fmt.Errorf("%w: compose content exceeds %d bytes", ErrValidation, MaxComposeBytes)
	}
	return guardValidate(content, appID)
}

// isComposeApp reports whether the application deploys through the compose
// runner: the document (stored or repo-backed) runs on the node with
// `pull + up -d`, and the routed web service's container is the release.
func isComposeApp(app Application) bool {
	return NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose
}

// cloneCompose resolves the raw compose document of a compose run into the
// run state. A rollback resolves the target release's stored raw document
// (re-rendered with the current environment at the build step); a fresh
// deploy resolves the live row. Nothing is executed.
func (o *Orchestrator) cloneCompose(ctx context.Context, st *runState) error {
	if st.dep.Kind == KindRollback {
		if strings.TrimSpace(st.dep.ComposeDocument) == "" {
			return fmt.Errorf("%w: rollback has no stored compose document", ErrValidation)
		}
		st.composeRaw = st.dep.ComposeDocument
		st.composeCommit = st.dep.ComposeCommit
		// No clone happens here, so there is nothing for recordCommit to
		// read: mark it done and keep the target's commit copied at creation.
		st.commitDone = true
		return nil
	}
	content, info, err := o.resolveComposeContent(ctx, st.app, st.repoDir, st.log)
	if err != nil {
		return err
	}
	st.composeRaw = content
	st.composeCommit = info.SHA
	// The commit was just read: recordCommit reuses it, no second git read.
	st.commitInfo = info
	st.commitDone = true
	return nil
}

// resolveComposeContent returns the raw compose document of an application
// and, for repo-backed sources, the commit it was read from: the stored text
// for pasted sources (validated, never logged), or the referenced file of a
// repo-backed source cloned into dir. A preview inherits the base
// document's shape with its managed binds rewritten to its own directory,
// so it can never mount (or write) the base application's data. It is
// shared by the deploy run and the best-effort teardown, so both resolve
// the same file.
func (o *Orchestrator) resolveComposeContent(ctx context.Context, app Application, dir string, log func(string)) (content string, info CommitInfo, err error) {
	if strings.TrimSpace(app.ComposeContent) != "" {
		content = app.ComposeContent
		if app.IsPreview {
			content = rewriteManagedBindsForPreview(content, managedVolumeRoot(), app.ID)
		}
		if err := ValidateComposeContent(content, app.ID); err != nil {
			return "", CommitInfo{}, err
		}
		if err := ValidateComposeService(content, app.ComposeService); err != nil {
			return "", CommitInfo{}, err
		}
		if log != nil {
			log("compose source materialized")
		}
		return content, CommitInfo{}, nil
	}
	if err := ValidateComposeFilePath(app.ComposeFile); err != nil {
		return "", CommitInfo{}, err
	}
	if err := o.source.Clone(ctx, app, dir, log); err != nil {
		return "", CommitInfo{}, err
	}
	content, err = readComposeFile(dir, app.ComposeFile)
	if err != nil {
		return "", CommitInfo{}, err
	}
	info = readCommit(dir)
	if app.IsPreview {
		content = rewriteManagedBindsForPreview(content, managedVolumeRoot(), app.ID)
	}
	if err := ValidateComposeContent(content, app.ID); err != nil {
		return "", CommitInfo{}, err
	}
	if err := ValidateComposeService(content, app.ComposeService); err != nil {
		return "", CommitInfo{}, err
	}
	if log != nil {
		log("compose file " + strings.TrimSpace(app.ComposeFile) + " read from the repository")
	}
	return content, info, nil
}

// readComposeFile reads the referenced compose file out of a checkout,
// keeping the read inside dir and under the size limit. The path was
// validated relative and clean before the join, and the lexical check plus
// the symlink walk below keep a hostile checkout from escaping through a
// symlink component.
func readComposeFile(dir, ref string) (string, error) {
	joined := filepath.Join(dir, filepath.Clean(strings.TrimSpace(ref)))
	rel, err := filepath.Rel(dir, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: compose file path %q escapes the repository", ErrValidation, ref)
	}
	if err := rejectSymlinkComponents(dir, joined); err != nil {
		return "", fmt.Errorf("%w: compose file path %q: %v", ErrValidation, ref, err)
	}
	file, err := os.Open(joined)
	if err != nil {
		return "", fmt.Errorf("%w: read compose file %q: %v", ErrValidation, strings.TrimSpace(ref), err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxComposeBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: read compose file %q: %v", ErrValidation, strings.TrimSpace(ref), err)
	}
	if len(data) > MaxComposeBytes {
		return "", fmt.Errorf("%w: compose content exceeds %d bytes", ErrValidation, MaxComposeBytes)
	}
	return string(data), nil
}

// loadComposeEnv assembles the substitution environment of a compose run:
// the application's plain env vars and opened secrets, layered over the
// shared scopes exactly like a container start, plus the PORT default. The
// map also redacts node errors on the compose path, so secret values the
// CLI echoes never reach the deployment row or the log.
func (o *Orchestrator) loadComposeEnv(ctx context.Context, app Application) (map[string]string, error) {
	envVars, secrets, err := o.repo.ListEnvConfig(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	sharedProject, sharedEnvironment, err := o.repo.ListSharedVariables(ctx, app.ProjectID, app.EnvironmentID)
	if err != nil {
		return nil, err
	}
	return composeProjectEnv(app, envVars, secrets, sharedProject, sharedEnvironment, o.secret)
}

// renderComposeDocument renders the raw document against the merged project
// environment (app env and secrets are the compose project env) and injects
// the web service's port mapping, so the stored domain/port routing reaches
// that service. It answers the rendered document, the image references it
// declares and the environment it was rendered with. A render failure is
// redacted of environment values, exactly like the Services surface.
func (o *Orchestrator) renderComposeDocument(ctx context.Context, app Application, raw string) (rendered string, images []string, env map[string]string, err error) {
	env, err = o.loadComposeEnv(ctx, app)
	if err != nil {
		return "", nil, nil, err
	}
	spec, err := services.Render(raw, env)
	if err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	// The rendered document must carry no live reference: every dollar the
	// node will see is a `$$` escape. A lone `$` (a tagged scalar Render
	// could not normalize, or a substituted value smuggled past the
	// escaping) would interpolate on the node after validation.
	if err := composeguard.CheckNoInterpolation(spec.ComposeYAML); err != nil {
		return "", nil, nil, composeValidationError(err)
	}
	// The guard quotes the offending value, which now holds substituted
	// secrets: redact it before it can reach the deployment row or log.
	if err := guardValidate(spec.ComposeYAML, app.ID); err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	images, err = ComposeImages(spec.ComposeYAML)
	if err != nil {
		return "", nil, nil, err
	}
	injected, err := InjectComposeWebPorts(spec.ComposeYAML, app.ComposeService, app.Port, app.HostPort)
	if err != nil {
		return "", nil, nil, err
	}
	return injected, images, env, nil
}

// composeValidationError maps a Services compose error onto the deploy
// validation sentinel, so the routes answer 400 instead of 500. The message
// is preserved verbatim for the wizard and the deploy log.
func composeValidationError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrValidation, err)
}

// buildCompose renders the resolved document of a compose run into the run
// state and records the raw document on the deployment row, so a rollback
// re-applies this release's file with the environment current at that
// time. Only the raw (uninterpolated) text is stored: secret values never
// reach a stored row. The step is what assembles and validates the
// complete runtime input (config reads, secret decryption, substitution)
// before the previous release is touched, mirroring startContainer's
// ordering guarantee.
func (o *Orchestrator) buildCompose(ctx context.Context, st *runState) error {
	if strings.TrimSpace(st.composeRaw) == "" {
		return fmt.Errorf("%w: compose: no document resolved", ErrValidation)
	}
	rendered, images, env, err := o.renderComposeDocument(ctx, st.app, st.composeRaw)
	if err != nil {
		return err
	}
	st.composeYAML = rendered
	st.composeImages = images
	st.composeEnv = env
	st.dep.ComposeDocument = st.composeRaw
	st.dep.ComposeCommit = st.composeCommit
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return fmt.Errorf("deploy: persist compose document: %w", err)
	}
	st.log("compose document rendered")
	return nil
}

// pushCompose pulls every image the rendered document declares and runs the
// project up on the node. A pull failure is best effort (the node's up still
// runs, so a locally present image keeps deploying when the registry is
// unreachable); an up failure is terminal. The document itself never reaches
// the deploy log, and node errors are redacted of project secret values.
func (o *Orchestrator) pushCompose(ctx context.Context, st *runState) error {
	if strings.TrimSpace(st.composeYAML) == "" {
		return fmt.Errorf("%w: compose: no rendered document", ErrValidation)
	}
	// The document the node is about to run carries no live reference,
	// whether it was just rendered or restored from a stored release.
	if err := composeguard.CheckNoInterpolation(st.composeYAML); err != nil {
		return services.RedactError(composeValidationError(err), st.composeEnv)
	}
	// The guard quotes the offending value, which holds substituted
	// secrets: redact it before it can reach the deployment row or log.
	if err := guardValidate(st.composeYAML, st.app.ID); err != nil {
		return services.RedactError(err, st.composeEnv)
	}
	project := services.ProjectName(st.app.ID)
	for _, image := range st.composeImages {
		st.log("pulling image " + services.Redact(image, st.composeEnv))
		if err := st.node.Pull(ctx, image); err != nil {
			st.log("could not refresh image " + services.Redact(image, st.composeEnv) +
				": " + truncateError(services.RedactError(err, st.composeEnv)))
		}
	}
	if err := st.node.ComposeUp(ctx, project, []byte(st.composeYAML)); err != nil {
		return services.RedactError(err, st.composeEnv)
	}
	st.log("compose project up")
	return nil
}

// startCompose records the routed web service's container as the release and
// gates success on its health, like a container start. An up that left the
// container untouched (no config change) retires nothing; otherwise the
// replaced container is retired with the same confirmed-retirement rule as a
// container start.
func (o *Orchestrator) startCompose(ctx context.Context, st *runState) error {
	project := services.ProjectName(st.app.ID)
	containers, err := st.node.ComposePs(ctx, project)
	if err != nil {
		return services.RedactError(err, st.composeEnv)
	}
	web := strings.TrimSpace(st.app.ComposeService)
	var found *ComposeContainer
	for i, candidate := range containers {
		if candidate.Service != web || strings.TrimSpace(candidate.ID) == "" {
			continue
		}
		if found == nil || strings.EqualFold(candidate.State, "running") {
			found = &containers[i]
		}
		if strings.EqualFold(found.State, "running") {
			break
		}
	}
	if found == nil {
		return fmt.Errorf("%w: compose service %q has no container", ErrValidation, web)
	}
	if st.previous != "" && st.previous != found.ID {
		if err := o.retirePrevious(ctx, st); err != nil {
			return err
		}
	} else {
		// The web container is the previous one: up changed nothing, so
		// there is nothing to retire and nothing for removeRetired to do.
		st.previous = ""
		st.log("web service " + web + " unchanged")
	}
	st.dep.ContainerID = found.ID
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return fmt.Errorf("deploy: persist container: %w", err)
	}
	st.log("container " + shortID(found.ID) + " started (" + web + ")")

	healthCtx, cancel := context.WithTimeout(ctx, o.healthTimeout)
	defer cancel()
	return o.waitHealthy(healthCtx, st, found.ID)
}
