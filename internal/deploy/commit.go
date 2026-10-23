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

const maxCommitMessageLen = 200

type CommitInfo struct {
	SHA       string
	Message   string
	Author    string
	Committed string
}

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

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

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

func cutCommitLog(out string) (subject, author, date string, ok bool) {
	parts := strings.SplitN(strings.TrimSuffix(out, "\n"), "\x00", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

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
