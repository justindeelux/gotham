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

func TestControlPlaneTrialSyncIgnoresOtherApplications(t *testing.T) {
	for _, failure := range []string{"", "write", "panel route"} {
		t.Run("failure="+failure, func(t *testing.T) {
			ctx := context.Background()
			f := panelFixture(t)
			remote := uuid.New()
			f.service.nodes = panelNodes{[]NamedNode{{ID: f.serverID, Name: "localhost"}, {ID: remote, Name: "remote"}}}
			f.source.setApps(ProxiedApplication{ID: uuid.New(), ServerID: f.serverID, BaseDomain: "app.example.com"})
			// A remote node failure must not affect the panel's local write.
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
			svc := instance.NewService(repo, nil, slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(svc.Close)
			svc.ResyncProxy = f.service.SyncControlPlane
			f.service.controlPlaneURLs = svc.ControlPlaneURLs
			state, err := svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{
				ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham", Timezone: "UTC",
			})
			if err != nil || state.PendingControlPlaneURL == nil || state.PendingControlPlaneURL.Synced != (failure == "") {
				t.Fatalf("trial=%+v err=%v", state, err)
			}
			if failure != "" {
				if _, err := svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com"); !errors.Is(err, instance.ErrDomainNotSynced) {
					t.Fatalf("confirm failed panel=%v", err)
				}
				f.agent.respond = nil
				f.service.serverPort = 8000
				state, err = svc.Get(ctx)
				if err != nil || !state.PendingControlPlaneURL.Synced {
					t.Fatalf("retry=%+v err=%v", state, err)
				}
			}
			var content string
			for _, file := range f.agent.calls[len(f.agent.calls)-1].Files {
				content += string(file.Content)
			}
			for _, want := range []string{"panel-web", "panel-previous-web", "old.example.com", "new.example.com"} {
				if !strings.Contains(content, want) {
					t.Fatalf("trial config missing %s: %s", want, content)
				}
			}
			state, err = svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com")
			if err != nil || state.PendingControlPlaneURL != nil {
				t.Fatalf("confirm=%+v err=%v", state, err)
			}
			if _, err := svc.UpdateGeneral(ctx, uuid.Nil, instance.GeneralInput{
				ControlPlaneURL: "https://third.example.com", InstanceName: "Gotham", Timezone: "UTC",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.RevertControlPlaneURL(ctx, uuid.Nil); err != nil {
				t.Fatal(err)
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
