package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// maxCommitMessageLen bounds the stored commit subject: the first line of the
// message, kept short for the Deployment section of the UI.
const maxCommitMessageLen = 200

// CommitInfo is the git commit a deployment cloned: the full SHA, the subject
// (first message line, sanitised and truncated), the author name (never the
// email) and the committer date as RFC3339. Zero when the source never cloned
// or the checkout has nothing to report.
type CommitInfo struct {
	SHA       string
	Message   string
	Author    string
	Committed string
}

// sanitizeCommitText makes s safe for the text column and the deploy log:
// invalid UTF-8 is replaced (Postgres text rejects it), control characters
// are stripped (keeping tab; newline is split before this runs) and Unicode
// format characters (category Cf: bidi overrides, zero-width marks) are
// stripped so a hostile commit message cannot spoof the log line.
func sanitizeCommitText(s string) string {
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// parseCommit builds a CommitInfo from raw git output: the rev-parse SHA, the
// log subject line, the author name and the committer date. Anything
// unparseable stays empty rather than failing the deploy.
func parseCommit(sha, subject, author, date string) CommitInfo {
	var info CommitInfo
	if isCommitSHA(strings.TrimSpace(sha)) {
		info.SHA = strings.TrimSpace(sha)
	}
	if line, _, _ := strings.Cut(subject, "\n"); strings.TrimSpace(line) != "" {
		info.Message = truncateRunes(sanitizeCommitText(strings.TrimSpace(line)), maxCommitMessageLen)
	}
	info.Author = strings.TrimSpace(sanitizeCommitText(author))
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(date)); err == nil {
		info.Committed = t.Format(time.RFC3339)
	}
	return info
}

// isCommitSHA reports whether s is a full commit SHA (40 hex for SHA-1,
// 64 hex for SHA-256 repos).
func isCommitSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		isHex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		if !isHex {
			return false
		}
	}
	return true
}

// truncateRunes cuts s to at most n characters (runes, not bytes).
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// gitCmd builds an isolated git read of dir: --git-dir pins the checkout so
// a non-repo directory never resolves a parent checkout (a Dockerfile source
// must record nothing, never the control plane's own commit), the -c flags
// disable config-driven execution from a hostile clone, and the scrubbed env
// drops inherited GIT_* overrides (GIT_DIR, GIT_WORK_TREE, ...) and pagers.
func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := make([]string, 0, len(args)+5)
	full = append(full,
		"--git-dir="+filepath.Join(dir, ".git"),
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false")
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CEILING_DIRECTORIES=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
	}
	return cmd
}

// readCommit resolves the checked-out commit of a repo directory: the SHA via
// rev-parse and the subject, author name and committer date via a single log
// call. It only reads the local checkout, so no credential is ever involved.
// A failure (a fake source in tests) answers the zero value rather than
// failing the deploy; a log failure keeps the SHA, which is all the compose
// provenance needs.
func readCommit(dir string) CommitInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rawSHA, err := gitCmd(ctx, dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return CommitInfo{}
	}
	sha := strings.TrimSpace(string(rawSHA))
	if !isCommitSHA(sha) {
		return CommitInfo{}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := gitCmd(ctx, dir, "log", "-1",
		"--format=%s%x00%an%x00%cI").Output()
	if err != nil {
		return CommitInfo{SHA: sha}
	}
	subject, author, date, _ := cutCommitLog(string(out))
	return parseCommit(sha, subject, author, date)
}

// cutCommitLog splits one "%s%x00%an%x00%cI" log record on NUL bytes: %an may
// itself contain a newline, so newline-splitting would shift the date line.
func cutCommitLog(out string) (subject, author, date string, ok bool) {
	parts := strings.SplitN(strings.TrimSuffix(out, "\n"), "\x00", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// recordCommit carries the clone's commit into the deployment, persists it
// and logs one "commit <sha7> <subject>" line. A compose run reuses the
// commit resolveComposeContent already read (see cloneCompose) instead of
// forking git again. An empty checkout (a fake source) leaves the deployment
// untouched. The metadata is best effort: a persist failure is logged and
// the deploy continues, because later steps persist the same row again.
func (o *Orchestrator) recordCommit(ctx context.Context, st *runState) error {
	info := st.commitInfo
	if !st.commitDone {
		info = readCommit(st.repoDir)
	}
	if info.SHA == "" {
		return nil
	}
	st.dep.CommitSHA = info.SHA
	st.dep.CommitMessage = info.Message
	st.dep.CommitAuthor = info.Author
	st.dep.CommittedAt = info.Committed
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		o.logger.Warn("deploy: commit metadata not persisted; continuing without it",
			"deployment_id", st.dep.ID, "error", err)
	}
	line := "commit " + info.SHA[:7]
	if info.Message != "" {
		line += " " + info.Message
	}
	st.log(line)
	return nil
}
