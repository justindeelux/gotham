package deploy

import (
	"context"
	"os/exec"
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
// email) and the author date as RFC3339. Zero when the source never cloned or
// the checkout has nothing to report.
type CommitInfo struct {
	SHA       string
	Message   string
	Author    string
	Committed string
}

// sanitizeCommitText strips control characters (keeping tab, newline is split
// before this runs) so a hostile commit message cannot smuggle formatting
// into the deploy log or the API.
func sanitizeCommitText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// parseCommit builds a CommitInfo from raw git output: the rev-parse SHA, the
// log subject line, the author name and the author date. Anything unparseable
// stays empty rather than failing the deploy.
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

// isCommitSHA reports whether s is a full 40-hex commit SHA.
func isCommitSHA(s string) bool {
	if len(s) != 40 {
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

// readCommit resolves the checked-out commit of a repo directory: the SHA via
// rev-parse and the subject, author name and date via a single log call. It
// only reads the local checkout, so no credential is ever involved. A failure
// (a fake source in tests) answers the zero value rather than failing the
// deploy.
func readCommit(dir string) CommitInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sha, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return CommitInfo{}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1",
		"--format=%s%n%an%n%aI").Output()
	if err != nil {
		return CommitInfo{}
	}
	subject, author, date, _ := cutCommitLog(string(out))
	return parseCommit(string(sha), subject, author, date)
}

// cutCommitLog splits one "%s%n%an%n%aI" log record into its three lines.
func cutCommitLog(out string) (subject, author, date string, ok bool) {
	lines := strings.SplitN(strings.TrimSuffix(out, "\n"), "\n", 3)
	if len(lines) != 3 {
		return "", "", "", false
	}
	return lines[0], lines[1], lines[2], true
}

// recordCommit reads the commit of a successful clone into the deployment,
// persists it and logs one "commit <sha7> <subject>" line. An empty checkout
// (a fake source) leaves the deployment untouched.
func (o *Orchestrator) recordCommit(ctx context.Context, st *runState) error {
	info := readCommit(st.repoDir)
	if info.SHA == "" {
		return nil
	}
	st.dep.CommitSHA = info.SHA
	st.dep.CommitMessage = info.Message
	st.dep.CommitAuthor = info.Author
	st.dep.CommittedAt = info.Committed
	if _, err := o.repo.UpdateDeployment(ctx, st.dep); err != nil {
		return err
	}
	line := "commit " + info.SHA[:7]
	if info.Message != "" {
		line += " " + info.Message
	}
	st.log(line)
	return nil
}
