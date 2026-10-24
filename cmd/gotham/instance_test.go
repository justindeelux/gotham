package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/instance"
)

type instanceCLIRepo struct{ rec instance.Stored }

func (r *instanceCLIRepo) Load(context.Context) (instance.Stored, error) { return r.rec, nil }
func (r *instanceCLIRepo) Save(_ context.Context, _ uuid.UUID, _ string, rec instance.Stored, _ any) error {
	r.rec = rec
	return nil
}

func TestInstanceSetURL(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		syncErr   error
		wantErr   bool
	}{
		{name: "trial", url: "https://panel.example.com"},
		{name: "invalid", url: "javascript:alert(1)", wantErr: true},
		{name: "sync failure", url: "https://panel.example.com", syncErr: errors.New("agent unavailable"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, zone := "My instance", "Asia/Ho_Chi_Minh"
			repo := &instanceCLIRepo{rec: instance.Stored{InstanceName: &name, Timezone: &zone}}
			svc := instance.NewService(repo, nil, nil)
			defer svc.Close()
			syncs := 0
			svc.ResyncProxy = func(context.Context) error { syncs++; return tc.syncErr }
			_, err := setInstanceURL(context.Background(), svc, tc.url)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if *repo.rec.InstanceName != name || *repo.rec.Timezone != zone {
				t.Fatal("unrelated settings changed")
			}
			if tc.wantErr {
				if repo.rec.ControlPlaneURL != nil || repo.rec.PendingControlPlaneURL != nil {
					t.Fatal("failed change kept URL")
				}
				return
			}
			if syncs != 1 || repo.rec.PendingControlPlaneURL == nil || !repo.rec.PendingControlPlaneURL.Synced {
				t.Fatal("service trial/proxy flow was bypassed")
			}
			if *repo.rec.ControlPlaneURL != tc.url {
				t.Fatal("URL not saved")
			}
			if _, err := setInstanceURL(context.Background(), svc, tc.url); err != nil {
				t.Fatalf("repeat: %v", err)
			}
		})
	}
}

func TestInstanceCommandUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"set-url"}, {"other"}, {"set-url", "a", "b"}} {
		if got := runInstance(args); got != exitUsage {
			t.Fatalf("%v: exit %d", args, got)
		}
	}
}

func TestPanelURLRejectsMalformedDomains(t *testing.T) {
	for _, raw := range []string{"https://bad/path", "https://panel.example.com:443", "https://user@panel.example.com", "https://panel.example.com?x=y", "https://-bad.example.com", "file:///etc/passwd"} {
		if err := checkPanelURL(context.Background(), raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
