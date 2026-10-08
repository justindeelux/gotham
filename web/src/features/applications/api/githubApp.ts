import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";

/**
 * Typed client for the GitHub App routes served by `internal/githubapp`:
 *
 *   POST   /providers/github-app/manifest
 *   POST   /providers/github-app/callback
 *   GET    /providers/github-app
 *   GET    /providers/github-app/{id}/install
 *   POST   /providers/github-app/{id}/installations
 *   GET    /providers/github-app/{id}/repos
 *   GET    /providers/github-app/{id}/repos/{owner}/{repo}/branches
 *   DELETE /providers/github-app/{id}
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so
 * auth and refresh-on-401 come from `./http` unchanged. Secrets never appear
 * in these shapes: the API seals the private key and webhook secret.
 */

/** A connected GitHub App, without credentials. */
export interface GitHubApp {
  id: string;
  app_id: number;
  slug: string;
  name: string;
  base_url: string;
  connected: boolean;
  /** When the connection was stored (GS-10 lists the connection age). */
  created_at: string;
  installations: GitHubInstallation[];
}

/** One installation of a GitHub App. */
export interface GitHubInstallation {
  id: string;
  installation_id: number;
  account: string;
}

/** A repository visible through an installation. */
export interface GitHubRepo {
  id: string;
  name: string;
  full_name: string;
  private: boolean;
  default_branch: string;
  clone_url: string;
  ssh_url: string;
  html_url: string;
}

/** A branch of a repository. */
export interface GitHubBranch {
  name: string;
  commit: string;
  protected: boolean;
}

/** The manifest form the browser posts to the Git host. */
export interface GitHubManifest {
  action_url: string;
  manifest: Record<string, unknown>;
  state: string;
}

/** The installation step: where to send the user plus its state. */
export interface GitHubInstall {
  install_url: string;
  state: string;
}

interface AppListEnvelope {
  apps: GitHubApp[];
}

interface RepoListEnvelope {
  repos: GitHubRepo[];
  truncated: boolean;
}

interface BranchListEnvelope {
  branches: GitHubBranch[];
}

interface DisconnectEnvelope {
  deleted: boolean;
  applications_using: number;
}

/** listGitHubApps returns every GitHub App of the current user. */
export async function listGitHubApps(): Promise<GitHubApp[]> {
  const response = await http.get<AppListEnvelope>("/providers/github-app");
  return response.data.apps ?? [];
}

/** startManifest serves the manifest form the browser posts to the Git host. */
export async function startManifest(baseUrl: string, name: string): Promise<GitHubManifest> {
  const response = await http.post<GitHubManifest>("/providers/github-app/manifest", {
    base_url: baseUrl,
    name,
  });
  return response.data;
}

/** finishCallback exchanges the manifest code for stored app credentials. */
export async function finishCallback(code: string, state: string): Promise<GitHubApp> {
  const response = await http.post<GitHubApp>("/providers/github-app/callback", {
    code,
    state,
  });
  return response.data;
}

/** installUrl starts the installation step for one app. */
export async function installUrl(appId: string): Promise<GitHubInstall> {
  const response = await http.get<GitHubInstall>(`/providers/github-app/${appId}/install`);
  return response.data;
}

/** installStateApp resolves a pending install state to its app id. */
export async function installStateApp(state: string): Promise<string> {
  const response = await http.get<{ app_id: string }>(
    `/providers/github-app/install-state?state=${encodeURIComponent(state)}`,
  );
  return response.data.app_id;
}

/** recordInstallation stores the installation_id callback. */
export async function recordInstallation(
  appId: string,
  installationId: number,
  state: string,
): Promise<GitHubInstallation> {
  const response = await http.post<GitHubInstallation>(
    `/providers/github-app/${appId}/installations`,
    { installation_id: installationId, state },
  );
  return response.data;
}

/** A repository list with its truncation flag. */
export interface GitHubRepoList {
  repos: GitHubRepo[];
  truncated: boolean;
}

/** listGitHubRepos returns the repositories of an app installation. */
export async function listGitHubRepos(appId: string): Promise<GitHubRepoList> {
  const response = await http.get<RepoListEnvelope>(`/providers/github-app/${appId}/repos`);
  return { repos: response.data.repos ?? [], truncated: response.data.truncated ?? false };
}

/** listGitHubBranches returns the branches of a repository. */
export async function listGitHubBranches(appId: string, repo: string): Promise<GitHubBranch[]> {
  const segments = repo.split("/");
  if (segments.length !== 2 || segments[0].trim() === "" || segments[1].trim() === "") {
    throw new Error("repository must be owner/name");
  }
  const [owner, name] = segments.map((segment) => encodeURIComponent(segment.trim()));
  const response = await http.get<BranchListEnvelope>(
    `/providers/github-app/${appId}/repos/${owner}/${name}/branches`,
  );
  return response.data.branches ?? [];
}

/** disconnectGitHubApp deletes the stored credentials. */
export async function disconnectGitHubApp(appId: string): Promise<DisconnectEnvelope> {
  const response = await http.delete<DisconnectEnvelope>(`/providers/github-app/${appId}`);
  return response.data;
}

/** describeGitHubAppError maps a thrown error to a user-facing message. */
export function describeGitHubAppError(error: unknown): string {
  const t = i18n.global.t.bind(i18n.global);
  if (isApiError(error)) {
    const raw = stripErrorPrefix(error.message);
    return raw
      ? `${String(t("common.errors.requestFailed"))} ${raw}`
      : String(t("common.errors.requestFailed"));
  }
  if (error instanceof Error) {
    const raw = stripErrorPrefix(error.message);
    return raw
      ? `${String(t("common.errors.unexpected"))} ${raw}`
      : String(t("common.errors.unexpected"));
  }
  return String(t("common.errors.unexpected"));
}