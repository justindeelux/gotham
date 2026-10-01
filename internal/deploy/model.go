package deploy

import (
	"time"

	"github.com/google/uuid"
)

// Application is the parent resource of every deployment: it pins the
// repository, branch, build pack and runtime shape (server, port, domains).
// Env vars, secrets and storages hang off the application so every redeploy
// and rollback sees the same configuration.
type Application struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TeamID     uuid.UUID
	ServerID   uuid.UUID
	Name       string
	Provider   string
	Repo       string
	CloneURL   string
	Branch     string
	BuildPack  string
	BaseDomain string
	// BaseDomainDisabled marks a binding the domain-uniqueness migration had
	// to disable because another application owned the domain first. The
	// value is preserved; an explicit domain update re-enables it.
	BaseDomainDisabled bool
	// IsPreview marks an application created by the preview controller
	// (BE-8.1). A preview reuses its base application's remote deploy key, so
	// deleting one must never remove the key from the Git host; the marker is
	// also what lets the orphan sweep find a preview whose binding is gone.
	IsPreview bool
	Port      int32
	HostPort  int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Deployment is one attempt to run an application revision (kind "deploy" or
// "rollback"), walking the state machine queued → cloning → building →
// pushing → starting → running | failed. The image reference built for the
// attempt is kept forever: that is what rollback redeploys.
type Deployment struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Kind          Kind
	State         State
	ImageTag      string
	RegistryImage string
	Digest        string
	Error         string
	Attempt       int32
	ContainerID   string
	RollbackFrom  uuid.UUID
	StartedAt     time.Time
	FinishedAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EnvVar is a plain KEY=VALUE setting sent to the container. It is not
// confidential; secrets carry the sealed values.
type EnvVar struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Key           string
	Value         string
	CreatedAt     time.Time
}

// Secret is a confidential KEY=VALUE setting. Ciphertext is base64
// (nonce||ciphertext) sealed with AES-256-GCM (providers.SealSecret); it is
// decrypted only when the runtime payload is assembled for the agent.
type Secret struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Key           string
	Ciphertext    string
	CreatedAt     time.Time
}

// CertificateIntent is the slice of a domain_certificates row the preview
// clone copies onto a sibling: the per-application certificate configuration.
// The preview surface only ever clones an enabled wildcard DNS-01 intent; the
// exported shape keeps the repository seam free of proxy types.
type CertificateIntent struct {
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	// Challenge is "http-01" or "dns-01" (proxy.ChallengeMode values).
	Challenge     string
	DNSProviderID uuid.UUID
	Wildcard      bool
}

// DNSProviderInfo is the slice of a dns_providers row the preview clone needs:
// the zones the credential may certify in, and whether the provider is usable.
type DNSProviderInfo struct {
	Zones   []string
	Enabled bool
}

// Storage is a persistent directory on the node mounted into the container.
type Storage struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Name          string
	HostPath      string
	ContainerPath string
	CreatedAt     time.Time
}
