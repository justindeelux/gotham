import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";

/**
 * Typed client for the source-provider routes served by `internal/providers`:
 *
 *   GET /providers
 *   DELETE /providers/{id}
 *   GET /providers/{id}/repos
 *   GET /providers/{id}/branches?repo=<full-name>
 *   POST /providers/gitlab/auto-provision
 *   GET /providers/gitlab/setup-info
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 */

/** A connected source provider, without credentials (see routes.go). */
export interface SourceProvider {
  id: string;
  provider: string;
  base_url: string;
  connected: boolean;
  scopes: string;
  created_at: string;
  updated_at: string;
}

/** A repository visible through a connected provider (see routes.go). */
export interface ProviderRepo {
  id: string;
  name: string;
  full_name: string;
  private: boolean;
  default_branch: string;
  clone_url: string;
  ssh_url: string;
  html_url: string;
}

/** Wire envelope for a provider list. */
interface ProviderListEnvelope {
  providers: SourceProvider[];
}

/** Wire envelope for a repository list. */
interface RepoListEnvelope {
  repos: ProviderRepo[];
}

/** A branch of a repository visible through a connected provider. */
export interface ProviderBranch {
  name: string;
  commit: string;
  protected: boolean;
}

/** Wire envelope for a branch list. */
interface BranchListEnvelope {
  branches: ProviderBranch[];
}

/** Manual-application details for a GitLab instance (see routes.go). */
export interface GitLabSetupInfo {
  base_url: string;
  redirect_uri: string;
  scopes: string;
}

/** Input for automatic GitLab OAuth application creation. The admin token is
 * one-time: it authenticates the single provisioning call and is never stored. */
export interface AutoProvisionGitLabInput {
  base_url: string;
  admin_token: string;
  name?: string;
  redirect_url: string;
  scopes?: string;
}

/** listProviders returns every source provider of the current user. */
export async function listProviders(): Promise<SourceProvider[]> {
  const response = await http.get<ProviderListEnvelope>("/providers");
  return response.data.providers ?? [];
}

/** listRepos returns the repositories visible through one provider. */
export async function listRepos(providerId: string): Promise<ProviderRepo[]> {
  const response = await http.get<RepoListEnvelope>(
    `/providers/${providerId}/repos`,
  );
  return response.data.repos ?? [];
}

/** listBranches returns the branches of repo through one provider. */
export async function listBranches(
  providerId: string,
  repo: string,
): Promise<ProviderBranch[]> {
  const response = await http.get<BranchListEnvelope>(
    `/providers/${providerId}/branches`,
    { params: { repo } },
  );
  return response.data.branches ?? [];
}

/** deleteProvider disconnects a provider: credentials and cached repos are forgotten. */
export async function deleteProvider(providerId: string): Promise<void> {
  await http.delete(`/providers/${providerId}`);
}

/** autoProvisionGitLab creates the GitLab OAuth application from a one-time
 * admin token and returns the stored (unconnected) connection. */
export async function autoProvisionGitLab(
  input: AutoProvisionGitLabInput,
): Promise<SourceProvider> {
  const response = await http.post<SourceProvider>(
    "/providers/gitlab/auto-provision",
    input,
  );
  return response.data;
}

/** gitlabSetupInfo returns the exact redirect URI and scopes for a manually
 * created GitLab OAuth application. */
export async function gitlabSetupInfo(
  baseUrl: string,
  redirectUrl: string,
): Promise<GitLabSetupInfo> {
  const response = await http.get<GitLabSetupInfo>(
    "/providers/gitlab/setup-info",
    { params: { base_url: baseUrl, redirect_url: redirectUrl } },
  );
  return response.data;
}

/** describeProviderError maps a thrown error to a user-facing message. */
export function describeProviderError(error: unknown): string {
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
