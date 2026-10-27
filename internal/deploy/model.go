package deploy

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/runtimewatch"
)

const (
	SourceGitPublic  = "git_public"
	SourceGitPrivate = "git_private"
	SourceGitHubApp  = "github_app"
	SourceGitLabApp  = "gitlab_app"
	SourceDockerfile = "dockerfile"
	SourceCompose    = "compose"
	SourceImage      = "image"
)

func ValidSourceType(s string) bool {
	switch s {
	case "", SourceGitPublic, SourceGitPrivate, SourceGitHubApp,
		SourceGitLabApp, SourceDockerfile, SourceCompose, SourceImage:
		return true
	default:
		return false
	}
}

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

func SourceTypeImplemented(s string) bool {
	switch s {
	case "", SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp, SourceDockerfile, SourceImage, SourceCompose:
		return true
	default:
		return false
	}
}

func BranchDefaultsToRemote(sourceType, provider string) bool {
	switch NormalizeSourceType(sourceType, provider) {
	case SourceGitPublic, SourceGitPrivate:
		return true
	default:
		return false
	}
}

type Application struct {
	ID                         uuid.UUID
	UserID                     uuid.UUID
	ServerID                   uuid.UUID
	EnvironmentID              uuid.UUID
	Name                       string
	Provider                   string
	Repo                       string
	CloneURL                   string
	SourceType                 string
	DockerfileContent          string
	BuildArgs                  map[string]string
	Branch                     string
	BuildPack                  string
	ImageRef                   string
	RegistryUsername           string
	RegistryPasswordCiphertext string
	ComposeContent             string
	ComposeFile                string
	ComposeService             string
	IsPreview                  bool
	GitHubAppID                uuid.UUID
	PrimaryDomain              *ApplicationDomain
	EnvironmentName            string
	ProjectID                  uuid.UUID
	ProjectName                string
	ServerName                 string
	PendingChanges             []string
	RuntimeHealth              *runtimewatch.Health
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// PendingChange categories match what a container reads at creation time, so
// the banner and the notification can name the same four kinds of change.
const (
	PendingChangeDomains     = "domains"
	PendingChangeEnvironment = "environment"
	PendingChangeSecrets     = "secrets"
	PendingChangeStorage     = "storage"
)

var pendingChangeOrder = map[string]int{
	PendingChangeDomains:     0,
	PendingChangeEnvironment: 1,
	PendingChangeSecrets:     2,
	PendingChangeStorage:     3,
}

// SortPendingChanges keeps the banner and notification order stable.
func SortPendingChanges(changes []string) []string {
	sorted := append([]string(nil), changes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, okLeft := pendingChangeOrder[sorted[i]]
		right, okRight := pendingChangeOrder[sorted[j]]
		if okLeft && okRight {
			return left < right
		}
		return sorted[i] < sorted[j]
	})
	return sorted
}

func PendingChangeLabel(change string) string {
	switch change {
	case PendingChangeDomains:
		return "domains or ports"
	case PendingChangeEnvironment:
		return "environment variables"
	case PendingChangeSecrets:
		return "secrets"
	case PendingChangeStorage:
		return "storage"
	default:
		return change
	}
}

func PendingChangeLabels(changes []string) string {
	labels := make([]string, 0, len(changes))
	for _, change := range SortPendingChanges(changes) {
		if change == "" {
			continue
		}
		labels = append(labels, PendingChangeLabel(change))
	}
	return strings.Join(labels, ", ")
}

const (
	ProtocolHTTP  = "http"
	ProtocolHTTPS = "https"
)

func ValidProtocol(p string) bool {
	return p == ProtocolHTTP || p == ProtocolHTTPS
}

func NormalizeProtocol(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case ProtocolHTTPS:
		return ProtocolHTTPS
	case "", ProtocolHTTP:
		return ProtocolHTTP
	default:
		return strings.TrimSpace(p)
	}
}

type ApplicationDomain struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Domain        string
	IsPrimary     bool
	Disabled      bool
	Port          int32
	Service       string
	Protocol      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Deployment struct {
	ID              uuid.UUID
	ApplicationID   uuid.UUID
	Kind            Kind
	State           State
	ImageTag        string
	RegistryImage   string
	Digest          string
	Error           string
	Attempt         int32
	ContainerID     string
	RollbackFrom    uuid.UUID
	ComposeDocument string
	ComposeCommit   string
	CommitSHA       string
	CommitMessage   string
	CommitAuthor    string
	CommittedAt     string
	BuildLog        string
	StartedAt       time.Time
	FinishedAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ApplicationDeployment struct {
	Deployment
	ApplicationName   string
	ApplicationRepo   string
	ApplicationBranch string
}

type EnvironmentRef struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
}

type EnvVar struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Key           string
	Value         string
	CreatedAt     time.Time
}

type Secret struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Key           string
	Ciphertext    string
	CreatedAt     time.Time
}

type SharedVariable struct {
	Key        string
	Value      string
	Ciphertext string
	Secret     bool
}

type CertificateIntent struct {
	ApplicationID uuid.UUID
	Domain        string
	Enabled       bool
	Challenge     string
	DNSProviderID uuid.UUID
	Wildcard      bool
}

type DNSProviderInfo struct {
	Zones   []string
	Enabled bool
}

type Storage struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Name          string
	HostPath      string
	ContainerPath string
	CreatedAt     time.Time
}
