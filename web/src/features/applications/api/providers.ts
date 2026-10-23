import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { isApiError, stripErrorPrefix } from "@/features/servers";

export interface SourceProvider {
  id: string;
  provider: string;
  base_url: string;
  connected: boolean;
  scopes: string;
  created_at: string;
  updated_at: string;
}

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

interface ProviderListEnvelope {
  providers: SourceProvider[];
}

interface RepoListEnvelope {
  repos: ProviderRepo[];
}

export interface ProviderBranch {
  name: string;
  commit: string;
  protected: boolean;
}

interface BranchListEnvelope {
  branches: ProviderBranch[];
}

export interface GitLabSetupInfo {
  base_url: string;
  redirect_uri: string;
  scopes: string;
}

export interface CreateProviderInput {
  provider: string;
  base_url: string;
  client_id: string;
  client_secret: string;
  redirect_url: string;
  scopes?: string;
}

export interface ProviderAuthorize {
  url: string;
  state: string;
}

export interface AutoProvisionGitLabInput {
  base_url: string;
  admin_token: string;
  name?: string;
  redirect_url: string;
  scopes?: string;
}

export async function listProviders(): Promise<SourceProvider[]> {
  const response = await http.get<ProviderListEnvelope>("/providers");
  return response.data.providers ?? [];
}

export async function listRepos(providerId: string): Promise<ProviderRepo[]> {
  const response = await http.get<RepoListEnvelope>(
    `/providers/${providerId}/repos`,
  );
  return response.data.repos ?? [];
}

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

export async function deleteProvider(providerId: string): Promise<void> {
  await http.delete(`/providers/${providerId}`);
}

export async function autoProvisionGitLab(
  input: AutoProvisionGitLabInput,
): Promise<SourceProvider> {
  const response = await http.post<SourceProvider>(
    "/providers/gitlab/auto-provision",
    input,
  );
  return response.data;
}

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

export async function createProvider(input: CreateProviderInput): Promise<SourceProvider> {
  const response = await http.post<SourceProvider>("/providers", input);
  return response.data;
}

export async function authorizeProvider(providerId: string): Promise<ProviderAuthorize> {
  const response = await http.get<ProviderAuthorize>(`/providers/${providerId}/authorize`);
  return response.data;
}

const gitlabCallbackPath = "/api/v1/providers/gitlab/callback";

export function gitlabCallbackUrl(): string {
  return `${window.location.origin}${gitlabCallbackPath}`;
}

interface ControlPlaneUrlSettings {
  settings?: {
    general?: {
      control_plane_url?: { value?: string };
    };
  };
}

let cachedControlPlaneBase: string | undefined;

export function resetControlPlaneUrlCache(): void {
  cachedControlPlaneBase = undefined;
}

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

export async function resolveGitlabCallbackUrl(): Promise<string> {
  const base = await controlPlaneBase();
  if (base) {
    return `${base.replace(/\/+$/, "")}${gitlabCallbackPath}`;
  }
  return gitlabCallbackUrl();
}

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
