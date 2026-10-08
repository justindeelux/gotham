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
// server. Plain environment variables are copied verbatim and the base's
// deploy key is re-registered on the sibling (same key material) so a private
// repository stays cloneable.
//
// Sealed secrets and storages are deliberately NOT copied: a preview is served
// publicly from a PR branch, so a contributor's code must not be able to read
// the base application's production secrets or write its volumes. An operator
// who wants shared configuration must move those values into plain env vars
// (visible) or configure the preview application explicitly.
//
// Host port is never copied: the preview must not fight the base application
// for a pinned host port.
//
// The returned application is a normal applications row flagged is_preview, so
// the whole Phase 4 deploy path and the Phase 6 proxy sync work on it
// unchanged.
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
		UserID:        base.UserID,
		TeamID:        base.TeamID,
		ServerID:      base.ServerID,
		EnvironmentID: base.EnvironmentID,
		Name:          strings.TrimSpace(in.Name),
		Provider:      base.Provider,
		Repo:          base.Repo,
		CloneURL:      base.CloneURL,
		// Normalize a legacy empty type from the provider, exactly like
		// creation does: the preview is a new row and must pass the
		// provider/source agreement check below.
		SourceType: NormalizeSourceType(base.SourceType, base.Provider),
		Branch:     strings.TrimSpace(in.Branch),
		BuildPack:  base.BuildPack,
		BaseDomain: proxy.NormalizeDomain(in.BaseDomain),
		IsPreview:  true,
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

	created, err := s.repo.CreateApplication(ctx, app, envVars, nil, nil)
	if err != nil {
		return Application{}, err
	}
	s.copyDeployKey(ctx, base, created)
	s.cloneWildcardCertificate(ctx, base, created)
	return created, nil
}

// cloneWildcardCertificate copies the base application's enabled wildcard
// DNS-01 certificate intent onto its preview sibling, so the preview is served
// over HTTPS through the same DNS provider instead of staying HTTP-only. The
// clone is deliberately narrow:
//
//   - only an enabled wildcard intent with a DNS provider is cloned (wildcards
//     require DNS-01, and a plain HTTP-01 intent names the base host only);
//   - the base intent must still record the base application's current
//     base_domain (a stale record never activates the base route either);
//   - the preview host must be certifiable through the provider's DNS zones,
//     so the sibling never gets an intent that can only stay HTTP-only.
//
// The sibling's intent records the preview host, which is exactly what the
// route generator needs to activate HTTPS for the sibling (the exact host is
// always the main certificate name). Best effort: a failure logs and leaves
// the preview HTTP-only. The row cascades away with the sibling application
// (domain_certificates.application_id ON DELETE CASCADE), so teardown never
// orphans an intent.
func (s *Service) cloneWildcardCertificate(ctx context.Context, base, preview Application) {
	intent, err := s.repo.GetCertificateIntent(ctx, base.ID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Warn("deploy: preview certificate intent lookup failed",
				"application_id", base.ID, "error", err)
		}
		return
	}
	if !intent.Enabled || !intent.Wildcard ||
		intent.Challenge != string(proxy.ChallengeDNS01) || intent.DNSProviderID == uuid.Nil {
		return
	}
	if proxy.NormalizeDomain(intent.Domain) != proxy.NormalizeDomain(base.BaseDomain) {
		return
	}
	provider, err := s.repo.GetDNSProviderInfo(ctx, intent.DNSProviderID)
	if err != nil {
		s.logger.Warn("deploy: preview certificate provider lookup failed",
			"application_id", base.ID, "provider_id", intent.DNSProviderID, "error", err)
		return
	}
	if !provider.Enabled {
		return
	}
	host := proxy.NormalizeDomain(preview.BaseDomain)
	if _, ok := proxy.WildcardBase(host, provider.Zones); !ok {
		return
	}
	if err := s.repo.CreateCertificateIntent(ctx, CertificateIntent{
		ApplicationID: preview.ID,
		Domain:        host,
		Enabled:       true,
		Challenge:     string(proxy.ChallengeDNS01),
		DNSProviderID: intent.DNSProviderID,
		Wildcard:      true,
	}); err != nil {
		s.logger.Warn("deploy: could not clone the preview certificate intent",
			"application_id", preview.ID, "error", err)
	}
}

// copyDeployKey re-registers the base application's deploy key on the preview
// sibling (same key material, new mapping row) so a private repository stays
// cloneable. The remote key is shared: teardown never removes it from the Git
// host (see DeleteSystemApplication and DeleteApplication). Best effort: a
// failure is logged, the sibling still deploys against a public repository,
// and a private one reports the clone failure on its deployment row.
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
// teardown). There is no caller to authorize, and the Git host is never
// touched: a preview reuses its base application's deploy key, so removing it
// would break the base (and every sibling). The local deploy key rows are
// deleted explicitly first — deleting the application only cascades the
// mapping, which would strand the sealed private key row. A deployment that is
// in flight is refused with ErrConflict (the teardown removes the container and
// cascades the row); the callers retry or the preview sweep picks it up. An
// unreachable node never blocks the teardown: the container removal stays best
// effort, exactly like the user-facing delete. An application that is already
// gone is a success (the teardown must be idempotent).
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
	// Defence in depth: this system path exists for preview siblings only. An
	// ordinary application must never be deleted through it, because the
	// teardown deliberately skips the remote deploy-key detach.
	if !app.IsPreview {
		return fmt.Errorf("%w: application %s is not a preview", ErrValidation, appID)
	}
	// Serialize with deployment submission and manual control, and refuse while
	// a deployment is in flight: the teardown removes the container and
	// cascades the row, which would orphan a container the worker is replacing.
	// Re-read under the lock so a concurrent move cannot leave the teardown
	// targeting the previous node.
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.repo.GetApplication(ctx, appID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.rejectInFlight(ctx, app.ID); err != nil {
		return err
	}
	// Local key rows go first: a failure aborts before the application row
	// disappears, so the teardown (and its binding) stays retryable and no
	// orphan private key is left behind.
	switch key, err := s.repo.GetDeployKey(ctx, appID); {
	case err == nil:
		if err := s.deleteLocalDeployKey(ctx, key); err != nil {
			return err
		}
	case !errors.Is(err, ErrNotFound):
		return err
	}
	s.removeApplicationContainers(ctx, app)
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
