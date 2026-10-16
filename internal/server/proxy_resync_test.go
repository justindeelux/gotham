package server

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/proxy"
)

type resyncProxy struct {
	results []proxy.SyncResult
	err     error
}

func (s resyncProxy) SyncServer(context.Context, uuid.UUID) error         { return nil }
func (s resyncProxy) RevertServer(context.Context, uuid.UUID) error       { return nil }
func (s resyncProxy) SyncAll(context.Context) ([]proxy.SyncResult, error) { return s.results, s.err }

func TestResyncProxySeparatesSkippedRoutesFromFailedNodes(t *testing.T) {
	diagnostic := proxy.Diagnostic{Kind: string(proxy.RouterKindControlPlane), Reason: "invalid control_plane_url host"}
	for _, tc := range []struct {
		name        string
		svc         resyncProxy
		wantFailure bool
	}{
		{name: "localhost diagnostic", svc: resyncProxy{results: []proxy.SyncResult{{Error: "partial sync", Diagnostics: []proxy.Diagnostic{diagnostic}}, {}}}},
		{name: "global diagnostic", svc: resyncProxy{results: []proxy.SyncResult{{}, {}}, err: &proxy.PartialError{Diagnostics: []proxy.Diagnostic{diagnostic}}}},
		{name: "node failure", svc: resyncProxy{results: []proxy.SyncResult{{Error: "agent unavailable"}, {}}}, wantFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{proxy: tc.svc}
			err := server.resyncProxyNodes(context.Background())
			if err == nil {
				t.Fatal("missing diagnostic")
			}
			if strings.Contains(err.Error(), "failed on") != tc.wantFailure {
				t.Fatalf("misleading resync error: %v", err)
			}
			if !tc.wantFailure && !strings.Contains(err.Error(), diagnostic.Reason) {
				t.Fatalf("diagnostic lost: %v", err)
			}
		})
	}
}

func TestPanelCertificateReconciliationRequiresInstanceService(t *testing.T) {
	s := &Server{}
	if err := s.reconcilePanelCertificates(context.Background()); err == nil {
		t.Fatal("missing instance service must fail without mutating certificate intents")
	}
}
