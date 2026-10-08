package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestCreateApplicationGitHubAppLink proves the link is explicit and owned:
// an owned connection links, a foreign id is not-found, and a link on a
// non-github_app source is refused.
func TestCreateApplicationGitHubAppLink(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	owned := uuid.New()
	foreign := uuid.New()
	repo := &fakeRepository{ownedGitHubApps: map[uuid.UUID]bool{owned: true}}
	svc := newTestService(t, repo)

	base := CreateApplicationInput{
		Name:          "linked",
		EnvironmentID: uuid.New(),
		Provider:      "github",
		Repo:          "acme/web",
		CloneURL:      "https://github.com/acme/web.git",
		SourceType:    SourceGitHubApp,
		Branch:        "main",
		BuildPack:     "dockerfile",
		ServerID:      uuid.New(),
	}
	linked, err := svc.CreateApplication(ctx, userID, func() CreateApplicationInput {
		in := base
		in.GitHubAppID = owned
		return in
	}())
	if err != nil {
		t.Fatalf("linked create: %v", err)
	}
	if linked.GitHubAppID != owned {
		t.Fatalf("link = %v, want %v", linked.GitHubAppID, owned)
	}

	foreignIn := base
	foreignIn.GitHubAppID = foreign
	if _, err := svc.CreateApplication(ctx, userID, foreignIn); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign link err = %v, want ErrNotFound", err)
	}

	publicIn := base
	publicIn.SourceType = SourceGitPublic
	publicIn.Provider = ""
	publicIn.GitHubAppID = owned
	if _, err := svc.CreateApplication(ctx, userID, publicIn); !errors.Is(err, ErrValidation) {
		t.Fatalf("linked public app err = %v, want ErrValidation", err)
	}
}

// TestUpdateApplicationGitHubAppLink proves relinking: set, clear, foreign
// refused, and mismatch with the source refused.
func TestUpdateApplicationGitHubAppLink(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	owned := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	repo := &fakeRepository{app: app, ownedGitHubApps: map[uuid.UUID]bool{owned: true}}
	svc := newTestService(t, repo)

	str := func(s string) *string { return &s }
	updated, err := svc.UpdateApplication(ctx, userID, app.ID, UpdateApplicationInput{
		GitHubAppID: str(owned.String()),
	})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if updated.GitHubAppID != owned {
		t.Fatalf("link = %v, want %v", updated.GitHubAppID, owned)
	}

	cleared, err := svc.UpdateApplication(ctx, userID, app.ID, UpdateApplicationInput{
		GitHubAppID: str(""),
	})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.GitHubAppID != uuid.Nil {
		t.Fatalf("link = %v, want cleared", cleared.GitHubAppID)
	}

	foreign := uuid.New().String()
	if _, err := svc.UpdateApplication(ctx, userID, app.ID, UpdateApplicationInput{
		GitHubAppID: &foreign,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign link err = %v, want ErrNotFound", err)
	}

	public := testApplication(userID)
	public.SourceType = SourceGitPublic
	public.Provider = ""
	publicRepo := &fakeRepository{app: public, ownedGitHubApps: map[uuid.UUID]bool{owned: true}}
	publicSvc := newTestService(t, publicRepo)
	if _, err := publicSvc.UpdateApplication(ctx, userID, public.ID, UpdateApplicationInput{
		GitHubAppID: str(owned.String()),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("linked public app err = %v, want ErrValidation", err)
	}
}

// TestCreateLinkedHTTPCloneURLRefused proves a linked application with a
// plaintext http clone URL fails validation: no token may travel over
// plaintext. An unlinked http row and a linked ssh row stay valid.
func TestCreateLinkedHTTPCloneURLRefused(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	owned := uuid.New()
	repo := &fakeRepository{ownedGitHubApps: map[uuid.UUID]bool{owned: true}}
	svc := newTestService(t, repo)

	base := CreateApplicationInput{
		Name:          "linked",
		EnvironmentID: uuid.New(),
		Provider:      "github",
		Repo:          "acme/web",
		CloneURL:      "http://github.com/acme/web.git",
		SourceType:    SourceGitHubApp,
		Branch:        "main",
		BuildPack:     "dockerfile",
		ServerID:      uuid.New(),
	}
	httpIn := base
	httpIn.GitHubAppID = owned
	if _, err := svc.CreateApplication(ctx, userID, httpIn); !errors.Is(err, ErrValidation) {
		t.Fatalf("linked http err = %v, want ErrValidation", err)
	}

	// Unlinked http rows keep their legacy behaviour.
	unlinkedIn := base
	unlinkedIn.Name = "plain-http"
	if _, err := svc.CreateApplication(ctx, userID, unlinkedIn); err != nil {
		t.Fatalf("unlinked http create: %v", err)
	}

	// A linked ssh row never takes the token path, so it stays valid.
	sshIn := base
	sshIn.Name = "linked-ssh"
	sshIn.CloneURL = "git@github.com:acme/web.git"
	sshIn.GitHubAppID = owned
	if _, err := svc.CreateApplication(ctx, userID, sshIn); err != nil {
		t.Fatalf("linked ssh create: %v", err)
	}
}

// TestUpdateLinkHTTPCloneURLRefused proves relinking an http clone URL is
// refused like creation.
func TestUpdateLinkHTTPCloneURLRefused(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	owned := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	app.CloneURL = "http://github.com/acme/demo.git"
	repo := &fakeRepository{app: app, ownedGitHubApps: map[uuid.UUID]bool{owned: true}}
	svc := newTestService(t, repo)

	id := owned.String()
	if _, err := svc.UpdateApplication(ctx, userID, app.ID, UpdateApplicationInput{
		GitHubAppID: &id,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("relink http err = %v, want ErrValidation", err)
	}
}
