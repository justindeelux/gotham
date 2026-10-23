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

func guardOptions(appID uuid.UUID) composeguard.Options {
	return composeguard.Options{AppID: appID.String(), ManagedRoot: managedVolumeRoot()}
}

func guardValidate(content string, appID uuid.UUID) error {
	if err := composeguard.Validate(content, guardOptions(appID)); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

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

func isComposeApp(app Application) bool {
	return NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose
}

func (o *Orchestrator) cloneCompose(ctx context.Context, st *runState) error {
	if st.dep.Kind == KindRollback {
		if strings.TrimSpace(st.dep.ComposeDocument) == "" {
			return fmt.Errorf("%w: rollback has no stored compose document", ErrValidation)
		}
		st.composeRaw = st.dep.ComposeDocument
		st.composeCommit = st.dep.ComposeCommit
		st.commitDone = true
		return nil
	}
	content, info, err := o.resolveComposeContent(ctx, st.app, st.repoDir, st.log)
	if err != nil {
		return err
	}
	st.composeRaw = content
	st.composeCommit = info.SHA
	st.commitInfo = info
	st.commitDone = true
	return nil
}

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

func (o *Orchestrator) loadComposeEnv(ctx context.Context, app Application, port int32) (map[string]string, error) {
	envVars, secrets, err := o.repo.ListEnvConfig(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	sharedProject, sharedEnvironment, err := o.repo.ListSharedVariables(ctx, app.ProjectID, app.EnvironmentID)
	if err != nil {
		return nil, err
	}
	return composeProjectEnv(envVars, secrets, sharedProject, sharedEnvironment, o.secret, port)
}

func (o *Orchestrator) composeDomainTargets(ctx context.Context, app Application) ([]ComposePortTarget, int32, error) {
	domains, err := o.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return nil, 0, err
	}
	targets := make([]ComposePortTarget, 0, len(domains))
	primaryPort := int32(0)
	for _, domain := range domains {
		if domain.Disabled || domain.Port <= 0 {
			continue
		}
		service := strings.TrimSpace(domain.Service)
		if service == "" {
			service = strings.TrimSpace(app.ComposeService)
		}
		if service == "" {
			return nil, 0, fmt.Errorf("%w: domain %q has no compose service to target", ErrValidation, domain.Domain)
		}
		targets = append(targets, ComposePortTarget{Service: service, Port: domain.Port})
		if domain.IsPrimary {
			primaryPort = domain.Port
		}
	}
	if primaryPort == 0 && len(targets) > 0 {
		primaryPort = targets[0].Port
	}
	return targets, primaryPort, nil
}

func (o *Orchestrator) renderComposeDocument(ctx context.Context, app Application, raw string) (rendered string, images []string, env map[string]string, err error) {
	targets, primaryPort, err := o.composeDomainTargets(ctx, app)
	if err != nil {
		return "", nil, nil, err
	}
	env, err = o.loadComposeEnv(ctx, app, primaryPort)
	if err != nil {
		return "", nil, nil, err
	}
	spec, err := services.Render(raw, env)
	if err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	if err := composeguard.CheckNoInterpolation(spec.ComposeYAML); err != nil {
		return "", nil, nil, composeValidationError(err)
	}
	if err := guardValidate(spec.ComposeYAML, app.ID); err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	images, err = ComposeImages(spec.ComposeYAML)
	if err != nil {
		return "", nil, nil, err
	}
	injected, err := InjectComposeDomainPorts(spec.ComposeYAML, targets)
	if err != nil {
		return "", nil, nil, err
	}
	injected, err = RewriteComposeManagedVolumes(injected, managedVolumeRoot(), app.ID)
	if err != nil {
		return "", nil, nil, err
	}
	if err := guardValidate(injected, app.ID); err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	return injected, images, env, nil
}

func composeValidationError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrValidation, err)
}

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

func (o *Orchestrator) pushCompose(ctx context.Context, st *runState) error {
	if strings.TrimSpace(st.composeYAML) == "" {
		return fmt.Errorf("%w: compose: no rendered document", ErrValidation)
	}
	if err := composeguard.CheckNoInterpolation(st.composeYAML); err != nil {
		return services.RedactError(composeValidationError(err), st.composeEnv)
	}
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
