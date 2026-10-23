package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/justindeelux/gotham/internal/providers"
)

type DeployKey struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	PrivateKeyID  uuid.UUID
	Provider      string
	Repo          string
	ProviderKeyID string
	Fingerprint   string
	PublicKey     string
	CreatedAt     time.Time
}

type KeyRegistrar interface {
	AddDeployKey(ctx context.Context, target providers.HookTarget, key providers.DeployKey) (string, error)
	RemoveDeployKey(ctx context.Context, target providers.HookTarget, keyID string) error
}

func deployKeyRowName(appID uuid.UUID) string {
	return "deploy-key:" + appID.String()
}

func deployKeyComment(appID uuid.UUID) string {
	return "gotham:deploy:" + appID.String()
}

func deployKeyTitle(appName string) string {
	const maxLength = 100
	title := "gotham:" + strings.TrimSpace(appName)
	if len(title) > maxLength {
		title = title[:maxLength]
	}
	return title
}

func generateDeployKeyPair(comment string) (privatePEM, publicKey, fingerprint string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("deploy: generate deploy key: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", "", fmt.Errorf("deploy: marshal deploy key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", "", fmt.Errorf("deploy: marshal deploy public key: %w", err)
	}

	line := strings.TrimRight(string(ssh.MarshalAuthorizedKey(sshPub)), "\n")
	return string(pem.EncodeToMemory(block)), line + " " + comment, ssh.FingerprintSHA256(sshPub), nil
}

func deployKeyTarget(app Application) providers.HookTarget {
	return providers.HookTarget{
		UserID:   app.UserID,
		Provider: app.Provider,
		CloneURL: app.CloneURL,
		Repo:     app.Repo,
	}
}

func validateDeployKeyTarget(app Application) error {
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceGitPrivate &&
		strings.TrimSpace(app.Provider) == "" {
		if strings.TrimSpace(app.Repo) == "" && strings.TrimSpace(app.CloneURL) == "" {
			return fmt.Errorf("%w: application has no repository", ErrValidation)
		}
		return nil
	}
	if !supportedSourceProvider(app.Provider) {
		return fmt.Errorf("%w: application provider %q cannot register deploy keys", ErrValidation, app.Provider)
	}
	if strings.TrimSpace(app.Repo) == "" {
		return fmt.Errorf("%w: application has no repository", ErrValidation)
	}
	return nil
}

func supportedSourceProvider(provider string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitLab, providers.NameGitea:
		return true
	default:
		return false
	}
}

func (s *Service) CreateDeployKey(ctx context.Context, userID, appID uuid.UUID) (DeployKey, error) {
	if !Enabled() {
		return DeployKey{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return DeployKey{}, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return DeployKey{}, err
	}
	if err := validateDeployKeyTarget(app); err != nil {
		return DeployKey{}, err
	}
	existing, err := s.repo.GetDeployKey(ctx, appID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return DeployKey{}, err
	}
	if deployKeyLocalOnly(app) {
		return s.storeLocalDeployKey(ctx, app)
	}
	if s.registrar == nil {
		return DeployKey{}, errors.New("deploy: deploy key registrar is not configured")
	}

	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair(deployKeyComment(app.ID))
	if err != nil {
		return DeployKey{}, err
	}
	providerKeyID, err := s.registrar.AddDeployKey(ctx, deployKeyTarget(app), providers.DeployKey{
		Title: deployKeyTitle(app.Name),
		Key:   publicKey,
	})
	if err != nil {
		return DeployKey{}, mapProviderError(app.Provider, err)
	}

	key, err := s.repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: app.ID,
		Provider:      app.Provider,
		Repo:          app.Repo,
		ProviderKeyID: providerKeyID,
		Fingerprint:   fingerprint,
		PublicKey:     publicKey,
	}, privatePEM)
	if err != nil {
		s.bestEffortRemoveKey(ctx, app, providerKeyID)
		return DeployKey{}, err
	}
	return key, nil
}

func deployKeyLocalOnly(app Application) bool {
	return NormalizeSourceType(app.SourceType, app.Provider) == SourceGitPrivate &&
		strings.TrimSpace(app.Provider) == ""
}

func (s *Service) storeLocalDeployKey(ctx context.Context, app Application) (DeployKey, error) {
	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair(deployKeyComment(app.ID))
	if err != nil {
		return DeployKey{}, err
	}
	repo := strings.TrimSpace(app.Repo)
	if repo == "" {
		repo = strings.TrimSpace(app.CloneURL)
	}
	return s.repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: app.ID,
		Provider:      "",
		Repo:          repo,
		Fingerprint:   fingerprint,
		PublicKey:     publicKey,
	}, privatePEM)
}

func (s *Service) DeleteDeployKey(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	if !Enabled() {
		return false, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return false, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return false, err
	}
	key, err := s.repo.GetDeployKey(ctx, appID)
	switch {
	case errors.Is(err, ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	if err := s.removeHostDeployKey(ctx, app, key); err != nil {
		return false, err
	}
	if _, err := s.repo.DeleteDeployKey(ctx, key); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (s *Service) detachDeployKey(ctx context.Context, app Application) error {
	key, err := s.repo.GetDeployKey(ctx, app.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	hostErr := s.removeHostDeployKey(ctx, app, key)
	localErr := s.deleteLocalDeployKey(ctx, key)
	switch {
	case localErr != nil && hostErr != nil:
		return fmt.Errorf("%w; local key cleanup also failed: %v", hostErr, localErr)
	case localErr != nil:
		return localErr
	default:
		return hostErr
	}
}

func (s *Service) deleteLocalDeployKey(ctx context.Context, key DeployKey) error {
	if _, err := s.repo.DeleteDeployKey(ctx, key); err != nil && !errors.Is(err, ErrNotFound) {
		current, getErr := s.repo.GetDeployKey(ctx, key.ApplicationID)
		if getErr != nil || current.PrivateKeyID != key.PrivateKeyID {
			return err
		}
		if purgeErr := s.repo.DeletePrivateKey(ctx, key.PrivateKeyID); purgeErr != nil {
			return errors.Join(err, purgeErr)
		}
	}
	return nil
}

func (s *Service) removeHostDeployKey(ctx context.Context, app Application, key DeployKey) error {
	if key.ProviderKeyID == "" {
		return nil
	}
	if s.registrar == nil {
		s.logger.Warn("deploy: deploy key registrar is not configured; leaving the key on the Git host",
			"application_id", app.ID, "provider", app.Provider, "provider_key_id", key.ProviderKeyID)
		return nil
	}
	if err := s.registrar.RemoveDeployKey(ctx, deployKeyTarget(app), key.ProviderKeyID); err != nil {
		return mapProviderError(app.Provider, err)
	}
	return nil
}

const deployKeyRollbackTimeout = 3 * time.Second

func (s *Service) bestEffortRemoveKey(ctx context.Context, app Application, providerKeyID string) {
	if s.registrar == nil {
		return
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deployKeyRollbackTimeout)
	defer cancel()
	if err := s.registrar.RemoveDeployKey(rollbackCtx, deployKeyTarget(app), providerKeyID); err != nil {
		s.logger.Warn("deploy: could not roll back deploy key registration",
			"application_id", app.ID, "provider_key_id", providerKeyID, "error", err)
	}
}

func mapProviderError(provider string, err error) error {
	switch {
	case errors.Is(err, providers.ErrNotConnected), errors.Is(err, providers.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrNotConnected, provider)
	case errors.Is(err, providers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return fmt.Errorf("%w: %s: %v", ErrProvider, provider, err)
	}
}
