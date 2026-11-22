package proxy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/instance"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type panelSettingsRepo struct{ rec instance.Stored }

func (r *panelSettingsRepo) Load(context.Context) (instance.Stored, error) { return r.rec, nil }
func (r *panelSettingsRepo) Save(_ context.Context, _ uuid.UUID, _ string, rec instance.Stored, _ any) error {
	r.rec = rec
	return nil
}
func (r *panelSettingsRepo) SaveCleanup(_ context.Context, _ uuid.UUID, rec instance.Stored, _ any) error {
	r.rec = rec
	return nil
}

func TestControlPlaneURLChangeIgnoresOtherApplications(t *testing.T) {
	for _, failure := range []string{"", "write", "panel route"} {
		t.Run("failure="+failure, func(t *testing.T) {
			ctx := context.Background()
			f := panelFixture(t)
			remote := uuid.New()
			f.service.nodes = panelNodes{[]NamedNode{{ID: f.serverID, Name: "localhost"}, {ID: remote, Name: "remote"}}}
			f.source.setApps(ProxiedApplication{ID: uuid.New(), ServerID: f.serverID, Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 80}}})
			dial := f.service.dial
			f.service.dial = func(ctx context.Context, id uuid.UUID) (AgentClient, error) {
				if id == remote {
					return nil, errors.New("remote offline")
				}
				return dial(ctx, id)
			}
			if failure == "write" {
				f.agent.respond = func(*agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
					return nil, errors.New("panel write failed")
				}
			}
			if failure == "panel route" {
				f.service.serverPort = 0
			}
			old := "https://old.example.com"
			repo := &panelSettingsRepo{rec: instance.Stored{ControlPlaneURL: &old}}
			var logs bytes.Buffer
			svc := instance.NewService(repo, slog.New(slog.NewTextHandler(&logs, nil)))
			svc.ResyncProxy = f.service.SyncControlPlane
			f.service.controlPlaneURLs = svc.ControlPlaneURLs
			state, err := svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{
				ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham",
			})
			if err != nil || state.General.ControlPlaneURL.Value != "https://new.example.com" {
				t.Fatalf("save=%+v err=%v", state, err)
			}
			if repo.rec.ControlPlaneURL == nil || *repo.rec.ControlPlaneURL != "https://new.example.com" {
				t.Fatal("URL not applied directly")
			}
			if failure == "" {
				var content string
				for _, file := range f.agent.calls[len(f.agent.calls)-1].Files {
					content += string(file.Content)
				}
				for _, want := range []string{"panel-web", "new.example.com"} {
					if !strings.Contains(content, want) {
						t.Fatalf("panel config missing %s: %s", want, content)
					}
				}
				if strings.Contains(content, "panel-previous-web") || strings.Contains(content, "old.example.com") {
					t.Fatalf("retired trial route still pushed: %s", content)
				}
			}
			if strings.Contains(logs.String(), "proxy cleanup failed") {
				t.Fatalf("unrelated route caused cleanup warning: %s", logs.String())
			}
		})
	}
}

func TestSyncControlPlaneRequiresLocalhost(t *testing.T) {
	f := panelFixture(t)
	f.service.nodes = panelNodes{}
	if err := f.service.SyncControlPlane(context.Background()); err == nil {
		t.Fatal("missing localhost reported synced")
	}
}
