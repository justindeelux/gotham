package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
	"github.com/justindeelux/gotham/internal/webhooks"
)

var errGitHubAppUnavailable = errors.New("server: github app service is not configured")

type githubAppTokenAdapter struct {
	svc *githubapp.Service
}

func (a githubAppTokenAdapter) TokenCloneURL(ctx context.Context, userID, appID uuid.UUID, repo, cloneURL string) (string, error) {
	if a.svc == nil {
		return "", errGitHubAppUnavailable
	}
	return a.svc.TokenCloneURL(ctx, userID, appID, repo, cloneURL)
}

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
