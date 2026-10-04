import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";

/**
 * Typed client for the source-provider routes served by `internal/providers`:
 *
 *   GET /providers
 *   GET /providers/{id}/repos
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 * There is no branch-listing route yet — the repo's `default_branch` is the
 * only branch signal the API exposes, so the wizard prefills it as free text.
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

/** describeProviderError maps a thrown error to a user-facing message. */
export function describeProviderError(error: unknown): string {
  if (isApiError(error)) {
    return stripErrorPrefix(error.message) || "Request failed";
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || "Something went wrong. Please try again.";
  }
  return "Something went wrong. Please try again.";
}
