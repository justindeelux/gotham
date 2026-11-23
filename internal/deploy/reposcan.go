package deploy

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	// WorkingDir is the folder the user picked inside the repository; empty
	// scans the root.
	WorkingDir string
}

// RepositoryScan reports what the repository root implies for the build pack
// and the env step. A failed scan keeps BuildPack, EnvKeys and EnvDefaults
// empty and explains itself in Error; the wizard treats all of them as a hint,
// never as a blocker. EnvKeys carries variable names only; EnvDefaults adds the
// values the documented template files ship for those names.
type RepositoryScan struct {
	BuildPack       string
	MarkerFile      string
	ComposeFiles    []string
	ComposeFile     string
	ComposeServices []string
	EnvKeys         []string
	EnvDefaults     []EnvDefault
	Directories     []string
	Error           string
}

// EnvDefault is one value a template .env file gives a scanned key.
type EnvDefault struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	File  string `json:"file"`
}

const repositoryScanTimeout = 60 * time.Second

const scanFailureWorkspace = "the scan workspace could not be created"

const (
	// maxEnvFileBytes, maxEnvKeys and maxEnvValueBytes bound what a committed
	// .env* file can add to one scan response.
	maxEnvFileBytes  = 256 << 10
	maxEnvKeys       = 100
	maxEnvValueBytes = 1024
	// maxScanDirectories bounds the folder picker: the first folders in
	// lexical order are enough to find a monorepo package.
	maxScanDirectories = 200
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envDefaultFiles are the documented templates a repository ships for its
// variables. Their values are placeholders meant to be copied, so they are the
// only .env* files whose values leave the scan.
var envDefaultFiles = []string{".env.example", ".env.sample", ".env.template", ".env.dist"}

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
	workingDir, err := normalizeWorkingDir(in.WorkingDir)
	if err != nil {
		return RepositoryScan{}, err
	}
	return detectRepository(dir, workingDir), nil
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
	return RepositoryScan{
		ComposeFiles:    []string{},
		ComposeServices: []string{},
		EnvKeys:         []string{},
		EnvDefaults:     []EnvDefault{},
		Directories:     []string{},
		Error:           message,
	}
}

func scanErrorMessage(err error, secret string) string {
	message := redactCloneError(err.Error())
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "***")
	}
	return tail(message, 300)
}

// detectRepository is the one place that decides a repository's build pack.
// workingDir narrows the markers to a monorepo folder, exactly like the build
// context the deploy uses. Precedence, first match wins: a compose file
// (compose.yaml, compose.yml, docker-compose.yaml, docker-compose.yml) selects
// Docker Compose; then Dockerfile selects Dockerfile; then a language marker
// selects Railpack with package.json for Node, go.mod for Go, and
// requirements.txt or pyproject.toml for Python. Railpack is the existing
// language auto-detection pack, so the scan never names a pack id the platform
// cannot build.
func detectRepository(root, workingDir string) RepositoryScan {
	result := RepositoryScan{
		ComposeFiles:    []string{},
		ComposeServices: []string{},
		EnvKeys:         []string{},
		EnvDefaults:     []EnvDefault{},
		Directories:     repoDirectories(root),
	}
	dir := filepath.Join(root, workingDir)
	if !isDirectory(dir) {
		result.Error = fmt.Sprintf("working directory %q is not in the repository", workingDir)
		return result
	}
	result.EnvKeys, result.EnvDefaults = envScanFromDir(dir)
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

// repoDirectories lists the folders the picker offers as relative slash paths,
// skipping .git, hidden folders (.github, .opencode, ...) and dependency
// folders (node_modules, vendor), and stops at maxScanDirectories. A hidden
// folder can still be typed as the working directory. Symlinks are not
// followed.
func repoDirectories(root string) []string {
	dirs := []string{}
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return nil
		}
		if skipScanDirectory(entry.Name()) {
			return fs.SkipDir
		}
		rel, relErr := filepath.Rel(root, current)
		if relErr != nil || rel == "." {
			return nil
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		if len(dirs) >= maxScanDirectories {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return []string{}
	}
	return dirs
}

// skipScanDirectory keeps git metadata, tooling folders and installed
// dependencies out of the picker; they are never an app.
func skipScanDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor"
}

func isDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// isRegularFile ignores symlinks, so a checked-out file cannot point the scan
// at anything outside the repository root.
func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// envScanFromDir lists the variable names every .env* file in dir declares and
// the defaults envDefaultFiles give them. Names are collected in the lexical
// file order the previous scan used, so callers see the same key list. Values
// come from the documented templates only: a committed plain .env or
// .env.local may hold real secrets, so only names come out of those. When two
// template files set a key, the first in envDefaultFiles order wins, and a
// blank value never becomes a default. Symlinks are skipped like every other
// root marker.
func envScanFromDir(dir string) ([]string, []EnvDefault) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}, []EnvDefault{}
	}
	keys := []string{}
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if len(keys) >= maxEnvKeys {
			break
		}
		if !strings.HasPrefix(entry.Name(), ".env") || !entry.Type().IsRegular() {
			continue
		}
		for _, item := range readEnvFile(filepath.Join(dir, entry.Name())) {
			if _, ok := seen[item.key]; ok {
				continue
			}
			seen[item.key] = struct{}{}
			keys = append(keys, item.key)
			if len(keys) >= maxEnvKeys {
				break
			}
		}
	}
	defaults := []EnvDefault{}
	withDefault := make(map[string]struct{})
	for _, name := range envDefaultFiles {
		if !isRegularFile(filepath.Join(dir, name)) {
			continue
		}
		for _, entry := range readEnvFile(filepath.Join(dir, name)) {
			if entry.value == "" {
				continue
			}
			if _, ok := seen[entry.key]; !ok {
				continue
			}
			if _, ok := withDefault[entry.key]; ok {
				continue
			}
			withDefault[entry.key] = struct{}{}
			defaults = append(defaults, EnvDefault{Key: entry.key, Value: entry.value, File: name})
		}
	}
	return keys, defaults
}

type envEntry struct {
	key   string
	value string
}

func readEnvFile(path string) []envEntry {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxEnvFileBytes))
	if err != nil {
		return nil
	}
	entries := []envEntry{}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := envLine(line); ok {
			entries = append(entries, envEntry{key: key, value: value})
		}
	}
	return entries
}

// envLine reads one KEY=value line, with or without the export prefix. The
// caller decides whether the value may leave the process.
func envLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if !envKeyPattern.MatchString(key) {
		return "", "", false
	}
	return key, envLineValue(value), true
}

// envValue strips the quotes or the trailing comment around a value.
// shortcut: escapes in a double-quoted value stay literal, enough for the
// placeholders templates ship.
func envLineValue(raw string) string {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return ""
	case value[0] == '"' || value[0] == '\'':
		end := strings.IndexByte(value[1:], value[0])
		if end < 0 {
			return ""
		}
		value = value[1 : end+1]
	default:
		if comment := strings.Index(value, " #"); comment >= 0 {
			value = strings.TrimSpace(value[:comment])
		}
	}
	if len(value) > maxEnvValueBytes {
		return ""
	}
	return value
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
