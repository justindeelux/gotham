package deploy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// autoDomainPort picks the container port an empty domain port stands for at
// deploy time: a port the source declares (Dockerfile EXPOSE, compose
// ports/expose), otherwise a stable port derived from the application id. The
// deploy then publishes it and sets PORT to the same value.
func autoDomainPort(app Application, domain ApplicationDomain, composeRaw string) int32 {
	if isComposeApp(app) {
		service := strings.TrimSpace(domain.Service)
		if service == "" {
			service = strings.TrimSpace(app.ComposeService)
		}
		if port := composeServiceContainerPort(composeRaw, service); port > 0 {
			return port
		}
	} else if app.SourceType == SourceDockerfile {
		if port := dockerfileExposedPort(app.DockerfileContent); port > 0 {
			return port
		}
	}
	return generatedContainerPort(app.ID)
}

func generatedContainerPort(id uuid.UUID) int32 {
	if id == uuid.Nil {
		return 3000
	}
	return 10000 + int32(((uint32(id[0])<<8)|uint32(id[1]))%50000)
}

func dockerfileExposedPort(content string) int32 {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !strings.EqualFold(fields[0], "EXPOSE") {
			continue
		}
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "#") {
				break
			}
			spec := field
			if index := strings.IndexByte(spec, '/'); index >= 0 {
				spec = spec[:index]
			}
			if index := strings.IndexByte(spec, '-'); index > 0 {
				spec = spec[:index]
			}
			port, err := strconv.Atoi(spec)
			if err == nil && port > 0 && port <= 65535 {
				return int32(port)
			}
		}
	}
	return 0
}

// resolveAutoDomainPorts pins each empty port to the port this deploy will
// publish, so the proxy sees a concrete target after the deploy succeeds.
func (o *Orchestrator) resolveAutoDomainPorts(ctx context.Context, app Application, domains []ApplicationDomain, composeRaw string) ([]ApplicationDomain, error) {
	resolved := make([]ApplicationDomain, len(domains))
	copy(resolved, domains)
	declared := int32(0)
	for _, domain := range resolved {
		if domain.Disabled || domain.Port <= 0 {
			continue
		}
		if declared == 0 || domain.IsPrimary {
			declared = domain.Port
		}
	}
	for i := range resolved {
		if resolved[i].Disabled || resolved[i].Port > 0 {
			continue
		}
		port := autoDomainPort(app, resolved[i], composeRaw)
		if !isComposeApp(app) && declared > 0 {
			port = declared
		}
		resolved[i].Port = port
		if _, err := o.repo.UpdateApplicationDomain(ctx, resolved[i]); err != nil {
			return nil, fmt.Errorf("deploy: resolve the port for domain %q: %w", resolved[i].Domain, err)
		}
	}
	return resolved, nil
}
