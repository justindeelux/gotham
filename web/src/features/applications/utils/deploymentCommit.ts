/** commitSubject renders the first line of a commit message. */
export function commitSubject(message: string | undefined | null): string {
  return (message ?? "").split("\n")[0].trim();
}

/** shortCommitSha renders the 7-char head of a commit hash. */
export function shortCommitSha(sha: string | undefined | null): string {
  return (sha ?? "").slice(0, 7);
}

/** hasCommitSha reports whether a deployment carries commit metadata. */
export function hasCommitSha(sha: string | undefined | null): boolean {
  return Boolean(sha);
}

/** repoPath extracts owner/name for github.com remotes, else "". */
export function repoPath(repo: string | undefined | null, cloneUrl: string | undefined | null): string {
  const fromClone = /github\.com[/:]([\w.-]+\/[\w.-]+?)(?:\.git)?\/?$/i.exec(
    (cloneUrl ?? "").trim(),
  );
  if (fromClone) {
    return fromClone[1];
  }
  const plain = /^([\w.-]+\/[\w.-]+?)(?:\.git)?$/.exec((repo ?? "").trim());
  return plain ? plain[1] : "";
}

/** commitUrl links a commit on github.com when the repo is known, else "". */
export function commitUrl(
  repo: string | undefined | null,
  cloneUrl: string | undefined | null,
  sha: string | undefined | null,
): string {
  if (!sha) {
    return "";
  }
  const path = repoPath(repo, cloneUrl);
  return path ? `https://github.com/${path}/commit/${sha}` : "";
}
