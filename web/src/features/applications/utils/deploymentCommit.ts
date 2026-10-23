const SHA_RE = /^[0-9a-f]{7,64}$/i;

const PLAIN_REPO_RE = /^([\w.-]+\/[\w.-]+?)(?:\.git)?$/;

export function commitSubject(message: string | undefined | null): string {
  return (message ?? "").split("\n")[0].trim();
}

export function shortCommitSha(sha: string | undefined | null): string {
  return (sha ?? "").slice(0, 7);
}

export function hasCommitSha(sha: string | undefined | null): boolean {
  return Boolean(sha?.trim());
}

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

export function repoPath(repo: string | undefined | null, cloneUrl: string | undefined | null): string {
  if ((cloneUrl ?? "").trim()) {
    return githubRepoFromClone(cloneUrl ?? "");
  }
  const plain = PLAIN_REPO_RE.exec((repo ?? "").trim());
  return plain ? plain[1] : "";
}

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
