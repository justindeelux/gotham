package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestCertificateTeamIsolation is the F3 regression for certificate configs:
// a per-application resource only exists for the application's team. A
// stranger's read/update/delete answers ErrNotFound, a read_only member's
// mutation answers ErrForbidden, and the list is filtered to the active team.
func TestCertificateTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	appInB := ApplicationInfo{ID: uuid.New(), TeamID: teamB, BaseDomain: "app.example.com", ServerID: uuid.New()}
	appInA := ApplicationInfo{ID: uuid.New(), TeamID: teamA, BaseDomain: "other.example.com", ServerID: uuid.New()}

	store := newFakeSSLStore()
	store.applications[appInB.ID] = appInB
	store.applications[appInA.ID] = appInA
	svc := NewDefaultCertificateService(SSLConfig{Store: store, Logger: discardLogger()})
	if svc == nil {
		t.Fatal("certificate service not built")
	}
	bg := context.Background()

	// A member of another team cannot create, read, update or delete.
	stranger := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.CreateCertificate(stranger, CreateCertificateInput{ApplicationID: appInB.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign CreateCertificate = %v, want ErrNotFound", err)
	}

	// The owning team creates one; a read_only member may read it but not
	// mutate it.
	owner := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleOwner})
	certificate, err := svc.CreateCertificate(owner, CreateCertificateInput{ApplicationID: appInB.ID})
	if err != nil {
		t.Fatalf("owner CreateCertificate: %v", err)
	}
	viewer := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := svc.GetCertificate(viewer, certificate.ID); err != nil {
		t.Fatalf("read_only GetCertificate: %v", err)
	}
	if _, err := svc.UpdateCertificate(viewer, certificate.ID, UpdateCertificateInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only UpdateCertificate = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteCertificate(viewer, certificate.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only DeleteCertificate = %v, want ErrForbidden", err)
	}
	// A stranger cannot even see it.
	if _, err := svc.GetCertificate(stranger, certificate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign GetCertificate = %v, want ErrNotFound", err)
	}
	if _, err := svc.UpdateCertificate(stranger, certificate.ID, UpdateCertificateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign UpdateCertificate = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteCertificate(stranger, certificate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign DeleteCertificate = %v, want ErrNotFound", err)
	}

	// Lists only contain the active team's certificates.
	if all, err := svc.ListCertificates(owner); err != nil || len(all) != 1 || all[0].ID != certificate.ID {
		t.Fatalf("team B list = %+v, %v; want only its certificate", all, err)
	}
	if all, err := svc.ListCertificates(stranger); err != nil || len(all) != 0 {
		t.Fatalf("team A list = %+v, %v; want none", all, err)
	}
}

// TestRedirectTeamIsolation is the F3 regression for redirect rules, which had
// the same global read/CRUD surface.
func TestRedirectTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	appInB := ApplicationInfo{ID: uuid.New(), TeamID: teamB, BaseDomain: "app.example.com", ServerID: uuid.New()}

	store := newFakeRedirectStore()
	store.addApp(appInB)
	svc := NewDefaultRedirectService(RedirectConfig{Store: store, Logger: discardLogger()})
	if svc == nil {
		t.Fatal("redirect service not built")
	}
	bg := context.Background()

	stranger := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamA, Role: teams.RoleOwner})
	if _, err := svc.CreateRedirect(stranger, CreateRedirectInput{
		ApplicationID: appInB.ID, SourceDomain: "go.example.com", TargetDomain: "app.example.com",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign CreateRedirect = %v, want ErrNotFound", err)
	}

	owner := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleOwner})
	redirect, err := svc.CreateRedirect(owner, CreateRedirectInput{
		ApplicationID: appInB.ID, SourceDomain: "go.example.com", TargetDomain: "app.example.com",
	})
	if err != nil {
		t.Fatalf("owner CreateRedirect: %v", err)
	}
	viewer := teams.WithScope(bg, teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := svc.GetRedirect(viewer, redirect.ID); err != nil {
		t.Fatalf("read_only GetRedirect: %v", err)
	}
	enabled := false
	if _, err := svc.UpdateRedirect(viewer, redirect.ID, UpdateRedirectInput{Enabled: &enabled}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only UpdateRedirect = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteRedirect(viewer, redirect.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read_only DeleteRedirect = %v, want ErrForbidden", err)
	}
	if _, err := svc.GetRedirect(stranger, redirect.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign GetRedirect = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteRedirect(stranger, redirect.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign DeleteRedirect = %v, want ErrNotFound", err)
	}

	if all, err := svc.ListRedirects(owner, uuid.Nil); err != nil || len(all) != 1 {
		t.Fatalf("team B list = %+v, %v; want only its rule", all, err)
	}
	if all, err := svc.ListRedirects(stranger, uuid.Nil); err != nil || len(all) != 0 {
		t.Fatalf("team A list = %+v, %v; want none", all, err)
	}
}
