package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// PreviewApplicationInput is the system-path payload of a preview sibling: the
// PR-derived name and host, and the head branch to build. Everything else is
// cloned from the base application.
type PreviewApplicationInput struct {
	Name       string
	Branch     string
	BaseDomain string
}

// CreatePreviewApplication clones the configuration of a base application into
// a new sibling application (BE-8.1). It is a system path: the PR webhook
// established which base application may be built, so there is no caller to
// authorize and the preview inherits the base application's team, creator and
// server. Env vars, sealed secrets and storages are copied verbatim, and the
// base's deploy key is re-registered on the sibling (same key material) so a
// private repository stays cloneable. Host port is never copied: the preview
// must not fight the base application for a pinned host port.
//
// The returned application is a normal applications row, so the whole Phase 4
// deploy path and the Phase 6 proxy sync work on it unchanged.
func (s *Service) CreatePreviewApplication(ctx context.Context, baseAppID uuid.UUID, in PreviewApplicationInput) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return Application{}, errors.New("deploy: repository is not configured")
	}
	if baseAppID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: invalid application id", ErrValidation)
	}

	base, err := s.repo.GetApplication(ctx, baseAppID)
	if err != nil {
		return Application{}, err
	}

	app := Application{
		UserID:     base.UserID,
		TeamID:     base.TeamID,
		ServerID:   base.ServerID,
		Name:       strings.TrimSpace(in.Name),
		Provider:   base.Provider,
		Repo:       base.Repo,
		CloneURL:   base.CloneURL,
		Branch:     strings.TrimSpace(in.Branch),
		BuildPack:  base.BuildPack,
		BaseDomain: proxy.NormalizeDomain(in.BaseDomain),
		Port:       base.Port,
		// HostPort stays 0: the agent assigns a free port, so the preview
		// never collides with the base application's binding.
	}
	if app.Branch == "" {
		app.Branch = base.Branch
	}
	if app.Branch == "" {
		app.Branch = defaultBranch
	}
	if err := validateApplication(app, true); err != nil {
		return Application{}, err
	}

	envVars, err := s.repo.ListEnvVars(ctx, base.ID)
	if err != nil {
		return Application{}, err
	}
	secrets, err := s.repo.ListSecrets(ctx, base.ID)
	if err != nil {
		return Application{}, err
	}
	storages, err := s.repo.ListStorages(ctx, base.ID)
	if err != nil {
		return Application{}, err
	}

	created, err := s.repo.CreateApplication(ctx, app, envVars, secrets, storages)
	if err != nil {
		return Application{}, err
	}
	s.copyDeployKey(ctx, base, created)
	return created, nil
}

// copyDeployKey re-registers the base application's deploy key on the preview
// sibling (same key material, new mapping row) so a private repository stays
// cloneable. Best effort: a failure is logged, the sibling still deploys
// against a public repository, and a private one reports the clone failure on
// its deployment row.
func (s *Service) copyDeployKey(ctx context.Context, base, preview Application) {
	key, err := s.repo.GetDeployKey(ctx, base.ID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Warn("deploy: preview deploy key lookup failed",
				"application_id", base.ID, "error", err)
		}
		return
	}
	privatePEM, err := s.repo.DeployKeyPrivatePEM(ctx, base.ID)
	if err != nil || privatePEM == "" {
		if err != nil {
			s.logger.Warn("deploy: preview deploy key could not be opened",
				"application_id", base.ID, "error", err)
		}
		return
	}
	if _, err := s.repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: preview.ID,
		Provider:      key.Provider,
		Repo:          key.Repo,
		ProviderKeyID: key.ProviderKeyID,
		Fingerprint:   key.Fingerprint,
		PublicKey:     key.PublicKey,
	}, privatePEM); err != nil {
		s.logger.Warn("deploy: could not copy the deploy key to a preview",
			"application_id", preview.ID, "error", err)
	}
}

// DeleteSystemApplication removes an application on a system path (the preview
// teardown). There is no caller to authorize, and the Git host is not touched:
// a preview reuses its base application's deploy key, so detaching it would
// break the base. Stopping the container stays best effort, exactly like the
// user-facing delete, so an unreachable node cannot block the teardown. An
// application that is already gone is a success (the teardown must be
// idempotent).
func (s *Service) DeleteSystemApplication(ctx context.Context, appID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	if s == nil || s.repo == nil {
		return errors.New("deploy: repository is not configured")
	}
	if appID == uuid.Nil {
		return fmt.Errorf("%w: invalid application id", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	s.stopBestEffort(ctx, app)
	if err := s.repo.DeleteApplication(ctx, appID); err != nil {
		return err
	}
	// The row is gone: refresh the node's routing so the deleted preview stops
	// being served immediately.
	if app.BaseDomain != "" {
		s.syncProxyServer(ctx, app.ServerID)
	}
	return nil
}
