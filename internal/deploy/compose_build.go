package deploy

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/services"
)

// composeBuildTag names the image one compose service builds for one release.
// The release tag is the prefix, so image retention keeps every service of the
// running release and a rollback can address the images of the release it
// restores.
func composeBuildTag(appID, deployID uuid.UUID, service string) string {
	return fmt.Sprintf("gotham/%s:%s-%s", appID, deployID, service)
}

type composeBuildArgs map[string]string

// UnmarshalYAML accepts the mapping form and the KEY=VALUE list form.
func (a *composeBuildArgs) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		values := make(map[string]string, len(node.Content))
		for _, entry := range node.Content {
			key, value, ok := strings.Cut(entry.Value, "=")
			if !ok || strings.TrimSpace(key) == "" {
				return fmt.Errorf("%w: build args list entries must be KEY=VALUE strings", ErrValidation)
			}
			values[strings.TrimSpace(key)] = value
		}
		*a = values
		return nil
	}
	var values map[string]string
	if err := node.Decode(&values); err != nil {
		return fmt.Errorf("%w: build args: %v", ErrValidation, err)
	}
	*a = values
	return nil
}

type composeBuildDirective struct {
	Context    string
	Dockerfile string
	Target     string
	Args       composeBuildArgs
}

// UnmarshalYAML accepts `build: .` as well as the mapping form.
func (d *composeBuildDirective) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		d.Context = node.Value
		return nil
	}
	var raw struct {
		Context    string           `yaml:"context"`
		Dockerfile string           `yaml:"dockerfile"`
		Target     string           `yaml:"target"`
		Args       composeBuildArgs `yaml:"args"`
	}
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("%w: build directive: %v", ErrValidation, err)
	}
	*d = composeBuildDirective{
		Context:    raw.Context,
		Dockerfile: raw.Dockerfile,
		Target:     raw.Target,
		Args:       raw.Args,
	}
	return nil
}

// composeBuildServices lists the services that build an image, keyed by service
// name, in the order the document declares them.
func composeBuildServices(document string) ([]string, map[string]composeBuildDirective, error) {
	var doc struct {
		Services map[string]struct {
			Build *composeBuildDirective `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(document), &doc); err != nil {
		return nil, nil, fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	built := make(map[string]composeBuildDirective, len(doc.Services))
	for name, service := range doc.Services {
		if service.Build != nil {
			built[name] = *service.Build
		}
	}
	names := make([]string, 0, len(built))
	for name := range built {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, built, nil
}

// rejectPastedBuild refuses build directives in a pasted document: a pasted
// compose has no repository to build from.
func rejectPastedBuild(content string) error {
	spec, err := services.Parse(content)
	if err != nil {
		return nil // the guard reports shape errors first
	}
	if len(spec.Built) > 0 {
		return fmt.Errorf(
			"%w: pasted compose cannot build: service %q declares build; use a git source with the Docker Compose build pack",
			ErrValidation, spec.Built[0])
	}
	return nil
}

// buildComposeImages builds every service that declares build, one image per
// service and release, and rewrites the document to run those images instead.
// A rollback reuses the images its target release built, so it never rebuilds.
func (o *Orchestrator) buildComposeImages(ctx context.Context, st *runState, rendered string) (string, []string, error) {
	names, directives, err := composeBuildServices(rendered)
	if err != nil {
		return "", nil, err
	}
	if len(names) == 0 {
		return rendered, nil, nil
	}
	tags := make(map[string]string, len(names))
	if st.dep.Kind == KindRollback {
		for _, name := range names {
			tags[name] = composeBuildTag(st.app.ID, st.dep.RollbackFrom, name)
		}
		st.log("compose: running the images built by deployment " + st.dep.RollbackFrom.String())
		rewritten, err := rewriteComposeBuilds(rendered, tags)
		return rewritten, composeBuildTagList(tags), err
	}

	// Compose resolves a build context relative to the compose file, not the
	// project root.
	baseDir := filepath.Join(st.buildDir(), filepath.Dir(st.app.ComposeFile))
	logs := newLineLog(st.log)
	builder := newNodeBuilder(st.node, st.app.ID, st.dep.ID, func(chunk []byte) {
		_, _ = logs.Write(chunk)
	})
	engine := builds.NewDockerfileEngine(builder)
	for _, name := range names {
		directive := directives[name]
		contextDir, err := composeBuildContextDir(baseDir, directive.Context)
		if err != nil {
			return "", nil, fmt.Errorf("compose: service %q: %w", name, err)
		}
		st.log("compose: building service " + name)
		tag := composeBuildTag(st.app.ID, st.dep.ID, name)
		if _, err := engine.Build(ctx, builds.BuildOptions{
			RepoDir:    contextDir,
			AppID:      st.app.ID,
			DeployID:   st.dep.ID,
			Tag:        tag,
			Dockerfile: directive.Dockerfile,
			Target:     directive.Target,
			BuildArgs:  directive.Args,
			LogWriter:  logs,
		}); err != nil {
			logs.Flush()
			return "", nil, fmt.Errorf("compose: build service %q: %w", name, err)
		}
		tags[name] = tag
	}
	logs.Flush()
	st.log("compose: built images for " + strings.Join(names, ", "))
	rewritten, err := rewriteComposeBuilds(rendered, tags)
	if err != nil {
		return "", nil, err
	}
	return rewritten, composeBuildTagList(tags), nil
}

func composeBuildTagList(tags map[string]string) []string {
	list := make([]string, 0, len(tags))
	for _, tag := range tags {
		list = append(list, tag)
	}
	sort.Strings(list)
	return list
}

// composeBuildContextDir keeps the build context inside the clone: a compose
// file must not point the node build at anything outside the repository.
func composeBuildContextDir(baseDir, context string) (string, error) {
	trimmed := strings.TrimSpace(context)
	if trimmed == "" {
		trimmed = "."
	}
	cleaned := filepath.Clean(trimmed)
	if filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: build context %q escapes the repository", ErrValidation, context)
	}
	dir := filepath.Join(baseDir, cleaned)
	rel, err := filepath.Rel(baseDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: build context %q escapes the repository", ErrValidation, context)
	}
	return dir, nil
}

// rewriteComposeBuilds replaces each built service's build directive with the
// image the deploy produced, so `docker compose up` runs the built image
// instead of asking the node for a build context it does not have.
func rewriteComposeBuilds(document string, tags map[string]string) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(document), &root); err != nil {
		return "", fmt.Errorf("%w: compose document is not valid YAML: %v", ErrValidation, err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("%w: compose document is not a mapping", ErrValidation)
	}
	services := mappingValue(root.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return "", fmt.Errorf("%w: compose document declares no services", ErrValidation)
	}
	for index := 0; index+1 < len(services.Content); index += 2 {
		name := services.Content[index].Value
		tag, ok := tags[name]
		if !ok {
			continue
		}
		service := services.Content[index+1]
		if service.Kind != yaml.MappingNode {
			return "", fmt.Errorf("%w: compose service %q is not a mapping", ErrValidation, name)
		}
		removeMappingKey(service, "build")
		setMappingValue(service, "image", tag)
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("%w: render compose document: %v", ErrValidation, err)
	}
	return buf.String(), nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func setMappingValue(node *yaml.Node, key, value string) {
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			node.Content[index+1] = scalar
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, scalar)
}

func removeMappingKey(node *yaml.Node, key string) {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			node.Content = append(node.Content[:index], node.Content[index+2:]...)
			return
		}
	}
}
