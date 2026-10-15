package server

import "context"

func (s *Server) guardDomains(ctx context.Context, hosts ...string) (func(), error) {
	if s.instanceSvc == nil {
		return func() {}, nil
	}
	return s.instanceSvc.GuardDomains(ctx, hosts...)
}
