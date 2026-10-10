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

/** Input for storing a manually created OAuth application (POST /providers).
 * Client secrets are write-only: they are sent once and never read back. */
export interface CreateProviderInput {
  provider: string;
  base_url: string;
  client_id: string;
  client_secret: string;
  redirect_url: string;
  scopes?: string;
}

/** An OAuth authorization start: redirect the browser to url. */
export interface ProviderAuthorize {
  url: string;
  state: string;
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

/** createProvider stores a manually created OAuth application, unconnected
 * until the authorize step. The client secret is write-only. */
export async function createProvider(input: CreateProviderInput): Promise<SourceProvider> {
  const response = await http.post<SourceProvider>("/providers", input);
  return response.data;
}

/** authorizeProvider starts the OAuth (PKCE) connect: redirect the browser
 * to the returned url; the provider calls back to the API, which lands the
 * browser on the SPA provider-callback route. */
export async function authorizeProvider(providerId: string): Promise<ProviderAuthorize> {
  const response = await http.get<ProviderAuthorize>(`/providers/${providerId}/authorize`);
  return response.data;
}

/** gitlabCallbackPath is the API GitLab callback under the control-plane
 * origin. The backend rejects any redirect_url naming another host. */
const gitlabCallbackPath = "/api/v1/providers/gitlab/callback";

/** gitlabCallbackUrl is the OAuth redirect URL of this control plane: the
 * API GitLab callback under the current origin. Prefer
 * resolveGitlabCallbackUrl, which uses the configured control-plane URL when
 * the API reports one. */
export function gitlabCallbackUrl(): string {
  return `${window.location.origin}${gitlabCallbackPath}`;
}

/** Instance-settings shape this module reads: only the control-plane URL.
 * The endpoint is platform-operator only, so any failure (403 for other
 * roles, network error) falls back to the current origin. */
interface ControlPlaneUrlSettings {
  settings?: {
    general?: {
      control_plane_url?: { value?: string };
    };
  };
}

/** Cached control-plane base URL ("" when the API reports none or refuses).
 * One fetch per page load: every GitLab connect action reuses it. */
let cachedControlPlaneBase: string | undefined;

/** resetControlPlaneUrlCache clears the cached control-plane base URL. Tests
 * call it between cases so each resolves against its own mocks. */
export function resetControlPlaneUrlCache(): void {
  cachedControlPlaneBase = undefined;
}

/** controlPlaneBase returns the configured control-plane base URL ("" when
 * the API reports none or refuses, e.g. 403 for non-operators). */
async function controlPlaneBase(): Promise<string> {
  if (cachedControlPlaneBase === undefined) {
    try {
      const response = await http.get<ControlPlaneUrlSettings>("/instance/settings");
      cachedControlPlaneBase =
        response.data?.settings?.general?.control_plane_url?.value?.trim() ?? "";
    } catch {
      cachedControlPlaneBase = "";
    }
  }
  return cachedControlPlaneBase;
}

/** resolveGitlabCallbackUrl is the OAuth redirect URL of this control plane:
 * the API GitLab callback under the configured control-plane URL when the
 * API reports one, otherwise under the current origin (see
 * gitlabCallbackUrl). */
export async function resolveGitlabCallbackUrl(): Promise<string> {
  const base = await controlPlaneBase();
  if (base) {
    return `${base.replace(/\/+$/, "")}${gitlabCallbackPath}`;
  }
  return gitlabCallbackUrl();
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
