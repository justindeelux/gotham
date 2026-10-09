/** SHA_RE accepts a full or abbreviated hex commit hash. */
const SHA_RE = /^[0-9a-f]{7,64}$/i;

/** PLAIN_REPO_RE accepts a bare owner/name reference. */
const PLAIN_REPO_RE = /^([\w.-]+\/[\w.-]+?)(?:\.git)?$/;

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
  return Boolean(sha?.trim());
}

/**
 * githubRepoFromClone extracts owner/name from a github.com remote, else "".
 * The host must be exactly github.com (optional www. stripped, userinfo
 * ignored); lookalikes such as notgithub.com or github.com.evil.com never
 * match, and neither do paths that merely contain "github.com".
 */
function githubRepoFromClone(cloneUrl: string): string {
  const raw = cloneUrl.trim();
  if (!raw) {
    return "";
  }
  const scp = /^(?:[^@/:\s]+@)?(?:www\.)?github\.com[/:]([\w.-]+\/[\w.-]+?)(?:\.git)?\/?$/i.exec(raw);
  if (scp) {
    return scp[1];
  }
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return "";
  }
  if (url.hostname.toLowerCase().replace(/^www\./, "") !== "github.com") {
    return "";
  }
  const segments = url.pathname.split("/").filter(Boolean);
  if (segments.length !== 2) {
    return "";
  }
  const plain = PLAIN_REPO_RE.exec(`${segments[0]}/${segments[1].replace(/\.git$/, "")}`);
  return plain ? plain[1] : "";
}

/**
 * repoPath extracts owner/name for github.com remotes, else "". The plain
 * repo fallback applies only when clone_url is empty; a non-empty non-GitHub
 * remote (GitLab etc.) never yields a github.com path.
 */
export function repoPath(repo: string | undefined | null, cloneUrl: string | undefined | null): string {
  if ((cloneUrl ?? "").trim()) {
    return githubRepoFromClone(cloneUrl ?? "");
  }
  const plain = PLAIN_REPO_RE.exec((repo ?? "").trim());
  return plain ? plain[1] : "";
}

/** commitUrl links a commit on github.com when the repo is known, else "". */
export function commitUrl(
  repo: string | undefined | null,
  cloneUrl: string | undefined | null,
  sha: string | undefined | null,
): string {
  const hash = (sha ?? "").trim();
  if (!SHA_RE.test(hash)) {
    return "";
  }
  const path = repoPath(repo, cloneUrl);
  return path ? `https://github.com/${path}/commit/${hash}` : "";
}
