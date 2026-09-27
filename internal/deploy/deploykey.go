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
	if !supportedDeployKeyProvider(app.Provider) {
		return fmt.Errorf("%w: application provider %q cannot register deploy keys", ErrValidation, app.Provider)
	}
	if strings.TrimSpace(app.Repo) == "" {
		return fmt.Errorf("%w: application has no repository", ErrValidation)
	}
	return nil
}

// supportedDeployKeyProvider reports whether a provider name has deploy-key
// implementations (exactly the SourceProvider set).
func supportedDeployKeyProvider(provider string) bool {
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
	app, err := s.application(ctx, userID, appID)
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
	app, err := s.application(ctx, userID, appID)
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
	if _, err := s.repo.DeleteDeployKey(ctx, appID); err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	return true, nil
}

// detachDeployKey removes an application's deploy key as part of deleting the
// application itself. Unlike DeleteDeployKey it tolerates a missing registrar
// (logging instead): an application must never become undeletable because the
// Git host cannot be reached — the row cascade would orphan the host key, so
// the warning names what to clean up by hand.
func (s *Service) detachDeployKey(ctx context.Context, app Application) error {
	key, err := s.repo.GetDeployKey(ctx, app.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	if err := s.removeHostDeployKey(ctx, app, key); err != nil {
		return err
	}
	if _, err := s.repo.DeleteDeployKey(ctx, app.ID); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// removeHostDeployKey deletes the key from the Git host when one was
// registered. A host failure is returned as ErrProvider so the caller aborts
// before touching the row.
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

// bestEffortRemoveKey rolls back a host-side key registration whose row could
// not be stored, so a retry does not accumulate orphan keys on the repository.
func (s *Service) bestEffortRemoveKey(ctx context.Context, app Application, providerKeyID string) {
	if s.registrar == nil {
		return
	}
	if err := s.registrar.RemoveDeployKey(ctx, deployKeyTarget(app), providerKeyID); err != nil {
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
