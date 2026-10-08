package server

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

// TestConnectionApplicationsOfMapsIdentity pins the disconnect in-use wiring:
// the provider slug and clone URL must land in the connection identity the
// check matches on, so a rename cannot silently disable the 409.
func TestConnectionApplicationsOfMapsIdentity(t *testing.T) {
	applications := []deploy.Application{
		{Name: "shop", Provider: "gitlab", CloneURL: "https://git.example/acme/shop.git"},
		{Name: "docs", Provider: "github", CloneURL: "git@github.com:acme/docs.git"},
	}

	got := connectionApplicationsOf(applications)

	want := []providers.ConnectionApplication{
		{Name: "shop", Provider: "gitlab", CloneURL: "https://git.example/acme/shop.git"},
		{Name: "docs", Provider: "github", CloneURL: "git@github.com:acme/docs.git"},
	}
	if len(got) != len(want) {
		t.Fatalf("mapped = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mapped[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestProviderConnectionApplicationsWithoutDeploy pins the disabled path: no
// deploy service lists nothing, so disconnect stays allowed where
// applications are off.
func TestProviderConnectionApplicationsWithoutDeploy(t *testing.T) {
	s := newTestServer(t, stubPinger{}, stubPinger{})
	if s.deploy != nil {
		t.Fatalf("test server holds a deploy service")
	}

	got, err := s.providerConnectionApplications(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("wiring: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("applications = %+v, want none without a deploy service", got)
	}
}
