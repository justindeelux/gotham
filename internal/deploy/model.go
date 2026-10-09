package deploy

import (
	"time"

	"github.com/google/uuid"
)

// Application source types (GS-2, GS-7). Git-backed types share the deploy-key
// cloner; pasted Dockerfiles materialize as the build context. Compose and
// image sources land in GS-8..GS-9 and fail closed in the orchestrator
// until then.
const (
	SourceGitPublic  = "git_public"
	SourceGitPrivate = "git_private"
	SourceGitHubApp  = "github_app"
	SourceGitLabApp  = "gitlab_app"
	SourceDockerfile = "dockerfile"
	SourceCompose    = "compose"
	SourceImage      = "image"
)

// ValidSourceType reports whether s names a known application source type.
// Empty is valid: legacy rows and callers predate the column and behave
// like SourceGitPublic.
func ValidSourceType(s string) bool {
	switch s {
	case "", SourceGitPublic, SourceGitPrivate, SourceGitHubApp,
		SourceGitLabApp, SourceDockerfile, SourceCompose, SourceImage:
		return true
	default:
		return false
	}
}

// NormalizeSourceType fills an empty source type from the provider, so API
// clients that predate GS-2 keep their behaviour: a provider-connected
// application stays on the provider-backed flow, everything else is public
// git. It mirrors the 00037 backfill.
func NormalizeSourceType(sourceType, provider string) string {
	if sourceType != "" {
		return sourceType
	}
	switch provider {
	case "github":
		return SourceGitHubApp
	case "gitlab":
		return SourceGitLabApp
	default:
		return SourceGitPublic
	}
}

// SourceTypeImplemented reports whether the deploy pipeline can fetch
// the type yet: public and private git, the connected-provider flows,
// pasted Dockerfiles (GS-7), prebuilt container images (GS-9) and pasted or
// repo-backed compose documents (GS-8, last).
func SourceTypeImplemented(s string) bool {
	switch s {
	case "", SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp, SourceDockerfile, SourceImage, SourceCompose:
		return true
	default:
		return false
	}
}

// BranchDefaultsToRemote reports whether an empty branch resolves to the
// remote default via ls-remote at clone time (public and private git)
// instead of falling back to "main". Provider flows keep the "main"
// fallback: their wizard always prefills a branch, and empty would break
// push-branch matching.
func BranchDefaultsToRemote(sourceType, provider string) bool {
	switch NormalizeSourceType(sourceType, provider) {
	case SourceGitPublic, SourceGitPrivate:
		return true
	default:
		return false
	}
}

// Application is the parent resource of every deployment: it pins the
// repository, branch, build pack and runtime shape (server, port, domains).
// Env vars, secrets and storages hang off the application so every redeploy
// and rollback sees the same configuration.
type Application struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	TeamID   uuid.UUID
	ServerID uuid.UUID
	// EnvironmentID is the environment the application belongs to (Phase 13,
	// PE-2); ProjectID derives from it.
	EnvironmentID uuid.UUID
	Name          string
	Provider      string
	Repo          string
	CloneURL      string
	// SourceType names how the application fetches its code (GS-2). Empty
	// means a legacy row or caller: NormalizeSourceType derives the effective
	// type from the provider (github/gitlab keep their provider flow,
	// everything else behaves like SourceGitPublic).
	SourceType string
	// DockerfileContent holds pasted Dockerfile text for SourceDockerfile
	// applications (GS-7); empty for every other source type. Redeploys
	// always build the latest stored text.
	DockerfileContent string
	// BuildArgs holds the optional --build-arg pairs for SourceDockerfile
	// applications, sent with the build context to the node.
	BuildArgs map[string]string
	Branch    string
	BuildPack string
	// ImageRef is the prebuilt reference an image source pulls
	// (registry/repo:tag, optionally digest-pinned). Only image sources
	// carry it.
	ImageRef string
	// RegistryUsername and RegistryPasswordCiphertext are the optional
	// private-registry credential of an image source. The password is
	// AES-256-GCM sealed; neither value is ever returned by the API or
	// logged.
	RegistryUsername           string
	RegistryPasswordCiphertext string
	// ComposeContent holds the pasted compose file text of a compose
	// application (empty for repo-backed ones and every other source type).
	// ComposeFile holds the in-repo compose file path of a repo-backed
	// compose application (empty for pasted ones). ComposeService names the
	// compose service the application's domain/port routing targets.
	ComposeContent string
	ComposeFile    string
	ComposeService string
	BaseDomain     string
	// BaseDomainDisabled marks a binding the domain-uniqueness migration had
	// to disable because another application owned the domain first. The
	// value is preserved; an explicit domain update re-enables it.
	BaseDomainDisabled bool
	// IsPreview marks an application created by the preview controller
	// (BE-8.1). A preview reuses its base application's remote deploy key, so
	// deleting one must never remove the key from the Git host; the marker is
	// also what lets the orphan sweep find a preview whose binding is gone.
	IsPreview bool
	// GitHubAppID links the application to its GitHub App connection (GS-5,
	// migration 00038). Only a linked application with an https clone URL
	// ever takes the installation-token clone path; uuid.Nil (every legacy
	// row) keeps the previous OAuth/deploy-key behaviour.
	GitHubAppID uuid.UUID
	Port        int32
	HostPort    int32
	// EnvironmentName, ProjectID, ProjectName and ServerName enrich the list
	// and get responses per the Phase 13 contract; they derive from
	// EnvironmentID and ServerID and are never written directly.
	EnvironmentName string
	ProjectID       uuid.UUID
	ProjectName     string
	ServerName      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
	// ComposeDocument is the raw uninterpolated compose document a compose
	// run recorded; ComposeCommit is the repo commit it was read from (""
	// for pasted sources). A compose rollback re-renders the target's
	// document with the current environment. Secret values never reach the
	// row (only ${VAR} references), which is never returned by the API.
	ComposeDocument string
	ComposeCommit   string
	// CommitSHA, CommitMessage, CommitAuthor and CommittedAt record the git
	// commit a deployment cloned ("" when the source never clones). The
	// message is the subject line only, sanitised and truncated; the author
	// is the name without the email; the date is RFC3339 or "".
	CommitSHA     string
	CommitMessage string
	CommitAuthor  string
	CommittedAt   string
	StartedAt     time.Time
	FinishedAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EnvironmentRef is the slice of an environment row resource validation and
// response enrichment needs: which project it belongs to and its name.
type EnvironmentRef struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
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

// SharedVariable is one project- or environment-level variable resolved for
// one application deploy. Plain pairs carry Value; secret pairs carry the
// sealed Ciphertext end to end (it is opened with providers.OpenSecret when
// the runtime payload is assembled, exactly like application secrets).
type SharedVariable struct {
	Key        string
	Value      string
	Ciphertext string
	Secret     bool
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
