package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
	"github.com/justindeelux/gotham/internal/webhooks"
)

// errGitHubAppUnavailable is returned when the GitHub App service is absent
// (no database) but a github_app flow needs it.
var errGitHubAppUnavailable = errors.New("server: github app service is not configured")

// githubAppTokenAdapter builds token-authenticated clone URLs for github_app
// applications. It adapts the GitHub App service to the deploy cloner's
// resolver seam; a nil service fails clones closed.
type githubAppTokenAdapter struct {
	svc *githubapp.Service
}

func (a githubAppTokenAdapter) TokenCloneURL(ctx context.Context, userID, appID uuid.UUID, repo, cloneURL string) (string, error) {
	if a.svc == nil {
		return "", errGitHubAppUnavailable
	}
	return a.svc.TokenCloneURL(ctx, userID, appID, repo, cloneURL)
}

// githubAppPushAdapter routes verified GitHub App push deliveries to
// github_app applications. It adapts the GitHub App service to the webhook
// push fallback seam; a nil service verifies nothing.
type githubAppPushAdapter struct {
	svc *githubapp.Service
}

func (a githubAppPushAdapter) VerifyPush(header http.Header, body []byte) (uuid.UUID, bool) {
	if a.svc == nil {
		return uuid.Nil, false
	}
	return a.svc.VerifyPush(header, body)
}

func (a githubAppPushAdapter) PushTargets(ctx context.Context, appID uuid.UUID, repo string) ([]webhooks.AppPushTarget, error) {
	if a.svc == nil {
		return nil, errGitHubAppUnavailable
	}
	targets, err := a.svc.PushTargetsForWebhook(ctx, appID, repo)
	if err != nil {
		return nil, err
	}
	out := make([]webhooks.AppPushTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, webhooks.AppPushTarget{
			ApplicationID: target.ApplicationID,
			Branch:        target.Branch,
		})
	}
	return out, nil
}
