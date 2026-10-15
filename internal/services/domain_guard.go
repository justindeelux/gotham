package services

import (
	"context"
	"fmt"
)

func (s *service) guardComposeDomains(ctx context.Context, document string, env map[string]string) (func(), error) {
	rendered, err := Render(document, env)
	if err != nil {
		return nil, RedactError(err, env)
	}
	hosts := make([]string, 0, len(rendered.Spec.Domains))
	for _, route := range rendered.Spec.Domains {
		hosts = append(hosts, route.Domain)
	}
	release, err := s.guardDomains.Acquire(ctx, hosts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return release, nil
}
