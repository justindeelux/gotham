package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/services"
)

// ScanRepositoryInput is what the create wizard knows before the application
// exists: the source it would submit, plus the inline HTTPS credential it holds
// until the application is created.
type ScanRepositoryInput struct {
	SourceType         string
	Provider           string
	Repo               string
	CloneURL           string
	Branch             string
	GitHubAppID        uuid.UUID
	CredentialUsername string
	CredentialToken    string
}

// RepositoryScan reports what the repository root implies for the build pack.
// A failed scan keeps BuildPack empty and explains itself in Error; the wizard
// treats both as a hint, never as a blocker.
type RepositoryScan struct {
	BuildPack       string
	MarkerFile      string
	ComposeFiles    []string
	ComposeFile     string
	ComposeServices []string
	Error           string
}

const repositoryScanTimeout = 60 * time.Second

const scanFailureWorkspace = "the scan workspace could not be created"

var composeCandidateFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

var languageMarkerFiles = []struct {
	file string
	pack builds.EngineKind
}{
	{"package.json", builds.EngineRailpack},
	{"go.mod", builds.EngineRailpack},
	{"requirements.txt", builds.EngineRailpack},
	{"pyproject.toml", builds.EngineRailpack},
}

// ScanRepository inspects the repository root without an application: the
// wizard has no app row yet, so the clone uses the inline credential when it
// supplied one and the linked GitHub App installation token otherwise. The
// clone is the normal shallow clone bounded by a timeout, and the workspace is
// always removed.
func (s *Service) ScanRepository(ctx context.Context, userID uuid.UUID, in ScanRepositoryInput) (RepositoryScan, error) {
	if !Enabled() {
		return RepositoryScan{}, ErrDisabled
	}
	app, err := scanApplication(userID, in)
	if err != nil {
		return RepositoryScan{}, err
	}
	if s == nil || s.Orchestrator == nil || s.source == nil {
		return scanFailure("repository scanning is not configured"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, repositoryScanTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "gotham-repo-scan-*")
	if err != nil {
		return scanFailure(scanFailureWorkspace), nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := s.scanSource(app, in).Clone(ctx, app, dir, nil); err != nil {
		return scanFailure(scanErrorMessage(err, strings.TrimSpace(in.CredentialToken))), nil
	}
	return detectRepository(dir), nil
}

func scanApplication(userID uuid.UUID, in ScanRepositoryInput) (Application, error) {
	app := Application{
		// The scan runs before the application exists; the id keys the
		// credential lookup and nothing is stored under it.
		ID:          uuid.New(),
		UserID:      userID,
		SourceType:  strings.TrimSpace(in.SourceType),
		Provider:    strings.TrimSpace(in.Provider),
		Repo:        strings.TrimSpace(in.Repo),
		CloneURL:    strings.TrimSpace(in.CloneURL),
		Branch:      strings.TrimSpace(in.Branch),
		GitHubAppID: in.GitHubAppID,
	}
	switch app.SourceType {
	case SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp:
	default:
		return Application{}, fmt.Errorf("%w: a repository scan needs a git source, not %q", ErrValidation, app.SourceType)
	}
	if err := validateSourceFields(app); err != nil {
		return Application{}, err
	}
	if err := validateScanCredential(app, in); err != nil {
		return Application{}, err
	}
	return app, nil
}

func validateScanCredential(app Application, in ScanRepositoryInput) error {
	username := strings.TrimSpace(in.CredentialUsername)
	token := strings.TrimSpace(in.CredentialToken)
	if username == "" && token == "" {
		return nil
	}
	if app.SourceType != SourceGitPrivate {
		return fmt.Errorf("%w: a scanned credential is only accepted for %q sources", ErrValidation, SourceGitPrivate)
	}
	if !isHTTPSURL(app.CloneURL) {
		return fmt.Errorf("%w: token authentication needs an https clone URL", ErrValidation)
	}
	if token == "" {
		return fmt.Errorf("%w: credential token is required", ErrValidation)
	}
	if err := checkTextBytes(username, "credential username"); err != nil {
		return err
	}
	if err := checkTextBytes(token, "credential token"); err != nil {
		return err
	}
	if len(token) > maxGitTokenLength {
		return fmt.Errorf("%w: credential token is too long", ErrValidation)
	}
	if len(username) > 255 {
		return fmt.Errorf("%w: credential username is too long", ErrValidation)
	}
	return nil
}

// scanSource swaps the stored HTTPS credential for the one the wizard holds.
// Deploy keys and provider tokens keep coming from the same resolvers the
// deploy uses, so the scan cannot reach a host the app itself could not.
func (s *Service) scanSource(app Application, in ScanRepositoryInput) Source {
	git, ok := s.source.(gitSource)
	if !ok {
		return s.source
	}
	token := strings.TrimSpace(in.CredentialToken)
	if token == "" {
		return git
	}
	git.creds = scanCredential{
		appID:    app.ID,
		username: strings.TrimSpace(in.CredentialUsername),
		token:    token,
		next:     git.creds,
	}
	return git
}

type scanCredential struct {
	appID    uuid.UUID
	username string
	token    string
	next     gitCredentialResolver
}

func (c scanCredential) GitCredential(ctx context.Context, appID uuid.UUID) (string, string, error) {
	if appID == c.appID {
		return c.username, c.token, nil
	}
	if c.next == nil {
		return "", "", nil
	}
	return c.next.GitCredential(ctx, appID)
}

func scanFailure(message string) RepositoryScan {
	return RepositoryScan{ComposeFiles: []string{}, ComposeServices: []string{}, Error: message}
}

func scanErrorMessage(err error, secret string) string {
	message := redactCloneError(err.Error())
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "***")
	}
	return tail(message, 300)
}

// detectRepository is the one place that decides a repository's build pack
// from its root. Precedence, first match wins: a compose file (compose.yaml,
// compose.yml, docker-compose.yaml, docker-compose.yml) selects Docker Compose;
// then Dockerfile selects Dockerfile; then a language marker selects Railpack
// with package.json for Node, go.mod for Go, and requirements.txt or
// pyproject.toml for Python. Railpack is the existing language auto-detection
// pack, so the scan never names a pack id the platform cannot build.
func detectRepository(dir string) RepositoryScan {
	result := RepositoryScan{ComposeFiles: []string{}, ComposeServices: []string{}}
	for _, name := range composeCandidateFiles {
		if isRegularFile(filepath.Join(dir, name)) {
			result.ComposeFiles = append(result.ComposeFiles, name)
		}
	}
	if len(result.ComposeFiles) > 0 {
		result.BuildPack = string(builds.EngineCompose)
		result.MarkerFile = result.ComposeFiles[0]
		result.ComposeFile = result.ComposeFiles[0]
		if content, err := readComposeFile(dir, result.ComposeFile); err == nil {
			result.ComposeServices = composeServiceNames(content)
		}
		return result
	}
	if isRegularFile(filepath.Join(dir, "Dockerfile")) {
		result.BuildPack = string(builds.EngineDockerfile)
		result.MarkerFile = "Dockerfile"
		return result
	}
	for _, marker := range languageMarkerFiles {
		if isRegularFile(filepath.Join(dir, marker.file)) {
			result.BuildPack = string(marker.pack)
			result.MarkerFile = marker.file
			break
		}
	}
	return result
}

// isRegularFile ignores symlinks, so a checked-out file cannot point the scan
// at anything outside the repository root.
func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// composeServiceNames lists services through the same parser the deploy uses,
// so the wizard only offers names that deploy accepts. A file deploy would
// reject reports no names and the wizard keeps its free-text field.
func composeServiceNames(content string) []string {
	spec, err := services.Parse(content)
	if err != nil || len(spec.Services) == 0 {
		return []string{}
	}
	return spec.Services
}
