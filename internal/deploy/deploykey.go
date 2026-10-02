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

// DeployKey is the SSH deploy key of an application: the mapping row that
// points at a sealed private key in private_keys and at the public key
// registered on the Git host. The private half never appears here — it is
// opened only by the cloner, just before a clone.
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

// KeyRegistrar is the slice of providers.ProviderService the deploy-key
// lifecycle needs. Declaring it here keeps this package off the provider
// service's repository and credential handling, and lets tests fake the Git
// host (it mirrors webhooks.Installer for the same reason).
type KeyRegistrar interface {
	// AddDeployKey registers a public key on target.Repo and returns the
	// provider's own key ID.
	AddDeployKey(ctx context.Context, target providers.HookTarget, key providers.DeployKey) (string, error)
	// RemoveDeployKey removes the key identified by keyID; a key the provider
	// no longer knows about is a success.
	RemoveDeployKey(ctx context.Context, target providers.HookTarget, keyID string) error
}

// deployKeyRowName names the private_keys row of an application's deploy key.
// The prefix keeps it recognisable in the node SSH key listing the servers UI
// reads (the table is shared with Phase 2).
func deployKeyRowName(appID uuid.UUID) string {
	return "deploy-key:" + appID.String()
}

// deployKeyComment is the OpenSSH comment Gotham puts on the public key, so a
// human reading the host's key list can tell whose key it is.
func deployKeyComment(appID uuid.UUID) string {
	return "gotham:deploy:" + appID.String()
}

// deployKeyTitle is the key title shown in the provider's key list.
func deployKeyTitle(appName string) string {
	const maxLength = 100
	title := "gotham:" + strings.TrimSpace(appName)
	if len(title) > maxLength {
		title = title[:maxLength]
	}
	return title
}

// generateDeployKeyPair creates an ed25519 keypair for one application. It
// returns the private half as an OpenSSH PEM (the format `ssh -i` reads), the
// authorized_keys line to register on the Git host and that key's SHA-256
// fingerprint (`SHA256:…`), computed locally so the control plane does not
// have to trust (or query) the host for it.
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

	// MarshalAuthorizedKey writes "type base64" with a trailing newline and no
	// comment; the comment is what makes the key identifiable in the host UI.
	line := strings.TrimRight(string(ssh.MarshalAuthorizedKey(sshPub)), "\n")
	return string(pem.EncodeToMemory(block)), line + " " + comment, ssh.FingerprintSHA256(sshPub), nil
}

// deployKeyTarget names the repository (and stored connection) a deploy key is
// registered on.
func deployKeyTarget(app Application) providers.HookTarget {
	return providers.HookTarget{
		UserID:   app.UserID,
		Provider: app.Provider,
		CloneURL: app.CloneURL,
		Repo:     app.Repo,
	}
}

// validateDeployKeyTarget rejects an application no deploy key can be
// registered for: without a provider connection and a repository identifier
// there is no API to call (a pasted public URL is cloned anonymously).
func validateDeployKeyTarget(app Application) error {
	if !supportedSourceProvider(app.Provider) {
		return fmt.Errorf("%w: application provider %q cannot register deploy keys", ErrValidation, app.Provider)
	}
	if strings.TrimSpace(app.Repo) == "" {
		return fmt.Errorf("%w: application has no repository", ErrValidation)
	}
	return nil
}

// supportedSourceProvider reports whether a provider name has source-provider
// implementations: the Git hosts whose deploy keys, webhooks and repository
// APIs Gotham can call (exactly the providers.SourceProvider set).
func supportedSourceProvider(provider string) bool {
	switch provider {
	case providers.NameGitHub, providers.NameGitLab, providers.NameGitea:
		return true
	default:
		return false
	}
}

// CreateDeployKey generates an SSH deploy keypair for an application and
// registers its public half with the Git host. It is idempotent: an
// application that already has a key gets the same row back, because
// regenerating would leave the old public key registered on the host.
//
// Nothing is stored until the host accepted the key, so a failing provider
// never leaves an unusable half-configured key behind.
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
		// The host now holds a key this control plane cannot use. Undo it so
		// a retry starts from a clean state (mirrors the webhook install).
		s.bestEffortRemoveKey(ctx, app, providerKeyID)
		return DeployKey{}, err
	}
	return key, nil
}

// DeleteDeployKey removes the deploy key of an application, first on the Git
// host and then in the database. It is idempotent: an application with no key
// reports false and no error. A host that fails for any reason other than
// "already gone" aborts the call so the row and the host stay in step.
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
		return false, nil // already detached: deleting twice is a success
	case err != nil:
		return false, err
	}
	if err := s.removeHostDeployKey(ctx, app, key); err != nil {
		return false, err
	}
	// Fenced on the mapping ID we just read: a concurrent delete that already
	// removed this row (and possibly had a replacement installed behind it)
	// leaves the replacement untouched, and we honestly report that this call
	// removed no local row.
	if _, err := s.repo.DeleteDeployKey(ctx, key); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// detachDeployKey removes an application's deploy key as part of deleting the
// application itself. The local rows — the mapping and the sealed private key
// it points at — always go, regardless of the host outcome: private_keys has
// no application FK, so deleting the application cascades only the mapping and
// would strand the sealed credential. The host removal stays best effort; its
// error is returned so the caller logs the remote key for manual cleanup (a
// missing registrar is tolerated the same way).
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

// deleteLocalDeployKey removes the mapping and the sealed private key it points
// at. The fenced delete normally takes both; when it fails, the private key is
// removed directly by ID — its FK cascades the mapping — so the application
// delete that follows cannot strand a sealed credential with no owner (C3-10).
func (s *Service) deleteLocalDeployKey(ctx context.Context, key DeployKey) error {
	if _, err := s.repo.DeleteDeployKey(ctx, key); err != nil && !errors.Is(err, ErrNotFound) {
		if purgeErr := s.repo.DeletePrivateKey(ctx, key.PrivateKeyID); purgeErr != nil {
			return errors.Join(err, purgeErr)
		}
	}
	return nil
}

// removeHostDeployKey deletes the key from the Git host when one was
// registered. A host failure is returned as ErrProvider so the caller can log
// the orphan; detachDeployKey removes the local rows regardless, because the
// sealed private key must not survive the application.
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

// deployKeyRollbackTimeout bounds the best-effort rollback of a deploy key
// whose row could not be stored. It is deliberately short: the rollback runs on
// a detached context (see bestEffortRemoveKey), so nothing else bounds it.
const deployKeyRollbackTimeout = 3 * time.Second

// bestEffortRemoveKey rolls back a host-side key registration whose row could
// not be stored, so a retry does not accumulate orphan keys on the repository.
//
// It detaches from the create context, which carries the caller's deadline and
// is exactly what may have just expired (a provider that answered near the
// timeout, a store write that failed on the expired context); removal must
// still happen then. The detached context is bounded so a stalled host cannot
// hold the rollback open forever, mirroring the webhook helper.
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

// mapProviderError turns a Git-host failure into a deploy sentinel so the
// route can answer without leaking the provider's response body.
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
