package updates

import (
	"crypto/ed25519"

	"github.com/justindeelux/gotham/updatecore"
)

type (
	Applier      = updatecore.Applier
	ApplyOutcome = updatecore.ApplyOutcome
	RestartFunc  = updatecore.RestartFunc
	Manifest     = updatecore.Manifest
	Status       = updatecore.Status
	StatusStore  = updatecore.StatusStore
	Verifier     = updatecore.Verifier
	Signer       = updatecore.Signer
	Version      = updatecore.Version
)

const (
	SignatureSize     = updatecore.SignatureSize
	ManifestPrefix    = updatecore.ManifestPrefix
	ManifestSuffix    = updatecore.ManifestSuffix
	ManifestSigSuffix = updatecore.ManifestSigSuffix
	OldSuffix         = updatecore.OldSuffix
	DefaultMaxBytes   = updatecore.DefaultMaxBytes
	PublicKeyEnv      = updatecore.PublicKeyEnv
)

const (
	StatusStaged         = updatecore.StatusStaged
	StatusOK             = updatecore.StatusOK
	StatusRolledBack     = updatecore.StatusRolledBack
	StatusRollbackFailed = updatecore.StatusRollbackFailed
	StatusNoBackup       = updatecore.StatusNoBackup
	StatusWrapperFailed  = updatecore.StatusWrapperFailed
	StatusResuming       = updatecore.StatusResuming
)

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

func NewVerifier(key ed25519.PublicKey) (*Verifier, error) { return updatecore.NewVerifier(key) }

func NewVerifierSet(keys ...ed25519.PublicKey) (*Verifier, error) {
	return updatecore.NewVerifierSet(keys...)
}

func NewSigner(key ed25519.PrivateKey) (*Signer, error) { return updatecore.NewSigner(key) }

func NewStatusStore(path string) *StatusStore { return updatecore.NewStatusStore(path) }

func LoadPublicKey() (ed25519.PublicKey, error) { return updatecore.LoadPublicKey() }

func LoadPublicKeys() ([]ed25519.PublicKey, error) { return updatecore.LoadPublicKeys() }
