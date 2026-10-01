package updates

import (
	"crypto/ed25519"

	"github.com/justindeelux/gotham/updatecore"
)

// This file re-exports the shared self-update engine from updatecore so the
// control-plane package keeps its surface (and its BE-9.1 tests) while the
// implementation lives in a neutral top-level package the node agent can also
// import (AGENTS.md: agent/ must not import internal/).

// Shared types.
type (
	// Applier downloads, verifies and installs a release.
	Applier = updatecore.Applier
	// ApplyOutcome reports what an Apply did.
	ApplyOutcome = updatecore.ApplyOutcome
	// RestartFunc launches the privileged restart/healthcheck wrapper.
	RestartFunc = updatecore.RestartFunc
	// Manifest is the signed release descriptor.
	Manifest = updatecore.Manifest
	// Status is the durable outcome of the most recent update attempt.
	Status = updatecore.Status
	// StatusStore reads and writes the update status/pending file.
	StatusStore = updatecore.StatusStore
	// Verifier verifies detached Ed25519 signatures.
	Verifier = updatecore.Verifier
	// Signer signs release artifacts.
	Signer = updatecore.Signer
	// Version is a parsed semver-ish version.
	Version = updatecore.Version
)

// Shared constants.
const (
	// SignatureSize is the fixed size of an Ed25519 signature.
	SignatureSize = updatecore.SignatureSize
	// ManifestPrefix, ManifestSuffix and ManifestSigSuffix name a release
	// manifest and its detached signature.
	ManifestPrefix    = updatecore.ManifestPrefix
	ManifestSuffix    = updatecore.ManifestSuffix
	ManifestSigSuffix = updatecore.ManifestSigSuffix
	// OldSuffix is the suffix of the retained previous binary.
	OldSuffix = updatecore.OldSuffix
	// DefaultMaxBytes bounds the release artifact download.
	DefaultMaxBytes = updatecore.DefaultMaxBytes
	// PublicKeyEnv optionally supplies a public key for development builds.
	PublicKeyEnv = updatecore.PublicKeyEnv
)

// Status result values recorded by the wrapper and the pending marker.
const (
	StatusStaged         = updatecore.StatusStaged
	StatusOK             = updatecore.StatusOK
	StatusRolledBack     = updatecore.StatusRolledBack
	StatusRollbackFailed = updatecore.StatusRollbackFailed
	StatusNoBackup       = updatecore.StatusNoBackup
	StatusWrapperFailed  = updatecore.StatusWrapperFailed
	StatusResuming       = updatecore.StatusResuming
)

// Shared sentinels. Assigning the shared values (rather than wrapping them)
// keeps errors.Is working across both packages.
var (
	ErrNoPublicKey      = updatecore.ErrNoPublicKey
	ErrBadSignature     = updatecore.ErrBadSignature
	ErrBadURL           = updatecore.ErrBadURL
	ErrManifest         = updatecore.ErrManifest
	ErrChecksumMismatch = updatecore.ErrChecksumMismatch
	ErrDownload         = updatecore.ErrDownload
	ErrTooLarge         = updatecore.ErrTooLarge
	ErrApply            = updatecore.ErrApply
	ErrNoBackup         = updatecore.ErrNoBackup
	ErrNoUpdate         = updatecore.ErrNoUpdate
	ErrUpdatePending    = updatecore.ErrUpdatePending
)

// NewVerifier wraps a single release public key. It fails closed on a
// wrong-sized key.
func NewVerifier(key ed25519.PublicKey) (*Verifier, error) { return updatecore.NewVerifier(key) }

// NewVerifierSet wraps the release key ring (current + pre-positioned next).
// It fails closed on an empty set or a wrong-sized key.
func NewVerifierSet(keys ...ed25519.PublicKey) (*Verifier, error) {
	return updatecore.NewVerifierSet(keys...)
}

// NewSigner wraps a release private key.
func NewSigner(key ed25519.PrivateKey) (*Signer, error) { return updatecore.NewSigner(key) }

// NewStatusStore returns a store at path, or nil when path is empty.
func NewStatusStore(path string) *StatusStore { return updatecore.NewStatusStore(path) }

// LoadPublicKey resolves the single release public key: the embedded current
// key is authoritative, the development environment override applies only when
// no key is embedded.
func LoadPublicKey() (ed25519.PublicKey, error) { return updatecore.LoadPublicKey() }

// LoadPublicKeys resolves the release key ring (embedded current + optional
// next key; the development environment override applies only when nothing is
// embedded). It fails closed when no usable key is configured.
func LoadPublicKeys() ([]ed25519.PublicKey, error) { return updatecore.LoadPublicKeys() }
