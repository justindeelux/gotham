package deploy

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/services"
)

// ApplicationSource is the read-only view of the source Gotham deploys: the
// document as saved, and the document the next deploy hands to Docker. Kind
// is empty for image and build-pack applications, which have no such file.
type ApplicationSource struct {
	Kind       string
	Raw        string
	Deployable string
}

func (s *Service) GetApplicationSource(ctx context.Context, userID, appID uuid.UUID) (ApplicationSource, error) {
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return ApplicationSource{}, err
	}
	return s.applicationSource(ctx, app)
}

func (s *Service) applicationSource(ctx context.Context, app Application) (ApplicationSource, error) {
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceDockerfile {
		deployable, err := dockerfileDeployable(app.DockerfileContent)
		if err != nil {
			return ApplicationSource{}, err
		}
		return ApplicationSource{Kind: SourceDockerfile, Raw: app.DockerfileContent, Deployable: deployable}, nil
	}
	if !isComposeApp(app) {
		return ApplicationSource{}, nil
	}
	dir, err := os.MkdirTemp("", "gotham-source-view-*")
	if err != nil {
		return ApplicationSource{}, fmt.Errorf("deploy: source view: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	raw, _, err := s.resolveComposeContent(ctx, app, dir, nil)
	if err != nil {
		return ApplicationSource{}, err
	}
	rendered, _, err := s.renderComposeDocument(ctx, app, raw)
	if err != nil {
		return ApplicationSource{}, err
	}
	values, err := s.composeInjectedValues(ctx, app)
	if err != nil {
		return ApplicationSource{}, err
	}
	deployable, err := redactComposeDocument(rendered, values)
	if err != nil {
		return ApplicationSource{}, err
	}
	return ApplicationSource{Kind: SourceCompose, Raw: raw, Deployable: deployable}, nil
}

// redactComposeDocument masks the values Gotham injects into the rendered
// document. Redacting the string alone misses values YAML re-encoded (a
// multi-line secret becomes a block scalar, a quoted one changes escapes), so
// redact the parsed scalars and marshal the same document back.
func redactComposeDocument(rendered string, values map[string]string) (string, error) {
	if len(values) == 0 {
		return rendered, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	redactComposeNode(&doc, values)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return string(out), nil
}

func redactComposeNode(node *yaml.Node, values map[string]string) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			redactComposeNode(child, values)
		}
	case yaml.ScalarNode:
		node.Value = services.Redact(node.Value, values)
	}
}

// composeInjectedValues opens every value the deploy injects into the compose
// document: the app's plain and sealed environment plus the merged
// project/environment variables. The view masks them so the Environment tab's
// hidden values stay hidden here too.
func (s *Service) composeInjectedValues(ctx context.Context, app Application) (map[string]string, error) {
	envVars, secrets, err := s.repo.ListEnvConfig(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	project, environment, err := s.repo.ListSharedVariables(ctx, app.ProjectID, app.EnvironmentID)
	if err != nil {
		return nil, err
	}
	envVars, secrets = mergeSharedVariables(project, environment, envVars, secrets)
	values := make(map[string]string, len(envVars)+len(secrets))
	for _, v := range envVars {
		if isMaskableValue(v.Value) {
			values[v.Key] = v.Value
		}
	}
	for _, secret := range secrets {
		if secret.Key == "" {
			continue
		}
		value, err := providers.OpenSecret(s.secret, secret.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: open secret %s: %v", ErrValidation, secret.Key, err)
		}
		if isMaskableValue(value) {
			values[secret.Key] = value
		}
	}
	return values, nil
}

// isMaskableValue skips values masking would corrupt the document with: a
// value under three characters, a boolean or a number also occurs in unrelated
// text, so replacing it would rewrite the document rather than hide a secret.
func isMaskableValue(value string) bool {
	if utf8.RuneCountInString(value) < 3 {
		return false
	}
	if strings.EqualFold(value, "true") || strings.EqualFold(value, "false") {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err != nil
}
