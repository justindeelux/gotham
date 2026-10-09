package deploy

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseCommit(t *testing.T) {
	sha := strings.Repeat("ab12", 10)
	got := parseCommit("  "+sha+"\n", "Add widgets\n", "Ada Lovelace", "2026-10-01T12:34:56Z")
	if got.SHA != sha || got.Message != "Add widgets" || got.Author != "Ada Lovelace" ||
		got.Committed != "2026-10-01T12:34:56Z" {
		t.Errorf("parseCommit = %+v, want the four fields", got)
	}
}

func TestParseCommitTruncatesSubjectTo200(t *testing.T) {
	subject := strings.Repeat("é", 250)
	got := parseCommit(strings.Repeat("a", 40), subject, "Ada", "2026-10-01T12:34:56+07:00")
	if n := len([]rune(got.Message)); n != maxCommitMessageLen {
		t.Errorf("subject runes = %d, want %d", n, maxCommitMessageLen)
	}
	if got.Committed != "2026-10-01T12:34:56+07:00" {
		t.Errorf("committed = %q, want the RFC3339 date kept", got.Committed)
	}
}

func TestParseCommitStripsControlChars(t *testing.T) {
	got := parseCommit(strings.Repeat("a", 40), "Fix\x00 it\x1b[2J", "A\x07da", "2026-10-01T12:34:56Z")
	if strings.ContainsAny(got.Message, "\x00\x1b") || strings.Contains(got.Author, "\x07") {
		t.Errorf("control chars survive: %+v", got)
	}
	if !strings.Contains(got.Message, "Fix") || got.Author != "Ada" {
		t.Errorf("over-sanitised: %+v", got)
	}
}

func TestParseCommitEmpty(t *testing.T) {
	if got := parseCommit("", "", "", ""); got != (CommitInfo{}) {
		t.Errorf("parseCommit(empty) = %+v, want zero", got)
	}
	if got := parseCommit("not-a-sha", "subject", "author", "not-a-date"); got.SHA != "" || got.Committed != "" ||
		got.Message != "subject" || got.Author != "author" {
		t.Errorf("invalid sha/date must stay empty: %+v", got)
	}
}

// initGitRepo builds a one-commit repository for the readCommit tests.
func initGitRepo(t *testing.T, message string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Ada Lovelace",
			"GIT_AUTHOR_EMAIL=ada@example.com",
			"GIT_AUTHOR_DATE=2026-10-01T12:34:56Z",
			"GIT_COMMITTER_NAME=Ada Lovelace",
			"GIT_COMMITTER_EMAIL=ada@example.com",
			"GIT_COMMITTER_DATE=2026-10-01T12:34:56Z",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(dir+"/README.md", []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-qm", message)
	return dir
}

func TestReadCommitEmptyDir(t *testing.T) {
	if got := readCommit(t.TempDir()); got != (CommitInfo{}) {
		t.Errorf("readCommit(empty) = %+v, want zero", got)
	}
}

func TestReadCommitRepo(t *testing.T) {
	dir := initGitRepo(t, "Add widgets")
	got := readCommit(dir)
	if !isCommitSHA(got.SHA) {
		t.Errorf("sha = %q, want 40 hex", got.SHA)
	}
	if got.Message != "Add widgets" {
		t.Errorf("message = %q, want the subject", got.Message)
	}
	if got.Author != "Ada Lovelace" {
		t.Errorf("author = %q, want the name without the email", got.Author)
	}
	if _, err := time.Parse(time.RFC3339, got.Committed); err != nil {
		t.Errorf("committed = %q, want RFC3339: %v", got.Committed, err)
	}
}

func TestRecordCommitPersistsAndLogs(t *testing.T) {
	dir := initGitRepo(t, "Add widgets")
	repo := &fakeRepository{}
	app := testApplication(uuid.New())
	repo.app = app
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateCloning})
	o := newTestOrchestrator(Config{Repository: repo})
	var logs []string
	st := &runState{app: app, dep: dep, repoDir: dir, log: func(s string) { logs = append(logs, s) }}
	if err := o.recordCommit(context.Background(), st); err != nil {
		t.Fatalf("recordCommit: %v", err)
	}
	stored, err := repo.GetDeployment(context.Background(), app.ID, dep.ID)
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if stored.CommitSHA != st.dep.CommitSHA || !isCommitSHA(stored.CommitSHA) {
		t.Errorf("stored sha = %q, want the clone commit", stored.CommitSHA)
	}
	if stored.CommitMessage != "Add widgets" || stored.CommitAuthor != "Ada Lovelace" || stored.CommittedAt == "" {
		t.Errorf("stored commit = %+v, want the clone metadata", stored)
	}
	if len(logs) != 1 || !strings.HasPrefix(logs[0], "commit "+stored.CommitSHA[:7]+" Add widgets") {
		t.Errorf("logs = %q, want one commit line", logs)
	}
}

func TestRecordCommitEmptyDirIsNoop(t *testing.T) {
	repo := &fakeRepository{}
	app := testApplication(uuid.New())
	repo.app = app
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateCloning})
	o := newTestOrchestrator(Config{Repository: repo})
	logged := false
	st := &runState{app: app, dep: dep, repoDir: t.TempDir(), log: func(string) { logged = true }}
	if err := o.recordCommit(context.Background(), st); err != nil {
		t.Fatalf("recordCommit: %v", err)
	}
	if logged || st.dep.CommitSHA != "" {
		t.Error("empty checkout must leave the deployment untouched and log nothing")
	}
}

func TestRollbackKeepsCommit(t *testing.T) {
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	user := uuid.New()
	app := testApplication(user)
	repo.app = app
	target := seedDeployment(t, repo, app, Deployment{
		Kind: KindDeploy, State: StateRunning, ImageTag: "app:1",
		CommitSHA: strings.Repeat("b", 40), CommitMessage: "Add widgets",
		CommitAuthor: "Ada Lovelace", CommittedAt: "2026-10-01T12:34:56Z",
	})
	rolled, err := svc.Rollback(context.Background(), user, app.ID, target.ID)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolled.CommitSHA != target.CommitSHA || rolled.CommitMessage != target.CommitMessage ||
		rolled.CommitAuthor != target.CommitAuthor || rolled.CommittedAt != target.CommittedAt {
		t.Errorf("rollback commit = %+v, want the target's %+v",
			rolled, target)
	}
}

func TestDeploymentResponseAlwaysHasCommitFields(t *testing.T) {
	raw, err := json.Marshal(newDeploymentResponse(Deployment{}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"commit_sha", "commit_message", "commit_author", "committed_at"} {
		value, ok := body[key]
		if !ok {
			t.Errorf("response lacks %q: %s", key, raw)
		} else if value != "" {
			t.Errorf("%s = %v, want empty string", key, value)
		}
	}
}
