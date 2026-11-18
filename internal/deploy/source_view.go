package deploy

import (
	"context"
	"fmt"
	"os"

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
	secrets, err := s.composeSecretValues(ctx, app)
	if err != nil {
		return ApplicationSource{}, err
	}
	deployable, err := redactComposeDocument(rendered, secrets)
	if err != nil {
		return ApplicationSource{}, err
	}
	return ApplicationSource{Kind: SourceCompose, Raw: raw, Deployable: deployable}, nil
}

// redactComposeDocument masks secret values in the rendered document. Redacting
// the string alone misses values YAML re-encoded (a multi-line secret becomes a
// block scalar, a quoted one changes escapes), so redact the parsed scalars and
// marshal the same document back.
func redactComposeDocument(rendered string, secrets map[string]string) (string, error) {
	if len(secrets) == 0 {
		return rendered, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	redactComposeNode(&doc, secrets)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return string(out), nil
}

func redactComposeNode(node *yaml.Node, secrets map[string]string) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			redactComposeNode(child, secrets)
		}
	case yaml.ScalarNode:
		node.Value = services.Redact(node.Value, secrets)
	}
}

// composeSecretValues opens the sealed values the deploy renders into the
// document, so the view can mask them with the same values the deploy uses.
func (s *Service) composeSecretValues(ctx context.Context, app Application) (map[string]string, error) {
	_, secrets, err := s.repo.ListEnvConfig(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	project, environment, err := s.repo.ListSharedVariables(ctx, app.ProjectID, app.EnvironmentID)
	if err != nil {
		return nil, err
	}
	_, secrets = mergeSharedVariables(project, environment, nil, secrets)
	values := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		if secret.Key == "" {
			continue
		}
		value, err := providers.OpenSecret(s.secret, secret.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: open secret %s: %v", ErrValidation, secret.Key, err)
		}
		if value != "" {
			values[secret.Key] = value
		}
	}
	return values, nil
}
