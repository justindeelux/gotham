package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

type PreviewApplicationInput struct {
	Name       string
	Branch     string
	BaseDomain string
}

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
	if NormalizeSourceType(base.SourceType, base.Provider) == SourceImage {
		return Application{}, fmt.Errorf("%w: source type %q has no branches to preview",
			ErrValidation, SourceImage)
	}

	app := Application{
		UserID:            base.UserID,
		TeamID:            base.TeamID,
		ServerID:          base.ServerID,
		EnvironmentID:     base.EnvironmentID,
		Name:              strings.TrimSpace(in.Name),
		Provider:          base.Provider,
		Repo:              base.Repo,
		CloneURL:          base.CloneURL,
		SourceType:        NormalizeSourceType(base.SourceType, base.Provider),
		GitHubAppID:       base.GitHubAppID,
		DockerfileContent: base.DockerfileContent,
		BuildArgs:         base.BuildArgs,
		Branch:            strings.TrimSpace(in.Branch),
		BuildPack:         base.BuildPack,
		ID:                uuid.New(),
		ComposeContent:    base.ComposeContent,
		ComposeFile:       base.ComposeFile,
		ComposeService:    base.ComposeService,
		IsPreview:         true,
	}
	if app.Branch == "" {
		app.Branch = base.Branch
	}
	if app.Branch == "" && !BranchDefaultsToRemote(base.SourceType, base.Provider) {
		app.Branch = defaultBranch
	}
	if app.SourceType == SourceCompose && strings.TrimSpace(app.ComposeContent) != "" {
		app.Branch = ""
		app.ComposeContent = rewriteManagedBindsForPreview(app.ComposeContent, managedVolumeRoot(), app.ID)
	}
	if err := validateApplication(app, true); err != nil {
		return Application{}, err
	}
	if base.PrimaryDomain == nil {
		return Application{}, fmt.Errorf("%w: the base application has no routeable domain to preview", ErrValidation)
	}
	host := proxy.NormalizeDomain(in.BaseDomain)
	if host != "" {
		if err := proxy.ValidateDomain(host); err != nil {
			return Application{}, fmt.Errorf("%w: %v", ErrValidation, err)
		}
		if base.PrimaryDomain == nil {
			return Application{}, fmt.Errorf("%w: the base application has no routeable domain to preview", ErrValidation)
		}
		releaseDomain, domainErr := s.guardDomains.Acquire(ctx, host)
		if domainErr != nil {
			return Application{}, fmt.Errorf("%w: %v", ErrValidation, domainErr)
		}
		defer releaseDomain()
	}

	envVars, err := s.repo.ListEnvVars(ctx, base.ID)
	if err != nil {
		return Application{}, err
	}

	created, err := s.repo.CreateApplication(ctx, app, envVars, nil, nil)
	if err != nil {
		return Application{}, err
	}
	if host == "" {
		s.copyDeployKey(ctx, base, created)
		s.copyGitCredential(ctx, base, created)
		return created, nil
	}
	domain := ApplicationDomain{
		ApplicationID: created.ID,
		Domain:        host,
		IsPrimary:     true,
		Port:          base.PrimaryDomain.Port,
		Service:       base.PrimaryDomain.Service,
		Protocol:      base.PrimaryDomain.Protocol,
	}
	if _, err := s.repo.CreateApplicationDomain(ctx, domain); err != nil {
		_ = s.repo.DeleteApplication(ctx, created.ID)
		return Application{}, err
	}
	created.PrimaryDomain = &domain
	s.copyDeployKey(ctx, base, created)
	s.copyGitCredential(ctx, base, created)
	s.cloneWildcardCertificate(ctx, base, created)
	return created, nil
}

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
	if proxy.NormalizeDomain(intent.Domain) != proxy.NormalizeDomain(base.PrimaryDomain.Domain) {
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
	host := proxy.NormalizeDomain(preview.PrimaryDomain.Domain)
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

func (s *Service) copyGitCredential(ctx context.Context, base, preview Application) {
	if err := s.repo.CopyGitCredential(ctx, base.ID, preview.ID); err != nil {
		s.logger.Warn("deploy: could not copy the git credential to a preview",
			"application_id", preview.ID, "error", err)
	}
}

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
	if !app.IsPreview {
		return fmt.Errorf("%w: application %s is not a preview", ErrValidation, appID)
	}
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
	if app.PrimaryDomain != nil {
		s.syncProxyServer(ctx, app.ServerID)
	}
	return nil
}
