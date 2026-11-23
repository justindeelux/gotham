package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/composeguard"
	"github.com/justindeelux/gotham/internal/services"
)

// controlComposeProject stops or starts every container of the application's
// compose project. A deployment records only the web service container, so
// acting on that id alone leaves the other services running.
func controlComposeProject(ctx context.Context, node Node, app Application, target Deployment, start bool) error {
	containers, err := node.ComposePs(ctx, services.ProjectName(app.ID))
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return fmt.Errorf("%w: the compose project has no container", ErrNotFound)
	}
	if start {
		containers = composeStartOrder(containers, target.ComposeDocument)
	}
	for _, container := range containers {
		if strings.TrimSpace(container.ID) == "" {
			continue
		}
		if start {
			err = node.Start(ctx, container.ID)
		} else {
			err = node.Stop(ctx, container.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// composeStartOrder starts a service after the services it depends on. A
// document that cannot be read, or a dependency cycle, keeps the listing order.
func composeStartOrder(containers []ComposeContainer, document string) []ComposeContainer {
	dependencies := composeDependencies(document)
	if len(dependencies) == 0 {
		return containers
	}
	byService := make(map[string][]ComposeContainer, len(containers))
	services := make([]string, 0, len(containers))
	for _, container := range containers {
		if _, seen := byService[container.Service]; !seen {
			services = append(services, container.Service)
		}
		byService[container.Service] = append(byService[container.Service], container)
	}
	ordered := make([]ComposeContainer, 0, len(containers))
	placed := make(map[string]bool, len(services))
	for len(ordered) < len(containers) {
		progress := false
		for _, service := range services {
			if placed[service] || !dependenciesPlaced(dependencies[service], placed, byService) {
				continue
			}
			ordered = append(ordered, byService[service]...)
			placed[service] = true
			progress = true
		}
		if !progress {
			for _, service := range services {
				if !placed[service] {
					ordered = append(ordered, byService[service]...)
				}
			}
			break
		}
	}
	return ordered
}

func dependenciesPlaced(dependencies []string, placed map[string]bool, byService map[string][]ComposeContainer) bool {
	for _, dependency := range dependencies {
		if _, inProject := byService[dependency]; !inProject {
			continue
		}
		if !placed[dependency] {
			return false
		}
	}
	return true
}

// composeDependencies reads the depends_on edges of a compose document, in the
// list form and the mapping form.
func composeDependencies(document string) map[string][]string {
	if strings.TrimSpace(document) == "" {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			DependsOn any `yaml:"depends_on"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(document), &doc); err != nil {
		return nil
	}
	dependencies := make(map[string][]string, len(doc.Services))
	for name, service := range doc.Services {
		switch value := service.DependsOn.(type) {
		case []any:
			for _, entry := range value {
				if dependency, ok := entry.(string); ok {
					dependencies[name] = append(dependencies[name], dependency)
				}
			}
		case map[string]any:
			for dependency := range value {
				dependencies[name] = append(dependencies[name], dependency)
			}
		}
	}
	return dependencies
}

func guardOptions(appID uuid.UUID) composeguard.Options {
	return composeguard.Options{AppID: appID.String(), ManagedRoot: managedVolumeRoot()}
}

func guardValidate(content string, appID uuid.UUID) error {
	if err := composeguard.Validate(content, guardOptions(appID)); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// sanitizeCompose drops the service keys Gotham manages itself and returns the
// document to deploy plus one warning per removed key.
func sanitizeCompose(content string, appID uuid.UUID) (string, []ComposeWarning, error) {
	cleaned, dropped, err := composeguard.Sanitize(content, guardOptions(appID))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	warnings := make([]ComposeWarning, 0, len(dropped))
	for _, key := range dropped {
		warnings = append(warnings, ComposeWarning{Service: key.Service, Key: key.Key})
	}
	return cleaned, warnings, nil
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
	_, _, err := sanitizeCompose(content, appID)
	return err
}

func isComposeApp(app Application) bool {
	return NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose ||
		IsComposeBuildPack(app.BuildPack)
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
		if err := rejectPastedBuild(content); err != nil {
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

func (o *Orchestrator) loadComposeEnv(ctx context.Context, app Application, port int32) (map[string]string, map[string]bool, error) {
	envVars, secrets, err := o.repo.ListEnvConfig(ctx, app.ID)
	if err != nil {
		return nil, nil, err
	}
	sharedProject, sharedEnvironment, err := o.repo.ListSharedVariables(ctx, app.ProjectID, app.EnvironmentID)
	if err != nil {
		return nil, nil, err
	}
	return composeProjectEnv(envVars, secrets, sharedProject, sharedEnvironment, o.secret, port)
}

func (o *Orchestrator) composeDomainTargets(ctx context.Context, app Application, composeRaw string) ([]ComposePortTarget, int32, error) {
	domains, err := o.repo.ListApplicationDomains(ctx, app.ID)
	if err != nil {
		return nil, 0, err
	}
	domains, err = o.resolveAutoDomainPorts(ctx, app, domains, composeRaw)
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

func (o *Orchestrator) renderComposeDocument(ctx context.Context, app Application, raw string) (rendered string, env map[string]string, warnings []ComposeWarning, err error) {
	targets, primaryPort, err := o.composeDomainTargets(ctx, app, raw)
	if err != nil {
		return "", nil, nil, err
	}
	env, generated, err := o.loadComposeEnv(ctx, app, primaryPort)
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
	cleaned, warnings, err := sanitizeCompose(spec.ComposeYAML, app.ID)
	if err != nil {
		return "", nil, nil, services.RedactError(err, env)
	}
	injected, err := InjectComposeDomainPorts(cleaned, targets)
	if err != nil {
		return "", nil, nil, err
	}
	injected, err = RewriteComposeManagedVolumes(injected, managedVolumeRoot(), app.ID)
	if err != nil {
		return "", nil, nil, err
	}
	injected, err = InjectComposeEnvironment(injected, composeAppEnv(env, generated))
	if err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	if err := guardValidate(injected, app.ID); err != nil {
		return "", nil, nil, composeValidationError(services.RedactError(err, env))
	}
	return injected, env, warnings, nil
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
	rendered, env, warnings, err := o.renderComposeDocument(ctx, st.app, st.composeRaw)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		st.log("compose: " + warning.Key + " is set by Gotham and was removed from service " + warning.Service)
	}
	rendered, built, err := o.buildComposeImages(ctx, st, rendered)
	if err != nil {
		return err
	}
	if err := guardValidate(rendered, st.app.ID); err != nil {
		return composeValidationError(services.RedactError(err, env))
	}
	images, err := ComposeImages(rendered)
	if err != nil {
		return err
	}
	st.composeYAML = rendered
	st.composeImages = images
	st.composeBuilt = built
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
	built := make(map[string]bool, len(st.composeBuilt))
	for _, tag := range st.composeBuilt {
		built[tag] = true
	}
	for _, image := range st.composeImages {
		if built[image] {
			continue
		}
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
