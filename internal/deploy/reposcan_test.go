package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
)

func scanTestService(t *testing.T, source Source) *Service {
	t.Helper()
	svc := NewService(Config{
		Source: source,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func writeScanDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestDetectRepositoryMarkers(t *testing.T) {
	compose := "services:\n  web:\n    image: example.com/web:1.0\n"
	cases := []struct {
		name   string
		files  map[string]string
		pack   string
		marker string
	}{
		{"compose yaml", map[string]string{"compose.yaml": compose}, "docker_compose", "compose.yaml"},
		{"compose yml", map[string]string{"compose.yml": compose}, "docker_compose", "compose.yml"},
		{"docker compose yaml", map[string]string{"docker-compose.yaml": compose}, "docker_compose", "docker-compose.yaml"},
		{"docker compose yml", map[string]string{"docker-compose.yml": compose}, "docker_compose", "docker-compose.yml"},
		{"dockerfile", map[string]string{"Dockerfile": "FROM scratch\n"}, string(builds.EngineDockerfile), "Dockerfile"},
		{"node", map[string]string{"package.json": "{}"}, string(builds.EngineRailpack), "package.json"},
		{"go", map[string]string{"go.mod": "module demo\n"}, string(builds.EngineRailpack), "go.mod"},
		{"python requirements", map[string]string{"requirements.txt": "flask\n"}, string(builds.EngineRailpack), "requirements.txt"},
		{"python pyproject", map[string]string{"pyproject.toml": "[project]\n"}, string(builds.EngineRailpack), "pyproject.toml"},
		{"empty", map[string]string{"README.md": "hello\n"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := detectRepository(writeScanDir(t, tc.files), "")
			if result.BuildPack != tc.pack || result.MarkerFile != tc.marker {
				t.Fatalf("detect = pack %q marker %q, want %q %q",
					result.BuildPack, result.MarkerFile, tc.pack, tc.marker)
			}
		})
	}
}

func TestDetectRepositoryPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		pack   string
		marker string
	}{
		{
			"compose beats dockerfile and language",
			map[string]string{
				"package.json":       "{}",
				"Dockerfile":         "FROM scratch\n",
				"docker-compose.yml": "services:\n  web:\n    image: example.com/web:1.0\n",
			},
			"docker_compose",
			"docker-compose.yml",
		},
		{
			"dockerfile beats language",
			map[string]string{"package.json": "{}", "go.mod": "module demo\n", "Dockerfile": "FROM scratch\n"},
			string(builds.EngineDockerfile),
			"Dockerfile",
		},
		{
			"language markers keep their order",
			map[string]string{"pyproject.toml": "[project]\n", "requirements.txt": "flask\n", "go.mod": "module demo\n"},
			string(builds.EngineRailpack),
			"go.mod",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := detectRepository(writeScanDir(t, tc.files), "")
			if result.BuildPack != tc.pack || result.MarkerFile != tc.marker {
				t.Fatalf("detect = pack %q marker %q, want %q %q",
					result.BuildPack, result.MarkerFile, tc.pack, tc.marker)
			}
		})
	}
}

func TestDetectRepositoryListsComposeCandidates(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{
		"docker-compose.yml":  "services:\n  web:\n    image: example.com/web:1.0\n",
		"docker-compose.yaml": "services:\n  web:\n    image: example.com/web:1.0\n",
		"compose.yml":         "services:\n  web:\n    image: example.com/web:1.0\n",
		"compose.yaml":        "services:\n  web:\n    image: example.com/web:1.0\n",
		"deploy/compose.yaml": "services:\n  hidden:\n    image: example.com/hidden:1.0\n",
	}), "")
	want := []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
	if strings.Join(result.ComposeFiles, ",") != strings.Join(want, ",") {
		t.Fatalf("compose files = %v, want %v", result.ComposeFiles, want)
	}
	if result.ComposeFile != "compose.yaml" || result.MarkerFile != "compose.yaml" {
		t.Fatalf("chosen compose file = %q marker %q, want compose.yaml", result.ComposeFile, result.MarkerFile)
	}
}

func TestDetectRepositoryParsesComposeServices(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{
		"docker-compose.yml": "services:\n  worker:\n    image: example.com/worker:1.0\n  web:\n    image: example.com/web:1.0\n",
	}), "")
	if strings.Join(result.ComposeServices, ",") != "web,worker" {
		t.Fatalf("services = %v, want the sorted declared services", result.ComposeServices)
	}
}

func TestDetectRepositoryKeepsFreeTextForUnsupportedCompose(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: x\n    env_file: .env\n",
	}), "")
	if result.BuildPack != "docker_compose" || result.ComposeFile != "docker-compose.yml" {
		t.Fatalf("detect = %+v, want the compose pack", result)
	}
	if len(result.ComposeServices) != 0 {
		t.Fatalf("services = %v, want none for a file deploy rejects", result.ComposeServices)
	}
}

func TestDetectRepositoryListsServicesOfABuiltCompose(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{
		"docker-compose.yml": "services:\n  db:\n    image: postgres:16\n  web:\n    build: .\n",
	}), "")
	if result.BuildPack != "docker_compose" {
		t.Fatalf("build pack = %q, want the compose pack", result.BuildPack)
	}
	if strings.Join(result.ComposeServices, ",") != "db,web" {
		t.Fatalf("services = %v, want db,web", result.ComposeServices)
	}
}

func TestDetectRepositoryReadsEnvKeys(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{
		".env.example": "" +
			"# Postgres\n" +
			"DATABASE_URL=\n" +
			"SECRET_KEY=abc\n" +
			"\n" +
			"export EXPORTED_KEY=\"quoted = value\"\n" +
			"1BAD_KEY=nope\n" +
			"bad-key=nope\n" +
			"NO_EQUALS\n",
		".env": "" +
			"DATABASE_URL=postgres://hidden\n" +
			"  SPACED_KEY = value  \n" +
			"SECOND_KEY='single'\n",
		"env.txt": "ALSO_IGNORED=1\n",
	}), "")
	want := []string{"DATABASE_URL", "SPACED_KEY", "SECOND_KEY", "SECRET_KEY", "EXPORTED_KEY"}
	if strings.Join(result.EnvKeys, ",") != strings.Join(want, ",") {
		t.Fatalf("env keys = %v, want %v", result.EnvKeys, want)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal scan: %v", err)
	}
	for _, value := range []string{"abc", "postgres://hidden", "quoted", "single", "value"} {
		if strings.Contains(string(encoded), value) {
			t.Errorf("scan JSON leaks a value: %s", encoded)
		}
	}
}

func TestDetectRepositoryWithoutEnvFiles(t *testing.T) {
	result := detectRepository(writeScanDir(t, map[string]string{"README.md": "hello\n"}), "")
	if len(result.EnvKeys) != 0 {
		t.Fatalf("env keys = %v, want none", result.EnvKeys)
	}
	if result.EnvKeys == nil {
		t.Fatal("env keys = nil, want an empty list")
	}
}

func TestDetectRepositoryCapsEnvKeys(t *testing.T) {
	var content strings.Builder
	for i := 0; i < maxEnvKeys+20; i++ {
		fmt.Fprintf(&content, "KEY_%03d=value\n", i)
	}
	result := detectRepository(writeScanDir(t, map[string]string{".env": content.String()}), "")
	if len(result.EnvKeys) != maxEnvKeys {
		t.Fatalf("env keys = %d, want %d", len(result.EnvKeys), maxEnvKeys)
	}
	if result.EnvKeys[0] != "KEY_000" || result.EnvKeys[maxEnvKeys-1] != fmt.Sprintf("KEY_%03d", maxEnvKeys-1) {
		t.Fatalf("env keys = %v, want the first %d in order", result.EnvKeys[:2], maxEnvKeys)
	}
}

func TestDetectRepositoryIgnoresOversizedEnvFiles(t *testing.T) {
	padding := strings.Repeat("# pad\n", maxEnvFileBytes/6+1)
	result := detectRepository(writeScanDir(t, map[string]string{
		".env": padding + "LATE_KEY=1\n",
	}), "")
	if len(result.EnvKeys) != 0 {
		t.Fatalf("env keys = %v, want none past the size cap", result.EnvKeys)
	}
}

func TestDetectRepositoryIgnoresSymlinksAndDirectories(t *testing.T) {
	outside := writeScanDir(t, map[string]string{
		"docker-compose.yml": "services:\n  stolen:\n    image: example.com/stolen:1.0\n",
		".env":               "STOLEN_KEY=1\n",
	})
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "docker-compose.yml"), filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "docker-compose.yml"), filepath.Join(dir, "Dockerfile")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "docker-compose.yml"), filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "package.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	result := detectRepository(dir, "")
	if result.BuildPack != "" || result.MarkerFile != "" {
		t.Fatalf("detect = %+v, want nothing for symlinks and directories", result)
	}
	if len(result.ComposeFiles) != 0 || len(result.ComposeServices) != 0 {
		t.Fatalf("compose = %v %v, want nothing read outside the root", result.ComposeFiles, result.ComposeServices)
	}
	if len(result.EnvKeys) != 0 {
		t.Fatalf("env keys = %v, want nothing read outside the root", result.EnvKeys)
	}
}

func TestDetectRepositoryReadsOnlyTheWorkingDirectory(t *testing.T) {
	root := writeScanDir(t, map[string]string{
		"package.json":    "{}",
		"docs/Dockerfile": "FROM scratch\n",
		"apps/api/go.mod": "module api\n",
		"apps/web/.env":   "API_URL=http://localhost\n",
	})
	cases := []struct {
		name   string
		dir    string
		pack   string
		marker string
	}{
		{"root", "", string(builds.EngineRailpack), "package.json"},
		{"dockerfile folder", "docs", string(builds.EngineDockerfile), "Dockerfile"},
		{"nested folder", "apps/api", string(builds.EngineRailpack), "go.mod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := detectRepository(root, tc.dir)
			if result.Error != "" {
				t.Fatalf("scan error = %q, want none", result.Error)
			}
			if result.BuildPack != tc.pack || result.MarkerFile != tc.marker {
				t.Errorf("detect = pack %q marker %q, want %q %q",
					result.BuildPack, result.MarkerFile, tc.pack, tc.marker)
			}
		})
	}
	result := detectRepository(root, "docs")
	if len(result.EnvKeys) != 0 {
		t.Errorf("env keys = %v, want none from the repository root", result.EnvKeys)
	}
	web := detectRepository(root, "apps/web")
	if strings.Join(web.EnvKeys, ",") != "API_URL" {
		t.Errorf("env keys = %v, want the ones inside apps/web", web.EnvKeys)
	}
	if web.BuildPack != "" {
		t.Errorf("build pack = %q, want none for a folder without markers", web.BuildPack)
	}
}

func TestDetectRepositoryReportsAMissingWorkingDirectory(t *testing.T) {
	root := writeScanDir(t, map[string]string{"package.json": "{}"})
	result := detectRepository(root, "docs")
	if !strings.Contains(result.Error, "not in the repository") {
		t.Fatalf("error = %q, want the missing folder", result.Error)
	}
	if result.BuildPack != "" {
		t.Errorf("build pack = %q, want none for a missing folder", result.BuildPack)
	}
}

func TestDetectRepositoryListsFoldersForThePicker(t *testing.T) {
	root := writeScanDir(t, map[string]string{
		"README.md":       "hello\n",
		"apps/web/app.js": "console.log(1)\n",
		"apps/api/go.mod": "module api\n",
	})
	result := detectRepository(root, "")
	want := []string{"apps", "apps/api", "apps/web"}
	if strings.Join(result.Directories, ",") != strings.Join(want, ",") {
		t.Fatalf("directories = %v, want %v", result.Directories, want)
	}
}

func TestDetectRepositoryCapsAndSkipsGitFolders(t *testing.T) {
	files := map[string]string{".git/objects/info/keep": ""}
	for _, name := range []string{"a", "b", "c"} {
		files[name+"/keep"] = ""
	}
	root := writeScanDir(t, files)
	result := detectRepository(root, "")
	if strings.Join(result.Directories, ",") != "a,b,c" {
		t.Fatalf("directories = %v, want a,b,c without .git", result.Directories)
	}
}

func TestScanRepositoryRejectsAPathTraversal(t *testing.T) {
	svc := scanTestService(t, &writeFileSource{files: map[string]string{"Dockerfile": "FROM scratch\n"}})
	_, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType: SourceGitPublic,
		CloneURL:   "https://github.com/acme/demo.git",
		Branch:     "main",
		WorkingDir: "../etc",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestScanRepositoryScansTheWorkingDirectory(t *testing.T) {
	svc := scanTestService(t, &writeFileSource{files: map[string]string{
		"package.json":    "{}",
		"docs/Dockerfile": "FROM scratch\n",
	}})
	scan, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType: SourceGitPublic,
		CloneURL:   "https://github.com/acme/demo.git",
		Branch:     "main",
		WorkingDir: "docs",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Error != "" {
		t.Fatalf("scan error = %q, want none", scan.Error)
	}
	if scan.BuildPack != string(builds.EngineDockerfile) || scan.MarkerFile != "Dockerfile" {
		t.Fatalf("scan = %+v, want the Dockerfile inside docs", scan)
	}
	if strings.Join(scan.Directories, ",") != "docs" {
		t.Fatalf("directories = %v, want docs", scan.Directories)
	}
}

func TestScanRepositoryReportsCloneFailure(t *testing.T) {
	svc := scanTestService(t, &fakeSource{err: errors.New("git clone: repository not found")})
	scan, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType: SourceGitPublic,
		CloneURL:   "https://github.com/acme/demo.git",
		Branch:     "main",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(scan.Error, "repository not found") {
		t.Fatalf("error = %q, want the clone failure", scan.Error)
	}
	if scan.BuildPack != "" || len(scan.ComposeFiles) != 0 {
		t.Fatalf("scan = %+v, want an empty result", scan)
	}
}

func TestScanRepositoryRedactsTheInlineToken(t *testing.T) {
	svc := scanTestService(t, &fakeSource{err: errors.New("git clone: auth failed for tok-s3cret")})
	scan, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType:         SourceGitPrivate,
		CloneURL:           "https://git.example.com/acme/demo.git",
		Branch:             "main",
		CredentialUsername: "ada",
		CredentialToken:    "tok-s3cret",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if strings.Contains(scan.Error, "tok-s3cret") {
		t.Fatalf("error = %q, want the token redacted", scan.Error)
	}
}

func TestScanRepositoryDetectsThroughTheSource(t *testing.T) {
	svc := scanTestService(t, &writeFileSource{files: map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: example.com/web:1.0\n",
	}})
	scan, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType: SourceGitPublic,
		CloneURL:   "https://github.com/acme/demo.git",
		Branch:     "main",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.BuildPack != "docker_compose" || scan.ComposeFile != "docker-compose.yml" {
		t.Fatalf("scan = %+v, want the compose pack", scan)
	}
	if strings.Join(scan.ComposeServices, ",") != "web" {
		t.Fatalf("services = %v, want web", scan.ComposeServices)
	}
}

func TestScanRepositoryClonesALocalRepository(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	origin := seedGitFixture(t)
	svc := scanTestService(t, gitSource{})
	scan, err := svc.ScanRepository(context.Background(), uuid.New(), ScanRepositoryInput{
		SourceType: SourceGitPublic,
		CloneURL:   origin,
		Branch:     "main",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.BuildPack != string(builds.EngineDockerfile) || scan.MarkerFile != "Dockerfile" {
		t.Fatalf("scan = %+v, want the Dockerfile pack", scan)
	}
}

func TestScanApplicationRejectsNonRepositorySources(t *testing.T) {
	for _, sourceType := range []string{SourceDockerfile, SourceCompose, SourceImage, ""} {
		_, err := scanApplication(uuid.New(), ScanRepositoryInput{SourceType: sourceType, CloneURL: "https://github.com/acme/demo.git"})
		if !errors.Is(err, ErrValidation) {
			t.Errorf("source %q: err = %v, want ErrValidation", sourceType, err)
		}
	}
}

func TestScanApplicationBoundsInlineCredentials(t *testing.T) {
	cases := []struct {
		name string
		in   ScanRepositoryInput
	}{
		{
			"credential on a public source",
			ScanRepositoryInput{SourceType: SourceGitPublic, CloneURL: "https://github.com/acme/demo.git", CredentialToken: "tok"},
		},
		{
			"credential on an ssh url",
			ScanRepositoryInput{SourceType: SourceGitPrivate, CloneURL: "git@git.example.com:acme/demo.git", CredentialToken: "tok"},
		},
		{
			"username without a token",
			ScanRepositoryInput{SourceType: SourceGitPrivate, CloneURL: "https://git.example.com/acme/demo.git", CredentialUsername: "ada"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scanApplication(uuid.New(), tc.in)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestScanCredentialServesOnlyTheScanApplication(t *testing.T) {
	scanID, otherID := uuid.New(), uuid.New()
	cred := scanCredential{
		appID:    scanID,
		username: "ada",
		token:    "tok",
		next:     &staticCredResolver{username: "stored", token: "stored-token"},
	}
	username, token, err := cred.GitCredential(context.Background(), scanID)
	if err != nil || username != "ada" || token != "tok" {
		t.Fatalf("scan credential = %q %q %v, want the inline credential", username, token, err)
	}
	username, token, err = cred.GitCredential(context.Background(), otherID)
	if err != nil || username != "stored" || token != "stored-token" {
		t.Fatalf("delegated credential = %q %q %v, want the stored credential", username, token, err)
	}
}

func TestScanSourceInjectsTheInlineCredential(t *testing.T) {
	app := Application{ID: uuid.New(), SourceType: SourceGitPrivate, CloneURL: "https://git.example.com/acme/demo.git"}
	svc := &Service{Orchestrator: &Orchestrator{source: gitSource{creds: &staticCredResolver{username: "stored"}}}}
	source, ok := svc.scanSource(app, ScanRepositoryInput{CredentialUsername: "ada", CredentialToken: "tok"}).(gitSource)
	if !ok {
		t.Fatal("scanSource kept no git source")
	}
	username, token, err := source.gitCredential(context.Background(), app.ID)
	if err != nil || username != "ada" || token != "tok" {
		t.Fatalf("credential = %q %q %v, want the inline credential", username, token, err)
	}
}
