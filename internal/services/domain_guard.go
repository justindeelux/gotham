package services

import (
	"context"
	"fmt"
)

func (s *service) guardHosts(ctx context.Context, hosts []string) (func(), error) {
	release, err := s.guardDomains.Acquire(ctx, hosts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return release, nil
}
