import type { ProviderRepo } from "@/features/applications/api/providers";

/**
 * cloneUrlFor returns the clone URL stored for a provider repository
 * (BE-4.4b). A private repository is cloned with an SSH deploy key, so the
 * provider's own ssh_url — authoritative for the host and any non-default SSH
 * port — is stored. An empty ssh_url for a private repo returns "" and the
 * wizard rejects the step (sourceError): falling back to https would let the
 * keyed cloner rewrite it and silently drop the port. A public repository
 * keeps its https URL, which the cloner fetches anonymously.
 */
export function cloneUrlFor(repo: ProviderRepo): string {
  const ssh = repo.ssh_url?.trim() ?? "";
  if (repo.private) {
    return ssh;
  }
  return repo.clone_url;
}

/** suggestAppName derives a wizard default name from a repository name. */
export function suggestAppName(repoName: string): string {
  return repoName.toLowerCase().replace(/[^a-z0-9-]+/g, "-").slice(0, 31);
}
