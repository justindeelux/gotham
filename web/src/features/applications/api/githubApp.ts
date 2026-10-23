import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";

export interface GitHubApp {
  id: string;
  app_id: number;
  slug: string;
  name: string;
  base_url: string;
  connected: boolean;
  created_at: string;
  installations: GitHubInstallation[];
}

export interface GitHubInstallation {
  id: string;
  installation_id: number;
  account: string;
}

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

export interface GitHubBranch {
  name: string;
  commit: string;
  protected: boolean;
}

export interface GitHubManifest {
  action_url: string;
  manifest: Record<string, unknown>;
  state: string;
}

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

export async function listGitHubApps(): Promise<GitHubApp[]> {
  const response = await http.get<AppListEnvelope>("/providers/github-app");
  return response.data.apps ?? [];
}

export async function startManifest(baseUrl: string, name: string): Promise<GitHubManifest> {
  const response = await http.post<GitHubManifest>("/providers/github-app/manifest", {
    base_url: baseUrl,
    name,
  });
  return response.data;
}

export async function finishCallback(code: string, state: string): Promise<GitHubApp> {
  const response = await http.post<GitHubApp>("/providers/github-app/callback", {
    code,
    state,
  });
  return response.data;
}

export async function installUrl(appId: string): Promise<GitHubInstall> {
  const response = await http.get<GitHubInstall>(`/providers/github-app/${appId}/install`);
  return response.data;
}

export async function installStateApp(state: string): Promise<string> {
  const response = await http.get<{ app_id: string }>(
    `/providers/github-app/install-state?state=${encodeURIComponent(state)}`,
  );
  return response.data.app_id;
}

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

export interface GitHubRepoList {
  repos: GitHubRepo[];
  truncated: boolean;
}

export async function listGitHubRepos(appId: string): Promise<GitHubRepoList> {
  const response = await http.get<RepoListEnvelope>(`/providers/github-app/${appId}/repos`);
  return { repos: response.data.repos ?? [], truncated: response.data.truncated ?? false };
}

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

export async function disconnectGitHubApp(appId: string): Promise<DisconnectEnvelope> {
  const response = await http.delete<DisconnectEnvelope>(`/providers/github-app/${appId}`);
  return response.data;
}

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